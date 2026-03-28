// Dosya: cmd/agent-service/main.go
package main

import (
	"io/ioutil"
	"os"

	"github.com/sentiric/sentiric-agent-service/internal/app"
	"github.com/sentiric/sentiric-agent-service/internal/config"
	"github.com/sentiric/sentiric-agent-service/internal/logger"
	"google.golang.org/grpc/grpclog"
)

var (
	ServiceVersion string
	GitCommit      string
	BuildDate      string
)

const serviceName = "agent-service"

func initGrpcLogger(logLevel string) {
	if logLevel != "debug" {
		grpclog.SetLoggerV2(grpclog.NewLoggerV2(ioutil.Discard, ioutil.Discard, ioutil.Discard))
	}
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		// [ARCH-COMPLIANCE] ARCH-005 Düzeltmesi: log.Fatalf kullanılamaz.
		bootstrapLog := logger.New(serviceName, "unknown", "production", "info", "json", "system")
		bootstrapLog.Fatal().Str("event", "CONFIG_LOAD_FAIL").Err(err).Msg("Konfigürasyon yüklenemedi")
		os.Exit(1)
	}

	version := ServiceVersion
	if version == "" {
		version = "unknown"
	}

	appLog := logger.New(serviceName, version, cfg.Env, cfg.LogLevel, cfg.LogFormat, cfg.TenantID)
	initGrpcLogger(cfg.LogLevel)

	appLog.Info().
		Str("event", "SERVICE_STARTING").
		Str("version", version).
		Str("commit", GitCommit).
		Str("build_date", BuildDate).
		Str("profile", cfg.Env).
		Msg("🚀 agent-service başlatılıyor...")

	application := app.NewApp(cfg, appLog)
	application.Run()
}
