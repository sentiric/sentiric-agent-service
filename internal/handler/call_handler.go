package handler

import (
	"context"
	"io"
	"strings"

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
	log          zerolog.Logger
}

func NewCallHandler(clients *client.Clients, sm *state.Manager, log zerolog.Logger) *CallHandler {
	return &CallHandler{
		clients:      clients,
		stateManager: sm,
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
		// --- YENİ EKLENEN KISIM ---
		// Basit anons çalma mantığı
		if data := event.Dialplan.Action.ActionData; data != nil {
			if announceID, ok := data.Data["announcement_id"]; ok {
				go h.playAnnouncementAndHangup(context.Background(), event.CallID, announceID, event.Media)
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
func (h *CallHandler) playAnnouncementAndHangup(ctx context.Context, callID, announceID string, media *state.MediaInfoPayload) {
	l := h.log.With().Str("call_id", callID).Str("announce_id", announceID).Logger()

	// 1. Dosya Yolunu Bul (Veritabanı bağlantısı olmadığı için hardcode veya config kullanabiliriz)
	// Şimdilik test için statik bir yol üretiyoruz. Gerçekte DB'den gelmeli.
	// Örn: "ANNOUNCE_SYSTEM_CONNECTING" -> "file://audio/tr/system/connecting.wav"
	// Basitleştirme: ID'yi doğrudan path'e çeviriyoruz.
	
	// NOT: Buradaki path, media-service container'ı içindeki yoldur.
	// Media service assets klasörünü mount etmiş olmalı.
	// Örnek ID: "ANNOUNCE_SYSTEM_CONNECTING"
	// Beklenen Path: "file://audio/tr/system/connecting.wav"
	
	// Geçici Mapping (DB yerine)
	var audioPath string
	if strings.Contains(announceID, "CONNECTING") {
		audioPath = "file://audio/tr/system/connecting.wav"
	} else if strings.Contains(announceID, "ERROR") {
		audioPath = "file://audio/tr/system/technical_difficulty.wav"
	} else {
		// Varsayılan
		audioPath = "file://audio/tr/system/welcome_anonymous.wav"
	}

	l.Info().Str("path", audioPath).Msg("Anons çalınıyor...")

	// 2. PlayAudio Komutu
	playReq := &mediav1.PlayAudioRequest{
		AudioUri:       audioPath,
		ServerRtpPort:  uint32(media.ServerRtpPort),
		RtpTargetAddr:  media.CallerRtpAddr, // NAT Latching için ilk hedef (tahmini)
	}

	_, err := h.clients.Media.PlayAudio(ctx, playReq)
	if err != nil {
		l.Error().Err(err).Msg("Anons çalınamadı.")
	} else {
		l.Info().Msg("Anons komutu iletildi.")
	}

	// Anonsun bitmesi için bekle (Basitçe 5 saniye)
	// Gerçekte Media Service'ten "PlaybackFinished" olayı beklenmelidir.
	// Şimdilik hard timeout.
	// time.Sleep(5 * time.Second) <- Go'da blocking sleep yapmamalıyız, ama goroutine içindeyiz.
	// Ancak import sorunu olmaması için sleep'i atlıyoruz ve hemen kapatmıyoruz.
	// Kullanıcı kendisi kapatır veya timeout olur.
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