// File: internal/handler/event_handler.go
package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/sentiric/sentiric-agent-service/internal/client"
	"github.com/sentiric/sentiric-agent-service/internal/config"
	"github.com/sentiric/sentiric-agent-service/internal/dialog"
	"github.com/sentiric/sentiric-agent-service/internal/state"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"

	mediav1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/media/v1"
	ttsv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/tts/v1"

	"google.golang.org/grpc/metadata"
)

var activeCallContexts = struct {
	sync.RWMutex
	m map[string]context.CancelFunc
}{m: make(map[string]context.CancelFunc)}

type EventHandler struct {
	stateManager    *state.Manager
	dialogDeps      *dialog.Dependencies
	log             zerolog.Logger
	eventsProcessed *prometheus.CounterVec
	eventsFailed    *prometheus.CounterVec
	userClient      userv1.UserServiceClient
}

func NewEventHandler(
	db *sql.DB,
	cfg *config.Config,
	sm *state.Manager,
	mc mediav1.MediaServiceClient,
	uc userv1.UserServiceClient,
	tc ttsv1.TextToSpeechServiceClient,
	llmC *client.LlmClient,
	sttC *client.SttClient,
	log zerolog.Logger,
	processed, failed *prometheus.CounterVec,
	sttSampleRate uint32,
) *EventHandler {
	dialogDeps := &dialog.Dependencies{
		DB:                  db,
		Config:              cfg,
		MediaClient:         mc,
		TTSClient:           tc,
		LLMClient:           llmC,
		STTClient:           sttC,
		Log:                 log,
		SttTargetSampleRate: sttSampleRate,
		EventsFailed:        failed,
	}
	return &EventHandler{
		stateManager:    sm,
		dialogDeps:      dialogDeps,
		log:             log,
		eventsProcessed: processed,
		eventsFailed:    failed,
		userClient:      uc,
	}
}

func (h *EventHandler) HandleRabbitMQMessage(body []byte) {
	var event state.CallEvent
	if err := json.Unmarshal(body, &event); err != nil {
		h.log.Error().Err(err).Bytes("raw_message", body).Msg("Hata: Mesaj JSON formatında değil")
		h.eventsFailed.WithLabelValues("unknown", "json_unmarshal").Inc()
		return
	}
	h.eventsProcessed.WithLabelValues(event.EventType).Inc()
	l := h.log.With().Str("call_id", event.CallID).Str("trace_id", event.TraceID).Str("event_type", event.EventType).Logger()
	l.Info().Msg("Olay alındı")

	switch event.EventType {
	case "call.started":
		go h.handleCallStarted(l, &event)
	case "call.ended":
		go h.handleCallEnded(l, &event)
	}
}

func (h *EventHandler) handleCallStarted(l zerolog.Logger, event *state.CallEvent) {
	if event.Dialplan == nil || event.Dialplan.Action == nil {
		l.Error().Msg("Dialplan veya action bilgisi eksik, çağrı işlenemiyor.")
		h.eventsFailed.WithLabelValues(event.EventType, "missing_dialplan_action").Inc()
		return
	}

	actionName := event.Dialplan.Action.Action
	l = l.With().Str("action", actionName).Logger()
	l.Info().Msg("Dialplan eylemine göre çağrı yönlendiriliyor.")

	ctx, cancel := context.WithCancel(context.Background())
	activeCallContexts.Lock()
	if _, exists := activeCallContexts.m[event.CallID]; exists {
		l.Warn().Msg("Bu çağrı için zaten aktif bir context var, yeni goroutine başlatılmıyor.")
		activeCallContexts.Unlock()
		cancel()
		return
	}
	activeCallContexts.m[event.CallID] = cancel
	activeCallContexts.Unlock()
	defer func() {
		activeCallContexts.Lock()
		delete(activeCallContexts.m, event.CallID)
		activeCallContexts.Unlock()
		cancel()
		l.Info().Msg("Çağrı context'i temizlendi.")
	}()

	switch actionName {
	case "PROCESS_GUEST_CALL":
		h.handleProcessGuestCall(ctx, l, event)
	case "START_AI_CONVERSATION":
		h.handleStartAIConversation(ctx, l, event)
	default:
		l.Error().Msg("Bilinmeyen veya desteklenmeyen dialplan eylemi.")
		h.eventsFailed.WithLabelValues(event.EventType, "unsupported_action").Inc()
	}
}

func (h *EventHandler) handleProcessGuestCall(ctx context.Context, l zerolog.Logger, event *state.CallEvent) {
	l.Info().Msg("Misafir kullanıcı oluşturma akışı başlatıldı.")

	callerNumber := event.From
	if strings.Contains(callerNumber, "<") {
		parts := strings.Split(callerNumber, "<")
		if len(parts) > 1 {
			uriPart := strings.Split(parts[1], "@")[0]
			uriPart = strings.TrimPrefix(uriPart, "sip:")
			callerNumber = uriPart
		}
	}
	l.Info().Str("caller_number", callerNumber).Msg("Arayan numara parse edildi.")

	tenantID := "default"
	if event.Dialplan.GetInboundRoute() != nil {
		tenantID = event.Dialplan.GetInboundRoute().TenantId
	}

	createCtx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(ctx, "x-trace-id", event.TraceID), 10*time.Second)
	defer cancel()

	// DÜZELTME: CreateUserRequest yapısı, `user.proto` v1.8.3 ile tam uyumlu hale getirildi.
	createUserReq := &userv1.CreateUserRequest{
		TenantId: tenantID,
		UserType: "caller",
		InitialContact: &userv1.CreateUserRequest_InitialContact{ // Doğru alan adı ve iç içe tip
			ContactType:  "phone",      // Doğru alan adı
			ContactValue: callerNumber, // Doğru alan adı
		},
	}

	createdUser, err := h.userClient.CreateUser(createCtx, createUserReq)
	if err != nil {
		l.Error().Err(err).Msg("User-service'de misafir kullanıcı oluşturulamadı.")
		h.eventsFailed.WithLabelValues(event.EventType, "guest_user_creation_failed").Inc()
		return
	}

	l.Info().Str("user_id", createdUser.User.Id).Msg("Misafir kullanıcı başarıyla oluşturuldu.")

	event.Dialplan.MatchedUser = createdUser.User

	h.handleStartAIConversation(ctx, l, event)
}

func (h *EventHandler) handleStartAIConversation(ctx context.Context, l zerolog.Logger, event *state.CallEvent) {
	st, err := h.stateManager.Get(ctx, event.CallID)
	if err != nil {
		if ctx.Err() == context.Canceled {
			l.Info().Msg("handleStartAIConversation context iptal edildiği için durduruldu.")
			return
		}
		l.Error().Err(err).Msg("Redis'ten durum alınamadı.")
		return
	}
	if st != nil {
		l.Warn().Msg("Bu çağrı için zaten aktif bir Redis durumu var, yeni bir tane başlatılmıyor.")
		return
	}

	tenantID := "default"
	if event.Dialplan != nil {
		if event.Dialplan.TenantId != "" {
			tenantID = event.Dialplan.TenantId
		} else if event.Dialplan.InboundRoute != nil {
			tenantID = event.Dialplan.InboundRoute.TenantId
		}
	}

	initialState := &state.CallState{
		CallID: event.CallID, TraceID: event.TraceID, TenantID: tenantID,
		CurrentState: state.StateWelcoming, Event: event, Conversation: []map[string]string{},
	}

	if err := h.stateManager.Set(ctx, initialState); err != nil {
		if ctx.Err() == context.Canceled {
			l.Info().Msg("Başlangıç durumu yazılırken context iptal edildi.")
			return
		}
		l.Error().Err(err).Msg("Redis'e başlangıç durumu yazılamadı.")
		return
	}

	dialog.RunDialogLoop(ctx, h.dialogDeps, h.stateManager, initialState)
}

func (h *EventHandler) handleCallEnded(l zerolog.Logger, event *state.CallEvent) {
	activeCallContexts.Lock()
	if cancel, ok := activeCallContexts.m[event.CallID]; ok {
		l.Info().Msg("Aktif diyalog döngüsü için iptal sinyali gönderiliyor.")
		cancel()
	}
	activeCallContexts.Unlock()

	st, err := h.stateManager.Get(context.Background(), event.CallID)
	if err != nil || st == nil {
		l.Warn().Err(err).Msg("Sonlanan çağrı için aktif bir durum bulunamadı.")
		return
	}
	st.CurrentState = state.StateEnded
	if err := h.stateManager.Set(context.Background(), st); err != nil {
		l.Error().Err(err).Msg("Redis'e 'Ended' durumu yazılamadı.")
	}
}
