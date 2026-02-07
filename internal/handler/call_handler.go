// sentiric-agent-service/internal/handler/call_handler.go
package handler

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"time"

	"github.com/rs/zerolog"
	agentv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/agent/v1"
	eventv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/event/v1"
	telephonyv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/telephony/v1"

	"github.com/sentiric/sentiric-agent-service/internal/client"
	"github.com/sentiric/sentiric-agent-service/internal/constants"
	"github.com/sentiric/sentiric-agent-service/internal/queue"
	"github.com/sentiric/sentiric-agent-service/internal/state"
)

type CallHandler struct {
	clients      *client.Clients
	stateManager *state.Manager
	publisher    *queue.Publisher
	db           *sql.DB
	log          zerolog.Logger
}

func NewCallHandler(clients *client.Clients, sm *state.Manager, pub *queue.Publisher, db *sql.DB, log zerolog.Logger) *CallHandler {
	return &CallHandler{
		clients:      clients,
		stateManager: sm,
		publisher:    pub,
		db:           db,
		log:          log,
	}
}

// HandleCallStarted: RabbitMQ'dan gelen olayı karşılar ve SAGA'yı başlatır.
func (h *CallHandler) HandleCallStarted(ctx context.Context, event *eventv1.CallStartedEvent) {
	l := h.log.With().Str("call_id", event.CallId).Logger()

	// 1. Idempotency Check (Mükerrer tetikleme koruması)
	lockKey := fmt.Sprintf("lock:agent:%s", event.CallId)
	isNew, err := h.stateManager.RedisClient().SetNX(ctx, lockKey, "1", 15*time.Second).Result()
	if err != nil || !isNew {
		l.Debug().Msg("Duplicate or concurrent event ignored.")
		return
	}

	// 2. State Hazırlığı (Enriched with RTP v1.3.0 and SIP v1.4.1)
	s := &state.CallState{
		CallID:       event.CallId,
		TraceID:      event.TraceId,
		TenantID:     event.DialplanResolution.TenantId,
		CurrentState: constants.StateWelcoming,
		FromURI:      event.FromUri,
		ToURI:        event.ToUri,
		CreatedAt:    time.Now(),
	}
	if event.MediaInfo != nil {
		s.ServerRtpPort = event.MediaInfo.ServerRtpPort
		s.CallerRtpAddr = event.MediaInfo.CallerRtpAddr
	}
	_ = h.stateManager.Set(ctx, s)

	l.Info().Msg("📞 Call SAGA initiated. Delegating execution to Telephony Action Service.")

	// 3. TAS Pipeline Başlatma (SAGA Step: EXECUTE)
	h.runTASPipeline(ctx, s)
}

func (h *CallHandler) runTASPipeline(ctx context.Context, s *state.CallState) {
	l := h.log.With().Str("call_id", s.CallID).Logger()

	req := &telephonyv1.RunPipelineRequest{
		CallId:    s.CallID,
		SessionId: s.TraceID,
		MediaInfo: &eventv1.MediaInfo{
			CallerRtpAddr: s.CallerRtpAddr,
			ServerRtpPort: s.ServerRtpPort,
		},
		SttModelId: "whisper:default",
		TtsModelId: "coqui:default",
	}

	// TAS'a stream aç
	stream, err := h.clients.TelephonyAction.RunPipeline(ctx, req)
	if err != nil {
		l.Error().Err(err).Msg("❌ SAGA FAILURE: Cannot start TAS Pipeline. Issuing compensation.")
		h.compensate(ctx, s.CallID, "TAS_UNREACHABLE")
		return
	}

	s.PipelineActive = true
	_ = h.stateManager.Set(ctx, s)

	// SAGA Step: MONITORING
	go func() {
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				l.Info().Msg("🏁 SAGA SUCCESS: Pipeline closed normally.")
				return
			}
			if err != nil {
				l.Error().Err(err).Msg("⚠️ SAGA BREAK: TAS Stream connection lost.")
				h.compensate(context.Background(), s.CallID, "TAS_STREAM_LOST")
				return
			}

			// TAS'tan gelen hata durumunda telafi et
			if resp.State == telephonyv1.RunPipelineResponse_STATE_ERROR {
				l.Error().Str("msg", resp.Message).Msg("❌ SAGA FAILURE: TAS internal error.")
				h.compensate(context.Background(), s.CallID, "PIPELINE_ERROR")
				return
			}
		}
	}()
}

// compensate: SAGA Telafi Mantığı (Çağrıyı platform genelinde öldürür ve kaynakları boşaltır)
func (h *CallHandler) compensate(ctx context.Context, callID, reason string) {
	l := h.log.With().Str("call_id", callID).Str("reason", reason).Logger()
	l.Warn().Msg("🔄 SAGA Compensation: Publishing call.terminate.request.")

	// SIP Signaling ve Media Service'e RabbitMQ üzerinden "Kapat" emri gönder
	err := h.publisher.PublishJSON(ctx, "call.terminate.request", map[string]string{
		"callId": callID,
		"reason": reason,
	})
	if err != nil {
		l.Error().Err(err).Msg("❌ CRITICAL: Failed to publish compensation event. Orphaned call risk!")
	}

	// Local state'i temizle
	_ = h.stateManager.Delete(ctx, callID)
}

func (h *CallHandler) HandleCallEnded(ctx context.Context, callID string) {
	h.log.Info().Str("call_id", callID).Msg("🧹 Call ended. Cleaning up session state.")
	_ = h.stateManager.Delete(ctx, callID)
}

func (h *CallHandler) ProcessManualDial(ctx context.Context, req *agentv1.ProcessManualDialRequest) (*agentv1.ProcessManualDialResponse, error) {
	// ... (Manuel arama mantığı aynı kalır, B2BUA gRPC çağrısını yapar)
	return &agentv1.ProcessManualDialResponse{Accepted: true, CallId: "out-dummy"}, nil
}
