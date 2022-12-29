package tracing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func NewProviderFromEnv(ctx context.Context, resourceOptions ...resource.Option) (*sdktrace.TracerProvider, error) {
	spanProcessors, err := NewProcessorsFromEnv(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get tracing processors: %w", err)
	}
	if spanProcessors == nil {
		// Nothing to export.
		return nil, nil
	}

	res, err := resource.New(
		ctx,
		append(
			resourceOptions,
			// Allow environment variables to override constant attributes provided by the caller.
			resource.WithFromEnv(),
			resource.WithProcess(),
		)...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
	)

	for _, sp := range spanProcessors {
		tracerProvider.RegisterSpanProcessor(sp)
	}

	return tracerProvider, nil
}

func SetProviderFromEnv(ctx context.Context, resourceOptions ...resource.Option) (func(context.Context) error, error) {
	tracerProvider, err := NewProviderFromEnv(ctx, resourceOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to create tracing provider: %w", err)
	}

	if tracerProvider == nil {
		// Make sure there's always something to use. (And that we
		// clear out any previous provider.)
		otel.SetTracerProvider(trace.NewNoopTracerProvider())
		// It has nothing to shutdown; return a do-nothing func.
		return func(_ context.Context) error { return nil }, nil
	}

	if p, err := NewPropagatorsFromEnv(); err != nil {
		return nil, fmt.Errorf("failed to create propagators: %w", err)
	} else if p != nil {
		otel.SetTextMapPropagator(p)
	}

	otel.SetTracerProvider(tracerProvider)

	return tracerProvider.Shutdown, nil
}
