package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sentiric/sentiric-agent-service/internal/constants"
	"github.com/sentiric/sentiric-agent-service/internal/ctxlogger"
	"github.com/sentiric/sentiric-agent-service/internal/database"
	"github.com/sentiric/sentiric-agent-service/internal/state"
	mediav1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/media/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// MediaManager, media-service ile olan tüm etkileşimleri yönetir.
type MediaManager struct {
	db           *sql.DB
	mediaClient  mediav1.MediaServiceClient
	eventsFailed *prometheus.CounterVec
}

// NewMediaManager, yeni bir MediaManager örneği oluşturur.
func NewMediaManager(db *sql.DB, mc mediav1.MediaServiceClient, failed *prometheus.CounterVec) *MediaManager {
	return &MediaManager{
		db:           db,
		mediaClient:  mc,
		eventsFailed: failed,
	}
}

// PlayAudio, verilen bir audio URI'sini kullanıcıya çalar.
func (m *MediaManager) PlayAudio(ctx context.Context, callState *state.CallState, audioURI string) {
	l := ctxlogger.FromContext(ctx)

	if callState.Event == nil || callState.Event.Media == nil {
		l.Error().Msg("PlayAudio için kritik medya bilgisi eksik (st.Event.Media is nil).")
		return
	}

	rtpTargetVal, ok1 := callState.Event.Media["caller_rtp_addr"]
	serverPortVal, ok2 := callState.Event.Media["server_rtp_port"]
	if !ok1 || !ok2 {
		l.Error().Msg("PlayAudio için medya bilgileri eksik (caller_rtp_addr veya server_rtp_port anahtarı yok).")
		return
	}

	rtpTarget, ok1 := rtpTargetVal.(string)
	serverPortFloat, ok2 := serverPortVal.(float64)
	if !ok1 || !ok2 {
		l.Error().Interface("rtp_target", rtpTargetVal).Interface("server_port", serverPortVal).Msg("PlayAudio için medya bilgileri geçersiz formatta.")
		return
	}

	serverPort := uint32(serverPortFloat)
	playReq := &mediav1.PlayAudioRequest{RtpTargetAddr: rtpTarget, ServerRtpPort: serverPort, AudioUri: audioURI}

	playCtx, playCancel := context.WithTimeout(metadata.AppendToOutgoingContext(ctx, "x-trace-id", callState.TraceID), 5*time.Minute)
	defer playCancel()

	_, err := m.mediaClient.PlayAudio(playCtx, playReq)
	if err != nil {
		stErr, ok := status.FromError(err)
		if ok && (stErr.Code() == codes.Canceled || stErr.Code() == codes.DeadlineExceeded) {
			l.Warn().Err(err).Msg("PlayAudio işlemi başka bir komutla veya zaman aşımı nedeniyle iptal edildi.")
		} else {
			l.Error().Err(err).Str("audio_uri", audioURI).Msg("Hata: Ses çalma komutu başarısız.")
			m.eventsFailed.WithLabelValues(callState.Event.EventType, "play_audio_failed").Inc()
		}
	} else {
		l.Info().Str("audio_uri_type", audioURI[:15]).Msg("Ses başarıyla çalındı ve tamamlandı.")
	}
}

// PlayAnnouncement, veritabanından anons yolunu alıp çalar.
func (m *MediaManager) PlayAnnouncement(ctx context.Context, callState *state.CallState, announcementID constants.AnnouncementID) {
	l := ctxlogger.FromContext(ctx)
	languageCode := getLanguageCode(callState.Event)
	audioPath, err := database.GetAnnouncementPathFromDB(m.db, string(announcementID), callState.TenantID, languageCode)
	if err != nil {
		l.Error().Err(err).Str("announcement_id", string(announcementID)).Msg("Anons yolu alınamadı, fallback deneniyor")
		audioPath, err = database.GetAnnouncementPathFromDB(m.db, string(announcementID), "system", "en")
		if err != nil {
			l.Error().Err(err).Str("announcement_id", string(announcementID)).Msg("KRİTİK HATA: Sistem fallback anonsu dahi yüklenemedi.")
			return
		}
	}
	audioURI := fmt.Sprintf("file://%s", audioPath)
	m.PlayAudio(ctx, callState, audioURI)
}

// StartRecording, çağrı kaydını başlatır.
func (m *MediaManager) StartRecording(ctx context.Context, callState *state.CallState) {
	l := ctxlogger.FromContext(ctx)
	recordingTenantID := callState.TenantID
	// MEDIA_SERVICE_RECORD_BASE_PATH="/sentiric-media-record"
	recordingURI := fmt.Sprintf("s3://sentiric-media-record/%s/%s.wav", recordingTenantID, callState.CallID)

	l.Info().Str("uri", recordingURI).Msg("Çağrı kaydı başlatılıyor...")
	startRecCtx, startRecCancel := context.WithTimeout(metadata.AppendToOutgoingContext(ctx, "x-trace-id", callState.TraceID), 10*time.Second)
	defer startRecCancel()

	_, err := m.mediaClient.StartRecording(startRecCtx, &mediav1.StartRecordingRequest{
		ServerRtpPort: uint32(callState.Event.Media["server_rtp_port"].(float64)),
		OutputUri:     recordingURI,
		CallId:        callState.CallID,
		TraceId:       callState.TraceID,
	})
	if err != nil {
		l.Error().Err(err).Msg("Media-service'e kayıt başlatma komutu gönderilemedi.")
	}
}

// StopRecording, çağrı kaydını durdurur.
func (m *MediaManager) StopRecording(ctx context.Context, callState *state.CallState) {
	l := ctxlogger.FromContext(ctx)
	l.Info().Msg("Çağrı kaydı durduruluyor...")
	stopRecCtx, stopRecCancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-trace-id", callState.TraceID), 10*time.Second)
	defer stopRecCancel()

	_, err := m.mediaClient.StopRecording(stopRecCtx, &mediav1.StopRecordingRequest{
		ServerRtpPort: uint32(callState.Event.Media["server_rtp_port"].(float64)),
	})
	if err != nil {
		l.Error().Err(err).Msg("Media-service'e kayıt durdurma komutu gönderilemedi.")
		m.eventsFailed.WithLabelValues(callState.Event.EventType, "stop_recording_failed").Inc()
	}
}
