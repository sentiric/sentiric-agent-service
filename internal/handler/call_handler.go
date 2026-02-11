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

	// 2. Dialplan Karar Analizi (v1.15.0 Standartları)
	res := event.GetDialplanResolution()
	if res == nil || res.Action == nil {
		l.Error().Msg("❌ CRITICAL: Event received without dialplan resolution!")
		return
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
		// Agent bu aşamada state'i 'Established' yapıp izlemeye geçer (veya pasif kalır)
		s.CurrentState = "BRIDGED"
		_ = h.stateManager.Set(ctx, s)

	case dialplanv1.ActionType_ACTION_TYPE_ECHO_TEST:
		l.Info().Msg("🔊 Action: ECHO_TEST. Agent in standby mode.")

	default:
		l.Warn().Interface("type", actionType).Msg("⚠️ Unhandled action type received.")
	}
}

func (h *CallHandler) runTASPipeline(ctx context.Context, s *state.CallState, actionData map[string]string) {
	l := h.log.With().Str("call_id", s.CallID).Logger()

	// [v1.15.0 FIX]: ActionData artık doğrudan bir map.
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

	// Monitor Loop
	go func() {
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				l.Info().Msg("🏁 SAGA SUCCESS: Pipeline finished.")
				return
			}
			if err != nil {
				l.Error().Err(err).Msg("⚠️ SAGA BREAK: TAS Stream connection lost.")
				h.compensate(context.Background(), s.CallID, "TAS_STREAM_LOST")
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
	// YENİ: TraceID'yi context'ten çekerek logla
	l := h.log.With().Str("call_id", callID).Str("reason", reason).Logger()

	l.Warn().Msg("🔄 SAGA Compensation: Publishing call.terminate.request.")

	// RabbitMQ'ya hata bilgisini yapılandırılmış olarak gönder
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
	_ = h.stateManager.Delete(ctx, callID)
}

func (h *CallHandler) ProcessManualDial(ctx context.Context, req *agentv1.ProcessManualDialRequest) (*agentv1.ProcessManualDialResponse, error) {
	// B2BUA üzerinden dış arama tetikleme mantığı
	return &agentv1.ProcessManualDialResponse{Accepted: true, CallId: "out-dummy"}, nil
}
