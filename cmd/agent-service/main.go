// sentiric-agent-service/cmd/agent-service/main.go
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

// Bu fonksiyon, gRPC'nin varsayılan logger'ını bizim istediğimiz şekilde yapılandırır.
func initGrpcLogger(logLevel string) {
	// Eğer log seviyesi DEBUG değilse, gRPC'nin tüm loglarını yoksay.
	if logLevel != "debug" {
		// grpclog.NewLoggerV2'nin ioutil.Discard'a yazmasını sağlayarak
		// tüm gRPC loglarını /dev/null gibi bir çöpe yönlendiriyoruz.
		grpclog.SetLoggerV2(grpclog.NewLoggerV2(ioutil.Discard, ioutil.Discard, ioutil.Discard))
	}
	// Eğer logLevel == "debug" ise, hiçbir şey yapmıyoruz ve gRPC'nin
	// varsayılan (ve genellikle çok detaylı olan) loglamasına izin veriyoruz.
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Konfigürasyon yüklenemedi: %v", err)
	}

	appLog := logger.New(serviceName, cfg.Env)
	// Artık cfg.LogLevel alanına erişebiliriz.
	initGrpcLogger(cfg.LogLevel) 

	appLog.Info().
		Str("version", ServiceVersion).
		Str("commit", GitCommit).
		Str("build_date", BuildDate).
		Str("profile", cfg.Env).
		Msg("🚀 agent-service başlatılıyor...")

	application := app.NewApp(cfg, appLog)
	application.Run()
}