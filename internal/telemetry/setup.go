package telemetry

import (
	"context"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

func Setup(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	if !enabled() {
		return func(context.Context) error { return nil }, nil
	}

	res, err := resource.New(ctx, resource.WithAttributes(semconv.ServiceName(serviceName)))
	if err != nil {
		return nil, err
	}

	traceExporter, err := stdouttrace.New()
	if err != nil {
		return nil, err
	}
	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(traceExporter),
	)

	metricExporter, err := stdoutmetric.New()
	if err != nil {
		return nil, err
	}
	metricProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
	)

	otel.SetTracerProvider(traceProvider)
	otel.SetMeterProvider(metricProvider)

	return func(ctx context.Context) error {
		if err := metricProvider.Shutdown(ctx); err != nil {
			return err
		}
		return traceProvider.Shutdown(ctx)
	}, nil
}

func enabled() bool {
	value := strings.ToLower(os.Getenv("SGNG_OTEL_STDOUT"))
	return value == "1" || value == "true" || value == "yes"
}
