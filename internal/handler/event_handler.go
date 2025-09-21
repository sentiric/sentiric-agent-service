// ========== DOSYA: sentiric-agent-service/internal/handler/event_handler.go (TAM VE NİHAİ İÇERİK) ==========
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

type EventHandler struct {
	log             zerolog.Logger
	eventsProcessed *prometheus.CounterVec
	eventsFailed    *prometheus.CounterVec
	callHandler     *CallHandler
}

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

func (h *EventHandler) HandleRabbitMQMessage(body []byte) {
	var genericEvent struct {
		EventType string `json:"eventType"`
		CallID    string `json:"callId"`
		TraceID   string `json:"traceId"`
	}

	h.log.Debug().Bytes("raw_message", body).Msg("RabbitMQ'dan ham mesaj alındı")

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

	switch constants.EventType(genericEvent.EventType) {
	case constants.EventTypeCallStarted:
		l.Info().Msg("Olay alındı ve işlenmeye başlandı.")
		var event state.CallEvent
		if err := json.Unmarshal(body, &event); err != nil {
			l.Error().Err(err).Msg("call.started olayı parse edilemedi. Gelen veri ile Go struct'ı arasında uyumsuzluk var.")
			h.eventsFailed.WithLabelValues(genericEvent.EventType, "json_unmarshal").Inc()
			return
		}
		go h.callHandler.HandleCallStarted(ctx, &event)

	case constants.EventTypeCallEnded:
		l.Info().Msg("Olay alındı ve işlenmeye başlandı.")
		var event state.CallEvent
		if err := json.Unmarshal(body, &event); err != nil {
			l.Error().Err(err).Msg("call.ended olayı parse edilemedi.")
			h.eventsFailed.WithLabelValues(genericEvent.EventType, "json_unmarshal").Inc()
			return
		}
		go h.callHandler.HandleCallEnded(ctx, &event)

	default:
		// --- DEĞİŞTİRİLDİ ---
		// Olayın ne olduğunu logluyoruz, ancak seviyesini `DEBUG` olarak ayarlıyoruz.
		// Bu sayede normal çalışmada logları kirletmez, ama hata ayıklama gerektiğinde
		// hangi olayların göz ardı edildiğini görebiliriz.
		l.Debug().Msg("Agent-service için tanımlanmamış olay türü, görmezden geliniyor.")
	}
}
