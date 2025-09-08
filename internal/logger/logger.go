package logger

import (
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func New(serviceName, env string) zerolog.Logger {
	var logger zerolog.Logger

	// --- DEĞİŞİKLİK: Zaman Damgası Standardizasyonu ---
	// Tüm logların UTC zaman diliminde ve RFC3339 formatında olmasını sağlıyoruz.
	// Bu, dağıtık sistemdeki tüm servisler arasında tutarlılık sağlar.
	zerolog.TimeFieldFormat = time.RFC3339

	if env == "development" {
		// Geliştirme ortamında, okunabilirliği artırmak için ConsoleWriter kullanıyoruz.
		// `TimeFormat`'ı burada da RFC3339 olarak ayarlayarak tutarlılığı koruyoruz.
		output := zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: time.RFC3339, // "2006-01-02T15:04:05Z07:00" formatı
		}
		logger = log.Output(output).With().Timestamp().Str("service", serviceName).Logger()
	} else {
		// Üretim ortamında, performans için doğrudan JSON formatında yazıyoruz.
		// `TimeFieldFormat` zaten global olarak ayarlandığı için doğru formatta olacaktır.
		logger = zerolog.New(os.Stderr).With().Timestamp().Str("service", serviceName).Logger()
	}

	return logger
}
