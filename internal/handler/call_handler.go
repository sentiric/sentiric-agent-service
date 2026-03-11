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
		l.Debug().Msg("Duplicate event ignored.")
		return
	}

	res := event.GetDialplanResolution()
	if res == nil || res.Action == nil {
		l.Error().Msg("❌ CRITICAL: Event received without dialplan resolution!")
		return
	}

	if err := database.CreateConversation(h.db, event.CallId, res.TenantId, "voice"); err != nil {
		l.Warn().Err(err).Msg("Konuşma kaydı veritabanına yazılamadı (Logic devam ediyor)")
	}

	actionType := res.Action.Type
	l.Info().Interface("action_type", actionType).Msg("🧠 Analyzing Dialplan Decision")

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

	switch actionType {

	case dialplanv1.ActionType_ACTION_TYPE_START_AI_CONVERSATION:
		l.Info().Msg("🤖 AI Çağrısı Algılandı. Workflow devri bekleniyor...")
		// [MİMARİ DÜZELTME]: Hardcoded 3 saniyelik sleep fallback iptal edildi.
		// Artık Workflow servisine %100 güveniyoruz.

	// [YENİ EKLENECEK KISIM]
	case dialplanv1.ActionType_ACTION_TYPE_PLAY_STATIC_ANNOUNCEMENT:
		l.Info().Msg("📢 Action: PLAY_STATIC_ANNOUNCEMENT. Agent görevi yok, izlemede.")
		// Hata basmadan çıkıyoruz, çünkü bu bir AI çağrısı değil.
		return

	case dialplanv1.ActionType_ACTION_TYPE_BRIDGE_CALL:
		l.Info().Msg("📞 Action: BRIDGE_CALL. Handed over to SIP Signaling.")
		s.CurrentState = "BRIDGED"
		_ = h.stateManager.Set(ctx, s)

	case dialplanv1.ActionType_ACTION_TYPE_ECHO_TEST:
		l.Info().Msg("🔊 Action: ECHO_TEST. Agent in standby mode.")

	case dialplanv1.ActionType_ACTION_TYPE_ENQUEUE_CALL:
		l.Info().Msg("👥 Action: ENQUEUE_CALL. Checking agent availability...")
		h.handleEnqueueCall(ctx, s, res.Action.ActionData)

	default:
		l.Warn().Interface("type", actionType).Msg("⚠️ Unhandled action type received.")
	}
}

func (h *CallHandler) handleEnqueueCall(ctx context.Context, s *state.CallState, actionData map[string]string) {
	l := h.log.With().Str("call_id", s.CallID).Logger()
	targetAgentID, hasTarget := actionData["target_agent_id"]

	if hasTarget {
		profile, err := h.clients.User.GetAgentProfile(ctx, &userv1.GetAgentProfileRequest{UserId: targetAgentID})
		if err == nil && profile.Profile.Status == "ONLINE" {
			l.Info().Str("agent_id", targetAgentID).Msg("✅ Hedef ajan ONLINE. Transfer başlatılıyor.")
			s.CurrentState = "TRANSFERRED"
			_ = h.stateManager.Set(ctx, s)
			return
		} else {
			l.Warn().Str("agent_id", targetAgentID).Msg("⛔ Hedef ajan OFFLINE veya meşgul. Fallback uygulanıyor.")
		}
	}
	l.Info().Msg("🎵 Kuyruk müziği başlatılıyor (Mock).")
}

func (h *CallHandler) runTASPipeline(grpcCtx context.Context, s *state.CallState, actionData map[string]string) {
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

	// =====================================================================
	// [CRITICAL FIX]: BACKGROUND CONTEXT ISOLATION
	// Gelen grpcCtx kısa ömürlüdür (Workflow gRPC çağrısı bittiğinde iptal olur).
	// TAS Pipeline ise dakikalarca sürebilir. Eğer grpcCtx'i paslarsak,
	// Workflow işini bitirdiği an TAS stream'i "context canceled" hatasıyla çöker.
	// Çözüm: Yepyeni bir Background context oluşturup TraceID'yi içine aşılıyoruz.
	// =====================================================================
	pipelineCtx := context.Background()
	if s.TraceID != "" {
		pipelineCtx = metadata.AppendToOutgoingContext(pipelineCtx, "x-trace-id", s.TraceID)
	}

	stream, err := h.clients.TelephonyAction.RunPipeline(pipelineCtx, req)
	if err != nil {
		l.Error().Err(err).Msg("❌ SAGA FAILURE: Cannot start TAS Pipeline.")
		h.compensate(context.Background(), s.CallID, "TAS_UNREACHABLE")
		return
	}

	s.PipelineActive = true
	_ = h.stateManager.Set(context.Background(), s)
	l.Info().Msg("▶️ TAS Pipeline Active")

	go func() {
		for {
			resp, err := stream.Recv()

			// [CRITICAL FIX]: GÜVENLİ KAPATMA MEKANİZMASI
			// Eğer stream bittiyse (EOF) veya hata verip koptuysa (Failsafe vs)
			// sistemi asılı bırakmamak için B2BUA'ya "Kapat" komutu gönderiyoruz.
			if err == io.EOF {
				l.Info().Msg("🏁 SAGA SUCCESS: Pipeline finished naturally.")
				h.compensate(context.Background(), s.CallID, "NORMAL_CLEARING")
				return
			}
			if err != nil {
				l.Error().Err(err).Msg("⚠️ SAGA BREAK: TAS Stream connection lost.")
				// BURASI EKSİKTİ: Stream koptuğunda B2BUA'ya kapat diyoruz!
				h.compensate(context.Background(), s.CallID, "PIPELINE_BROKEN")
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
	if err := database.UpdateConversationStatus(h.db, callID, "COMPLETED"); err != nil {
		h.log.Warn().Err(err).Msg("Konuşma durumu güncellenemedi")
	}
	_ = h.stateManager.Delete(ctx, callID)
}

func (h *CallHandler) ProcessManualDial(ctx context.Context, req *agentv1.ProcessManualDialRequest) (*agentv1.ProcessManualDialResponse, error) {
	return &agentv1.ProcessManualDialResponse{Accepted: true, CallId: "out-dummy"}, nil
}
