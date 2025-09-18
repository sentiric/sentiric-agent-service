package logger

import (
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func New(serviceName, env string) zerolog.Logger {
	var logger zerolog.Logger

	// Tüm logların UTC zaman diliminde ve RFC3339 formatında olmasını sağlıyoruz.
	zerolog.TimeFieldFormat = time.RFC3339

	if env == "development" {
		// Geliştirme ortamında, okunabilirliği artırmak için ConsoleWriter kullanıyoruz.
		output := zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: time.RFC3339,
		}
		logger = log.Output(output).With().Timestamp().Str("service", serviceName).Logger()
	} else {
		// Üretim/Core ortamında, performans için doğrudan JSON formatında yazıyoruz.
		logger = zerolog.New(os.Stderr).With().Timestamp().Str("service", serviceName).Logger()
	}

	return logger
}
