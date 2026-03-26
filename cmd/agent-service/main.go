// [ARCH-COMPLIANCE] Strict logging formats and events implemented
package main

import (
	"io/ioutil"
	"log"

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
		log.Fatalf("Konfigürasyon yüklenemedi: %v", err)
	}

	appLog := logger.New(serviceName, cfg.Env, cfg.LogLevel, cfg.LogFormat, cfg.TenantID)
	initGrpcLogger(cfg.LogLevel)

	appLog.Info().
		Str("event", "SERVICE_STARTING").
		Str("version", ServiceVersion).
		Str("commit", GitCommit).
		Str("build_date", BuildDate).
		Str("profile", cfg.Env).
		Msg("🚀 agent-service başlatılıyor...")

	application := app.NewApp(cfg, appLog)
	application.Run()
}
