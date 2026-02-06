// sentiric-agent-service/internal/handler/event_handler.go
package handler

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"

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
	// 1. Protobuf Unmarshal
	var protoEvent eventv1.CallStartedEvent
	if err := proto.Unmarshal(body, &protoEvent); err == nil {
		if protoEvent.EventType == string(constants.EventTypeCallStarted) {
			h.handleCallStartedProto(&protoEvent)
			return
		}
	}

	// 2. Hata Durumu
	h.log.Warn().Msg("Mesaj işlenemedi: Geçersiz format veya bilinmeyen event type.")
	h.eventsFailed.WithLabelValues("unknown", "proto_unmarshal_fail").Inc()
}

func (h *EventHandler) handleCallStartedProto(event *eventv1.CallStartedEvent) {
	l := h.log.With().
		Str("call_id", event.CallId).
		Str("trace_id", event.TraceId).
		Logger()

	h.eventsProcessed.WithLabelValues(event.EventType).Inc()
	l.Info().Msg("🚀 [EVENT] 'call.started' alındı (Protobuf).")

	ctx := ctxlogger.ToContext(context.Background(), l)

	// Gelen veriyi orkestrasyon state'ine dönüştür
	internalEvent := &state.CallState{
		CallID:       event.CallId,
		TraceID:      event.TraceId,
		FromURI:      event.FromUri,
		ToURI:        event.ToUri,
		CurrentState: constants.StateWelcoming,
	}

	if event.DialplanResolution != nil {
		internalEvent.TenantID = event.DialplanResolution.TenantId
	}

	if event.MediaInfo != nil {
		internalEvent.CallerRtpAddr = event.MediaInfo.CallerRtpAddr
		internalEvent.ServerRtpPort = event.MediaInfo.ServerRtpPort
	}

	// Async işleme gönder
	go h.callHandler.HandleCallStarted(ctx, event)
}
