package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/rs/zerolog"
	"github.com/sentiric/sentiric-agent-service/internal/client"
	"github.com/sentiric/sentiric-agent-service/internal/config"
	"github.com/sentiric/sentiric-agent-service/internal/database"
	"github.com/sentiric/sentiric-agent-service/internal/handler"
	"github.com/sentiric/sentiric-agent-service/internal/logger"
	"github.com/sentiric/sentiric-agent-service/internal/metrics"
	"github.com/sentiric/sentiric-agent-service/internal/queue"
)

const serviceName = "agent-service"

// YENİ: connectToRedisWithRetry fonksiyonu
func connectToRedisWithRetry(cfg *config.Config, log zerolog.Logger) *redis.Client {
	var rdb *redis.Client
	var err error

	opt, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		log.Fatal().Err(err).Msg("Redis URL'si parse edilemedi")
	}

	for i := 0; i < 10; i++ {
		rdb = redis.NewClient(opt)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if _, err = rdb.Ping(ctx).Result(); err == nil {
			log.Info().Msg("Redis bağlantısı başarılı.")
			return rdb
		}

		log.Warn().Err(err).Int("attempt", i+1).Int("max_attempts", 10).Msg("Redis'e bağlanılamadı, 5 saniye sonra tekrar denenecek...")
		time.Sleep(5 * time.Second)
	}

	log.Fatal().Err(err).Msgf("Maksimum deneme (%d) sonrası Redis'e bağlanılamadı", 10)
	return nil
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Konfigürasyon yüklenemedi: %v", err)
	}

	appLog := logger.New(serviceName, cfg.Env)
	appLog.Info().Msg("Konfigürasyon başarıyla yüklendi.")

	go metrics.StartServer(cfg.MetricsPort, appLog)

	db, err := database.Connect(cfg.PostgresURL, appLog)
	if err != nil {
		appLog.Fatal().Err(err).Msg("Veritabanı bağlantısı kurulamadı")
	}
	defer db.Close()

	// DÜZELTME: Yeni dayanıklı fonksiyonu kullanıyoruz
	redisClient := connectToRedisWithRetry(cfg, appLog)

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

	eventHandler := handler.NewEventHandler(
		db, redisClient, mediaClient, userClient, ttsClient,
		cfg.LlmServiceURL, cfg.SttServiceURL, appLog,
		metrics.EventsProcessed, metrics.EventsFailed,
	)

	rabbitCh, closeChan := queue.Connect(cfg.RabbitMQURL, appLog)
	if rabbitCh != nil {
		defer rabbitCh.Close()
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	go queue.StartConsumer(ctx, rabbitCh, eventHandler.HandleRabbitMQMessage, appLog, &wg)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		appLog.Info().Str("signal", sig.String()).Msg("Kapatma sinyali alındı, servis durduruluyor...")
	case err := <-closeChan:
		if err != nil {
			appLog.Error().Err(err).Msg("RabbitMQ bağlantısı koptu.")
		}
	}

	cancel()

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
