// File: internal/dialog/flow.go (TAM VE EKSİKSİZ SON HALİ)
package dialog

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sentiric/sentiric-agent-service/internal/state"
	mediav1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/media/v1"
	"google.golang.org/grpc/metadata"
)

type DialogFunc func(context.Context, *Dependencies, *state.CallState) (*state.CallState, error)

var stateMap = map[state.DialogState]DialogFunc{
	state.StateWelcoming: StateFnWelcoming,
	state.StateListening: StateFnListening,
	state.StateThinking:  StateFnThinking,
	state.StateSpeaking:  StateFnSpeaking,
}

type TerminationRequest struct {
	EventType string `json:"eventType"`
	CallID    string `json:"callId"`
}

func RunDialogLoop(ctx context.Context, deps *Dependencies, stateManager *state.Manager, initialSt *state.CallState) {
	currentCallID := initialSt.CallID
	l := deps.Log.With().Str("call_id", currentCallID).Str("trace_id", initialSt.TraceID).Logger()

	recordingTenantID := initialSt.TenantID

	recordingURI := fmt.Sprintf("s3://%s/%s_%s.wav",
		recordingTenantID,
		time.Now().UTC().Format("2006-01-02"),
		currentCallID,
	)
	l.Info().Str("uri", recordingURI).Msg("Çağrı kaydı başlatılıyor...")

	startRecCtx, startRecCancel := context.WithTimeout(metadata.AppendToOutgoingContext(ctx, "x-trace-id", initialSt.TraceID), 10*time.Second)

	_, err := deps.MediaClient.StartRecording(startRecCtx, &mediav1.StartRecordingRequest{
		ServerRtpPort: uint32(initialSt.Event.Media["server_rtp_port"].(float64)),
		OutputUri:     recordingURI,
		SampleRate:    &deps.SttTargetSampleRate,
		Format:        &[]string{"wav"}[0],
	})
	startRecCancel()

	if err != nil {
		l.Error().Err(err).Msg("Media-service'e kayıt başlatma komutu gönderilemedi. Diyalog kayıtsız devam edecek.")
		deps.EventsFailed.WithLabelValues(initialSt.Event.EventType, "start_recording_failed").Inc()
	}

	go func() {
		announcementCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		PlayAnnouncement(announcementCtx, deps, l, initialSt, "ANNOUNCE_SYSTEM_CONNECTING")
	}()

	defer func() {
		l.Info().Msg("Çağrı kaydı durduruluyor...")
		stopRecCtx, stopRecCancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-trace-id", initialSt.TraceID), 10*time.Second)
		defer stopRecCancel()

		_, err := deps.MediaClient.StopRecording(stopRecCtx, &mediav1.StopRecordingRequest{
			ServerRtpPort: uint32(initialSt.Event.Media["server_rtp_port"].(float64)),
		})

		if err != nil {
			l.Error().Err(err).Msg("Media-service'e kayıt durdurma komutu gönderilemedi.")
			deps.EventsFailed.WithLabelValues(initialSt.Event.EventType, "stop_recording_failed").Inc()
		}

		finalState, err := stateManager.Get(context.Background(), currentCallID)
		if err != nil || finalState == nil {
			l.Error().Err(err).Msg("Döngü sonu durumu alınamadı, sonlandırma isteği gönderilemiyor.")
			return
		}

		if finalState.CurrentState == state.StateTerminated {
			l.Info().Msg("Diyalog sonlandı, sip-signaling'e çağrıyı kapatma isteği gönderiliyor.")
			terminationReq := TerminationRequest{
				EventType: "call.terminate.request",
				CallID:    currentCallID,
			}
			err := deps.Publisher.PublishJSON(context.Background(), "call.terminate.request", terminationReq)
			if err != nil {
				l.Error().Err(err).Msg("Çağrı sonlandırma isteği yayınlanamadı.")
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			l.Info().Msg("Context iptal edildi, diyalog döngüsü temiz bir şekilde sonlandırılıyor.")
			return
		default:
		}

		st, err := stateManager.Get(ctx, currentCallID)
		if err != nil || st == nil {
			l.Error().Err(err).Msg("Döngü için durum Redis'ten alınamadı veya nil, döngü sonlandırılıyor.")
			return
		}

		if st.CurrentState == state.StateEnded {
			l.Info().Str("final_state", string(st.CurrentState)).Msg("Diyalog döngüsü dış bir olayla (call.ended) sonlandırıldı.")
			return
		}

		if st.CurrentState == state.StateTerminated {
			l.Info().Msg("Durum 'Terminated' olarak ayarlandı, döngü sonlandırılıyor.")
			return
		}

		handlerFunc, ok := stateMap[st.CurrentState]
		if !ok {
			l.Error().Str("state", string(st.CurrentState)).Msg("Bilinmeyen durum, döngü sonlandırılıyor.")
			st.CurrentState = state.StateTerminated
		} else {
			l.Info().Str("state", string(st.CurrentState)).Msg("Diyalog döngüsü adımı işleniyor.")
			st, err = handlerFunc(ctx, deps, st)
		}

		if st.CurrentState == state.StateTerminated {
			l.Info().Msg("Durum 'Terminated' olarak ayarlandı, döngü sonlandırılıyor.")
			if err := stateManager.Set(ctx, st); err != nil {
				l.Error().Err(err).Msg("Son 'Terminated' durumu güncellenemedi.")
			}
			return
		}

		if err != nil {
			if err == context.Canceled || strings.Contains(err.Error(), "context canceled") {
				l.Warn().Msg("İşlem context iptali nedeniyle durduruldu. Döngü sonlanıyor.")
				return
			}
			l.Error().Err(err).Msg("Durum işlenirken hata oluştu, sonlandırma deneniyor.")
			PlayAnnouncement(ctx, deps, l, st, "ANNOUNCE_SYSTEM_ERROR")
			st.CurrentState = state.StateTerminated
			if err := stateManager.Set(ctx, st); err != nil {
				l.Error().Err(err).Msg("Hata sonrası 'Terminated' durumu güncellenemedi.")
			}
			return
		}

		if err := stateManager.Set(ctx, st); err != nil {
			if err == context.Canceled {
				l.Warn().Msg("setCallState sırasında context iptal edildi, normal sonlanma.")
			} else {
				l.Error().Err(err).Msg("Döngü içinde durum güncellenemedi, sonlandırılıyor.")
			}
			return
		}
	}
}
