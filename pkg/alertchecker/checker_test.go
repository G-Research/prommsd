package alertchecker

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/trace"

	"github.com/G-Research/prommsd/pkg/alertmanager"
)

// Hides the log out; run with go test -v to see the output.
type testLogger struct {
	t *testing.T
}

func (tl testLogger) Write(n []byte) (int, error) {
	tl.t.Log(string(n))
	return len(n), nil
}

type testTransport struct {
	requests []*http.Request
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.requests = append(t.requests, req)
	return &http.Response{Proto: "HTTP/1.0",
		ProtoMajor: 1,
		Header:     make(http.Header),
		Close:      true,
		Status:     "200 OK",
		StatusCode: 200,
		Body:       ioutil.NopCloser(strings.NewReader("")),
	}, nil
}

var (
	tt = &testTransport{}
)

func init() {
	http.DefaultTransport.(*http.Transport).RegisterProtocol("alerttest", tt)
}

func test(t *testing.T, c func(*AlertChecker, trace.EventLog, *time.Time, *testTransport)) {
	log.SetOutput(&testLogger{t})
	log.SetFlags(0)

	events := trace.NewEventLog(t.Name(), "")
	ac := makeAlertChecker("http://localhost:0")

	now := time.Now()
	ac.now = func() time.Time { return now }

	// For tests we want control of time, so don't want the ticking done by
	// checker(), but we do need to have a goroutine to handle updateInstance.
	go func() {
		for handle := range ac.handleChan {
			ac.updateInstance(handle.key, handle.instance)
		}
	}()

	c(ac, events, &now, tt)

	// Force expire to clean up after this test...
	now = now.Add(3 * time.Hour)
	ac.checkMonitored(events, now)

	if len(ac.monitored) != 0 {
		t.Errorf("got %d monitored instances, want 0", len(ac.monitored))
	}

	// Make sure the goroutine for updateInstance ends.
	close(ac.handleChan)

	// Clean up the list of requests
	tt.requests = nil
}

func TestAlertCheckerBasics(t *testing.T) {
	test(t, func(ac *AlertChecker, events trace.EventLog, now *time.Time, tt *testTransport) {
		// Nothing registered, nothing should happen
		ac.checkMonitored(events, *now)

		a := alertmanager.NewAlert()
		a.Labels["job"] = "tester"
		a.Annotations["msd_alertmanagers"] = "alerttest://am1"
		a.Parent = &alertmanager.Message{}
		ac.HandleAlert(context.Background(), &a)
		// Wait for updateInstance
		time.Sleep(1 * time.Second)

		*now = now.Add(1 * time.Minute)
		ac.checkMonitored(events, *now)

		if len(tt.requests) != 0 {
			t.Errorf("got %d requests, want 0", len(tt.requests))
		}

		*now = now.Add(10 * time.Minute)
		ac.checkMonitored(events, *now)

		if len(tt.requests) != 1 {
			t.Errorf("got %d requests, want 1", len(tt.requests))
		}

		*now = now.Add(5 * time.Second)
		ac.checkMonitored(events, *now)

		if len(tt.requests) != 1 {
			t.Errorf("got %d requests, want 1", len(tt.requests))
		}

		*now = now.Add(56 * time.Second)
		// Now at 1m1s after send...
		ac.checkMonitored(events, *now)

		if len(tt.requests) != 2 {
			t.Errorf("got %d requests, want 2", len(tt.requests))
		}

		*now = now.Add(2 * time.Hour)
		// Now at 2h1m1s after activation, alert expires
		ac.checkMonitored(events, *now)

		if len(tt.requests) != 3 {
			t.Errorf("got %d requests, want 3", len(tt.requests))
		}

		if len(ac.monitored) != 0 {
			t.Errorf("got %d monitored instances, want 0", len(ac.monitored))
		}
	})
}

func TestAlertCheckerResolved(t *testing.T) {
	test(t, func(ac *AlertChecker, events trace.EventLog, now *time.Time, tt *testTransport) {
		// Nothing registered, nothing should happen
		ac.checkMonitored(events, *now)

		a := alertmanager.NewAlert()
		a.Labels["job"] = "testerresolved"
		a.Annotations["msd_alertmanagers"] = "alerttest://am1"
		a.Parent = &alertmanager.Message{}
		ac.HandleAlert(context.Background(), &a)
		// Wait for updateInstance
		time.Sleep(1 * time.Second)

		*now = now.Add(1 * time.Minute)
		ac.checkMonitored(events, *now)

		if len(tt.requests) != 0 {
			t.Errorf("got %d requests, want 0", len(tt.requests))
		}

		*now = now.Add(10 * time.Minute)
		ac.checkMonitored(events, *now)

		if len(tt.requests) != 1 {
			t.Errorf("got %d requests, want 1", len(tt.requests))
		}

		*now = now.Add(12 * time.Minute)
		ac.HandleAlert(context.Background(), &a)
		// Wait for updateInstance
		time.Sleep(1 * time.Second)
		ac.checkMonitored(events, *now)

		if len(tt.requests) != 2 {
			t.Errorf("got %d requests, want 2", len(tt.requests))
		}

		// Expected resolve sent to alertmanager
		resolve := tt.requests[1]
		resolveBody, err := ioutil.ReadAll(resolve.Body)
		if err != nil {
			t.Errorf("got error %v reading body", err)
		}
		t.Log(string(resolveBody))

		var alerts []alertmanager.Alert
		err = json.Unmarshal(resolveBody, &alerts)
		if err != nil {
			t.Errorf("got error %v decoding body", err)
		}
		if len(alerts) != 1 {
			t.Errorf("got %d alerts, want 1", len(alerts))
		}
		// JSON only reliably supports microsecond precision.
		nowTrunc := now.Truncate(time.Microsecond)
		endsAt := alerts[0].EndsAt.Truncate(time.Microsecond)
		if !endsAt.Equal(nowTrunc) {
			t.Errorf("got %v, want %v", endsAt, nowTrunc)
		}
	})
}

func TestAlertCheckerAlert(t *testing.T) {
	test(t, func(ac *AlertChecker, events trace.EventLog, now *time.Time, tt *testTransport) {
		a := alertmanager.NewAlert()
		a.Labels["job"] = "testeralert"
		a.Annotations["msd_alertmanagers"] = "alerttest://am1"
		a.Annotations["msda_test"] = "test annotation"
		a.Parent = &alertmanager.Message{}
		ac.HandleAlert(context.Background(), &a)
		// Wait for updateInstance
		time.Sleep(1 * time.Second)

		*now = now.Add(10*time.Minute + 1)
		ac.checkMonitored(events, *now)

		if len(tt.requests) != 1 {
			t.Errorf("got %d requests, want 1", len(tt.requests))
		}

		// Expected alert sent to alertmanager
		alertReq := tt.requests[0]
		alertBody, err := ioutil.ReadAll(alertReq.Body)
		if err != nil {
			t.Errorf("got error %v reading body", err)
		}
		t.Log(string(alertBody))
		var alerts []alertmanager.Alert
		err = json.Unmarshal(alertBody, &alerts)
		if err != nil {
			t.Errorf("got error %v decoding body", err)
		}
		if len(alerts) != 1 {
			t.Errorf("got %d alerts, want 1", len(alerts))
		}

		alert := alerts[0]

		// JSON only reliably supports microsecond precision.
		nowTrunc := now.Truncate(time.Microsecond)
		startsAt := alert.StartsAt.Truncate(time.Microsecond)
		if !startsAt.Equal(nowTrunc) {
			t.Errorf("got %v, want %v", startsAt, nowTrunc)
		}

		if alert.Status != "firing" {
			t.Errorf("got %v want %v", alert.Status, "firing")
		}

		if alert.GeneratorURL != "http://localhost:0" {
			t.Errorf("got %v want %v", alert.GeneratorURL, "http://localhost:0")
		}

		expectedLabels := map[string]string{
			"alertname": "NoAlertConnectivity",
			"job":       "testeralert",
			"severity":  "critical",
		}
		if !reflect.DeepEqual(expectedLabels, alert.Labels) {
			t.Errorf("got %v want %v", alert.Labels, expectedLabels)
		}

		expectedAnnotations := map[string]string{
			"test": "test annotation",
		}
		if !reflect.DeepEqual(expectedAnnotations, alert.Annotations) {
			t.Errorf("got %v want %v", alert.Annotations, expectedAnnotations)
		}
	})
}

func TestAlertCheckerWebhook(t *testing.T) {
	test(t, func(ac *AlertChecker, events trace.EventLog, now *time.Time, tt *testTransport) {
		a := alertmanager.NewAlert()
		a.Labels["job"] = "testerhook"
		a.Labels["severity"] = "test"
		a.Annotations["msd_identifiers"] = "job severity"
		a.Annotations["msd_alertmanagers"] = "webhook+alerttest://handler"
		a.Annotations["msda_test"] = "test annotation"
		a.Parent = &alertmanager.Message{}
		ac.HandleAlert(context.Background(), &a)
		// Wait for updateInstance
		time.Sleep(1 * time.Second)

		*now = now.Add(10*time.Minute + 1)
		ac.checkMonitored(events, *now)

		if len(tt.requests) != 1 {
			t.Errorf("got %d requests, want 1", len(tt.requests))
		}

		// Expected alert sent to webhook
		alertReq := tt.requests[0]
		alertBody, err := ioutil.ReadAll(alertReq.Body)
		if err != nil {
			t.Errorf("got error %v reading body", err)
		}
		t.Log(string(alertBody))
		var alert map[string]interface{}
		err = json.Unmarshal(alertBody, &alert)
		if err != nil {
			t.Errorf("got error %v decoding body", err)
		}

		if alert["status"].(string) != "firing" {
			t.Errorf("got %v, want firing", alert["status"])
		}

		groupLabels := alert["groupLabels"].(map[string]interface{})
		expectedLabels := map[string]interface{}{
			"job":      "testerhook",
			"severity": "critical",
		}
		if !reflect.DeepEqual(groupLabels, expectedLabels) {
			t.Errorf("got %v want %v", groupLabels, expectedLabels)
		}
	})
}
