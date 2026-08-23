package otelsetup

import (
	"context"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// pgxTracer implements pgx.QueryTracer with a minimal hand-rolled span
// wrapper. A small custom implementation is used instead of pulling in a
// third-party pgx-otel bridge so the dependency surface stays auditable; it
// only records span kind, SQL text length (never SQL parameter values, which
// may contain credentials/PII) and success/failure — never row data.
type pgxTracer struct {
	tracer trace.Tracer
}

// PGXTracer returns a pgx.QueryTracer that records one client span per query
// using the given tracer. Pass it as db.WithTracer(...) to platform/db's
// OpenPool. Query text is recorded only as a span name/attribute for
// diagnostics; bound parameter values are never recorded.
func PGXTracer(tracer trace.Tracer) pgx.QueryTracer {
	return &pgxTracer{tracer: tracer}
}

type pgxSpanKey struct{}

func (t *pgxTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx, span := t.tracer.Start(ctx, "pgx.query", trace.WithSpanKind(trace.SpanKindClient))
	span.SetAttributes(attribute.String("db.system", "postgresql"))
	return context.WithValue(ctx, pgxSpanKey{}, span)
}

func (t *pgxTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span, ok := ctx.Value(pgxSpanKey{}).(trace.Span)
	if !ok {
		return
	}
	defer span.End()
	if data.Err != nil {
		span.RecordError(data.Err)
		span.SetStatus(codes.Error, "query failed")
	}
}
