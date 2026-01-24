package app

import (
	"context"
	"database/sql"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/go-redis/redis/v8"
	"github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
	"github.com/sentiric/sentiric-agent-service/internal/client"
	"github.com/sentiric/sentiric-agent-service/internal/config"
	"github.com/sentiric/sentiric-agent-service/internal/database"
	"github.com/sentiric/sentiric-agent-service/internal/handler"
	"github.com/sentiric/sentiric-agent-service/internal/metrics"
	"github.com/sentiric/sentiric-agent-service/internal/queue"
	"github.com/sentiric/sentiric-agent-service/internal/state"
)

type App struct {
	Cfg *config.Config
	Log zerolog.Logger
}

func NewApp(cfg *config.Config, log zerolog.Logger) *App {
	return &App{Cfg: cfg, Log: log}
}

func (a *App) Run() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Metrik Sunucusu
	go metrics.StartServer(a.Cfg.MetricsPort, a.Log)

	// 2. Altyapı Bağlantıları
	db, redisClient, rabbitCh, closeChan := a.setupInfrastructure(ctx)
	if db != nil { defer db.Close() }
	if redisClient != nil { defer redisClient.Close() }
	if rabbitCh != nil { defer rabbitCh.Close() }

	// 3. gRPC İstemcileri
	clients, err := client.NewClients(a.Cfg)
	if err != nil {
		a.Log.Fatal().Err(err).Msg("gRPC istemcileri başlatılamadı")
	}

	// 4. Handler ve State Manager
	stateManager := state.NewManager(redisClient)
	
	// DÜZELTME: CallHandler artık DB bağlantısını da alıyor
	callHandler := handler.NewCallHandler(clients, stateManager, db, a.Log)

	// EventHandler Başlatma
	eventHandler := handler.NewEventHandler(
		a.Log,
		metrics.EventsProcessed,
		metrics.EventsFailed,
		callHandler,
	)
	
	var consumerWg sync.WaitGroup
	
	// RabbitMQ Consumer Başlat
	go queue.StartConsumer(ctx, rabbitCh, eventHandler.HandleRabbitMQMessage, a.Log, &consumerWg)

	// 5. Graceful Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	
	select {
	case <-quit:
		a.Log.Info().Msg("Kapatma sinyali alındı...")
	case err := <-closeChan:
		a.Log.Error().Err(err).Msg("RabbitMQ bağlantısı koptu!")
	}

	cancel() // Context'i iptal et
	a.Log.Info().Msg("Consumer işlemlerinin bitmesi bekleniyor...")
	consumerWg.Wait() // Consumer'ın bitmesini bekle
	a.Log.Info().Msg("Servis durduruldu.")
}

func (a *App) setupInfrastructure(ctx context.Context) (*sql.DB, *redis.Client, *amqp091.Channel, <-chan *amqp091.Error) {
	db, err := database.Connect(ctx, a.Cfg.PostgresURL, a.Log)
	if err != nil {
		a.Log.Fatal().Err(err).Msg("Postgres bağlantı hatası")
	}

	rdb, err := database.ConnectRedis(ctx, a.Cfg.RedisURL, a.Log)
	if err != nil {
		a.Log.Fatal().Err(err).Msg("Redis bağlantı hatası")
	}

	ch, closeCh, err := queue.Connect(ctx, a.Cfg.RabbitMQURL, a.Log)
	if err != nil {
		a.Log.Fatal().Err(err).Msg("RabbitMQ bağlantı hatası")
	}

	return db, rdb, ch, closeCh
}