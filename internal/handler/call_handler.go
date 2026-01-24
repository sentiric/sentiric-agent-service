package handler

import (
	"context"
	"database/sql"
	"fmt"
	"io"

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
	// YENİ: Veritabanı bağlantısı eklendi
	db           *sql.DB
	log          zerolog.Logger
}

// YENİ: Constructor db parametresi alıyor
func NewCallHandler(clients *client.Clients, sm *state.Manager, db *sql.DB, log zerolog.Logger) *CallHandler {
	return &CallHandler{
		clients:      clients,
		stateManager: sm,
		db:           db,
		log:          log,
	}
}

// HandleCallStarted: Çağrı başladığında tetiklenir.
func (h *CallHandler) HandleCallStarted(ctx context.Context, event *state.CallEvent) {
	l := h.log.With().Str("call_id", event.CallID).Logger()
	l.Info().Msg("📞 Yeni çağrı yakalandı. Orkestrasyon başlıyor.")

	if event.Media == nil {
		l.Error().Msg("Media bilgisi eksik, çağrı yönetilemez.")
		return
	}

	if event.Dialplan == nil || event.Dialplan.Action == nil {
		l.Warn().Msg("Dialplan bilgisi eksik, varsayılan AI akışı başlatılıyor.")
		go h.triggerPipeline(context.Background(), event.CallID, event.TraceID, event.Media)
		return
	}

	action := event.Dialplan.Action.Action
	l.Info().Str("action", action).Msg("Dialplan aksiyonu işleniyor.")

	switch action {
	case ActionPlayAnnouncement:
		// Dinamik anons çalma mantığı
		if data := event.Dialplan.Action.ActionData; data != nil {
			if announceID, ok := data.Data["announcement_id"]; ok {
				// TenantID ve LanguageCode bilgisini event'ten çekiyoruz
				tenantID := event.Dialplan.TenantID
				lang := event.Dialplan.InboundRoute.DefaultLanguageCode
				if lang == "" {
					lang = "tr" // Varsayılan dil
				}
				
				// Veritabanı sorgusu ile gerçek path'i bul
				go h.playAnnouncementAndHangup(context.Background(), event.CallID, announceID, tenantID, lang, event.Media)
				return
			}
		}
		l.Warn().Msg("Anons ID bulunamadı, varsayılan akışa dönülüyor.")
		go h.triggerPipeline(context.Background(), event.CallID, event.TraceID, event.Media)

	case ActionStartAIConversation:
		go h.triggerPipeline(context.Background(), event.CallID, event.TraceID, event.Media)

	default:
		l.Warn().Str("unknown_action", action).Msg("Bilinmeyen aksiyon, varsayılan akış başlatılıyor.")
		go h.triggerPipeline(context.Background(), event.CallID, event.TraceID, event.Media)
	}
}

// HandleCallEnded: Çağrı bittiğinde çalışır ve kaynakları temizler.
func (h *CallHandler) HandleCallEnded(ctx context.Context, event *state.CallEvent) {
	log := h.log.With().Str("call_id", event.CallID).Logger()
	log.Info().Msg("📴 Çağrı sonlandı. Temizlik işlemleri başlatılıyor.")

	if event.Media == nil {
		log.Warn().Msg("Etkinlikte medya bilgisi yok, port temizlenemedi.")
		return
	}

	if event.Media.ServerRtpPort > 0 {
		port := uint32(event.Media.ServerRtpPort)
		log.Info().Uint32("port", port).Msg("Media Service'e ReleasePort komutu gönderiliyor...")
		req := &mediav1.ReleasePortRequest{RtpPort: port}
		_, err := h.clients.Media.ReleasePort(context.Background(), req)
		if err != nil {
			log.Warn().Err(err).Msg("Port serbest bırakılırken hata oluştu.")
		} else {
			log.Info().Msg("Port başarıyla serbest bırakıldı.")
		}
	}
}

// playAnnouncementAndHangup: Anons çalar ve sonra telefonu kapatır.
func (h *CallHandler) playAnnouncementAndHangup(ctx context.Context, callID, announceID, tenantID, lang string, media *state.MediaInfoPayload) {
	l := h.log.With().Str("call_id", callID).Str("announce_id", announceID).Logger()

	// 1. Veritabanından Ses Dosyasının Yolunu Bul
	// database paketindeki hazır fonksiyonu kullanıyoruz.
	audioPath, err := database.GetAnnouncementPathFromDB(h.db, announceID, tenantID, lang)
	if err != nil {
		l.Error().Err(err).Msg("Anons dosyası veritabanında bulunamadı. Fallback uygulanıyor.")
		// Fallback: Veritabanı hatası durumunda hardcoded bir path dene veya hata dön.
		audioPath = "audio/tr/system/technical_difficulty.wav"
	}

	// Media Service "file://" şeması bekler
	fullURI := fmt.Sprintf("file://%s", audioPath)
	l.Info().Str("uri", fullURI).Msg("Anons çalınıyor...")

	// 2. PlayAudio Komutu
	playReq := &mediav1.PlayAudioRequest{
		AudioUri:       fullURI,
		ServerRtpPort:  uint32(media.ServerRtpPort),
		RtpTargetAddr:  media.CallerRtpAddr,
	}

	_, err = h.clients.Media.PlayAudio(ctx, playReq)
	if err != nil {
		l.Error().Err(err).Msg("Anons çalınamadı (Media Service hatası).")
	} else {
		l.Info().Msg("Anons komutu iletildi.")
	}

	// Not: Burada 'PlaybackFinished' olayını beklemek daha doğrudur ancak 
	// şimdilik basit tutmak için asenkron bırakıyoruz.
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

	log.Info().Msg("🚀 Pipeline isteği gönderildi, durum izleniyor...")

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			log.Info().Msg("Pipeline tamamlandı (Stream kapandı).")
			break
		}
		if err != nil {
			log.Error().Err(err).Msg("Pipeline bağlantısı koptu")
			break
		}

		switch resp.State {
		case telephonyv1.RunPipelineResponse_STATE_RUNNING:
			log.Info().Str("msg", resp.Message).Msg("🟢 Pipeline çalışıyor")
		case telephonyv1.RunPipelineResponse_STATE_ERROR:
			log.Error().Str("msg", resp.Message).Msg("🔴 Pipeline hatası")
			return 
		case telephonyv1.RunPipelineResponse_STATE_STOPPED:
			log.Info().Msg("🏁 Pipeline durduruldu")
			return
		}
	}
}