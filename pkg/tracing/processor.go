package tracing

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"golang.org/x/exp/slices"
)

var newExporters = map[string]func(context.Context) (sdktrace.SpanExporter, error){
	"stdout": func(_ context.Context) (sdktrace.SpanExporter, error) { return stdouttrace.New() },
	"otlp":   NewOTLPExporterFromEnv,
}

func NewOTLPExporterFromEnv(ctx context.Context) (sdktrace.SpanExporter, error) {
	protocol := "http/protobuf"
	if p, ok := os.LookupEnv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"); ok {
		protocol = strings.TrimSpace(p)
	} else if p, ok = os.LookupEnv("OTEL_EXPORTER_OTLP_PROTOCOL"); ok {
		protocol = strings.TrimSpace(p)
	}

	var otlpExporter *otlptrace.Exporter
	var err error
	switch protocol {
	case "grpc":
		otlpExporter, err = otlptracegrpc.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create OTLP gRPC trace exporter: %w", err)
		}
	case "http/protobuf":
		otlpExporter, err = otlptracehttp.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create OTLP HTTP trace exporter: %w", err)
		}
	default:
		return nil, fmt.Errorf("unimplemented OTLP protocol %s", protocol)
	}
	return otlpExporter, nil
}

func NewProcessorsFromEnv(ctx context.Context) ([]sdktrace.SpanProcessor, error) {
	enabled := strings.Split(strings.TrimSpace(os.Getenv("OTEL_TRACES_EXPORTER")), ",")

	// https://opentelemetry.io/docs/reference/specification/sdk-environment-variables/#exporter-selection
	// Default exporter should be "otlp"; however to preserve compatibiltiy
	// we will default to "none".
	if slices.Contains(enabled, "none") {
		// Short-circuit: If "none" is present, ignore everything else.
		enabled = nil
	}

	var spanProcessors []sdktrace.SpanProcessor

	for _, name := range enabled {
		if new, ok := newExporters[name]; ok {
			if exporter, err := new(ctx); err == nil {
				spanProcessors = append(spanProcessors, sdktrace.NewBatchSpanProcessor(exporter))
			} else {
				return nil, fmt.Errorf("failed to create exporter \"%s\": %w", name, err)
			}
		} else {
			return nil, fmt.Errorf("unknown trace exporter: \"%s\"", name)
		}
	}

	return spanProcessors, nil
}
