// Package transport provides an OTel-instrumented http.RoundTripper for
// cluster API calls made through AccessRequest kubeconfigs.
//
// Every HTTP round-trip gets its own child span.  4xx/5xx responses are
// recorded as span errors so that authentication failures (401 Unauthorized,
// 403 Forbidden) from the onboarding cluster are visible in traces without
// any log scraping.  Network-level errors (TLS, DNS, timeout) are also
// recorded as span errors.
package transport

import (
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/openmcp-project/openmcp-operator/lib/clusteraccess"

// WrapFunc returns a function compatible with rest.Config.WrapTransport.
// The returned function, when called with the default transport chosen by
// client-go, wraps it with OTel span recording.
//
// clusterID is attached as cluster.id on every span so spans from different
// target clusters can be filtered independently.
func WrapFunc(clusterID string) func(http.RoundTripper) http.RoundTripper {
	return func(base http.RoundTripper) http.RoundTripper {
		return Wrap(base, clusterID)
	}
}

// Wrap wraps base with OTel span recording.  base must not be nil; use
// WrapFunc when plugging into rest.Config.WrapTransport (client-go always
// supplies a non-nil base there).
func Wrap(base http.RoundTripper, clusterID string) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &tracingTransport{
		base:      base,
		clusterID: clusterID,
		tracer:    otel.Tracer(tracerName),
	}
}

type tracingTransport struct {
	base      http.RoundTripper
	clusterID string
	tracer    trace.Tracer
}

func (t *tracingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, span := t.tracer.Start(req.Context(), "cluster.http.roundtrip",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("cluster.id", t.clusterID),
			attribute.String("http.method", req.Method),
			attribute.String("http.url", req.URL.Redacted()),
		),
	)
	defer span.End()

	resp, err := t.base.RoundTrip(req.WithContext(ctx))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return resp, err
	}

	span.SetAttributes(attribute.Int("http.status_code", resp.StatusCode))

	if resp.StatusCode >= 400 {
		msg := fmt.Sprintf("HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
		span.RecordError(fmt.Errorf("%s: %s %s", msg, req.Method, req.URL.Redacted()))
		span.SetStatus(codes.Error, msg)
	}

	return resp, nil
}
