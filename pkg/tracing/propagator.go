package tracing

import (
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/otel/propagation"
)

var newPropagators = map[string]func() propagation.TextMapPropagator{
	"baggage":      func() propagation.TextMapPropagator { return propagation.Baggage{} },
	"tracecontext": func() propagation.TextMapPropagator { return propagation.TraceContext{} },
}

func NewPropagatorsFromEnv() (propagation.TextMapPropagator, error) {
	enabled := []string{"tracecontext", "baggage"}

	if v, ok := os.LookupEnv("OTEL_PROPAGATORS"); ok {
		enabled = strings.Split(strings.TrimSpace(v), ",")
	}

	var propagators []propagation.TextMapPropagator

	for _, name := range enabled {
		if new, ok := newPropagators[name]; ok {
			propagators = append(propagators, new())
		} else {
			return nil, fmt.Errorf("unknown propagator: \"%s\"", name)
		}
	}

	if len(propagators) > 1 {
		return propagation.NewCompositeTextMapPropagator(propagators...), nil
	}
	if len(propagators) == 1 {
		return propagators[0], nil
	}
	return nil, nil
}
