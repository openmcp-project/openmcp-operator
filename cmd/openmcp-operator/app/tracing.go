// tracing.go initialises the global OTel TracerProvider for the operator.

package app

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// setupTracing initialises the global OTel TracerProvider with an OTLP gRPC
// exporter.  The exporter endpoint is configured through the standard
// OTEL_EXPORTER_OTLP_ENDPOINT / OTEL_EXPORTER_OTLP_TRACES_ENDPOINT env vars.
//
// The returned shutdown func must be called on process exit to flush in-flight
// spans.  If the exporter cannot be created (e.g. no endpoint configured) the
// function logs a warning and returns a no-op shutdown so the operator still
// starts without tracing.
func setupTracing(ctx context.Context, serviceName, serviceVersion string) (shutdown func(context.Context) error, err error) {
	exporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		// No endpoint configured or connection failed — run without tracing.
		return func(context.Context) error { return nil },
			fmt.Errorf("tracing disabled: failed to create OTLP exporter: %w", err)
	}

	res, err := sdkresource.New(ctx,
		sdkresource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(serviceVersion),
		),
		sdkresource.WithFromEnv(),
	)
	if err != nil {
		res = sdkresource.Default()
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}
