// sentiric-agent-service/internal/handler/call_handler.go
package handler

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	agentv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/agent/v1"
	eventv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/event/v1"
	sipv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/sip/v1"
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

func (h *CallHandler) ProcessManualDial(ctx context.Context, req *agentv1.ProcessManualDialRequest) (*agentv1.ProcessManualDialResponse, error) {
	l := h.log.With().Str("dest", req.DestinationNumber).Str("agent", req.UserId).Logger()

	callID := fmt.Sprintf("out-%s", uuid.New().String())

	b2buaReq := &sipv1.InitiateCallRequest{
		CallId:  callID,
		FromUri: fmt.Sprintf("sip:%s@sentiric.cloud", req.UserId),
		ToUri:   fmt.Sprintf("sip:%s@sentiric.cloud", req.DestinationNumber),
	}

	_, err := h.clients.B2BUA.InitiateCall(ctx, b2buaReq)
	if err != nil {
		l.Error().Err(err).Msg("❌ B2BUA InitiateCall hatası")
		return &agentv1.ProcessManualDialResponse{Accepted: false, ErrorMessage: err.Error()}, nil
	}

	h.stateManager.Set(ctx, &state.CallState{
		CallID:       callID,
		TenantID:     req.TenantId,
		CurrentState: constants.StateWelcoming,
		CreatedAt:    time.Now(),
	})

	return &agentv1.ProcessManualDialResponse{Accepted: true, CallId: callID}, nil
}

func (h *CallHandler) HandleCallStarted(ctx context.Context, event *eventv1.CallStartedEvent) {
	l := h.log.With().Str("call_id", event.CallId).Logger()

	// 1. Double Trigger Koruması (Idempotency)
	lockKey := fmt.Sprintf("lock:call:%s", event.CallId)
	isNew, err := h.stateManager.RedisClient().SetNX(ctx, lockKey, "1", 10*time.Second).Result()
	if err != nil || !isNew {
		return
	}

	l.Info().Msg("📞 Çağrı orkestrasyonu başlıyor.")

	// 2. State Kaydı
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
	h.stateManager.Set(ctx, s)

	// 3. TAS Pipeline Devri
	h.delegateToTAS(ctx, s)
}

func (h *CallHandler) delegateToTAS(ctx context.Context, s *state.CallState) {
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

	stream, err := h.clients.TelephonyAction.RunPipeline(ctx, req)
	if err != nil {
		l.Error().Err(err).Msg("❌ TAS Pipeline başlatılamadı. Telafi işlemi tetikleniyor.")
		h.compensateFailedCall(ctx, s.CallID, "TAS_START_FAILED")
		return
	}

	// Stream Monitoring
	go func() {
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				l.Info().Msg("🏁 TAS Pipeline normal sonlandı.")
				return
			}
			if err != nil {
				l.Error().Err(err).Msg("⚠️ TAS Stream koptu! Kaynaklar temizleniyor.")
				h.compensateFailedCall(context.Background(), s.CallID, "TAS_STREAM_LOST")
				return
			}

			if resp.State == telephonyv1.RunPipelineResponse_STATE_ERROR {
				l.Error().Str("msg", resp.Message).Msg("🔴 TAS Pipeline Hatası!")
				h.compensateFailedCall(context.Background(), s.CallID, "TAS_INTERNAL_ERROR")
				return
			}
		}
	}()
}

// compensateFailedCall: SAGA Telafi işlemi. Çağrıyı tüm platformda sonlandırır.
func (h *CallHandler) compensateFailedCall(ctx context.Context, callID, reason string) {
	l := h.log.With().Str("call_id", callID).Str("reason", reason).Logger()
	l.Warn().Msg("🔄 SAGA Telafisi: call.terminate.request yayınlanıyor.")

	err := h.publisher.PublishJSON(ctx, string(constants.EventTypeCallTerminateRequest), map[string]string{
		"callId": callID,
		"reason": reason,
	})
	if err != nil {
		l.Error().Err(err).Msg("❌ Telafi olayı yayınlanamadı! KRİTİK VERİ TUTARSIZLIĞI.")
	}

	h.stateManager.Delete(ctx, callID)
}

func (h *CallHandler) HandleCallEnded(ctx context.Context, callID string) {
	h.log.Info().Str("call_id", callID).Msg("📴 Çağrı sonlandı. State temizleniyor.")
	h.stateManager.Delete(ctx, callID)
}
