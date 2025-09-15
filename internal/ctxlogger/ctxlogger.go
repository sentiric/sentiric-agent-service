package ctxlogger

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// loggerKey, context içinde logger'ı saklamak için özel bir tip ve anahtar tanımlar.
type loggerKey struct{}

// ToContext, verilen context'e bir zerolog.Logger ekler.
func ToContext(ctx context.Context, logger zerolog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// FromContext, context'ten zerolog.Logger'ı alır.
// Eğer context'te logger bulunamazsa, global (ve bağlamsız) log'u döndürür.
func FromContext(ctx context.Context) zerolog.Logger {
	if logger, ok := ctx.Value(loggerKey{}).(zerolog.Logger); ok {
		return logger
	}
	// Fallback to the default global logger if not found in context.
	return log.Logger
}
