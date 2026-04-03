// [ARCH-COMPLIANCE] Context timeout wrapper implemented on runTASPipeline
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
	"google.golang.org/grpc/metadata"

	"github.com/sentiric/sentiric-agent-service/internal/client"
	"github.com/sentiric/sentiric-agent-service/internal/constants"
	"github.com/sentiric/sentiric-agent-service/internal/database"
	"github.com/sentiric/sentiric-agent-service/internal/queue"
	"github.com/sentiric/sentiric-agent-service/internal/state"
)

type CallHandler struct {
	clients      *client.Clients
	stateManager *state.Manager
	publisher    *queue.RabbitMQ // BURASI DEĞİŞTİ
	db           *sql.DB
	log          zerolog.Logger
}

func NewCallHandler(clients *client.Clients, sm *state.Manager, pub *queue.RabbitMQ, db *sql.DB, log zerolog.Logger) *CallHandler {
	return &CallHandler{
		clients:      clients,
		stateManager: sm,
		publisher:    pub,
		db:           db,
		log:          log,
	}
}

func (h *CallHandler) GetStateManager() *state.Manager {
	return h.stateManager
}

func (h *CallHandler) RunTASPipelineWithPlan(ctx context.Context, s *state.CallState, actionData map[string]string) {
	h.runTASPipeline(ctx, s, actionData)
}

func (h *CallHandler) HandleCallStarted(ctx context.Context, event *eventv1.CallStartedEvent) {
	l := h.log.With().Str("call_id", event.CallId).Logger()

	lockKey := fmt.Sprintf("lock:agent:%s", event.CallId)
	isNew, err := h.stateManager.RedisClient().SetNX(ctx, lockKey, "1", 15*time.Second).Result()
	if err != nil || !isNew {
		l.Debug().Str("event", "DUPLICATE_EVENT_IGNORED").Msg("Duplicate event ignored.")
		return
	}

	res := event.GetDialplanResolution()
	if res == nil || res.Action == nil {
		l.Error().Str("event", "MISSING_DIALPLAN_RESOLUTION").Msg("❌ CRITICAL: Event received without dialplan resolution!")
		return
	}

	if err := database.CreateConversation(h.db, event.CallId, res.TenantId, "voice"); err != nil {
		l.Warn().Str("event", "DB_CONVERSATION_CREATE_FAILED").Err(err).Msg("Konuşma kaydı veritabanına yazılamadı (Logic devam ediyor)")
	}

	actionType := res.Action.Type
	l.Info().Str("event", "DIALPLAN_DECISION").Interface("action_type", actionType).Msg("🧠 Analyzing Dialplan Decision")

	lang := "tr"
	if res.InboundRoute != nil && res.InboundRoute.DefaultLanguageCode != "" {
		lang = res.InboundRoute.DefaultLanguageCode
	}

	s := &state.CallState{
		CallID:       event.CallId,
		TraceID:      event.TraceId,
		TenantID:     res.TenantId,
		LanguageCode: lang,
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

	switch actionType {
	case dialplanv1.ActionType_ACTION_TYPE_START_AI_CONVERSATION:
		l.Info().Str("event", "AI_CALL_DETECTED").Msg("🤖 AI Çağrısı Algılandı. Workflow devri bekleniyor...")
	case dialplanv1.ActionType_ACTION_TYPE_PLAY_STATIC_ANNOUNCEMENT:
		l.Info().Str("event", "ACTION_PLAY_STATIC").Msg("📢 Action: PLAY_STATIC_ANNOUNCEMENT. Agent görevi yok, izlemede.")
		return
	case dialplanv1.ActionType_ACTION_TYPE_BRIDGE_CALL:
		l.Info().Str("event", "ACTION_BRIDGE_CALL").Msg("📞 Action: BRIDGE_CALL. Handed over to SIP Signaling.")
		s.CurrentState = "BRIDGED"
		_ = h.stateManager.Set(ctx, s)
	case dialplanv1.ActionType_ACTION_TYPE_ECHO_TEST:
		l.Info().Str("event", "ACTION_ECHO_TEST").Msg("🔊 Action: ECHO_TEST. Agent in standby mode.")
	case dialplanv1.ActionType_ACTION_TYPE_ENQUEUE_CALL:
		l.Info().Str("event", "ACTION_ENQUEUE_CALL").Msg("👥 Action: ENQUEUE_CALL. Checking agent availability...")
		h.handleEnqueueCall(ctx, s, res.Action.ActionData)
	default:
		l.Warn().Str("event", "UNHANDLED_ACTION").Interface("type", actionType).Msg("⚠️ Unhandled action type received.")
	}
}

func (h *CallHandler) handleEnqueueCall(ctx context.Context, s *state.CallState, actionData map[string]string) {
	l := h.log.With().Str("call_id", s.CallID).Logger()
	targetAgentID, hasTarget := actionData["target_agent_id"]

	if hasTarget {
		reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		profile, err := h.clients.User.GetAgentProfile(reqCtx, &userv1.GetAgentProfileRequest{UserId: targetAgentID})
		if err == nil && profile.Profile.Status == "ONLINE" {
			l.Info().Str("event", "AGENT_ONLINE").Str("agent_id", targetAgentID).Msg("✅ Hedef ajan ONLINE. Transfer başlatılıyor.")
			s.CurrentState = "TRANSFERRED"
			_ = h.stateManager.Set(ctx, s)
			return
		} else {
			l.Warn().Str("event", "AGENT_OFFLINE").Str("agent_id", targetAgentID).Msg("⛔ Hedef ajan OFFLINE veya meşgul. Fallback uygulanıyor.")
		}
	}
	l.Info().Str("event", "QUEUE_MUSIC_STARTED").Msg("🎵 Kuyruk müziği başlatılıyor (Mock).")
}

func (h *CallHandler) runTASPipeline(grpcCtx context.Context, s *state.CallState, actionData map[string]string) {
	l := h.log.With().Str("call_id", s.CallID).Logger()

	voiceID := "coqui:default"
	if v, ok := actionData["voice_id"]; ok {
		voiceID = v
	}

	recordSession := false
	if r, ok := actionData["record"]; ok && r == "true" {
		recordSession = true
	}

	req := &telephonyv1.RunPipelineRequest{
		CallId:    s.CallID,
		SessionId: s.TraceID,
		MediaInfo: &eventv1.MediaInfo{
			CallerRtpAddr: s.CallerRtpAddr,
			ServerRtpPort: s.ServerRtpPort,
		},
		SttModelId:     "whisper:default",
		TtsModelId:     voiceID,
		RecordSession:  recordSession,
		LanguageCode:   s.LanguageCode,
		SystemPromptId: actionData["system_prompt_id"],
	}

	pipelineCtx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
	if s.TraceID != "" {
		pipelineCtx = metadata.AppendToOutgoingContext(pipelineCtx, "x-trace-id", s.TraceID)
	}

	// --- EKLENEN KRİTİK DÜZELTME ---
	// [ARCH-COMPLIANCE] Strict Tenant Isolation kuralı gereği STT/TTS gateway'lerine
	// giden isteklerde tenant_id bulunmak ZORUNDADIR.
	if s.TenantID != "" {
		pipelineCtx = metadata.AppendToOutgoingContext(pipelineCtx, "x-tenant-id", s.TenantID)
	}
	// -------------------------------

	stream, err := h.clients.TelephonyAction.RunPipeline(pipelineCtx, req)
	if err != nil {
		cancel()
		l.Error().Str("event", "TAS_PIPELINE_START_FAIL").Err(err).Msg("❌ SAGA FAILURE: Cannot start TAS Pipeline.")
		h.compensate(context.Background(), s.CallID, "TAS_UNREACHABLE")
		return
	}

	s.PipelineActive = true
	_ = h.stateManager.Set(context.Background(), s)
	l.Info().Str("event", "TAS_PIPELINE_ACTIVE").Msg("▶️ TAS Pipeline Active")

	go func() {
		defer cancel()
		for {
			resp, err := stream.Recv()

			if err == io.EOF {
				l.Info().Str("event", "TAS_PIPELINE_EOF").Msg("🏁 SAGA SUCCESS: Pipeline finished naturally.")
				h.compensate(context.Background(), s.CallID, "NORMAL_CLEARING")
				return
			}
			if err != nil {
				l.Error().Str("event", "TAS_PIPELINE_BROKEN").Err(err).Msg("⚠️ SAGA BREAK: TAS Stream connection lost.")
				h.compensate(context.Background(), s.CallID, "PIPELINE_BROKEN")
				return
			}

			if resp.State == telephonyv1.RunPipelineResponse_STATE_ERROR {
				l.Error().Str("event", "TAS_INTERNAL_ERROR").Str("msg", resp.Message).Msg("❌ SAGA FAILURE: TAS internal error.")
				h.compensate(context.Background(), s.CallID, "PIPELINE_ERROR")
				return
			}
		}
	}()
}

func (h *CallHandler) compensate(ctx context.Context, callID, reason string) {
	l := h.log.With().Str("call_id", callID).Str("reason", reason).Logger()
	l.Warn().Str("event", "SAGA_COMPENSATION").Msg("🔄 SAGA Compensation: Publishing call.terminate.request.")

	err := h.publisher.PublishJSON(ctx, "call.terminate.request", map[string]interface{}{
		"callId":    callID,
		"reason":    reason,
		"timestamp": time.Now().Format(time.RFC3339),
		"code":      "SAGA_FAILURE_COMPENSATION",
	})

	if err != nil {
		l.Error().Str("event", "COMPENSATION_PUBLISH_FAIL").Err(err).Msg("❌ CRITICAL: Failed to publish compensation event.")
	}
	_ = h.stateManager.Delete(ctx, callID)
}

func (h *CallHandler) HandleCallEnded(ctx context.Context, callID string) {
	h.log.Info().Str("event", "CALL_ENDED").Str("call_id", callID).Msg("🧹 Call ended. Session cleanup.")
	if err := database.UpdateConversationStatus(h.db, callID, "COMPLETED"); err != nil {
		h.log.Warn().Str("event", "DB_UPDATE_FAIL").Err(err).Msg("Konuşma durumu güncellenemedi")
	}
	_ = h.stateManager.Delete(ctx, callID)
}

func (h *CallHandler) ProcessManualDial(ctx context.Context, req *agentv1.ProcessManualDialRequest) (*agentv1.ProcessManualDialResponse, error) {
	return &agentv1.ProcessManualDialResponse{Accepted: true, CallId: "out-dummy"}, nil
}
