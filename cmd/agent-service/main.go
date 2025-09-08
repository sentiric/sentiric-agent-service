package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/rabbitmq/amqp091-go"
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

var (
	ServiceVersion string
	GitCommit      string
	BuildDate      string
)

const serviceName = "agent-service"

// setupInfrastructure, tüm altyapı bağlantılarını kendi goroutine'lerinde,
// başarılı olana kadar periyodik olarak deneyen bir fonksiyondur.
// DÜZELTME: Kullanılmayan `wg` parametresi kaldırıldı.
func setupInfrastructure(ctx context.Context, cfg *config.Config, appLog zerolog.Logger) (
	db *sql.DB,
	redisClient *redis.Client,
	rabbitCh *amqp091.Channel,
	closeChan <-chan *amqp091.Error,
) {
	var infraWg sync.WaitGroup
	infraWg.Add(3)

	// --- PostgreSQL Bağlantısı ---
	go func() {
		defer infraWg.Done()
		for {
			select {
			case <-ctx.Done():
				appLog.Info().Msg("PostgreSQL bağlantı denemesi context iptaliyle durduruldu.")
				return
			default:
				var err error
				db, err = database.Connect(ctx, cfg.PostgresURL, appLog)
				if err == nil {
					return
				}
				if ctx.Err() == nil {
					appLog.Warn().Err(err).Msg("PostgreSQL'e bağlanılamadı, 5 saniye sonra tekrar denenecek...")
				}

				select {
				case <-time.After(5 * time.Second):
				case <-ctx.Done():
					appLog.Info().Msg("PostgreSQL bağlantı beklemesi context iptaliyle durduruldu.")
					return
				}
			}
		}
	}()

	// --- Redis Bağlantısı ---
	go func() {
		defer infraWg.Done()
		for {
			select {
			case <-ctx.Done():
				appLog.Info().Msg("Redis bağlantı denemesi context iptaliyle durduruldu.")
				return
			default:
				opt, err := redis.ParseURL(cfg.RedisURL)
				if err != nil {
					if ctx.Err() == nil {
						appLog.Error().Err(err).Msg("redis URL'si parse edilemedi, 5 saniye sonra tekrar denenecek...")
					}
				} else {
					redisClient = redis.NewClient(opt)
					pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					err := redisClient.Ping(pingCtx).Err()
					cancel()

					if err == nil {
						appLog.Info().Msg("Redis bağlantısı başarılı.")
						return
					}
					if ctx.Err() == nil {
						appLog.Warn().Err(err).Msg("Redis'e bağlanılamadı, 5 saniye sonra tekrar denenecek...")
					}
				}

				select {
				case <-time.After(5 * time.Second):
				case <-ctx.Done():
					appLog.Info().Msg("Redis bağlantı beklemesi context iptaliyle durduruldu.")
					return
				}
			}
		}
	}()

	// --- RabbitMQ Bağlantısı ---
	go func() {
		defer infraWg.Done()
		for {
			select {
			case <-ctx.Done():
				appLog.Info().Msg("RabbitMQ bağlantı denemesi context iptaliyle durduruldu.")
				return
			default:
				var err error
				rabbitCh, closeChan, err = queue.Connect(ctx, cfg.RabbitMQURL, appLog)
				if err == nil {
					return
				}
				if ctx.Err() == nil {
					appLog.Warn().Err(err).Msg("RabbitMQ'ya bağlanılamadı, 5 saniye sonra tekrar denenecek...")
				}

				select {
				case <-time.After(5 * time.Second):
				case <-ctx.Done():
					appLog.Info().Msg("RabbitMQ bağlantı beklemesi context iptaliyle durduruldu.")
					return
				}
			}
		}
	}()

	infraWg.Wait()
	if ctx.Err() != nil {
		appLog.Info().Msg("Altyapı kurulumu, servis kapatıldığı için iptal edildi.")
		return
	}
	appLog.Info().Msg("Tüm altyapı bağlantıları başarıyla kuruldu.")
	return
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Konfigürasyon yüklenemedi: %v", err)
	}

	appLog := logger.New(serviceName, cfg.Env)
	appLog.Info().
		Str("version", ServiceVersion).
		Str("commit", GitCommit).
		Str("build_date", BuildDate).
		Str("profile", cfg.Env).
		Msg("🚀 agent-service başlatılıyor...")

	go metrics.StartServer(cfg.MetricsPort, appLog)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	var db *sql.DB
	var redisClient *redis.Client
	var rabbitCh *amqp091.Channel
	var rabbitCloseChan <-chan *amqp091.Error

	wg.Add(1)
	go func() {
		defer wg.Done()
		// DÜZELTME: Kullanılmayan `wg` parametresi kaldırıldı.
		db, redisClient, rabbitCh, rabbitCloseChan = setupInfrastructure(ctx, cfg, appLog)
		if db != nil {
			defer db.Close()
		}
		if redisClient != nil {
			defer redisClient.Close()
		}
		if rabbitCh != nil {
			defer rabbitCh.Close()
		}
		if ctx.Err() != nil {
			return
		}

		publisher := queue.NewPublisher(rabbitCh, appLog)
		mediaClient, _ := client.NewMediaServiceClient(cfg)
		userClient, _ := client.NewUserServiceClient(cfg)
		ttsClient, _ := client.NewTTSServiceClient(cfg)
		stateManager := state.NewManager(redisClient)
		llmClient := client.NewLlmClient(cfg.LlmServiceURL, appLog)
		sttClient := client.NewSttClient(cfg.SttServiceURL, appLog)
		eventHandler := handler.NewEventHandler(db, cfg, stateManager, publisher, mediaClient, userClient, ttsClient, llmClient, sttClient, appLog, metrics.EventsProcessed, metrics.EventsFailed, cfg.SttServiceTargetSampleRate)

		var consumerWg sync.WaitGroup
		go queue.StartConsumer(ctx, rabbitCh, eventHandler.HandleRabbitMQMessage, appLog, &consumerWg)

		select {
		case <-ctx.Done():
		case err := <-rabbitCloseChan:
			if err != nil {
				appLog.Error().Err(err).Msg("RabbitMQ bağlantısı koptu, servis durduruluyor.")
			}
			cancel()
		}

		appLog.Info().Msg("RabbitMQ tüketicisinin bitmesi bekleniyor...")
		consumerWg.Wait()
		appLog.Info().Msg("Aktif diyalogların bitmesi bekleniyor...")
		eventHandler.WaitOnDialogs()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	appLog.Info().Msg("Kapatma sinyali alındı, servis durduruluyor...")
	cancel()

	wg.Wait()
	appLog.Info().Msg("Tüm servisler başarıyla durduruldu. Çıkış yapılıyor.")
}
