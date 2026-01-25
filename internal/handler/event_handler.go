package handler

import (
	"context"
	
	"google.golang.org/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	
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
	// [YENİ] Sadece Protobuf decode denenir. JSON desteği kaldırıldı.
	var protoEvent eventv1.CallStartedEvent
	
	// Protobuf unmarshal işlemi
	if err := proto.Unmarshal(body, &protoEvent); err == nil {
        // EventType kontrolü (Opsiyonel, B2BUA doğru doldurmalı)
		if protoEvent.EventType == string(constants.EventTypeCallStarted) || protoEvent.EventType == "" {
		    h.handleCallStartedProto(&protoEvent)
		    return
        }
	}
	
	h.log.Warn().Msg("Mesaj işlenemedi. Protobuf decode hatası veya bilinmeyen format.")
	h.eventsFailed.WithLabelValues("unknown", "proto_unmarshal").Inc()
}

func (h *EventHandler) handleCallStartedProto(event *eventv1.CallStartedEvent) {
	l := h.log.With().
		Str("call_id", event.CallId).
		Str("trace_id", event.TraceId).
		Str("event_type", event.EventType).
		Logger()
	
	h.eventsProcessed.WithLabelValues(event.EventType).Inc()
	l.Info().Msg("🚀 PROTOBUF 'call.started' olayı alındı ve işleniyor.")

	ctx := ctxlogger.ToContext(context.Background(), l)
	
	// Protobuf -> Internal State dönüşümü
	var mediaInfo *state.MediaInfoPayload
	if event.MediaInfo != nil {
		mediaInfo = &state.MediaInfoPayload{
			CallerRtpAddr: event.MediaInfo.CallerRtpAddr,
			ServerRtpPort: float64(event.MediaInfo.ServerRtpPort),
		}
	}
    
    // Dialplan dönüşümü (Basitleştirilmiş)
    // Şimdilik dialplan bilgisini event'ten tam almıyor olabiliriz,
    // Agent ileride kendi DB'sinden veya event'in "dialplan_resolution" alanından okuyacak.
    // Şimdilik nil geçiyoruz, CallHandler bunu "varsayılan akış" olarak ele alacak.
    // (Gelişmiş implementasyon sonraki adımda yapılabilir)

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