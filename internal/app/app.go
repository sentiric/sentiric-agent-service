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
	"github.com/sentiric/sentiric-agent-service/internal/service"
	"github.com/sentiric/sentiric-agent-service/internal/state"
)

type Dependencies struct {
	CallHandler  *handler.CallHandler
	EventHandler *handler.EventHandler
}

type App struct {
	Cfg    *config.Config
	Log    zerolog.Logger
	wg     sync.WaitGroup
	cancel context.CancelFunc
}

func NewApp(cfg *config.Config, log zerolog.Logger) *App {
	return &App{
		Cfg: cfg,
		Log: log,
	}
}

func (a *App) Run() {
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel

	go metrics.StartServer(a.Cfg.MetricsPort, a.Log)

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		db, redisClient, rabbitCh, rabbitCloseChan := a.setupInfrastructure(ctx)
		if ctx.Err() != nil {
			return
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

func (a *App) buildDependencies(db *sql.DB, redisClient *redis.Client, rabbitCh *amqp091.Channel) *Dependencies {
	mediaClient, _ := client.NewMediaServiceClient(a.Cfg)
	userClient, _ := client.NewUserServiceClient(a.Cfg)
	ttsClient, _ := client.NewTTSServiceClient(a.Cfg)
	llmClient := client.NewLlmClient(a.Cfg.LlmServiceURL, a.Log)
	sttClient := client.NewSttClient(a.Cfg.SttServiceURL, a.Log)

	var knowledgeClient service.KnowledgeClientInterface
	if a.Cfg.KnowledgeServiceURL != "" {
		a.Log.Info().Str("url", a.Cfg.KnowledgeServiceURL).Msg("HTTP Knowledge Service istemcisi kullanılıyor.")
		knowledgeClient = client.NewKnowledgeClient(a.Cfg.KnowledgeServiceURL, a.Log)
	} else if a.Cfg.KnowledgeServiceGrpcURL != "" {
		a.Log.Info().Str("url", a.Cfg.KnowledgeServiceGrpcURL).Msg("gRPC Knowledge Service istemcisi kullanılıyor.")
		grpcClient, err := client.NewKnowledgeServiceClient(a.Cfg)
		if err != nil {
			a.Log.Error().Err(err).Msg("gRPC Knowledge Service istemcisi oluşturulamadı. RAG devre dışı kalacak.")
		} else {
			knowledgeClient = client.NewGrpcKnowledgeClientAdapter(grpcClient)
		}
	} else {
		a.Log.Warn().Msg("Knowledge service için ne gRPC ne de HTTP URL'si tanımlanmamış. RAG devre dışı.")
	}

	stateManager := state.NewManager(redisClient)
	publisher := queue.NewPublisher(rabbitCh, a.Log)
	templateProvider := service.NewTemplateProvider(db)
	mediaManager := service.NewMediaManager(db, mediaClient, metrics.EventsFailed, a.Cfg.BucketName)
	aiOrchestrator := service.NewAIOrchestrator(a.Cfg, llmClient, sttClient, ttsClient, mediaClient, knowledgeClient)
	dialogManager := service.NewDialogManager(a.Cfg, stateManager, aiOrchestrator, mediaManager, templateProvider, publisher)
	userManager := service.NewUserManager(userClient)
	callHandler := handler.NewCallHandler(userManager, dialogManager, stateManager)
	eventHandler := handler.NewEventHandler(a.Log, metrics.EventsProcessed, metrics.EventsFailed, callHandler)

	return &Dependencies{
		CallHandler:  callHandler,
		EventHandler: eventHandler,
	}
}

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
		db, err = database.Connect(ctx, a.Cfg.PostgresURL, a.Log)
		if err != nil && ctx.Err() == nil {
			a.Log.Error().Err(err).Msg("Veritabanı bağlantı denemeleri başarısız oldu, servis sonlandırılıyor.")
		}
	}()

	go func() {
		defer infraWg.Done()
		var err error
		redisClient, err = database.ConnectRedis(ctx, a.Cfg.RedisURL, a.Log)
		if err != nil && ctx.Err() == nil {
			a.Log.Error().Err(err).Msg("Redis bağlantı denemeleri başarısız oldu, servis sonlandırılıyor.")
		}
	}()

	go func() {
		defer infraWg.Done()
		var err error
		rabbitCh, closeChan, err = queue.Connect(ctx, a.Cfg.RabbitMQURL, a.Log)
		if err != nil && ctx.Err() == nil {
			a.Log.Error().Err(err).Msg("RabbitMQ bağlantı denemeleri başarısız oldu, servis sonlandırılıyor.")
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
