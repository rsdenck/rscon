package api

import (
	"context"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/rsdenck/rscon/internal/identity"
	"github.com/rsdenck/rscon/internal/observability"
)

// middleware wraps the API handler with OpenTelemetry tracing and correlation IDs.
func middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Extract existing traceparent header if present.
		traceID := r.Header.Get("X-Trace-ID")
		if traceID == "" {
			traceID = r.Header.Get("X-B3-Traceid")
		}
		if traceID == "" {
			traceID = r.Header.Get("traceparent")
			if traceID != "" {
				// traceparent format: "00-{trace_id}-{span_id}-{flags}"
				parts := strings.Split(traceID, "-")
				if len(parts) >= 2 && parts[0] == "00" {
					traceID = parts[1]
				}
			}
		}

		// Start a new span (or continue existing trace).
		var span trace.Span
		var ctxCtx context.Context

		if traceID != "" {
			// Continue existing trace - start a new span within it.
			ctxCtx, span = observability.Tracer("rscon").Start(
				ctx,
				r.URL.Path,
				trace.WithAttributes(
					attribute.String("http.method", r.Method),
					attribute.String("http.url", r.URL.Path),
				),
			)
		} else {
			// Start a new trace.
			ctxCtx, span = observability.Tracer("rscon").Start(
				ctx,
				r.URL.Path,
				trace.WithAttributes(
					attribute.String("http.method", r.Method),
					attribute.String("http.url", r.URL.Path),
				),
			)
			// Set the trace ID on the response header for client propagation.
			w.Header().Set("X-Trace-ID", span.SpanContext().TraceID().String())
		}

		// Ensure the request context carries the request ID.
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" || len(requestID) > 128 {
			requestID = identity.NewID()
		}
		ctxCtx = context.WithValue(ctxCtx, ctxRequestID, requestID)

		// Continue with the wrapped handler.
		next.ServeHTTP(w, r.WithContext(ctxCtx))
	})
}