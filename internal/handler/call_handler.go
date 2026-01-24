package handler

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/rs/zerolog"
	telephonyv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/telephony/v1"
	eventv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/event/v1"
	"github.com/sentiric/sentiric-agent-service/internal/client"
	"github.com/sentiric/sentiric-agent-service/internal/database"
	"github.com/sentiric/sentiric-agent-service/internal/state"
)

// DIALPLAN AKSİYONLARI
const (
	ActionStartAIConversation = "START_AI_CONVERSATION"
	ActionPlayAnnouncement    = "PLAY_ANNOUNCEMENT"
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

// HandleCallStarted
func (h *CallHandler) HandleCallStarted(ctx context.Context, event *state.CallEvent) {
	l := h.log.With().Str("call_id", event.CallID).Logger()
	l.Info().Msg("📞 Yeni çağrı yakalandı. Orkestrasyon başlıyor.")

	if event.Media == nil {
		l.Error().Msg("Media bilgisi eksik, çağrı yönetilemez.")
		return
	}

	// [MASTER PLAN]: Fallback - Anında Karşılama
	if event.Dialplan == nil {
		l.Info().Msg("Dialplan yok, varsayılan karşılama başlatılıyor.")
		go h.speakWelcomeMessage(context.Background(), event.CallID, "coqui:default", event.Media)
		return
	}
}

// HandleCallEnded
func (h *CallHandler) HandleCallEnded(ctx context.Context, event *state.CallEvent) {
	// Sadece logla, kaynak temizliği Media Service'in işidir.
	h.log.Info().Str("call_id", event.CallID).Msg("📴 Çağrı sonlandı.")
}

// [YENİ] speakWelcomeMessage: TelephonyAction üzerinden metin okutma
func (h *CallHandler) speakWelcomeMessage(ctx context.Context, callID, voiceID string, media *state.MediaInfoPayload) {
	l := h.log.With().Str("call_id", callID).Logger()
	l.Info().Msg("🗣️  Karşılama mesajı için Telephony Action tetikleniyor...")

	mediaInfoProto := &eventv1.MediaInfo{
		CallerRtpAddr: media.CallerRtpAddr,
		ServerRtpPort: uint32(media.ServerRtpPort),
	}

	req := &telephonyv1.SpeakTextRequest{
		CallId:    callID,
		Text:      "Merhaba, Sentiric iletişim sistemine hoş geldiniz. Size nasıl yardımcı olabilirim?",
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
	// Eski metot, şimdilik tutuyoruz ama SpeakText tercih edilmeli.
	l := h.log.With().Str("call_id", callID).Logger()
	
	if h.db == nil { return }

	audioPath, err := database.GetAnnouncementPathFromDB(h.db, announceID, tenantID, lang)
	if err != nil {
		audioPath = "audio/tr/system/connecting.wav" 
	}
	fullURI := fmt.Sprintf("file://%s", audioPath)

	req := &telephonyv1.PlayAudioRequest{
		CallId: callID,
		AudioUri: fullURI,
	}
	h.clients.TelephonyAction.PlayAudio(ctx, req)
}