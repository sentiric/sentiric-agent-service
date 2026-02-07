// sentiric-agent-service/internal/handler/event_handler.go
package handler

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"

	"github.com/sentiric/sentiric-agent-service/internal/constants"
	"github.com/sentiric/sentiric-agent-service/internal/ctxlogger"
	eventv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/event/v1"
)

type EventHandler struct {
	log             zerolog.Logger
	eventsProcessed *prometheus.CounterVec
	eventsFailed    *prometheus.CounterVec
	callHandler     *CallHandler
}

func NewEventHandler(log zerolog.Logger, processed, failed *prometheus.CounterVec, callHandler *CallHandler) *EventHandler {
	return &EventHandler{
		log:             log,
		eventsProcessed: processed,
		eventsFailed:    failed,
		callHandler:     callHandler,
	}
}

func (h *EventHandler) HandleRabbitMQMessage(body []byte) {
	// 1. Check if it's a CallStartedEvent (v1.15.0)
	var startedEvent eventv1.CallStartedEvent
	if err := proto.Unmarshal(body, &startedEvent); err == nil && startedEvent.EventType == string(constants.EventTypeCallStarted) {
		h.processCallStarted(&startedEvent)
		return
	}

	// 2. Check if it's a CallEndedEvent
	var endedEvent eventv1.CallEndedEvent
	if err := proto.Unmarshal(body, &endedEvent); err == nil && endedEvent.EventType == string(constants.EventTypeCallEnded) {
		h.processCallEnded(&endedEvent)
		return
	}

	h.log.Warn().Msg("⚠️ Unrecognized or malformed event received.")
	h.eventsFailed.WithLabelValues("unknown", "unmarshal_error").Inc()
}

func (h *EventHandler) processCallStarted(event *eventv1.CallStartedEvent) {
	h.eventsProcessed.WithLabelValues(event.EventType).Inc()
	ctx := ctxlogger.ToContext(context.Background(), h.log)
	h.callHandler.HandleCallStarted(ctx, event)
}

func (h *EventHandler) processCallEnded(event *eventv1.CallEndedEvent) {
	h.eventsProcessed.WithLabelValues(event.EventType).Inc()
	h.callHandler.HandleCallEnded(context.Background(), event.CallId)
}
