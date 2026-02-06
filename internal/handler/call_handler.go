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

// ProcessManualDial: Web UI'dan gelen manuel dış arama isteğini işler.
func (h *CallHandler) ProcessManualDial(ctx context.Context, req *agentv1.ProcessManualDialRequest) (*agentv1.ProcessManualDialResponse, error) {
	l := h.log.With().Str("dest", req.DestinationNumber).Str("agent", req.UserId).Logger()
	l.Info().Msg("☎️ Manuel dış arama orkestrasyonu tetiklendi.")

	if len(req.DestinationNumber) < 4 {
		l.Error().Msg("❌ Geçersiz hedef numara formatı.")
		return &agentv1.ProcessManualDialResponse{Accepted: false, ErrorMessage: "Geçersiz hedef numara"}, nil
	}

	callID := fmt.Sprintf("out-%s", uuid.New().String())

	// 1. Sinyalleşme Katmanına (B2BUA) Emir Ver
	b2buaReq := &sipv1.InitiateCallRequest{
		CallId:  callID,
		FromUri: fmt.Sprintf("sip:%s@sentiric.cloud", req.UserId),
		ToUri:   fmt.Sprintf("sip:%s@sentiric.cloud", req.DestinationNumber),
	}

	_, err := h.clients.B2BUA.InitiateCall(ctx, b2buaReq)
	if err != nil {
		l.Error().Err(err).Msg("❌ B2BUA servis çağrısı başarısız.")
		return &agentv1.ProcessManualDialResponse{Accepted: false, ErrorMessage: "Sinyalleşme hatası: " + err.Error()}, nil
	}

	// 2. State'i Başlat
	stateErr := h.stateManager.Set(ctx, &state.CallState{
		CallID:       callID,
		TenantID:     req.TenantId,
		CurrentState: constants.StateWelcoming,
		CreatedAt:    time.Now(),
	})

	if stateErr != nil {
		l.Warn().Err(stateErr).Msg("State kaydı oluşturulamadı (Kritik değil)")
	}

	l.Info().Str("call_id", callID).Msg("✅ Dış arama başarıyla kuyruğa alındı.")
	return &agentv1.ProcessManualDialResponse{Accepted: true, CallId: callID}, nil
}

// HandleCallStarted: RabbitMQ'dan gelen olayı işler.
func (h *CallHandler) HandleCallStarted(ctx context.Context, event *eventv1.CallStartedEvent) {
	// DÜZELTME: 'l' artık metodun her aşamasında kullanılıyor.
	l := h.log.With().Str("call_id", event.CallId).Logger()

	// 1. Idempotency Check
	lockKey := fmt.Sprintf("lock:call:%s", event.CallId)
	isNew, err := h.stateManager.RedisClient().SetNX(ctx, lockKey, "1", 10*time.Second).Result()
	if err != nil {
		l.Error().Err(err).Msg("❌ Redis kilit kontrolü başarısız.")
		return
	}
	if !isNew {
		l.Warn().Msg("⚠️ Mükerrer çağrı olayı (idempotency hit), yoksayılıyor.")
		return
	}

	l.Info().Msg("📞 Çağrı başladı. State kaydı oluşturuluyor.")

	// 2. State Kaydı
	s := &state.CallState{
		CallID:         event.CallId,
		TraceID:        event.TraceId,
		TenantID:       event.DialplanResolution.TenantId,
		CurrentState:   constants.StateWelcoming,
		FromURI:        event.FromUri,
		ToURI:          event.ToUri,
		CreatedAt:      time.Now(),
		PipelineActive: true,
	}
	if event.MediaInfo != nil {
		s.ServerRtpPort = event.MediaInfo.ServerRtpPort
		s.CallerRtpAddr = event.MediaInfo.CallerRtpAddr
	}

	if err := h.stateManager.Set(ctx, s); err != nil {
		l.Error().Err(err).Msg("❌ Redis durum kaydı başarısız.")
		// Kritik hata: State yoksa orkestrasyon devam edemez.
		return
	}

	l.Info().Str("trace_id", event.TraceId).Msg("✅ State kaydedildi. TAS Pipeline devri yapılıyor.")

	// 3. TAS Pipeline Devri
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

	stream, err := h.clients.TelephonyAction.RunPipeline(ctx, req)
	if err != nil {
		l.Error().Err(err).Msg("❌ SAGA FAILURE: TAS Pipeline başlatılamadı. Telafi tetikleniyor.")
		h.compensate(ctx, s.CallID, "TAS_START_FAILED")
		return
	}

	// SAGA Monitoring
	go func() {
		l.Debug().Msg("🟢 Pipeline monitoring loop aktif.")
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				l.Info().Msg("🏁 SAGA SUCCESS: TAS Pipeline normal şekilde kapandı.")
				return
			}
			if err != nil {
				l.Error().Err(err).Msg("⚠️ SAGA WARNING: TAS Pipeline stream koptu! Telafi tetikleniyor.")
				h.compensate(context.Background(), s.CallID, "TAS_STREAM_LOST")
				return
			}

			if resp.State == telephonyv1.RunPipelineResponse_STATE_ERROR {
				l.Error().Str("msg", resp.Message).Msg("❌ SAGA FAILURE: TAS İç hatası bildirildi.")
				h.compensate(context.Background(), s.CallID, "TAS_INTERNAL_ERROR")
				return
			}
		}
	}()
}

func (h *CallHandler) compensate(ctx context.Context, callID, reason string) {
	l := h.log.With().Str("call_id", callID).Str("reason", reason).Logger()
	l.Warn().Msg("🔄 SAGA Compensation: call.terminate.request yayınlanıyor.")

	err := h.publisher.PublishJSON(ctx, "call.terminate.request", map[string]string{
		"callId": callID,
		"reason": reason,
	})
	if err != nil {
		l.Error().Err(err).Msg("❌ CRITICAL: Telafi olayı yayınlanamadı!")
	}

	_ = h.stateManager.Delete(ctx, callID)
	l.Info().Msg("🧹 Local state temizlendi.")
}

func (h *CallHandler) HandleCallEnded(ctx context.Context, callID string) {
	h.log.Info().Str("call_id", callID).Msg("📴 Çağrı sonlandı. Kaynaklar temizleniyor.")
	_ = h.stateManager.Delete(ctx, callID)
}
