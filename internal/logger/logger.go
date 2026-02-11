// sentiric-agent-service/internal/logger/logger.go
// NOT: Bu dosya artık cdr-service ile aynı Sentiric Standart Logger'ı kullanacak.
// Proje genelinde tutarlılık için tam içerik cdr-service/internal/logger/logger.go ile aynıdır.

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

func New(serviceName, env, logLevel string) zerolog.Logger {
	level, _ := zerolog.ParseLevel(strings.ToLower(logLevel))
	zerolog.TimeFieldFormat = time.RFC3339

	var logger zerolog.Logger
	if env == "development" {
		output := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05"}
		logger = zerolog.New(output).With().Timestamp().Str("svc", serviceName).Logger()
	} else {
		logger = zerolog.New(os.Stderr).With().Timestamp().Str("svc", serviceName).Logger()
	}

	return logger.Level(level).Hook(PIIHook{})
}
