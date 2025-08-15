package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync" // WaitGroup için eklendi
	"syscall"
	"time" // Timeout için eklendi

	"github.com/sentiric/sentiric-agent-service/internal/client"
	"github.com/sentiric/sentiric-agent-service/internal/config"
	"github.com/sentiric/sentiric-agent-service/internal/database"
	"github.com/sentiric/sentiric-agent-service/internal/handler"
	"github.com/sentiric/sentiric-agent-service/internal/logger"
	"github.com/sentiric/sentiric-agent-service/internal/metrics"
	"github.com/sentiric/sentiric-agent-service/internal/queue"
)

const serviceName = "agent-service"

func main() {
	// 1. Konfigürasyonu yükle
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Konfigürasyon yüklenemedi: %v", err)
	}

	// 2. Ortama duyarlı logger'ı başlat
	appLog := logger.New(serviceName, cfg.Env)
	appLog.Info().Msg("Konfigürasyon başarıyla yüklendi.")

	// 3. Metrik sunucusunu başlat
	// Not: Metrik sunucusu için graceful shutdown eklemek de mümkündür,
	// ancak bu şablonu basit tutmak için şimdilik atlıyoruz.
	go metrics.StartServer(cfg.MetricsPort, appLog)

	// 4. Veritabanı bağlantısını kur
	db, err := database.Connect(cfg.PostgresURL, appLog)
	if err != nil {
		appLog.Fatal().Err(err).Msg("Veritabanı bağlantısı kurulamadı")
	}
	defer db.Close() // defer ile en son db bağlantısı kapatılacak.

	// 5. gRPC istemcilerini oluştur (bağlantıların kapatılması gRPC kütüphanesi tarafından yönetilir)
	mediaClient, err := client.NewMediaServiceClient(cfg)
	if err != nil {
		appLog.Fatal().Err(err).Msg("Media Service gRPC istemcisi oluşturulamadı")
	}
	userClient, err := client.NewUserServiceClient(cfg)
	if err != nil {
		appLog.Fatal().Err(err).Msg("User Service gRPC istemcisi oluşturulamadı")
	}
	ttsClient, err := client.NewTTSServiceClient(cfg)
	if err != nil {
		appLog.Fatal().Err(err).Msg("TTS Gateway gRPC istemcisi oluşturulamadı")
	}

	// 6. Olay işleyiciyi oluştur
	// DÜZELTME: Eski URL'ler yerine yeni ttsClient'ı veriyoruz
	eventHandler := handler.NewEventHandler(db, mediaClient, userClient, ttsClient, appLog, metrics.EventsProcessed, metrics.EventsFailed)

	// 7. RabbitMQ bağlantısını kur
	rabbitCh, closeChan := queue.Connect(cfg.RabbitMQURL, appLog)
	if rabbitCh != nil {
		defer rabbitCh.Close()
	}

	// DÜZELTME: Graceful shutdown için context ve WaitGroup yönetimi
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	// Tüketiciyi bir goroutine içinde başlat
	// DÜZELTME: Artık kullanılmayan cfg.QueueName parametresini kaldırıyoruz.
	go queue.StartConsumer(ctx, rabbitCh, eventHandler.HandleRabbitMQMessage, appLog, &wg)

	// 8. Kapatma sinyallerini dinle
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		appLog.Info().Str("signal", sig.String()).Msg("Kapatma sinyali alındı, servis durduruluyor...")
	case err := <-closeChan:
		// RabbitMQ bağlantısı koptuysa, zaten yapacak bir şey yok, çık.
		if err != nil {
			appLog.Error().Err(err).Msg("RabbitMQ bağlantısı koptu.")
		}
	}

	// Tüketici döngüsüne durmasını söyle
	cancel()

	// Tüm çalışan işleyicilerin bitmesini bekle (örneğin 10 saniye timeout ile)
	appLog.Info().Msg("Mevcut işlemlerin bitmesi bekleniyor...")
	waitChan := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitChan)
	}()

	select {
	case <-waitChan:
		appLog.Info().Msg("Tüm işlemler başarıyla tamamlandı. Çıkış yapılıyor.")
	case <-time.After(10 * time.Second):
		appLog.Warn().Msg("Graceful shutdown zaman aşımına uğradı. Çıkış yapılıyor.")
	}
}
