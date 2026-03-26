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
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/sentiric/sentiric-agent-service/internal/client"
	"github.com/sentiric/sentiric-agent-service/internal/config"
	"github.com/sentiric/sentiric-agent-service/internal/database"
	"github.com/sentiric/sentiric-agent-service/internal/handler"
	"github.com/sentiric/sentiric-agent-service/internal/metrics"
	"github.com/sentiric/sentiric-agent-service/internal/queue"
	"github.com/sentiric/sentiric-agent-service/internal/server"
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

	db, rdb, rabbitConn, closeChan, err := a.initInfra(ctx)
	if err != nil {
		a.Log.Fatal().Str("event", "INFRA_INIT_FAILED").Err(err).Msg("Altyapı başlatılamadı")
	}
	defer db.Close()
	defer rdb.Close()
	defer rabbitConn.Close()

	clients, err := client.NewClients(a.Cfg, a.Log)
	if err != nil {
		a.Log.Fatal().Str("event", "CLIENTS_INIT_FAILED").Err(err).Msg("İstemciler başlatılamadı")
	}

	stateMgr := state.NewManager(rdb)
	publisher := queue.NewPublisher(rabbitConn, a.Log)
	callHandler := handler.NewCallHandler(clients, stateMgr, publisher, db, a.Log)
	eventHandler := handler.NewEventHandler(a.Log, metrics.EventsProcessed, metrics.EventsFailed, callHandler)

	grpcServer := server.NewGrpcServer(a.Cfg, a.Log)
	agentv1.RegisterAgentOrchestrationServiceServer(grpcServer, &AgentServer{handler: callHandler})

	go func() {
		a.Log.Info().Str("event", "GRPC_SERVER_START").Msg("🚀 gRPC Server (Orchestration) active: 12031")
		if err := server.Start(grpcServer, "12031"); err != nil && err.Error() != "http: Server closed" {
			a.Log.Fatal().Str("event", "GRPC_SERVER_FAILED").Err(err).Msg("gRPC serve failed")
		}
	}()

	go metrics.StartServer(a.Cfg.MetricsPort, a.Log)

	var wg sync.WaitGroup
	go queue.StartConsumer(ctx, rabbitConn, eventHandler.HandleRabbitMQMessage, a.Log, &wg)

	a.handleShutdown(cancel, grpcServer, &wg, closeChan)
}

func (a *App) initInfra(ctx context.Context) (*sql.DB, *redis.Client, *amqp091.Connection, <-chan *amqp091.Error, error) {
	db, err := database.Connect(ctx, a.Cfg.PostgresURL, a.Log)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	rdb, err := database.ConnectRedis(ctx, a.Cfg.RedisURL, a.Log)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	conn, closeCh, err := queue.Connect(ctx, a.Cfg.RabbitMQURL, a.Log)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return db, rdb, conn, closeCh, nil
}

func (a *App) handleShutdown(cancel context.CancelFunc, srv *grpc.Server, wg *sync.WaitGroup, closeChan <-chan *amqp091.Error) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sig:
		a.Log.Info().Str("event", "SHUTDOWN_SIGNAL").Msg("Shutdown signal received.")
	case err := <-closeChan:
		a.Log.Error().Str("event", "RABBITMQ_CONN_LOST").Err(err).Msg("RabbitMQ connection lost.")
	}

	cancel()
	server.Stop(srv)
	wg.Wait()
	a.Log.Info().Str("event", "SERVICE_STOPPED").Msg("Agent Service stopped.")
}

type AgentServer struct {
	agentv1.UnimplementedAgentOrchestrationServiceServer
	handler *handler.CallHandler
}

func (s *AgentServer) ProcessCallStart(ctx context.Context, req *agentv1.ProcessCallStartRequest) (*agentv1.ProcessCallStartResponse, error) {
	stateMgr := s.handler.GetStateManager()
	callState, err := stateMgr.Get(ctx, req.CallId)
	if err != nil || callState == nil {
		return nil, status.Errorf(codes.NotFound, "Call state not found in Redis.")
	}

	s.handler.RunTASPipelineWithPlan(ctx, callState, map[string]string{
		"dialplan_id": req.DialplanId,
	})

	return &agentv1.ProcessCallStartResponse{Initiated: true}, nil
}

func (s *AgentServer) ProcessSagaStep(ctx context.Context, req *agentv1.ProcessSagaStepRequest) (*agentv1.ProcessSagaStepResponse, error) {
	return &agentv1.ProcessSagaStepResponse{Completed: true}, nil
}

func (s *AgentServer) GetConversationTranscript(ctx context.Context, req *agentv1.GetConversationTranscriptRequest) (*agentv1.GetConversationTranscriptResponse, error) {
	return &agentv1.GetConversationTranscriptResponse{}, nil
}

func (s *AgentServer) ProcessManualDial(ctx context.Context, req *agentv1.ProcessManualDialRequest) (*agentv1.ProcessManualDialResponse, error) {
	return s.handler.ProcessManualDial(ctx, req)
}
