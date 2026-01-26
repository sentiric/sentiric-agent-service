package handler

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/rs/zerolog"

	// Contracts v1.13.5
	eventv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/event/v1"
	telephonyv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/telephony/v1"

	"github.com/sentiric/sentiric-agent-service/internal/client"
	"github.com/sentiric/sentiric-agent-service/internal/constants"
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

// HandleCallStarted, çağrı başladığında iş mantığını yönlendirir.
func (h *CallHandler) HandleCallStarted(ctx context.Context, event *state.CallEvent) {
	l := h.log.With().Str("call_id", event.CallID).Logger()
	l.Info().Msg("📞 Yeni çağrı yakalandı. Orkestrasyon başlıyor.")

	if event.Media == nil {
		l.Error().Msg("🚨 KRİTİK: Media bilgisi eksik, çağrı yönetilemez.")
		return
	}

	// Durumu Redis'e kaydet (Initial State)
	err := h.stateManager.Set(ctx, &state.CallState{
		CallID:       event.CallID,
		TraceID:      event.TraceID,
		Event:        event,
		CurrentState: constants.StateWelcoming,
	})
	if err != nil {
		l.Error().Err(err).Msg("Redis durum kaydı başarısız.")
	}

	// 1. Dialplan Kontrolü (Fallback)
	if event.Dialplan == nil || event.Dialplan.Action == nil {
		l.Warn().Msg("⚠️ Dialplan çözülemedi veya aksiyon yok. Varsayılan (Misafir) akışı başlatılıyor.")
		h.startAIConversation(ctx, event, true) // isGuest = true
		return
	}

	// 2. Aksiyon Bazlı Yönlendirme (Action Routing)
	action := event.Dialplan.Action.Action
	l.Info().Str("action", action).Msg("🎯 Dialplan kararı uygulanıyor.")

	switch action {
	case "START_AI_CONVERSATION":
		h.startAIConversation(ctx, event, false)
	case "PLAY_ANNOUNCEMENT":
		h.handlePlayAnnouncement(ctx, event)
	default:
		l.Warn().Str("unknown_action", action).Msg("❓ Bilinmeyen aksiyon. Varsayılan akışa dönülüyor.")
		h.startAIConversation(ctx, event, true)
	}
}

func (h *CallHandler) HandleCallEnded(ctx context.Context, event *state.CallEvent) {
	h.log.Info().Str("call_id", event.CallID).Msg("📴 Çağrı sonlandı.")
	// Gelecekte: Redis'ten durumu temizle veya logla.
}

// --- ALT MANTIKLAR (SUB-LOGIC) ---

// startAIConversation: Yapay zeka destekli diyalog başlatır.
func (h *CallHandler) startAIConversation(ctx context.Context, event *state.CallEvent, isGuest bool) {
	l := h.log.With().Str("call_id", event.CallID).Logger()

	// 1. Karşılama Metnini Belirle
	welcomeText := "Merhaba, Sentiric iletişim sistemine hoş geldiniz."
	voiceID := "coqui:default"
	
	if !isGuest && event.Dialplan != nil && event.Dialplan.MatchedUser != nil {
		userName := "Misafir"
		if event.Dialplan.MatchedUser.Name != nil {
			userName = *event.Dialplan.MatchedUser.Name
		}
		welcomeText = fmt.Sprintf("Merhaba %s, tekrar hoş geldiniz. Size nasıl yardımcı olabilirim?", userName)
	}

	l.Info().Msg("🗣️  AI Karşılama başlatılıyor...")

	// 2. Telephony Action'a SpeakText Gönder
	mediaInfoProto := &eventv1.MediaInfo{
		CallerRtpAddr: event.Media.CallerRtpAddr,
		ServerRtpPort: uint32(event.Media.ServerRtpPort),
	}

	req := &telephonyv1.SpeakTextRequest{
		CallId:    event.CallID,
		Text:      welcomeText,
		VoiceId:   voiceID,
		MediaInfo: mediaInfoProto,
	}

	_, err := h.clients.TelephonyAction.SpeakText(ctx, req)
	if err != nil {
		l.Error().Err(err).Msg("❌ SpeakText başarısız oldu.")
		// Fail durumunda anons çalıp kapatabiliriz
		return
	}
	l.Info().Msg("✅ SpeakText iletildi. (Not: STT tetiklemesi TelephonyAction tarafından yönetilecek)")
	
	// State Güncelleme
	// TODO: Burada STT Gateway'in hazır olması beklenebilir.
}

// handlePlayAnnouncement: Sadece bir anons çalar ve (genellikle) kapatır.
func (h *CallHandler) handlePlayAnnouncement(ctx context.Context, event *state.CallEvent) {
	l := h.log.With().Str("call_id", event.CallID).Logger()
	
	announceID := "ANNOUNCE_GENERIC"
	lang := "tr"
	tenantID := "system"

	// Dialplan verisinden parametreleri al
	if event.Dialplan != nil {
		tenantID = event.Dialplan.TenantID
		if event.Dialplan.Action != nil && event.Dialplan.Action.ActionData != nil {
			if val, ok := event.Dialplan.Action.ActionData.Data["announcementId"]; ok {
				announceID = val
			}
		}
	}

	l.Info().Str("announce_id", announceID).Msg("📢 Anons çalma isteği.")
	h.playAnnouncementAndHangup(ctx, event.CallID, announceID, tenantID, lang, event.Media)
}

// playAnnouncementAndHangup: Veritabanından dosya yolunu bulup çalar.
func (h *CallHandler) playAnnouncementAndHangup(ctx context.Context, callID, announceID, tenantID, lang string, media *state.MediaInfoPayload) {
	l := h.log.With().Str("call_id", callID).Str("announce_id", announceID).Logger()

	if h.db == nil {
		l.Error().Msg("DB bağlantısı yok, anons çalınamıyor.")
		return
	}

	// DB'den dosya yolunu bul
	audioPath, err := database.GetAnnouncementPathFromDB(h.db, announceID, tenantID, lang)
	if err != nil {
		l.Error().Err(err).Msg("Anons dosyası bulunamadı, varsayılan çalınıyor.")
		// Fallback audio
		audioPath = "audio/tr/system/error.wav" 
	}

	// URI oluştur (Local file system veya S3 presigned URL olabilir, şimdilik file://)
	fullURI := fmt.Sprintf("file://%s", audioPath)
	
	// TelephonyAction'a PlayAudio Gönder
	// DÜZELTME: MediaInfo kaldırıldı (v1.13.5 contract uyumluluğu için)
	req := &telephonyv1.PlayAudioRequest{
		CallId:   callID,
		AudioUri: fullURI,
	}

	_, err = h.clients.TelephonyAction.PlayAudio(ctx, req)
	if err != nil {
		l.Error().Err(err).Msg("❌ Anons komutu iletilemedi.")
	} else {
		l.Info().Str("uri", fullURI).Msg("✅ PlayAudio komutu gönderildi.")
	}
}