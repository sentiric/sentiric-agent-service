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
	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
	eventv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/event/v1"
	telephonyv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/telephony/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"

	"github.com/sentiric/sentiric-agent-service/internal/client" // Config import edilmeli
	"github.com/sentiric/sentiric-agent-service/internal/constants"
	"github.com/sentiric/sentiric-agent-service/internal/database"
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

// HandleCallStarted: RabbitMQ'dan gelen olayı karşılar ve SAGA akışını dallandırır.
func (h *CallHandler) HandleCallStarted(ctx context.Context, event *eventv1.CallStartedEvent) {
	l := h.log.With().Str("call_id", event.CallId).Logger()

	// 1. Idempotency Check
	lockKey := fmt.Sprintf("lock:agent:%s", event.CallId)
	isNew, err := h.stateManager.RedisClient().SetNX(ctx, lockKey, "1", 15*time.Second).Result()
	if err != nil || !isNew {
		l.Debug().Msg("Duplicate event ignored.")
		return
	}

	// 2. Dialplan Karar Analizi
	res := event.GetDialplanResolution()
	if res == nil || res.Action == nil {
		l.Error().Msg("❌ CRITICAL: Event received without dialplan resolution!")
		return
	}

	// [YENİ] Veritabanında Konuşma Kaydı Oluştur
	if err := database.CreateConversation(h.db, event.CallId, res.TenantId, "voice"); err != nil {
		l.Warn().Err(err).Msg("Konuşma kaydı veritabanına yazılamadı (Logic devam ediyor)")
	}

	actionType := res.Action.Type
	l.Info().Interface("action_type", actionType).Msg("🧠 Analyzing Dialplan Decision")

	// 3. State Hazırlığı
	s := &state.CallState{
		CallID:       event.CallId,
		TraceID:      event.TraceId,
		TenantID:     res.TenantId,
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

	// 4. İş Akışı Dallanması (Workflow Branching)
	switch actionType {
	case dialplanv1.ActionType_ACTION_TYPE_START_AI_CONVERSATION:
		l.Info().Msg("🤖 Starting AI Pipeline Execution.")
		h.runTASPipeline(ctx, s, res.Action.ActionData)

	case dialplanv1.ActionType_ACTION_TYPE_BRIDGE_CALL:
		l.Info().Msg("📞 Action: BRIDGE_CALL. Handed over to SIP Signaling.")
		s.CurrentState = "BRIDGED"
		_ = h.stateManager.Set(ctx, s)
		// BridgeCall mantığı B2BUA veya Proxy tarafından yönetilir, Agent burada izleyici kalabilir.

	case dialplanv1.ActionType_ACTION_TYPE_ECHO_TEST:
		l.Info().Msg("🔊 Action: ECHO_TEST. Agent in standby mode.")

	// [YENİ] Kuyruk / Ajan Yönlendirme Mantığı
	case dialplanv1.ActionType_ACTION_TYPE_ENQUEUE_CALL:
		l.Info().Msg("👥 Action: ENQUEUE_CALL. Checking agent availability...")
		h.handleEnqueueCall(ctx, s, res.Action.ActionData)

	default:
		l.Warn().Interface("type", actionType).Msg("⚠️ Unhandled action type received.")
	}
}

// handleEnqueueCall: Çağrıyı bir insana aktarmadan önce durum kontrolü yapar.
func (h *CallHandler) handleEnqueueCall(ctx context.Context, s *state.CallState, actionData map[string]string) {
	l := h.log.With().Str("call_id", s.CallID).Logger()

	// Hedef ajan belirtilmiş mi? (Smart Routing)
	targetAgentID, hasTarget := actionData["target_agent_id"]

	if hasTarget {
		// User Service'e sor: Bu ajan uygun mu?
		profile, err := h.clients.User.GetAgentProfile(ctx, &userv1.GetAgentProfileRequest{UserId: targetAgentID})

		if err == nil && profile.Profile.Status == "ONLINE" {
			l.Info().Str("agent_id", targetAgentID).Msg("✅ Hedef ajan ONLINE. Transfer başlatılıyor.")

			// B2BUA'ya transfer emri ver (Bridge)
			// Not: B2BUA'nın BridgeCall RPC'si kullanılmalı.
			// Şimdilik stub olarak logluyoruz.
			// _, err := h.clients.B2BUA.TransferCall(...)

			// Transfer başarılı varsayalım ve state güncelle
			s.CurrentState = "TRANSFERRED"
			_ = h.stateManager.Set(ctx, s)
			return
		} else {
			l.Warn().Str("agent_id", targetAgentID).Msg("⛔ Hedef ajan OFFLINE veya meşgul. Fallback uygulanıyor.")
		}
	}

	// Eğer direkt ajan yoksa veya meşgulse, Bekletme Müziği veya Anons çal.
	// Burada TAS'ı kullanarak "Bütün temsilcilerimiz meşgul" anonsu çaldırabiliriz.
	l.Info().Msg("🎵 Kuyruk müziği başlatılıyor (Mock).")
	// h.runTASPipeline(ctx, s, map[string]string{"mode": "play_only", "file": "queue_music.wav"})
}

func (h *CallHandler) runTASPipeline(ctx context.Context, s *state.CallState, actionData map[string]string) {
	l := h.log.With().Str("call_id", s.CallID).Logger()

	voiceID := "coqui:default"
	if v, ok := actionData["voice_id"]; ok {
		voiceID = v
	}

	req := &telephonyv1.RunPipelineRequest{
		CallId:    s.CallID,
		SessionId: s.TraceID,
		MediaInfo: &eventv1.MediaInfo{
			CallerRtpAddr: s.CallerRtpAddr,
			ServerRtpPort: s.ServerRtpPort,
		},
		SttModelId: "whisper:default",
		TtsModelId: voiceID,
	}

	stream, err := h.clients.TelephonyAction.RunPipeline(ctx, req)
	if err != nil {
		l.Error().Err(err).Msg("❌ SAGA FAILURE: Cannot start TAS Pipeline.")
		h.compensate(ctx, s.CallID, "TAS_UNREACHABLE")
		return
	}

	s.PipelineActive = true
	_ = h.stateManager.Set(ctx, s)

	go func() {
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				l.Info().Msg("🏁 SAGA SUCCESS: Pipeline finished.")
				return
			}
			if err != nil {
				l.Error().Err(err).Msg("⚠️ SAGA BREAK: TAS Stream connection lost.")
				// h.compensate çağrısı burada loop yaratabilir, dikkatli olunmalı.
				return
			}

			if resp.State == telephonyv1.RunPipelineResponse_STATE_ERROR {
				l.Error().Str("msg", resp.Message).Msg("❌ SAGA FAILURE: TAS internal error.")
				h.compensate(context.Background(), s.CallID, "PIPELINE_ERROR")
				return
			}
		}
	}()
}

func (h *CallHandler) compensate(ctx context.Context, callID, reason string) {
	l := h.log.With().Str("call_id", callID).Str("reason", reason).Logger()
	l.Warn().Msg("🔄 SAGA Compensation: Publishing call.terminate.request.")

	err := h.publisher.PublishJSON(ctx, "call.terminate.request", map[string]interface{}{
		"callId":    callID,
		"reason":    reason,
		"timestamp": time.Now().Format(time.RFC3339),
		"code":      "SAGA_FAILURE_COMPENSATION",
	})

	if err != nil {
		l.Error().Err(err).Msg("❌ CRITICAL: Failed to publish compensation event.")
	}
	_ = h.stateManager.Delete(ctx, callID)
}

func (h *CallHandler) HandleCallEnded(ctx context.Context, callID string) {
	h.log.Info().Str("call_id", callID).Msg("🧹 Call ended. Session cleanup.")

	// [YENİ] Veritabanında durumu güncelle
	if err := database.UpdateConversationStatus(h.db, callID, "COMPLETED"); err != nil {
		h.log.Warn().Err(err).Msg("Konuşma durumu güncellenemedi")
	}

	_ = h.stateManager.Delete(ctx, callID)
}

func (h *CallHandler) ProcessManualDial(ctx context.Context, req *agentv1.ProcessManualDialRequest) (*agentv1.ProcessManualDialResponse, error) {
	// B2BUA üzerinden dış arama başlatma
	// Burada B2BUA.InitiateCall RPC'si çağrılmalı.
	// Şimdilik stub olarak bırakıyoruz.
	return &agentv1.ProcessManualDialResponse{Accepted: true, CallId: "out-dummy"}, nil
}
