package handler

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"time"

	"github.com/rs/zerolog"

	// Contracts v1.13.6
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

func (h *CallHandler) HandleCallStarted(ctx context.Context, event *state.CallEvent) {
	l := h.log.With().Str("call_id", event.CallID).Logger()

	// [FIX] Idempotency Check: Redis kilidi
	lockKey := fmt.Sprintf("lock:call_started:%s", event.CallID)
	isNew, err := h.stateManager.RedisClient().SetNX(ctx, lockKey, "1", 10*time.Second).Result()

	if err != nil {
		l.Error().Err(err).Msg("Redis kilit hatası")
		return
	}

	if !isNew {
		l.Warn().Msg("⚠️ Çift 'call.started' olayı algılandı ve yoksayıldı.")
		return
	}

	l.Info().Msg("📞 Yeni çağrı yakalandı. Orkestrasyon başlıyor.")

	if event.Media == nil {
		l.Error().Msg("🚨 KRİTİK: Media bilgisi eksik, çağrı yönetilemez.")
		// Hata anonsu çalıp kapatabiliriz
		h.playAnnouncementAndHangup(ctx, event.CallID, "ANNOUNCE_SYSTEM_ERROR", "system", "tr", event.Media)
		return
	}

	// Durumu kaydet
	err = h.stateManager.Set(ctx, &state.CallState{
		CallID:       event.CallID,
		TraceID:      event.TraceID,
		Event:        event,
		CurrentState: constants.StateWelcoming,
	})
	if err != nil {
		l.Error().Err(err).Msg("Redis durum kaydı başarısız.")
	}

	// Aksiyon Kararı
	// Eğer event içinde dialplan bilgisi yoksa varsayılan olarak AI başlat
	action := "START_AI_CONVERSATION"
	if event.Dialplan != nil && event.Dialplan.Action != nil {
		action = event.Dialplan.Action.Action
	}

	l.Info().Str("action", action).Msg("🎯 Aksiyon uygulanıyor.")

	switch action {
	case "START_AI_CONVERSATION":
		h.startAIConversation(ctx, event, false)
	case "PROCESS_GUEST_CALL":
		h.startAIConversation(ctx, event, true)
	case "PLAY_ANNOUNCEMENT":
		h.handlePlayAnnouncement(ctx, event)
	default:
		l.Warn().Str("unknown_action", action).Msg("❓ Bilinmeyen aksiyon. AI başlatılıyor.")
		h.startAIConversation(ctx, event, false)
	}
}

// startAIConversation: Sorumluluğu TelephonyActionService'e devreder.
func (h *CallHandler) startAIConversation(ctx context.Context, event *state.CallEvent, isGuest bool) {
	l := h.log.With().Str("call_id", event.CallID).Logger()

	l.Info().Msg("🤖 AI Pipeline Tetikleniyor (Delegation Mode)...")

	sessionID := event.TraceID
	if sessionID == "" {
		sessionID = fmt.Sprintf("sess-%s", event.CallID)
	}

	// Protobuf için MediaInfo hazırlığı
	mediaInfoProto := &eventv1.MediaInfo{
		CallerRtpAddr: event.Media.CallerRtpAddr,
		ServerRtpPort: uint32(event.Media.ServerRtpPort),
	}

	req := &telephonyv1.RunPipelineRequest{
		CallId:        event.CallID,
		SessionId:     sessionID,
		MediaInfo:     mediaInfoProto,
		SttModelId:    "whisper:default",
		TtsModelId:    "coqui:default",
		RecordSession: true, // Varsayılan kayıt açık
	}

	// Streaming RPC başlat
	stream, err := h.clients.TelephonyAction.RunPipeline(ctx, req)
	if err != nil {
		l.Error().Err(err).Msg("❌ RunPipeline başlatılamadı. Fallback anons çalınıyor.")
		h.playAnnouncementAndHangup(ctx, event.CallID, "ANNOUNCE_SYSTEM_ERROR", "system", "tr", event.Media)
		return
	}

	// Pipeline durumunu izleyen arka plan görevi
	go func() {
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				l.Info().Msg("🏁 Pipeline tamamlandı (EOF).")
				return
			}
			if err != nil {
				l.Error().Err(err).Msg("⚠️ Pipeline bağlantısı koptu.")
				return
			}

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

func (h *CallHandler) handlePlayAnnouncement(ctx context.Context, event *state.CallEvent) {
	l := h.log.With().Str("call_id", event.CallID).Logger()
	announceID := "ANNOUNCE_GENERIC"
	lang := "tr"
	tenantID := "system"

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

func (h *CallHandler) playAnnouncementAndHangup(ctx context.Context, callID, announceID, tenantID, lang string, media *state.MediaInfoPayload) {
	l := h.log.With().Str("call_id", callID).Str("announce_id", announceID).Logger()

	if h.db == nil {
		l.Error().Msg("DB bağlantısı yok, anons çalınamıyor.")
		return
	}

	audioPath, err := database.GetAnnouncementPathFromDB(h.db, announceID, tenantID, lang)
	if err != nil {
		l.Error().Err(err).Msg("Anons dosyası bulunamadı, varsayılan hata sesi çalınıyor.")
		audioPath = "audio/tr/system/technical_difficulty.wav"
	}

	fullURI := fmt.Sprintf("file://%s", audioPath)

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

func (h *CallHandler) HandleCallEnded(ctx context.Context, event *state.CallEvent) {
	h.log.Info().Str("call_id", event.CallID).Msg("📴 Çağrı sonlandı.")
}
