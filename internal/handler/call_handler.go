package handler

import (
	"context"
	"io"
    // "time" importu kaldırıldı

	"github.com/rs/zerolog"
	eventv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/event/v1"
	telephonyv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/telephony/v1"
	"github.com/sentiric/sentiric-agent-service/internal/client"
	"github.com/sentiric/sentiric-agent-service/internal/state"
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

// HandleCallStarted: Düzeltildi - Artık *state.CallEvent alıyor ([]byte değil)
func (h *CallHandler) HandleCallStarted(ctx context.Context, event *state.CallEvent) {
	log := h.log.With().Str("call_id", event.CallID).Logger()
	log.Info().Msg("📞 Yeni çağrı yakalandı. Orkestrasyon başlıyor.")

	// 1. Media Info Kontrolü
	if event.Media == nil {
		log.Error().Msg("Media bilgisi eksik, çağrı yönetilemez.")
		return
	}

	// 2. Telephony Action Service'i Tetikle
	go h.triggerPipeline(context.Background(), event.CallID, event.TraceID, event.Media)
}

// HandleCallEnded: Eksik metod eklendi
func (h *CallHandler) HandleCallEnded(ctx context.Context, event *state.CallEvent) {
	log := h.log.With().Str("call_id", event.CallID).Logger()
	log.Info().Msg("📴 Çağrı sonlandı.")
	
	// Gelecekte: Redis temizliği veya raporlama burada yapılabilir.
	// Şimdilik sadece logluyoruz.
}

func (h *CallHandler) triggerPipeline(ctx context.Context, callID, traceID string, media *state.MediaInfoPayload) {
	log := h.log.With().Str("call_id", callID).Logger()

	// MediaInfo dönüşümü (JSON -> Protobuf)
	mediaInfoProto := &eventv1.MediaInfo{
		CallerRtpAddr: media.CallerRtpAddr,
		ServerRtpPort: uint32(media.ServerRtpPort),
	}

	req := &telephonyv1.RunPipelineRequest{
		CallId:    callID,
		SessionId: traceID,
		MediaInfo: mediaInfoProto,
	}

	// gRPC Stream Başlat
	stream, err := h.clients.TelephonyAction.RunPipeline(ctx, req)
	if err != nil {
		log.Error().Err(err).Msg("Pipeline başlatılamadı")
		return
	}

	log.Info().Msg("🚀 Pipeline isteği gönderildi, durum izleniyor...")

	// Durum güncellemelerini dinle (Blocking Loop)
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
			return // Döngüden çık
		case telephonyv1.RunPipelineResponse_STATE_STOPPED:
			log.Info().Msg("🏁 Pipeline durduruldu")
			return
		}
	}
}