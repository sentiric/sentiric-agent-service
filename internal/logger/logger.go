// Package logger provides a standardized, environment-aware logger for all Go services.
package logger

import (
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// New initializes and configures a new zerolog.Logger based on the ENV environment variable.
//
// In "development" environment, it returns a human-friendly, colored console logger.
// In "production" or any other environment, it returns a structured JSON logger.
func New(serviceName string) zerolog.Logger {
	env := os.Getenv("ENV")
	var logger zerolog.Logger

	zerolog.TimeFieldFormat = time.RFC3339

	if env == "development" {
		// Geliştirme ortamı için renkli, okunabilir konsol logları
		output := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}
		logger = log.Output(output).With().Timestamp().Str("service", serviceName).Logger()
	} else {
		// Üretim ortamı için yapılandırılmış JSON logları
		logger = zerolog.New(os.Stderr).With().Timestamp().Str("service", serviceName).Logger()
	}

	return logger
}
