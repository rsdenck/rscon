package observability

import (
	"context"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	sdtrace "go.opentelemetry.io/otel/sdk/trace"
	otlptrace "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	atracetype "go.opentelemetry.io/otel/trace"
)

// TracerProvider holds the configured OpenTelemetry tracer provider.
var TracerProvider = newTracerProvider()

func newTracerProvider() *sdtrace.TracerProvider {
	// Set up resource with service information.
	res, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			semconv.ServiceName("rscon"),
			semconv.ServiceVersion("0.1.3"),
			attribute.String("deployment.environment", getEnv("RSCON_DEPLOYMENT_ENV", "development")),
			attribute.String("service.instance", getEnv("RSCON_INSTANCE_ID", "unknown")),
		),
		resource.WithHost(),
	)
	if err != nil {
		// If resource creation fails, fall back to a minimal resource.
		res, _ = resource.New(context.Background())
	}

	otelEndpoint := getEnv("RSCON_OTEL_ENDPOINT", "")

	var tp *sdtrace.TracerProvider

	if otelEndpoint != "" {
		// Use OTLP HTTP exporter when an endpoint is configured.
		otlpExporter, err := otlptrace.New(
			context.Background(),
			otlptrace.WithEndpoint(otelEndpoint),
			otlptrace.WithInsecure(),
		)
		if err != nil {
			// Fall back to no exporter if OTLP exporter fails.
			otlpExporter = nil
		}
		tp = sdtrace.NewTracerProvider(
			sdtrace.WithBatcher(otlpExporter,
				sdtrace.WithMaxExportBatchSize(100),
				sdtrace.WithExportTimeout(10*1e9)),
			sdtrace.WithResource(res),
		)
	} else {
		// Use console exporter for local development/debugging.
		consoleExporter, err := otlptrace.New(
			context.Background(),
			otlptrace.WithEndpoint("http://localhost:4317"),
			otlptrace.WithInsecure(),
		)
		if err != nil {
			consoleExporter = nil
		}
		tp = sdtrace.NewTracerProvider(
			sdtrace.WithBatcher(consoleExporter,
				sdtrace.WithMaxExportBatchSize(100),
				sdtrace.WithExportTimeout(10*1e9)),
			sdtrace.WithResource(res),
		)
	}

	otel.SetTracerProvider(tp)

	return tp
}

// getEnv returns the value of the environment variable, or the default.
func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

// Tracer returns a tracer for the given name.
func Tracer(name string) atracetype.Tracer {
	return otel.Tracer(name)
}

// StartSpan starts a new span from the given context, with the given options.
func StartSpan(ctx context.Context, name string, opts ...atracetype.SpanStartOption) (context.Context, atracetype.Span) {
	return otel.Tracer("rscon").Start(ctx, name, opts...)
}

// ShutdownShutdown gracefully shuts down the tracer provider.
func Shutdown(ctx context.Context) error {
	return TracerProvider.Shutdown(ctx)
}