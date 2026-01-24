package handler

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/rs/zerolog"
	// Bu importlar ZORUNLUDUR:
	eventv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/event/v1"
	telephonyv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/telephony/v1"
	
	"github.com/sentiric/sentiric-agent-service/internal/client"
	"github.com/sentiric/sentiric-agent-service/internal/database"
	"github.com/sentiric/sentiric-agent-service/internal/state"
)

type CallHandler struct {
	clients      *client.Clients
	stateManager *state.Manager
	db           *sql.DB
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

func (h *CallHandler) HandleCallStarted(ctx context.Context, event *state.CallEvent) {
	l := h.log.With().Str("call_id", event.CallID).Logger()
	l.Info().Msg("📞 Yeni çağrı yakalandı. Orkestrasyon başlıyor.")

	if event.Media == nil {
		l.Error().Msg("Media bilgisi eksik, çağrı yönetilemez.")
		return
	}

	if event.Dialplan == nil {
		l.Info().Msg("Dialplan yok, varsayılan karşılama başlatılıyor.")
		go h.speakWelcomeMessage(context.Background(), event.CallID, "coqui:default", event.Media)
		return
	}
}

func (h *CallHandler) HandleCallEnded(ctx context.Context, event *state.CallEvent) {
	h.log.Info().Str("call_id", event.CallID).Msg("📴 Çağrı sonlandı.")
}

func (h *CallHandler) speakWelcomeMessage(ctx context.Context, callID, voiceID string, media *state.MediaInfoPayload) {
	l := h.log.With().Str("call_id", callID).Logger()
	l.Info().Msg("🗣️  Karşılama mesajı için Telephony Action tetikleniyor...")

	// Internal state'den Protobuf'a dönüşüm
	mediaInfoProto := &eventv1.MediaInfo{
		CallerRtpAddr: media.CallerRtpAddr,
		ServerRtpPort: uint32(media.ServerRtpPort),
	}

	req := &telephonyv1.SpeakTextRequest{
		CallId:    callID,
		Text:      "Merhaba, Sentiric iletişim sistemine hoş geldiniz.",
		VoiceId:   voiceID,
		MediaInfo: mediaInfoProto,
	}

	_, err := h.clients.TelephonyAction.SpeakText(ctx, req)
	if err != nil {
		l.Error().Err(err).Msg("❌ SpeakText başarısız oldu.")
	} else {
		l.Info().Msg("✅ SpeakText komutu başarıyla iletildi.")
	}
}

func (h *CallHandler) playAnnouncementAndHangup(ctx context.Context, callID, announceID, tenantID, lang string, media *state.MediaInfoPayload) {
	l := h.log.With().Str("call_id", callID).Str("announce_id", announceID).Logger()

	if h.db == nil || h.clients == nil || h.clients.TelephonyAction == nil {
		l.Error().Msg("PANIC ÖNLENDİ: Kritik bağımlılıklar eksik!")
		return
	}

	audioPath, err := database.GetAnnouncementPathFromDB(h.db, announceID, tenantID, lang)
	if err != nil {
		l.Error().Err(err).Msg("Anons dosyası veritabanında bulunamadı.")
		audioPath = "audio/tr/system/connecting.wav" 
	}

	fullURI := fmt.Sprintf("file://%s", audioPath)
	l.Info().Str("uri", fullURI).Msg("🔊 Telephony Action'a Oynatma Emri Gönderiliyor...")

	req := &telephonyv1.PlayAudioRequest{
		CallId: callID,
		AudioUri: fullURI,
	}

	_, err = h.clients.TelephonyAction.PlayAudio(ctx, req)
	if err != nil {
		l.Error().Err(err).Msg("❌ Anons çalınamadı.")
	} else {
		l.Info().Msg("✅ Anons komutu başarıyla iletildi.")
	}
}