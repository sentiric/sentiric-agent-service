// sentiric-agent-service/internal/logger/logger.go

package logger

import (
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

var phoneRegex = regexp.MustCompile(`(90|0)?5[0-9]{9}`)

type PIIHook struct{}

func (h PIIHook) Run(e *zerolog.Event, level zerolog.Level, msg string) {
	if phoneRegex.MatchString(msg) {
		masked := phoneRegex.ReplaceAllStringFunc(msg, func(phone string) string {
			if len(phone) < 7 {
				return "****"
			}
			return phone[:5] + "***" + phone[len(phone)-2:]
		})
		e.Str("masked_msg", masked)
	}
}

func New(serviceName, env, logLevel, logFormat string) zerolog.Logger {
	// [GÜNCELLENDİ] OTEL (OpenTelemetry) Uyumlu Alan İsimleri
	zerolog.TimeFieldFormat = time.RFC3339
	zerolog.TimestampFieldName = "Timestamp"
	zerolog.LevelFieldName = "SeverityText"
	zerolog.MessageFieldName = "Body"
	// Diğer alanlar (trace_id vb.) context'ten veya With() ile eklenecek

	level, _ := zerolog.ParseLevel(strings.ToLower(logLevel))

	var logger zerolog.Logger

	// [GÜNCELLENDİ] Format Kontrolü
	if logFormat == "json" {
		// JSON Modu (Production)
		logger = zerolog.New(os.Stderr).
			With().
			Timestamp().
			Str("Resource.service.name", serviceName).
			Logger()
	} else {
		// Console Modu (Development)
		output := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05"}
		logger = zerolog.New(output).
			With().
			Timestamp().
			Str("svc", serviceName).
			Logger()
	}

	return logger.Level(level).Hook(PIIHook{})
}
