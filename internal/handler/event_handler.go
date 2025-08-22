// File: internal/handler/event_handler.go
package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/sentiric/sentiric-agent-service/internal/client"
	"github.com/sentiric/sentiric-agent-service/internal/dialog"
	"github.com/sentiric/sentiric-agent-service/internal/state"
	mediav1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/media/v1"
	ttsv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/tts/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
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
}

func NewEventHandler(
	db *sql.DB,
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
		MediaClient:         mc,
		TTSClient:           tc,
		LLMClient:           llmC,
		STTClient:           sttC,
		Log:                 log,
		SttTargetSampleRate: sttSampleRate,
	}
	return &EventHandler{
		stateManager:    sm,
		dialogDeps:      dialogDeps,
		log:             log,
		eventsProcessed: processed,
		eventsFailed:    failed,
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

	st, err := h.stateManager.Get(ctx, event.CallID)
	if err != nil {
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
		l.Error().Err(err).Msg("Redis'e başlangıç durumu yazılamadı.")
		return
	}

	// Sadece diyalog döngüsünü başlat
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
