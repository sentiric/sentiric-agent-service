// Package logger provides a standardized, environment-aware logger for all Go services.
package logger

import (
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// DÜZELTME: New fonksiyonu artık env parametresi alıyor.
// Bu, log formatını merkezi config'den gelen değere göre belirlememizi sağlar.
func New(serviceName, env string) zerolog.Logger {
	var logger zerolog.Logger

	zerolog.TimeFieldFormat = time.RFC3339

	if env == "development" {
		// Geliştirme ortamı için renkli, okunabilir konsol logları
		output := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05"} // Daha kısa zaman formatı
		logger = log.Output(output).With().Timestamp().Str("service", serviceName).Logger()
	} else {
		// Üretim ortamı için yapılandırılmış JSON logları
		logger = zerolog.New(os.Stderr).With().Timestamp().Str("service", serviceName).Logger()
	}

	return logger
}
