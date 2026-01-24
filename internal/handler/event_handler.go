package handler

import (
	"context"
	
	"google.golang.org/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	
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
	// [MASTER PLAN]: Protobuf Decode
	var protoEvent eventv1.CallStartedEvent
	
	// constants.EventTypeCallStarted string değerini kullanıyoruz
	if err := proto.Unmarshal(body, &protoEvent); err == nil && protoEvent.EventType == string(constants.EventTypeCallStarted) {
		h.handleCallStartedProto(&protoEvent)
		return
	}
	
	h.log.Warn().Msg("Protobuf decode edilemedi veya bilinmeyen olay tipi. Eski JSON formatı olabilir.")
	h.eventsFailed.WithLabelValues("unknown", "proto_unmarshal").Inc()
}

func (h *EventHandler) handleCallStartedProto(event *eventv1.CallStartedEvent) {
	l := h.log.With().
		Str("call_id", event.CallId).
		Str("trace_id", event.TraceId).
		Str("event_type", event.EventType).
		Logger()
	
	h.eventsProcessed.WithLabelValues(event.EventType).Inc()
	l.Info().Msg("🚀 PROTOBUF 'call.started' olayı başarıyla alındı.")

	ctx := ctxlogger.ToContext(context.Background(), l)
	
	var mediaInfo *state.MediaInfoPayload
	if event.MediaInfo != nil {
		mediaInfo = &state.MediaInfoPayload{
			CallerRtpAddr: event.MediaInfo.CallerRtpAddr,
			ServerRtpPort: float64(event.MediaInfo.ServerRtpPort),
		}
	}

	internalEvent := &state.CallEvent{
		EventType: event.EventType,
		CallID:    event.CallId,
		TraceID:   event.TraceId,
		Media:     mediaInfo,
		From:      event.FromUri,
		Dialplan:  nil, 
	}

	go h.callHandler.HandleCallStarted(ctx, internalEvent)
}