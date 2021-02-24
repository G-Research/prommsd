package alerthook

import (
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/net/trace"
)

// Serve provides an alertmanager webhook server. It registers a handler on
// '/alert' to receive alerts. It also registers handlers for '/metrics'
// (Prometheus metrics) and '/-/healthy' (health checking).
//
// Alerts are forwarded to the provided AlertHandler.
func Serve(listenAddr string, alertHandler AlertHandler, registerer prometheus.Registerer) {
	handler := New(alertHandler, registerer)
	registerHandlers(http.DefaultServeMux, handler)
	log.Print("Starting HTTP server on ", listenAddr)
	log.Fatal(http.ListenAndServe(listenAddr, tracing(http.DefaultServeMux)))
}

func registerHandlers(serveMux *http.ServeMux, handler *AlertHook) {
	serveMux.Handle("/alert", handler)
	serveMux.Handle("/metrics", promhttp.Handler())

	serveMux.HandleFunc("/-/healthy", func(w http.ResponseWriter, req *http.Request) {
		if !handler.Healthy() {
			http.Error(w, "unhealthy", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok"))
	})
}

// tracing adds a context with tracing to requests that pass through it
func tracing(mux *http.ServeMux) http.Handler {
	// Like Prometheus this should be wrapped in a sidecar for auth, or just
	// internal only and available to anyone as it's just monitoring details.
	trace.AuthRequest = func(req *http.Request) (any, sensitive bool) {
		return true, true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		handler, pattern := mux.Handler(req)

		traceName := "http.unknown"
		if len(pattern) > 0 {
			traceName = "http." + req.URL.Path
		}
		tr := trace.New(traceName, req.URL.Path)
		tr.LazyPrintf("%v %v %v", req.RemoteAddr, req.Method, req.URL.String())
		defer tr.Finish()

		handler.ServeHTTP(w, req.WithContext(trace.NewContext(req.Context(), tr)))
	})
}
