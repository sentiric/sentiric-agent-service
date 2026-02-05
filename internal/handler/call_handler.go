// sentiric-agent-service/internal/handler/call_handler.go
package handler

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	agentv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/agent/v1"
	eventv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/event/v1"
	sipv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/sip/v1"
	telephonyv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/telephony/v1"

	"github.com/sentiric/sentiric-agent-service/internal/client"
	"github.com/sentiric/sentiric-agent-service/internal/constants"
	"github.com/sentiric/sentiric-agent-service/internal/state"
)

type CallHandler struct {
	clients      *client.Clients
	stateManager *state.Manager
	db           *sql.DB // DB'yi şimdilik tutuyoruz, gelecekte prompt'lar için gerekebilir.
	log          zerolog.Logger
}

func NewCallHandler(clients *client.Clients, sm *state.Manager, db *sql.DB, log zerolog.Logger) *CallHandler {
	return &CallHandler{
		clients:      clients,
		stateManager: sm,
		db:           db,
		log:          log,
	}
}

func (h *CallHandler) ProcessManualDial(ctx context.Context, req *agentv1.ProcessManualDialRequest) (*agentv1.ProcessManualDialResponse, error) {
	l := h.log.With().Str("dest", req.DestinationNumber).Str("agent", req.UserId).Logger()
	l.Info().Msg("☎️ Manuel dış arama orkestrasyonu tetiklendi.")

	if len(req.DestinationNumber) < 4 {
		return &agentv1.ProcessManualDialResponse{Accepted: false, ErrorMessage: "Geçersiz hedef numara"}, nil
	}

	callID := fmt.Sprintf("out-%s", uuid.New().String())

	b2buaReq := &sipv1.InitiateCallRequest{
		CallId:  callID,
		FromUri: fmt.Sprintf("sip:%s@sentiric.cloud", req.UserId),
		ToUri:   fmt.Sprintf("sip:%s@sentiric.cloud", req.DestinationNumber),
	}

	_, err := h.clients.B2BUA.InitiateCall(ctx, b2buaReq)
	if err != nil {
		l.Error().Err(err).Msg("❌ B2BUA servis çağrısı başarısız.")
		return &agentv1.ProcessManualDialResponse{Accepted: false, ErrorMessage: "Sinyalleşme hatası: " + err.Error()}, nil
	}

	stateErr := h.stateManager.Set(ctx, &state.CallState{
		CallID:       callID,
		TenantID:     req.TenantId,
		CurrentState: constants.StateWelcoming,
	})

	if stateErr != nil {
		l.Warn().Err(stateErr).Msg("State kaydı oluşturulamadı (Kritik değil)")
	}

	l.Info().Str("call_id", callID).Msg("✅ Dış arama başarıyla kuyruğa alındı.")
	return &agentv1.ProcessManualDialResponse{Accepted: true, CallId: callID}, nil
}

func (h *CallHandler) HandleCallStarted(ctx context.Context, event *state.CallEvent) {
	l := h.log.With().Str("call_id", event.CallID).Logger()

	lockKey := fmt.Sprintf("lock:call_started:%s", event.CallID)
	isNew, err := h.stateManager.RedisClient().SetNX(ctx, lockKey, "1", 10*time.Second).Result()
	if err != nil || !isNew {
		if err != nil {
			l.Error().Err(err).Msg("Redis kilit hatası")
		} else {
			l.Warn().Msg("⚠️ Çift 'call.started' olayı algılandı ve yoksayıldı.")
		}
		return
	}

	l.Info().Msg("📞 Yeni çağrı yakalandı. Orkestrasyon başlıyor.")

	if event.Media == nil {
		l.Error().Msg("🚨 KRİTİK: Media bilgisi eksik, çağrı yönetilemez.")
		// Fallback anonsu doğrudan Media Service'e gönderemeyiz, bu yüzden sadece logluyoruz.
		return
	}

	err = h.stateManager.Set(ctx, &state.CallState{
		CallID:       event.CallID,
		TraceID:      event.TraceID,
		Event:        event,
		CurrentState: constants.StateWelcoming,
	})
	if err != nil {
		l.Error().Err(err).Msg("Redis durum kaydı başarısız.")
	}

	// Tüm iş mantığı `telephony-action-service`'e devrediliyor.
	h.delegateToTelephonyAction(ctx, event)
}

// YENİ METOT: delegateToTelephonyAction
// Bu metot, gelen çağrının tüm ses işleme döngüsünü `telephony-action-service`'e devreder.
func (h *CallHandler) delegateToTelephonyAction(ctx context.Context, event *state.CallEvent) {
	l := h.log.With().Str("call_id", event.CallID).Logger()
	l.Info().Msg("🤖 Pipeline, telephony-action-service'e devrediliyor...")

	sessionID := event.TraceID
	if sessionID == "" {
		sessionID = fmt.Sprintf("sess-%s", event.CallID)
	}

	mediaInfoProto := &eventv1.MediaInfo{
		CallerRtpAddr: event.Media.CallerRtpAddr,
		ServerRtpPort: uint32(event.Media.ServerRtpPort),
	}

	req := &telephonyv1.RunPipelineRequest{
		CallId:        event.CallID,
		SessionId:     sessionID,
		MediaInfo:     mediaInfoProto,
		SttModelId:    "whisper:default", // Gelecekte dialplan'dan gelebilir
		TtsModelId:    "coqui:default",   // Gelecekte dialplan'dan gelebilir
		RecordSession: true,
	}

	// gRPC stream'ini başlat
	stream, err := h.clients.TelephonyAction.RunPipeline(ctx, req)
	if err != nil {
		l.Error().Err(err).Msg("❌ RunPipeline başlatılamadı.")
		// Burada yapılacak fallback (örn: hata anonsu) yine telephony-action-service'de olmalı.
		return
	}

	l.Info().Msg("✅ Pipeline başarıyla devredildi. Durum güncellemeleri dinleniyor.")

	// Arka planda stream'den gelen durum güncellemelerini dinle.
	// Bu, Agent'ın pipeline'ın sağlığını izlemesini sağlar.
	go func() {
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				l.Info().Msg("🏁 Pipeline tamamlandı (EOF).")
				return
			}
			if err != nil {
				l.Error().Err(err).Msg("⚠️ Pipeline bağlantısı koptu.")
				// Burada yeniden bağlanma veya SAGA'yı fail etme mantığı eklenebilir.
				return
			}

			// Gelen durum güncellemelerini logla
			switch resp.State {
			case telephonyv1.RunPipelineResponse_STATE_RUNNING:
				l.Debug().Msg("🟢 Pipeline çalışıyor...")
			case telephonyv1.RunPipelineResponse_STATE_ERROR:
				l.Error().Str("msg", resp.Message).Msg("🔴 Pipeline Hatası")
			case telephonyv1.RunPipelineResponse_STATE_STOPPED:
				l.Info().Msg("🛑 Pipeline durdu.")
				return
			}
		}
	}()
}

func (h *CallHandler) HandleCallEnded(ctx context.Context, event *state.CallEvent) {
	h.log.Info().Str("call_id", event.CallID).Msg("📴 Çağrı sonlandı.")
	// Burada, eğer pipeline hala çalışıyorsa sonlandırma komutu gönderilebilir.
}
