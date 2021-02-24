package alerthook

import (
	"context"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/G-Research/prommsd/pkg/alertmanager"
)

type MockHandler struct {
	LastAlert *alertmanager.Alert
	Err       error
}

func (h *MockHandler) HandleAlert(ctx context.Context, alert *alertmanager.Alert) error {
	h.LastAlert = alert
	err := h.Err
	h.Err = nil
	return err
}

func (h *MockHandler) Healthy() bool {
	return true
}

func TestHandlers(t *testing.T) {
	mux := http.NewServeMux()
	mock := &MockHandler{}
	handler := New(mock, prometheus.DefaultRegisterer)
	registerHandlers(mux, handler)

	doRequest := func(method, path string, body io.Reader, wantStatus int) *http.Response {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, body))
		res := w.Result()
		if res.StatusCode != wantStatus {
			t.Errorf("%v %v: got %v, want %v", method, path, res.Status, wantStatus)
		}
		return res
	}

	// Check 404 explicitly because tracing hooks into the mux.
	// n.b. Can't check /debug/requests and /debug/events as those register on
	// DefaultServeMux and we don't use that for these tests.
	doRequest("GET", "/404", nil, http.StatusNotFound)

	res := doRequest("GET", "/-/healthy", nil, http.StatusOK)
	if body, _ := ioutil.ReadAll(res.Body); string(body) != "ok" {
		t.Errorf("/-/healthy: got %q, want %q", string(body), "ok")
	}

	res = doRequest("GET", "/metrics", nil, http.StatusOK)
	if body, _ := ioutil.ReadAll(res.Body); !strings.Contains(string(body), "promhttp_metric_handler_requests_total") {
		t.Errorf("/metrics: got %q, want string containing promhttp_metric_handler_requests_total", string(body))
	}

	// HEAD just returns OK.
	doRequest("HEAD", "/alert", nil, http.StatusOK)

	// GET isn't allowed.
	doRequest("GET", "/alert", nil, http.StatusBadRequest)

	// Good alert
	res = doRequest("POST", "/alert",
		strings.NewReader(`{"alerts":[{"labels":{"foo":"bar"}}]}`),
		http.StatusOK)
	if mock.LastAlert == nil {
		t.Errorf("/alert: got no alert, want one")
	}
	value, ok := mock.LastAlert.GetLabel("foo")
	if !ok || value != "bar" {
		t.Errorf("/alert: got %v, want label foo=bar", mock.LastAlert)
	}

	// Malformed alert
	doRequest("POST", "/alert",
		strings.NewReader(`{"alerts":[{"labels":{"foo":"bar"}}]`),
		http.StatusBadRequest)

	// Good alert but handler returns error
	mock.Err = errors.New("test error")
	doRequest("POST", "/alert",
		strings.NewReader(`{"alerts":[{"labels":{"foo":"bar"}}]}`),
		http.StatusInternalServerError)

	// Good alert but handler returns error, only for first alert
	mock.Err = errors.New("test error2")
	doRequest("POST", "/alert",
		strings.NewReader(`{"alerts":[{"labels":{"foo":"bar"}},{"labels":{"foo":"bar2"}}]}`),
		http.StatusInternalServerError)
	if body, _ := ioutil.ReadAll(res.Body); strings.Contains(string(body), "test error 2") {
		t.Errorf("/alert: got %q, want string containing %q", string(body), "test error 2")
	}
}
