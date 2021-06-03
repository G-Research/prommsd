package alertchecker

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"text/template"
	"time"

	"github.com/G-Research/prommsd/pkg/alertmanager"
)

var (
	flagSlackTemplate = flag.String("slack-template", "{{.Receiver}}: {{.GroupLabels}}{{range $k, $v := .CommonAnnotations}}\n{{$k}}: {{$v}}{{end}}", "Go text/template to use for formatting slack message")
)

func (ac *AlertChecker) sendAlerts(ctx context.Context, alertmanagers []string, receiver string, lastSent time.Time, resolved bool, groupLabels map[string]string, alert []alertmanager.Alert) error {
	var lastErr error
	t := "alert"
	if resolved {
		t = "resolved"
	}
	for _, alertURL := range alertmanagers {
		u, err := url.Parse(alertURL)
		if err != nil {
			log.Printf("Unable to parse alert destination URL %q: %v", alertURL, err)
			continue
		}

		// Accept type+http:// to allow specifing the kind of service.
		// Without + (e.g. http:// or https://) default to "am" (i.e.
		// "alertmanager").
		deliverType := "am"
		extraScheme := strings.SplitN(u.Scheme, "+", 2)
		if len(extraScheme) == 2 {
			deliverType = extraScheme[0]
			u.Scheme = extraScheme[1]
		}

		switch deliverType {
		case "am":
			func() {
				client := alertmanager.NewClient(u)
				ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
				defer cancel()
				log.Printf("Sending %s to %v", t, u)
				err := client.SendAlerts(ctx, alert)
				if err != nil {
					log.Printf("Error sending %s to %v: %v", t, u, err)
					lastErr = err
				}
			}()
		case "webhook":
			if err := sendWebhook(ctx, u, receiver, resolved, groupLabels, alert); err != nil {
				log.Printf("Error sending %s to %v: %v", t, u, err)
				lastErr = err
			}
		case "slack":
			if !ac.now().After(lastSent.Add(slackSendInterval)) {
				// Avoid repeating slack notifications frequently. This may mean resolves aren't always
				// sent, but this is better than a noisy alert, otherwise we're going to end up duplicating
				// all of alertmanager's logic here...
				continue
			}
			if err := sendSlack(ctx, u, receiver, resolved, groupLabels, alert); err != nil {
				log.Printf("Error sending %s to %v: %v", t, u, err)
				lastErr = err
			}
		default:
			lastErr = fmt.Errorf("Unknown alert delivery type %v (in %q)", deliverType, alertURL)
			log.Print(err)
		}
	}
	return lastErr
}

// alertBody is the body sent JSON encoded in webhook invocations, it aims to be compatible with
// https://prometheus.io/docs/alerting/latest/configuration/#webhook_config
type alertBody struct {
	Version           string               `json:"version"`
	Status            string               `json:"status"`
	Receiver          string               `json:"receiver"`
	GroupLabels       map[string]string    `json:"groupLabels"`
	CommonLabels      map[string]string    `json:"commonLabels"`
	CommonAnnotations map[string]string    `json:"commonAnnotations"`
	Alerts            []alertmanager.Alert `json:"alerts"`
}

// makeAlertBody creates an alertBody
func makeAlertBody(receiver string, resolved bool, groupLabels map[string]string, alerts []alertmanager.Alert) alertBody {
	status := "firing"
	if resolved {
		status = "resolved"
	}
	return alertBody{
		Version:           "4",
		Status:            status,
		Receiver:          receiver,
		GroupLabels:       groupLabels,
		CommonLabels:      alerts[0].Labels,
		CommonAnnotations: alerts[0].Annotations,
		Alerts:            alerts,
	}
}

// sendWebhook sends a notification to an alertmanager webhook compatible endpoint.
func sendWebhook(ctx context.Context, sendURL *url.URL, receiver string, resolved bool, groupLabels map[string]string, alerts []alertmanager.Alert) error {
	ctx, cancel := context.WithTimeout(ctx, 1*time.Minute)
	defer cancel()

	body := makeAlertBody(receiver, resolved, groupLabels, alerts)
	j, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := http.Post(sendURL.String(), "application/json", bytes.NewBuffer(j))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("Response %v", resp.Status)
	}
	return nil
}

// sendSlack sends a notification to a slack endpoint.
func sendSlack(ctx context.Context, sendURL *url.URL, receiver string, resolved bool, groupLabels map[string]string, alerts []alertmanager.Alert) error {
	ctx, cancel := context.WithTimeout(ctx, 1*time.Minute)
	defer cancel()

	body := makeAlertBody(receiver, resolved, groupLabels, alerts)
	// Default text used if templating fails
	text := fmt.Sprintf("%v: %v, %v.\n%#v\n(templating problem)", body.Receiver, body.Status, groupLabels, alerts[0])

	tmpl, err := template.New("slack").Parse(*flagSlackTemplate)
	if err != nil {
		log.Printf("Slack template.New: %v", err)
	} else {
		var buf bytes.Buffer
		err := tmpl.Execute(&buf, body)
		if err != nil {
			log.Printf("Slack tmpl.Execute: %v", err)
		} else {
			text = buf.String()
		}
	}

	emoji := "exclaimation"
	if resolved {
		emoji = "grey_exclamation"
	}
	j, err := json.Marshal(map[string]string{
		"username":   body.Receiver,
		"text":       text,
		"icon_emoji": emoji,
	})
	if err != nil {
		return err
	}
	resp, err := http.Post(sendURL.String(), "application/json", bytes.NewBuffer(j))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("Response %v", resp.Status)
	}
	return nil
}
