// sentiric-agent-service/internal/app/app.go
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

	"github.com/sentiric/sentiric-agent-service/internal/client"
	"github.com/sentiric/sentiric-agent-service/internal/config"
	"github.com/sentiric/sentiric-agent-service/internal/database"
	"github.com/sentiric/sentiric-agent-service/internal/handler"
	"github.com/sentiric/sentiric-agent-service/internal/metrics"
	"github.com/sentiric/sentiric-agent-service/internal/queue"
	"github.com/sentiric/sentiric-agent-service/internal/state"
	agentv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/agent/v1"
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

	// 1. Altyapı Bağlantıları
	db, rdb, rabbitCh, closeChan, err := a.initInfra(ctx)
	if err != nil {
		a.Log.Fatal().Err(err).Msg("Altyapı başlatılamadı")
	}
	defer db.Close()
	defer rdb.Close()
	defer rabbitCh.Close()

	// 2. Bağımlılık Enjeksiyonu
	clients, err := client.NewClients(a.Cfg)
	if err != nil {
		a.Log.Fatal().Err(err).Msg("gRPC istemcileri başlatılamadı")
	}

	stateMgr := state.NewManager(rdb)
	publisher := queue.NewPublisher(rabbitCh, a.Log)
	callHandler := handler.NewCallHandler(clients, stateMgr, publisher, db, a.Log)
	eventHandler := handler.NewEventHandler(a.Log, metrics.EventsProcessed, metrics.EventsFailed, callHandler)

	// 3. Sunucular
	grpcServer := grpc.NewServer()
	agentv1.RegisterAgentOrchestrationServiceServer(grpcServer, &AgentServer{handler: callHandler})

	go a.startGRPC(grpcServer)
	go metrics.StartServer(a.Cfg.MetricsPort, a.Log)

	// 4. RabbitMQ Worker
	var wg sync.WaitGroup
	go queue.StartConsumer(ctx, rabbitCh, eventHandler.HandleRabbitMQMessage, a.Log, &wg)

	// 5. Shutdown
	a.handleShutdown(cancel, grpcServer, &wg, closeChan)
}

func (a *App) initInfra(ctx context.Context) (*sql.DB, *redis.Client, *amqp091.Channel, <-chan *amqp091.Error, error) {
	db, err := database.Connect(ctx, a.Cfg.PostgresURL, a.Log)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	rdb, err := database.ConnectRedis(ctx, a.Cfg.RedisURL, a.Log)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	ch, closeCh, err := queue.Connect(ctx, a.Cfg.RabbitMQURL, a.Log)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return db, rdb, ch, closeCh, nil
}

func (a *App) startGRPC(srv *grpc.Server) {
	lis, err := net.Listen("tcp", ":12031")
	if err != nil {
		a.Log.Fatal().Err(err).Msg("gRPC dinleme hatası")
	}
	a.Log.Info().Msg("🚀 gRPC Sunucusu (Orchestration) dinleniyor: 12031")
	if err := srv.Serve(lis); err != nil {
		a.Log.Fatal().Err(err).Msg("gRPC sunucu hatası")
	}
}

func (a *App) handleShutdown(cancel context.CancelFunc, srv *grpc.Server, wg *sync.WaitGroup, closeChan <-chan *amqp091.Error) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sig:
		a.Log.Info().Msg("Kapatma sinyali alındı.")
	case err := <-closeChan:
		a.Log.Error().Err(err).Msg("RabbitMQ bağlantısı koptu.")
	}

	cancel()
	srv.GracefulStop()
	wg.Wait()
	a.Log.Info().Msg("Servis başarıyla durduruldu.")
}

type AgentServer struct {
	agentv1.UnimplementedAgentOrchestrationServiceServer
	handler *handler.CallHandler
}

func (s *AgentServer) ProcessManualDial(ctx context.Context, req *agentv1.ProcessManualDialRequest) (*agentv1.ProcessManualDialResponse, error) {
	return s.handler.ProcessManualDial(ctx, req)
}
