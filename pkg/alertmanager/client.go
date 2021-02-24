// Package alertmanager implements a very simple alertmanager client
package alertmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	sentMetric = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "prommsd",
			Subsystem: "alertmanager",
			Name:      "sent_total",
		})
	errorsMetric = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "prommsd",
			Subsystem: "alertmanager",
			Name:      "errors_total",
		}, []string{"type"})
)

func init() {
	prometheus.MustRegister(sentMetric)
	prometheus.MustRegister(errorsMetric)
}

type Client struct {
	baseURL url.URL
}

func NewClient(baseURL *url.URL) *Client {
	u := *baseURL
	if u.Path == "" || u.Path == "/" {
		u.Path = "/api/v1/alerts"
	}
	return &Client{
		baseURL: u,
	}
}

func (c *Client) SendAlerts(ctx context.Context, alerts []Alert) error {
	sentMetric.Add(1)
	body, err := json.Marshal(alerts)
	if err != nil {
		errorsMetric.With(prometheus.Labels{"type": "json_encode"}).Add(1)
		return err
	}
	req, err := http.NewRequest("POST", c.baseURL.String(), bytes.NewBuffer(body))
	if err != nil {
		errorsMetric.With(prometheus.Labels{"type": "make_request"}).Add(1)
		return err
	}
	req = req.WithContext(ctx)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		errorsMetric.With(prometheus.Labels{"type": "http_send"}).Add(1)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	errorsMetric.With(prometheus.Labels{"type": "http_response"}).Add(1)
	return errors.New(resp.Status)
}
