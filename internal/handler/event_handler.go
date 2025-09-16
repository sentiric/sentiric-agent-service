// internal/handler/event_handler.go

package handler

import (
	"context"
	"encoding/json"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/sentiric/sentiric-agent-service/internal/constants"
	"github.com/sentiric/sentiric-agent-service/internal/ctxlogger"
	"github.com/sentiric/sentiric-agent-service/internal/state"
)

// EventHandler, RabbitMQ'dan gelen mesajları deşifre eder ve ilgili işleyiciye (handler) yönlendirir.
type EventHandler struct {
	log             zerolog.Logger
	eventsProcessed *prometheus.CounterVec
	eventsFailed    *prometheus.CounterVec
	callHandler     *CallHandler
}

// NewEventHandler, yeni bir EventHandler örneği oluşturur.
func NewEventHandler(
	log zerolog.Logger,
	processed, failed *prometheus.CounterVec,
	callHandler *CallHandler,
) *EventHandler {
	return &EventHandler{
		log:             log,
		eventsProcessed: processed,
		eventsFailed:    failed,
		callHandler:     callHandler,
	}
}

// HandleRabbitMQMessage, gelen bir byte dizisini işler.
func (h *EventHandler) HandleRabbitMQMessage(body []byte) {
	var genericEvent struct {
		EventType string `json:"eventType"`
		CallID    string `json:"callId"`
		TraceID   string `json:"traceId"`
	}

	if err := json.Unmarshal(body, &genericEvent); err != nil {
		h.log.Error().Err(err).Bytes("raw_message", body).Msg("Hata: Mesaj JSON formatında değil")
		h.eventsFailed.WithLabelValues("unknown", "json_unmarshal").Inc()
		return
	}

	h.eventsProcessed.WithLabelValues(genericEvent.EventType).Inc()
	l := h.log.With().
		Str("call_id", genericEvent.CallID).
		Str("trace_id", genericEvent.TraceID).
		Str("event_type", genericEvent.EventType).
		Logger()

	ctx := ctxlogger.ToContext(context.Background(), l)

	l.Info().Msg("Olay alındı")

	switch constants.EventType(genericEvent.EventType) {
	case constants.EventTypeCallStarted:
		var event state.CallEvent
		if err := json.Unmarshal(body, &event); err == nil {
			go h.callHandler.HandleCallStarted(ctx, &event)
		} else {
			l.Error().Err(err).Msg("call.started olayı parse edilemedi.")
		}
	case constants.EventTypeCallEnded:
		var event state.CallEvent
		if err := json.Unmarshal(body, &event); err == nil {
			go h.callHandler.HandleCallEnded(ctx, &event)
		} else {
			l.Error().Err(err).Msg("call.ended olayı parse edilemedi.")
		}
	default:
		l.Warn().Msg("Bilinmeyen olay türü, görmezden geliniyor.")
	}
}
