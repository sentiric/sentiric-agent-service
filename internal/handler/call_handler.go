package handler

import (
	"context"
	"database/sql"
	"fmt"
    // "io" kaldırıldı
    // "eventv1" kaldırıldı

	"github.com/rs/zerolog"
	// eventv1 importunu sildik çünkü bu dosyada doğrudan kullanılmıyor
	telephonyv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/telephony/v1"
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

	if event.Dialplan == nil {
		l.Warn().Msg("⚠️ Dialplan bilgisi eksik (B2BUA Kaynaklı). TEST MODU: Doğrudan anons çalınıyor.")
		go h.playAnnouncementAndHangup(context.Background(), event.CallID, "ANNOUNCE_SYSTEM_CONNECTING", "system", "tr", event.Media)
		return
	}
}

// HandleCallEnded
func (h *CallHandler) HandleCallEnded(ctx context.Context, event *state.CallEvent) {
	log := h.log.With().Str("call_id", event.CallID).Logger()
	log.Info().Msg("📴 Çağrı sonlandı.")
}

func (h *CallHandler) playAnnouncementAndHangup(ctx context.Context, callID, announceID, tenantID, lang string, media *state.MediaInfoPayload) {
	l := h.log.With().Str("call_id", callID).Str("announce_id", announceID).Logger()

	if h.db == nil || h.clients == nil || h.clients.TelephonyAction == nil {
		l.Error().Msg("PANIC ÖNLENDİ: Kritik bağımlılıklar eksik!")
		return
	}

	audioPath, err := database.GetAnnouncementPathFromDB(h.db, announceID, tenantID, lang)
	if err != nil {
		l.Error().Err(err).Msg("Anons dosyası veritabanında bulunamadı, varsayılan kullanılıyor.")
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
		l.Error().Err(err).Msg("❌ Anons çalınamadı (Telephony Action hatası).")
	} else {
		l.Info().Msg("✅ Anons komutu başarıyla iletildi.")
	}
}