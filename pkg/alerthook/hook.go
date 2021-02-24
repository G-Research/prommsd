// Package alerthook implements reception of alertmanager webhooks.
package alerthook

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/G-Research/prommsd/pkg/alertmanager"
)

var (
	receivedMetric = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "prommsd",
			Subsystem: "alerthook",
			Name:      "received_total",
		})
	errorsMetric = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "prommsd",
			Subsystem: "alerthook",
			Name:      "errors_total",
		}, []string{"type"})
)

func init() {
	for _, errorType := range []string{"wrong_method", "decode", "handler"} {
		errorsMetric.With(prometheus.Labels{"type": errorType}).Add(0)
	}
}

// AlertHandler should be implemented by clients wishing to receive the alerts
// from the hook.
type AlertHandler interface {
	HandleAlert(context.Context, *alertmanager.Alert) error
	Healthy() bool
}

type AlertHook struct {
	handler AlertHandler
}

func New(handler AlertHandler, registerer prometheus.Registerer) *AlertHook {
	if registerer != nil {
		registerer.MustRegister(receivedMetric)
		registerer.MustRegister(errorsMetric)
	}
	return &AlertHook{
		handler: handler,
	}
}

func (ah *AlertHook) Healthy() bool {
	return ah.handler.Healthy()
}

func (ah *AlertHook) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method == "HEAD" || req.Method == "OPTIONS" {
		return
	}

	receivedMetric.Add(1)

	if req.Method != "POST" {
		errorsMetric.With(prometheus.Labels{"type": "wrong_method"}).Add(1)
		http.Error(w, "Expected alert to be POSTed", http.StatusBadRequest)
		return
	}

	defer req.Body.Close()

	var m alertmanager.Message
	err := json.NewDecoder(req.Body).Decode(&m)
	if err != nil {
		errorsMetric.With(prometheus.Labels{"type": "decode"}).Add(1)
		log.Printf("Error decoding alert: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err = nil
	for i, alert := range m.Alerts {
		alert.Parent = &m
		maybeErr := ah.handler.HandleAlert(req.Context(), alert)
		if maybeErr != nil {
			log.Printf("Error handling alert (%q:%d): %v", m.GroupKey, i, maybeErr)
		}
		if maybeErr != nil && err == nil {
			err = maybeErr
		}
	}

	if err != nil {
		errorsMetric.With(prometheus.Labels{"type": "handle"}).Add(1)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
