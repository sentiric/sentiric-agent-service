package handler

import (
	"context"
	"database/sql"
	"fmt"
	"io"
    // "strings" <- SİLİNDİ

	"github.com/rs/zerolog"
	eventv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/event/v1"
	mediav1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/media/v1"
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

	// FALLBACK: B2BUA Dialplan bilgisini göndermediği için bu blok çalışacak.
	if event.Dialplan == nil || event.Dialplan.Action == nil {
		l.Warn().Msg("⚠️ Dialplan bilgisi eksik (B2BUA Kaynaklı). TEST MODU: Doğrudan ses testi için ANONS çalınıyor.")
		
		// "ANNOUNCE_SYSTEM_CONNECTING" veritabanındaki ID'dir.
		go h.playAnnouncementAndHangup(context.Background(), event.CallID, "ANNOUNCE_SYSTEM_CONNECTING", "system", "tr", event.Media)
		return
	}

	action := event.Dialplan.Action.Action
	l.Info().Str("action", action).Msg("Dialplan aksiyonu işleniyor.")

	switch action {
	case ActionPlayAnnouncement:
		if data := event.Dialplan.Action.ActionData; data != nil {
			if announceID, ok := data.Data["announcement_id"]; ok {
				tenantID := event.Dialplan.TenantID
				lang := event.Dialplan.InboundRoute.DefaultLanguageCode
				if lang == "" { lang = "tr" }
				
				go h.playAnnouncementAndHangup(context.Background(), event.CallID, announceID, tenantID, lang, event.Media)
				return
			}
		}
		l.Warn().Msg("Anons ID bulunamadı.")
		// Fallback
		go h.playAnnouncementAndHangup(context.Background(), event.CallID, "ANNOUNCE_SYSTEM_ERROR", "system", "tr", event.Media)

	case ActionStartAIConversation:
		go h.triggerPipeline(context.Background(), event.CallID, event.TraceID, event.Media)

	default:
		l.Warn().Str("unknown_action", action).Msg("Bilinmeyen aksiyon.")
		go h.triggerPipeline(context.Background(), event.CallID, event.TraceID, event.Media)
	}
}

// HandleCallEnded
func (h *CallHandler) HandleCallEnded(ctx context.Context, event *state.CallEvent) {
	log := h.log.With().Str("call_id", event.CallID).Logger()
	log.Info().Msg("📴 Çağrı sonlandı. Temizlik işlemleri başlatılıyor.")

	if event.Media == nil { return }

	if event.Media.ServerRtpPort > 0 {
		port := uint32(event.Media.ServerRtpPort)
		req := &mediav1.ReleasePortRequest{RtpPort: port}
		if _, err := h.clients.Media.ReleasePort(context.Background(), req); err != nil {
			log.Warn().Err(err).Msg("Port serbest bırakma hatası")
		} else {
			log.Info().Msg("Port başarıyla serbest bırakıldı.")
		}
	}
}

// playAnnouncementAndHangup: DB'den yolu bulur ve çalar
func (h *CallHandler) playAnnouncementAndHangup(ctx context.Context, callID, announceID, tenantID, lang string, media *state.MediaInfoPayload) {
	l := h.log.With().Str("call_id", callID).Str("announce_id", announceID).Logger()

	// --- DEFANSİF KODLAMA (NIL CHECK) ---
	if h.db == nil {
		l.Error().Msg("PANIC ÖNLENDİ: Veritabanı bağlantısı (h.db) nil!")
		return
	}
	if h.clients == nil || h.clients.Media == nil {
		l.Error().Msg("PANIC ÖNLENDİ: Media Service istemcisi (h.clients.Media) nil!")
		return
	}
	// ------------------------------------

	// 1. Veritabanından Ses Dosyasının Yolunu Bul
	audioPath, err := database.GetAnnouncementPathFromDB(h.db, announceID, tenantID, lang)
	if err != nil {
		l.Error().Err(err).Msg("Anons dosyası veritabanında bulunamadı, varsayılan kullanılıyor.")
		audioPath = "audio/tr/system/connecting.wav" 
	}

	fullURI := fmt.Sprintf("file://%s", audioPath)
	l.Info().Str("uri", fullURI).Msg("🔊 Medya Servisine Oynatma Emri Gönderiliyor...")

	// 2. PlayAudio Komutu
	playReq := &mediav1.PlayAudioRequest{
		AudioUri:       fullURI,
		ServerRtpPort:  uint32(media.ServerRtpPort),
		RtpTargetAddr:  media.CallerRtpAddr,
	}

	_, err = h.clients.Media.PlayAudio(ctx, playReq)
	if err != nil {
		l.Error().Err(err).Msg("❌ Anons çalınamadı (Media Service hatası).")
	} else {
		l.Info().Msg("✅ Anons komutu başarıyla iletildi.")
	}
}

func (h *CallHandler) triggerPipeline(ctx context.Context, callID, traceID string, media *state.MediaInfoPayload) {
	log := h.log.With().Str("call_id", callID).Logger()

	mediaInfoProto := &eventv1.MediaInfo{
		CallerRtpAddr: media.CallerRtpAddr,
		ServerRtpPort: uint32(media.ServerRtpPort),
	}

	req := &telephonyv1.RunPipelineRequest{
		CallId:    callID,
		SessionId: traceID,
		MediaInfo: mediaInfoProto,
	}

	stream, err := h.clients.TelephonyAction.RunPipeline(ctx, req)
	if err != nil {
		log.Error().Err(err).Msg("Pipeline başlatılamadı")
		return
	}

	log.Info().Msg("🚀 Pipeline isteği gönderildi...")

	for {
		resp, err := stream.Recv()
		if err == io.EOF { break }
		if err != nil {
			log.Error().Err(err).Msg("Pipeline bağlantısı koptu")
			break
		}
		if resp.State == telephonyv1.RunPipelineResponse_STATE_ERROR {
			log.Error().Str("msg", resp.Message).Msg("🔴 Pipeline hatası")
			return
		}
	}
}