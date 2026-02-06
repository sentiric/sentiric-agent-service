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
	// 1. Unmarshal CallStartedEvent (Protobuf)
	var event eventv1.CallStartedEvent
	if err := proto.Unmarshal(body, &event); err == nil {
		if event.EventType == string(constants.EventTypeCallStarted) {
			h.processCallStarted(&event)
			return
		}
	}

	// 2. Unmarshal CallEndedEvent (Protobuf)
	var endedEvent eventv1.CallEndedEvent
	if err := proto.Unmarshal(body, &endedEvent); err == nil {
		if endedEvent.EventType == string(constants.EventTypeCallEnded) {
			h.processCallEnded(&endedEvent)
			return
		}
	}

	h.eventsFailed.WithLabelValues("unknown", "unmarshal_error").Inc()
}

func (h *EventHandler) processCallStarted(event *eventv1.CallStartedEvent) {
	h.eventsProcessed.WithLabelValues(event.EventType).Inc()
	ctx := ctxlogger.ToContext(context.Background(), h.log)
	h.callHandler.HandleCallStarted(ctx, event)
}

func (h *EventHandler) processCallEnded(event *eventv1.CallEndedEvent) {
	h.eventsProcessed.WithLabelValues(event.EventType).Inc()
	ctx := context.Background()
	h.callHandler.HandleCallEnded(ctx, event.CallId)
}
