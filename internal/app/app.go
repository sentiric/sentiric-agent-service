package app

import (
	"context"
	"database/sql"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/go-redis/redis/v8"
	"github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"

	agentv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/agent/v1"

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
	if db != nil {
		defer db.Close()
	}
	if redisClient != nil {
		defer redisClient.Close()
	}
	if rabbitCh != nil {
		defer rabbitCh.Close()
	}

	// 3. gRPC İstemcileri
	clients, err := client.NewClients(a.Cfg)
	if err != nil {
		a.Log.Fatal().Err(err).Msg("gRPC istemcileri başlatılamadı")
	}

	// 4. Handler ve State Manager
	stateManager := state.NewManager(redisClient)
	callHandler := handler.NewCallHandler(clients, stateManager, db, a.Log)

	// 5. gRPC Sunucusu (Orchestration Server)
	grpcServer := grpc.NewServer()
	// Wrapper ile sunucuya kayıt
	agentv1.RegisterAgentOrchestrationServiceServer(grpcServer, &AgentGrpcServerWrapper{handler: callHandler})

	// gRPC Dinleme (Port 12031)
	go func() {
		lis, err := net.Listen("tcp", ":12031")
		if err != nil {
			a.Log.Fatal().Err(err).Msg("gRPC portu dinlenemedi")
		}
		a.Log.Info().Msg("gRPC sunucusu (Orchestration) dinleniyor: 12031")
		if err := grpcServer.Serve(lis); err != nil {
			a.Log.Fatal().Err(err).Msg("gRPC sunucusu çöktü")
		}
	}()

	// 6. RabbitMQ Event Handler
	eventHandler := handler.NewEventHandler(
		a.Log,
		metrics.EventsProcessed,
		metrics.EventsFailed,
		callHandler,
	)

	var consumerWg sync.WaitGroup
	go queue.StartConsumer(ctx, rabbitCh, eventHandler.HandleRabbitMQMessage, a.Log, &consumerWg)

	// 7. Graceful Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		a.Log.Info().Msg("Kapatma sinyali alındı...")
	case err := <-closeChan:
		a.Log.Error().Err(err).Msg("RabbitMQ bağlantısı koptu!")
	}

	grpcServer.GracefulStop()
	cancel()
	consumerWg.Wait()
	a.Log.Info().Msg("Servis durduruldu.")
}

// Wrapper for gRPC Interface Compliance
type AgentGrpcServerWrapper struct {
	agentv1.UnimplementedAgentOrchestrationServiceServer
	handler *handler.CallHandler
}

// ProcessManualDial: Stream Gateway'den gelen çağrıyı handler'a iletir
func (s *AgentGrpcServerWrapper) ProcessManualDial(ctx context.Context, req *agentv1.ProcessManualDialRequest) (*agentv1.ProcessManualDialResponse, error) {
	return s.handler.ProcessManualDial(ctx, req)
}

// Dummy methods (Unimplemented override)
func (s *AgentGrpcServerWrapper) ProcessCallStart(ctx context.Context, req *agentv1.ProcessCallStartRequest) (*agentv1.ProcessCallStartResponse, error) {
	return &agentv1.ProcessCallStartResponse{Initiated: true}, nil
}

func (s *AgentGrpcServerWrapper) ProcessSagaStep(ctx context.Context, req *agentv1.ProcessSagaStepRequest) (*agentv1.ProcessSagaStepResponse, error) {
	return &agentv1.ProcessSagaStepResponse{Completed: true}, nil
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
