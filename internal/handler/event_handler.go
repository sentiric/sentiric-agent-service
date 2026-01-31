package handler

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"

	// Contracts Import
	eventv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/event/v1"

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
	// 1. Protobuf Olarak Dene (Standart)
	var protoEvent eventv1.CallStartedEvent
	if err := proto.Unmarshal(body, &protoEvent); err == nil {
		// EventType doluysa ve 'call.started' ise işle
		if protoEvent.EventType == string(constants.EventTypeCallStarted) {
			h.handleCallStartedProto(&protoEvent)
			return
		}
		// Diğer event tipleri (CallEnded vb.) buraya switch-case ile eklenebilir.
	}

	// 2. Başarısız Olursa Logla
	h.log.Warn().Msg("Mesaj işlenemedi. Geçersiz format (Protobuf bekleniyor) veya bilinmeyen event type.")
	h.eventsFailed.WithLabelValues("unknown", "proto_unmarshal_fail").Inc()
}

func (h *EventHandler) handleCallStartedProto(event *eventv1.CallStartedEvent) {
	l := h.log.With().
		Str("call_id", event.CallId).
		Str("trace_id", event.TraceId).
		Str("event_type", event.EventType).
		Logger()

	h.eventsProcessed.WithLabelValues(event.EventType).Inc()
	l.Info().Msg("🚀 [EVENT] 'call.started' alındı (Protobuf).")

	ctx := ctxlogger.ToContext(context.Background(), l)

	// Veri Dönüşümü (Contract -> Internal State)
	var mediaInfo *state.MediaInfoPayload
	if event.MediaInfo != nil {
		mediaInfo = &state.MediaInfoPayload{
			CallerRtpAddr: event.MediaInfo.CallerRtpAddr,
			ServerRtpPort: float64(event.MediaInfo.ServerRtpPort),
		}
	}

	// Dialplan verisi B2BUA eventinden gelmiyor olabilir, Agent servisi
	// gerekirse Dialplan servisini tekrar sorgulayabilir.
	// Şimdilik temel mapping ile devam ediyoruz.

	internalEvent := &state.CallEvent{
		EventType: event.EventType,
		CallID:    event.CallId,
		TraceID:   event.TraceId,
		Media:     mediaInfo,
		From:      event.FromUri,
		// Dialplan verisi event içinde varsa maple, yoksa nil.
		// B2BUA zenginleştirme yapana kadar bu alan boş gelebilir.
	}

	go h.callHandler.HandleCallStarted(ctx, internalEvent)
}
