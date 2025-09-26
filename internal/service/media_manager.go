// sentiric-agent-service/internal/service/media_manager.go
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

type MediaManager struct {
	db           *sql.DB
	mediaClient  mediav1.MediaServiceClient
	eventsFailed *prometheus.CounterVec
	bucketName   string
}

func NewMediaManager(db *sql.DB, mc mediav1.MediaServiceClient, failed *prometheus.CounterVec, bucketName string) *MediaManager {
	return &MediaManager{
		db:           db,
		mediaClient:  mc,
		eventsFailed: failed,
		bucketName:   bucketName,
	}
}

func (m *MediaManager) PlayAudio(ctx context.Context, callState *state.CallState, audioURI string) {
	l := ctxlogger.FromContext(ctx)
	if callState.Event == nil || callState.Event.Media == nil {
		l.Error().Msg("PlayAudio için kritik medya bilgisi eksik (st.Event.Media is nil).")
		return
	}

	rtpTarget := callState.Event.Media.CallerRtpAddr
	serverPort := uint32(callState.Event.Media.ServerRtpPort)

	playReq := &mediav1.PlayAudioRequest{RtpTargetAddr: rtpTarget, ServerRtpPort: serverPort, AudioUri: audioURI}
	playCtx, playCancel := context.WithTimeout(metadata.AppendToOutgoingContext(ctx, "x-trace-id", callState.TraceID), 5*time.Minute)
	defer playCancel()
	_, err := m.mediaClient.PlayAudio(playCtx, playReq)
	if err != nil {
		stErr, ok := status.FromError(err)
		if ok && (stErr.Code() == codes.Canceled || stErr.Code() == codes.DeadlineExceeded) {
			l.Warn().Err(err).Msg("PlayAudio işlemi başka bir komutla veya zaman aşımı nedeniyle iptal edildi.")
		} else {
			l.Error().Err(err).Str("audio_uri_len", fmt.Sprintf("%d", len(audioURI))).Msg("Hata: Ses çalma komutu başarısız.")
			m.eventsFailed.WithLabelValues(callState.Event.EventType, "play_audio_failed").Inc()
		}
	} else {
		l.Debug().Str("audio_uri_type", audioURI[:20]).Msg("Ses çalındı ve tamamlandı.")
	}
}

func (m *MediaManager) PlayAnnouncement(ctx context.Context, callState *state.CallState, announcementID constants.AnnouncementID) {
	l := ctxlogger.FromContext(ctx)
	languageCode := GetLanguageCode(callState.Event)
	audioPath, err := database.GetAnnouncementPathFromDB(m.db, string(announcementID), callState.TenantID, languageCode)
	if err != nil {
		l.Error().Err(err).Str("announcement_id", string(announcementID)).Msg("Anons yolu alınamadı, fallback deneniyor")
		// DÜZELTME: Fallback için tenant_id 'system' ve dil 'en' olmalı.
		audioPath, err = database.GetAnnouncementPathFromDB(m.db, string(announcementID), "system", "en")
		if err != nil {
			l.Error().Err(err).Str("announcement_id", string(announcementID)).Msg("KRİTİK HATA: Sistem fallback anonsu dahi yüklenemedi.")
			return
		}
	}
	audioURI := fmt.Sprintf("file://%s", audioPath)
	m.PlayAudio(ctx, callState, audioURI)
}

func (m *MediaManager) StartRecording(ctx context.Context, callState *state.CallState) {
	l := ctxlogger.FromContext(ctx)

	if callState.Event == nil || callState.Event.Media == nil {
		l.Error().Msg("StartRecording için kritik medya bilgisi eksik (st.Event.Media is nil). Kayıt başlatılamıyor.")
		return
	}
	serverRtpPort := uint32(callState.Event.Media.ServerRtpPort)

	recordingTenantID := callState.TenantID
	recordingURI := fmt.Sprintf("s3://%s/%s/%s.wav", m.bucketName, recordingTenantID, callState.CallID)
	l.Info().Str("uri", recordingURI).Msg("Çağrı kaydı başlatılıyor...")
	startRecCtx, startRecCancel := context.WithTimeout(metadata.AppendToOutgoingContext(ctx, "x-trace-id", callState.TraceID), 10*time.Second)
	defer startRecCancel()

	_, err := m.mediaClient.StartRecording(startRecCtx, &mediav1.StartRecordingRequest{
		ServerRtpPort: serverRtpPort,
		OutputUri:     recordingURI,
		CallId:        callState.CallID,
		TraceId:       callState.TraceID,
	})
	if err != nil {
		l.Error().Err(err).Msg("Media-service'e kayıt başlatma komutu gönderilemedi.")
	}
}

func (m *MediaManager) StopRecording(ctx context.Context, callState *state.CallState) {
	l := ctxlogger.FromContext(ctx)

	if callState.Event == nil || callState.Event.Media == nil {
		l.Error().Msg("StopRecording için kritik medya bilgisi eksik (st.Event.Media is nil). Kayıt durdurulamıyor.")
		return
	}
	serverRtpPort := uint32(callState.Event.Media.ServerRtpPort)

	l.Info().Msg("Çağrı kaydı durduruluyor...")
	// ==================== HATA DÜZELTMESİ ====================
	stopRecCtx, stopRecCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopRecCancel() // Cancel fonksiyonunu çağırıyoruz
	// ==================== DÜZELTME SONU ====================

	mdCtx := metadata.AppendToOutgoingContext(stopRecCtx, "x-trace-id", callState.TraceID)

	_, err := m.mediaClient.StopRecording(mdCtx, &mediav1.StopRecordingRequest{
		ServerRtpPort: serverRtpPort,
	})
	if err != nil {
		l.Error().Err(err).Msg("Media-service'e kayıt durdurma komutu gönderilemedi.")
		m.eventsFailed.WithLabelValues(callState.Event.EventType, "stop_recording_failed").Inc()
	}
}
