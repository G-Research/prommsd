// Package alertchecker implements the "business logic" of prommsd. It checks
// that alerts (heartbeats) are received regularly and raises alerts for
// instances that are missing regular heartbeats.
package alertchecker

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/trace"

	"github.com/G-Research/prommsd/pkg/alertmanager"
)

const (
	defaultActivation = 10 * time.Minute
	sendInterval      = 60 * time.Second
	slackSendInterval = 20 * time.Minute
	resolveRepeat     = 15 * time.Minute
	expireTime        = 2 * time.Hour

	annotationPrefix   = "msda_"
	defaultIdentifiers = "job namespace cluster"
)

var instanceMetric = prometheus.NewGauge(prometheus.GaugeOpts{
	Namespace: "prommsd",
	Subsystem: "alertchecker",
	Name:      "monitored_instances"})

// AlertChecker implements the alerthook.AlertHandler interface, it receives
// alerts and applies this package's business logic to them.
type AlertChecker struct {
	// Lock when accessing monitored. Needed because status runs in a different
	// goroutine.
	sync.RWMutex
	monitored   map[string]*instanceDetails
	handleChan  chan handleAlert
	healthChan  chan interface{}
	externalURL string
	// To allow testing with fake time
	now func() time.Time
}

// New returns a new AlertChecker. It is only expected there is one instance of
// this per binary as it runs a goroutine in the background.
func New(registerer prometheus.Registerer, externalURL string) *AlertChecker {
	ac := makeAlertChecker(externalURL)
	go ac.checker()
	registerer.MustRegister(instanceMetric)
	http.HandleFunc("/", ac.status)
	http.HandleFunc("/modify", ac.modify)
	return ac
}

func makeAlertChecker(externalURL string) *AlertChecker {
	return &AlertChecker{
		monitored:   make(map[string]*instanceDetails),
		handleChan:  make(chan handleAlert),
		healthChan:  make(chan interface{}),
		externalURL: externalURL,
		now:         time.Now,
	}
}

type handleAlert struct {
	key      string
	instance *instanceDetails
}

type instanceDetails struct {
	ActivateAt, LastSent    time.Time
	ActivatedAt, ResolvedAt time.Time
	AlertName               string
	Receiver                string
	AlertManagers           []string
	OverrideLabels          []string
	LastAlert               *alertmanager.Alert
	LastError               string
}

// HandleAlert receives a single alert from the alerts sent to an alertmanager
// webhook. It parses the annotations as configuration and then sends a
// "handleAlert" struct to handleChan, which the checker goroutine receives and
// calls updateInstance.
func (ac *AlertChecker) HandleAlert(ctx context.Context, alert *alertmanager.Alert) error {
	if alert.Status == "resolved" {
		// Ignore resolved because we only care about our activation timeout; we
		// suggest setting `send_resolved: false` in the alertmanager webhook, but
		// just ignore any misconfiguration.
		return nil
	}

	// Turn specified identifiers into key.
	identifierLabels := alert.GetAnnotationDefault("msd_identifiers", defaultIdentifiers)
	var ids []string
	for _, id := range splitAnnotation(identifierLabels) {
		ids = append(ids, id+"="+fmt.Sprintf("%q", alert.GetLabelDefault(id, "")))
	}
	sort.Strings(ids)
	key := strings.Join(ids, " ")

	alertName := alert.GetAnnotationDefault("msd_alertname", "NoAlertConnectivity")
	overrideLabels := alert.GetAnnotationDefault("msd_override_labels", "severity=critical")
	// ExternalURL is the best we can do for a default -- users really should
	// specify multiple URLs for reliability.
	alertManagers := alert.GetAnnotationDefault("msd_alertmanagers", alert.Parent.ExternalURL)

	activationDuration, err := time.ParseDuration(alert.GetAnnotationDefault("msd_activation", "10m"))
	if err != nil {
		log.Printf("Failed to parse msd_activation: %v, default to %d", err, defaultActivation)
		activationDuration = defaultActivation
	}

	instance := instanceDetails{
		ActivateAt:     ac.now().Add(activationDuration),
		AlertManagers:  splitAnnotation(alertManagers),
		AlertName:      alertName,
		Receiver:       alert.Parent.Receiver,
		OverrideLabels: splitAnnotation(overrideLabels),
		// n.b.: Holds a ref to parent and therefore other alerts which we
		// potentially don't need (but probably not very many), consider just
		// copying the data we want here instead.
		LastAlert: alert,
	}
	ac.handleChan <- handleAlert{key, &instance}

	return nil
}

func (ac *AlertChecker) Healthy() bool {
	// We rely on this chan being blocking, if the checker goroutine doesn't read
	// from it the request will simply timeout for the user.
	ac.healthChan <- nil
	return true
}

func (ac *AlertChecker) checker() {
	events := trace.NewEventLog("alertchecker.checker", "")
	tick := time.Tick(5 * time.Second)

	for {
		select {
		case <-tick:
			ac.checkMonitored(events, ac.now())
		case handle := <-ac.handleChan:
			ac.updateInstance(handle.key, handle.instance)
		case <-ac.healthChan:
			// See comment in Healthy.
		}
	}
}

// updateInstance receives messages from HandleAlert. It should be fast as
// operations here are on the single checking goroutine.
func (ac *AlertChecker) updateInstance(key string, instance *instanceDetails) {
	ac.Lock()
	defer ac.Unlock()
	oldInstance, ok := ac.monitored[key]
	ac.monitored[key] = instance
	instanceMetric.Set(float64(len(ac.monitored)))
	if !ok {
		log.Printf("New instance %v, will activate at %v and send to %v", key, instance.ActivateAt, instance.AlertManagers)
	} else {
		if oldInstance.LastSent.After(oldInstance.ActivateAt) {
			instance.ResolvedAt = ac.now()
			log.Printf("Alert resolved for instance %v", key)
		} else {
			instance.ResolvedAt = oldInstance.ResolvedAt
		}
		instance.ActivatedAt = oldInstance.ActivatedAt
		instance.LastSent = oldInstance.LastSent
		instance.LastError = oldInstance.LastError
	}
}

func (ac *AlertChecker) checkMonitored(events trace.EventLog, now time.Time) {
	events.Printf("Run check...")
	tr := trace.New("alertchecker.checkMonitored", "check")
	defer tr.Finish()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	toAlert := []*instanceDetails{}
	ac.Lock()
	for key, instance := range ac.monitored {
		active := now.After(instance.ActivateAt)
		sendResolved := now.Before(instance.ResolvedAt.Add(resolveRepeat))
		if active || sendResolved {
			if now.After(instance.LastSent.Add(sendInterval)) {
				events.Printf("Alerting (active=%v, resolved=%v): %v", active, sendResolved, key)
				if active && instance.ActivateAt.After(instance.ActivatedAt) {
					instance.ActivatedAt = now
				}
				toAlert = append(toAlert, instance)
			}
			if now.After(instance.ActivateAt.Add(expireTime)) {
				delete(ac.monitored, key)
				events.Printf("Expired %v", key)
				instanceMetric.Set(float64(len(ac.monitored)))
			}
		}
	}
	ac.Unlock()

	wg := sync.WaitGroup{}
	for _, instance := range toAlert {
		wg.Add(1)
		// n.b.: Safe to access instance from this goroutine as there is one per
		// instance and we only write to an existing instance here.
		go ac.alert(&wg, ctx, now, instance)
	}
	wg.Wait()
}

func (ac *AlertChecker) alert(wg *sync.WaitGroup, ctx context.Context, now time.Time, instance *instanceDetails) {
	defer wg.Done()

	alert := alertmanager.NewAlert()
	for k, v := range instance.LastAlert.GetLabels() {
		if k == "severity" || k == "alertname" {
			continue
		}
		alert.Labels[k] = v
	}
	alert.Labels["alertname"] = instance.AlertName
	for _, override := range instance.OverrideLabels {
		label := strings.SplitN(override, "=", 2)
		if len(label) < 2 {
			continue
		}
		alert.Labels[label[0]] = label[1]
	}

	for k, v := range instance.LastAlert.GetAnnotations() {
		if strings.HasPrefix(k, annotationPrefix) && len(k) > len(annotationPrefix) {
			alert.Annotations[k[len(annotationPrefix):]] = v
		}
	}

	// Calculate the group labels here, to ensure overrides are taken into account
	identifierLabels := instance.LastAlert.GetAnnotationDefault("msd_identifiers", defaultIdentifiers)
	groupLabels := map[string]string{}
	for _, id := range splitAnnotation(identifierLabels) {
		if label, ok := alert.GetLabel(id); ok {
			groupLabels[id] = label
		}
	}

	alert.GeneratorURL = ac.externalURL

	// We're here because the alert is either active or resolved, it's active if the time is after the
	// ActivateAt time.
	resolved := false
	if now.After(instance.ActivateAt) {
		alert.StartsAt = instance.ActivateAt
		alert.EndsAt = instance.ActivateAt.Add(expireTime)
		alert.Status = "firing"
	} else {
		// Send resolved
		alert.StartsAt = instance.ActivatedAt
		alert.EndsAt = instance.ResolvedAt
		alert.Status = "resolved"
		resolved = true
	}

	err := ac.sendAlerts(ctx, instance.AlertManagers, instance.Receiver, instance.LastSent, resolved, groupLabels, []alertmanager.Alert{alert})
	if err != nil {
		instance.LastError = err.Error()
	} else {
		instance.LastSent = now
	}
}

// Split into "words", allowing lines to be commented.
// i.e. This accepts input like "foo bar baz", or "foo\n#x\nbar baz", returning a
// list of (foo, bar, baz).
func splitAnnotation(s string) []string {
	var ret []string
	for _, line := range strings.Split(s, "\n") {
		text := strings.TrimSpace(line)
		if len(text) == 0 || text[0] == '#' {
			continue
		}
		for _, item := range strings.Split(text, " ") {
			ret = append(ret, item)
		}
	}
	return ret
}
