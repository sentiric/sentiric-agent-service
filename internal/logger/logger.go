// Dosya: internal/logger/logger.go
package logger

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/metadata"
)

const (
	SchemaVersion = "1.0.0"
)

type SutsHook struct {
	Resource map[string]string
	TenantID string
}

func (h SutsHook) Run(e *zerolog.Event, level zerolog.Level, msg string) {
	e.Str("schema_v", SchemaVersion)
	e.Str("tenant_id", h.TenantID)

	dict := zerolog.Dict()
	for k, v := range h.Resource {
		dict.Str(k, v)
	}
	e.Dict("resource", dict)
}

// [ARCH-COMPLIANCE] version parametresi eklendi
func New(serviceName, version, env, logLevel, logFormat, tenantID string) zerolog.Logger {
	var logger zerolog.Logger

	level, err := zerolog.ParseLevel(strings.ToLower(logLevel))
	if err != nil {
		level = zerolog.InfoLevel
	}

	zerolog.TimeFieldFormat = time.RFC3339Nano
	zerolog.TimestampFieldName = "ts"
	zerolog.LevelFieldName = "severity"
	zerolog.MessageFieldName = "message"

	zerolog.LevelFieldMarshalFunc = func(l zerolog.Level) string {
		return strings.ToUpper(l.String())
	}

	// [ARCH-COMPLIANCE] Dinamik versiyon ataması
	resource := map[string]string{
		"service.name":    serviceName,
		"service.version": version,
		"service.env":     env,
		"host.name":       os.Getenv("NODE_HOSTNAME"),
	}

	if logFormat == "json" {
		logger = zerolog.New(os.Stderr).
			Hook(SutsHook{Resource: resource, TenantID: tenantID}).
			With().
			Timestamp().
			Logger()
	} else {
		output := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}
		logger = zerolog.New(output).With().Timestamp().Str("service", serviceName).Str("tenant_id", tenantID).Logger()
	}

	return logger.Level(level)
}

func ContextLogger(ctx context.Context, baseLog zerolog.Logger) zerolog.Logger {
	l := baseLog.With()

	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("x-trace-id"); len(vals) > 0 && vals[0] != "" {
			l = l.Str("trace_id", vals[0])
		}
	}

	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		l = l.Str("span_id", spanCtx.SpanID().String())
	} else {
		l = l.Str("span_id", "0000000000000000")
	}

	return l.Logger()
}
