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
	// 1. CallStartedEvent
	var startedEvent eventv1.CallStartedEvent
	if err := proto.Unmarshal(body, &startedEvent); err == nil && startedEvent.EventType == string(constants.EventTypeCallStarted) {
		h.processCallStarted(&startedEvent)
		return
	}

	// 2. CallEndedEvent
	var endedEvent eventv1.CallEndedEvent
	if err := proto.Unmarshal(body, &endedEvent); err == nil && endedEvent.EventType == string(constants.EventTypeCallEnded) {
		h.processCallEnded(&endedEvent)
		return
	}

	// 3. Media Olayları (Zararsızca Yoksay)
	// [YENİ]: GenericEvent (Protobuf) kontrolü. Workflow'dan gelen "call.terminate.request" gibi olayları güvenle yut.
	var genericEvent eventv1.GenericEvent
	if err := proto.Unmarshal(body, &genericEvent); err == nil && genericEvent.EventType != "" {
		if genericEvent.EventType == "call.recording.available" ||
			genericEvent.EventType == "call.media.playback.finished" ||
			genericEvent.EventType == "call.terminate.request" { //[ARCH-COMPLIANCE] Hata basmadan yut
			h.eventsProcessed.WithLabelValues(genericEvent.EventType).Inc()
			return
		}
		h.eventsProcessed.WithLabelValues(genericEvent.EventType).Inc()
		h.log.Debug().Str("type", genericEvent.EventType).Msg("GenericEvent alındı (No-op).")
		return
	}

	// [YENİ]: Native struct kontrolü (Panopticon log kirliliğini önler)
	var recEvent eventv1.CallRecordingAvailableEvent
	if err := proto.Unmarshal(body, &recEvent); err == nil && recEvent.EventType == "call.recording.available" {
		h.eventsProcessed.WithLabelValues(recEvent.EventType).Inc()
		return
	}

	// YENİ: Playback bitiş eventini yut
	var playEvent eventv1.GenericEvent // Playback finished genelde Generic olarak atılır
	if err := proto.Unmarshal(body, &playEvent); err == nil && playEvent.EventType == "call.media.playback.finished" {
		return
	}

	// Buraya gelirse gerçekten bozuk bir eventtir.
	// Ancak log level'ı DEBUG yapıyoruz, ERROR veya WARN olmasın ki SRE dashboard'u kirletmesin.
	h.log.Debug().Msg("Unrecognized event structure received in Agent.")
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
