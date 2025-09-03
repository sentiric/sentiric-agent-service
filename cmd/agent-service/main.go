// File: cmd/agent-service/main.go

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
	"github.com/sentiric/sentiric-agent-service/internal/state"
)

// YENİ: ldflags ile doldurulacak değişkenler
var (
	ServiceVersion string
	GitCommit      string
	BuildDate      string
)

const serviceName = "agent-service"

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

		log.Warn().Err(err).Int("attempt", i+1).Msg("Redis'e bağlanılamadı, 5 saniye sonra tekrar denenecek...")
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

	// YENİ: Başlangıçta versiyon bilgisini logla
	appLog.Info().
		Str("version", ServiceVersion).
		Str("commit", GitCommit).
		Str("build_date", BuildDate).
		Str("profile", cfg.Env).
		Msg("🚀 agent-service başlatılıyor...")

	go metrics.StartServer(cfg.MetricsPort, appLog)

	db, err := database.Connect(cfg.PostgresURL, appLog)
	if err != nil {
		appLog.Fatal().Err(err).Msg("Veritabanı bağlantısı kurulamadı")
	}
	defer db.Close()

	redisClient := connectToRedisWithRetry(cfg, appLog)

	rabbitCh, closeChan := queue.Connect(cfg.RabbitMQURL, appLog)
	if rabbitCh != nil {
		defer rabbitCh.Close()
	}
	publisher := queue.NewPublisher(rabbitCh, appLog)

	// gRPC İstemcileri
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

	stateManager := state.NewManager(redisClient)
	llmClient := client.NewLlmClient(cfg.LlmServiceURL, appLog)
	sttClient := client.NewSttClient(cfg.SttServiceURL, appLog)

	eventHandler := handler.NewEventHandler(
		db,
		cfg,
		stateManager,
		publisher,
		mediaClient,
		userClient,
		ttsClient,
		llmClient,
		sttClient,
		appLog,
		metrics.EventsProcessed,
		metrics.EventsFailed,
		cfg.SttServiceTargetSampleRate,
	)

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
