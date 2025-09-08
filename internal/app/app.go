package app

import (
	"context"
	"database/sql"
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
	"github.com/sentiric/sentiric-agent-service/internal/metrics"
	"github.com/sentiric/sentiric-agent-service/internal/queue"
	"github.com/sentiric/sentiric-agent-service/internal/service"
	"github.com/sentiric/sentiric-agent-service/internal/state"
)

// Dependencies, uygulamanın farklı katmanları arasında paylaşılan
// tüm servisleri ve istemcileri bir araya getiren bir yapıdır.
type Dependencies struct {
	CallHandler  *handler.CallHandler
	EventHandler *handler.EventHandler
}

// App, uygulamanın tüm bileşenlerini ve yaşam döngüsünü yönetir.
type App struct {
	Cfg    *config.Config
	Log    zerolog.Logger
	wg     sync.WaitGroup
	cancel context.CancelFunc
}

// NewApp, yeni bir uygulama örneği oluşturur.
func NewApp(cfg *config.Config, log zerolog.Logger) *App {
	return &App{
		Cfg: cfg,
		Log: log,
	}
}

// Run, uygulamanın ana başlatma ve graceful shutdown mantığını içerir.
func (a *App) Run() {
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		db, redisClient, rabbitCh, rabbitCloseChan := a.setupInfrastructure(ctx)
		if ctx.Err() != nil {
			return // Kurulum iptal edildi.
		}
		if db != nil {
			defer db.Close()
		}
		if redisClient != nil {
			defer redisClient.Close()
		}
		if rabbitCh != nil {
			defer rabbitCh.Close()
		}

		deps := a.buildDependencies(db, redisClient, rabbitCh)

		var consumerWg sync.WaitGroup
		go queue.StartConsumer(ctx, rabbitCh, deps.EventHandler.HandleRabbitMQMessage, a.Log, &consumerWg)

		select {
		case <-ctx.Done():
		case err := <-rabbitCloseChan:
			if err != nil {
				a.Log.Error().Err(err).Msg("RabbitMQ bağlantısı koptu, servis durduruluyor.")
			}
			a.cancel()
		}

		a.Log.Info().Msg("RabbitMQ tüketicisinin bitmesi bekleniyor...")
		consumerWg.Wait()
		a.Log.Info().Msg("Aktif diyalogların bitmesi bekleniyor...")
		deps.CallHandler.WaitOnDialogs()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	a.Log.Info().Msg("Kapatma sinyali alındı, servis durduruluyor...")
	a.cancel()

	a.wg.Wait()
	a.Log.Info().Msg("Tüm servisler başarıyla durduruldu. Çıkış yapılıyor.")
}

// buildDependencies, tüm servisleri, yöneticileri ve işleyicileri oluşturup birbirine bağlar.
func (a *App) buildDependencies(db *sql.DB, redisClient *redis.Client, rabbitCh *amqp091.Channel) *Dependencies {
	// Clients
	mediaClient, _ := client.NewMediaServiceClient(a.Cfg)
	userClient, _ := client.NewUserServiceClient(a.Cfg)
	ttsClient, _ := client.NewTTSServiceClient(a.Cfg)
	llmClient := client.NewLlmClient(a.Cfg.LlmServiceURL, a.Log)
	sttClient := client.NewSttClient(a.Cfg.SttServiceURL, a.Log)

	// Infrastructure Wrappers
	stateManager := state.NewManager(redisClient)
	publisher := queue.NewPublisher(rabbitCh, a.Log)

	// Service Layer
	templateProvider := service.NewTemplateProvider(db)
	mediaManager := service.NewMediaManager(db, mediaClient, metrics.EventsFailed)
	aiOrchestrator := service.NewAIOrchestrator(a.Cfg, llmClient, sttClient, ttsClient, mediaClient)
	dialogManager := service.NewDialogManager(a.Cfg, stateManager, aiOrchestrator, mediaManager, templateProvider, publisher)
	userManager := service.NewUserManager(userClient)

	// Handler Layer
	callHandler := handler.NewCallHandler(userManager, dialogManager, stateManager)
	eventHandler := handler.NewEventHandler(a.Log, metrics.EventsProcessed, metrics.EventsFailed, callHandler)

	return &Dependencies{
		CallHandler:  callHandler,
		EventHandler: eventHandler,
	}
}

// setupInfrastructure, altyapı bağlantılarını kurar.
func (a *App) setupInfrastructure(ctx context.Context) (
	db *sql.DB,
	redisClient *redis.Client,
	rabbitCh *amqp091.Channel,
	closeChan <-chan *amqp091.Error,
) {
	var infraWg sync.WaitGroup
	infraWg.Add(3)

	go func() {
		defer infraWg.Done()
		var err error
		for {
			select {
			case <-ctx.Done():
				a.Log.Info().Msg("PostgreSQL bağlantı denemesi context iptaliyle durduruldu.")
				return
			default:
				db, err = database.Connect(ctx, a.Cfg.PostgresURL, a.Log)
				if err == nil {
					return
				}
				if ctx.Err() == nil {
					a.Log.Warn().Err(err).Msg("PostgreSQL'e bağlanılamadı, 5 saniye sonra tekrar denenecek...")
				}

				select {
				case <-time.After(5 * time.Second):
				case <-ctx.Done():
					a.Log.Info().Msg("PostgreSQL bağlantı beklemesi context iptaliyle durduruldu.")
					return
				}
			}
		}
	}()

	go func() {
		defer infraWg.Done()
		for {
			select {
			case <-ctx.Done():
				a.Log.Info().Msg("Redis bağlantı denemesi context iptaliyle durduruldu.")
				return
			default:
				opt, err := redis.ParseURL(a.Cfg.RedisURL)
				if err != nil {
					if ctx.Err() == nil {
						a.Log.Error().Err(err).Msg("redis URL'si parse edilemedi, 5 saniye sonra tekrar denenecek...")
					}
				} else {
					redisClient = redis.NewClient(opt)
					pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					err := redisClient.Ping(pingCtx).Err()
					cancel()
					if err == nil {
						a.Log.Info().Msg("Redis bağlantısı başarılı.")
						return
					}
					if ctx.Err() == nil {
						a.Log.Warn().Err(err).Msg("Redis'e bağlanılamadı, 5 saniye sonra tekrar denenecek...")
					}
				}

				select {
				case <-time.After(5 * time.Second):
				case <-ctx.Done():
					a.Log.Info().Msg("Redis bağlantı beklemesi context iptaliyle durduruldu.")
					return
				}
			}
		}
	}()

	go func() {
		defer infraWg.Done()
		for {
			select {
			case <-ctx.Done():
				a.Log.Info().Msg("RabbitMQ bağlantı denemesi context iptaliyle durduruldu.")
				return
			default:
				var err error
				rabbitCh, closeChan, err = queue.Connect(ctx, a.Cfg.RabbitMQURL, a.Log)
				if err == nil {
					return
				}
				if ctx.Err() == nil {
					a.Log.Warn().Err(err).Msg("RabbitMQ'ya bağlanılamadı, 5 saniye sonra tekrar denenecek...")
				}

				select {
				case <-time.After(5 * time.Second):
				case <-ctx.Done():
					a.Log.Info().Msg("RabbitMQ bağlantı beklemesi context iptaliyle durduruldu.")
					return
				}
			}
		}
	}()

	infraWg.Wait()
	if ctx.Err() != nil {
		a.Log.Info().Msg("Altyapı kurulumu, servis kapatıldığı için iptal edildi.")
		return
	}
	a.Log.Info().Msg("Tüm altyapı bağlantıları başarıyla kuruldu.")
	return
}
