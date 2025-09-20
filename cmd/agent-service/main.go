// sentiric-agent-service/cmd/agent-service/main.go
package main

import (
	"log"
	// YENİ IMPORT: gRPC'nin loglama davranışını kontrol etmek için
	"google.golang.org/grpc/grpclog"
	"io/ioutil"

	"github.com/sentiric/sentiric-agent-service/internal/app"
	"github.com/sentiric/sentiric-agent-service/internal/config"
	"github.com/sentiric/sentiric-agent-service/internal/logger"
)

var (
	ServiceVersion string
	GitCommit      string
	BuildDate      string
)

const serviceName = "agent-service"

// --- YENİ FONKSİYON ---
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
// --- YENİ FONKSİYON SONU ---

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Konfigürasyon yüklenemedi: %v", err)
	}

	// --- DEĞİŞİKLİK BURADA ---
	// Kendi logger'ımızı oluşturduktan hemen sonra, gRPC logger'ını yapılandırıyoruz.
	appLog := logger.New(serviceName, cfg.Env)
	initGrpcLogger(cfg.LogLevel) // cfg.LogLevel, .env'deki LOG_LEVEL'i okuyacak.
	// --- DEĞİŞİKLİK SONU ---

	appLog.Info().
		Str("version", ServiceVersion).
		Str("commit", GitCommit).
		Str("build_date", BuildDate).
		Str("profile", cfg.Env).
		Msg("🚀 agent-service başlatılıyor...")

	application := app.NewApp(cfg, appLog)
	application.Run()
}