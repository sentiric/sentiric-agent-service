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
	var protoEvent eventv1.CallStartedEvent

	// Protobuf unmarshal işlemi
	if err := proto.Unmarshal(body, &protoEvent); err == nil {
		// EventType kontrolü
		if protoEvent.EventType == string(constants.EventTypeCallStarted) || protoEvent.EventType == "" {
			h.handleCallStartedProto(&protoEvent)
			return
		}
		// Diğer event tipleri buraya eklenebilir (switch-case)
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

	// 1. Media Info Dönüşümü
	var mediaInfo *state.MediaInfoPayload
	if event.MediaInfo != nil {
		mediaInfo = &state.MediaInfoPayload{
			CallerRtpAddr: event.MediaInfo.CallerRtpAddr,
			ServerRtpPort: float64(event.MediaInfo.ServerRtpPort),
		}
	}

	// 2. Dialplan Dönüşümü (Mapping)
	var dialplan *state.DialplanPayload
	if event.DialplanResolution != nil {
		dialplan = &state.DialplanPayload{
			DialplanID: event.DialplanResolution.DialplanId,
			TenantID:   event.DialplanResolution.TenantId,
		}

		// Action Mapping
		if event.DialplanResolution.Action != nil {
			dialplan.Action = &state.DialplanActionPayload{
				Action: event.DialplanResolution.Action.Action,
			}
			if event.DialplanResolution.Action.ActionData != nil {
				dialplan.Action.ActionData = &state.ActionDataPayload{
					Data: event.DialplanResolution.Action.ActionData.Data,
				}
			}
		}

		// Matched User Mapping
		if event.DialplanResolution.MatchedUser != nil {
			u := event.DialplanResolution.MatchedUser
			dialplan.MatchedUser = &state.MatchedUserPayload{
				ID:       u.Id,
				TenantID: u.TenantId,
				UserType: u.UserType,
			}
			// DÜZELTME: Pointer ataması (Pointer to Pointer engellendi)
			dialplan.MatchedUser.Name = u.Name
			dialplan.MatchedUser.PreferredLanguageCode = u.PreferredLanguageCode
		}
	}

	internalEvent := &state.CallEvent{
		EventType: event.EventType,
		CallID:    event.CallId,
		TraceID:   event.TraceId,
		Media:     mediaInfo,
		From:      event.FromUri,
		Dialplan:  dialplan,
	}

	// Asenkron işleme gönder
	go h.callHandler.HandleCallStarted(ctx, internalEvent)
}