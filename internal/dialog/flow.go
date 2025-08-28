// File: internal/dialog/flow.go

package dialog

import (
	"context"
	"strings"

	"github.com/sentiric/sentiric-agent-service/internal/state"
)

type DialogFunc func(context.Context, *Dependencies, *state.CallState) (*state.CallState, error)

var stateMap = map[state.DialogState]DialogFunc{
	state.StateWelcoming: StateFnWelcoming,
	state.StateListening: StateFnListening,
	state.StateThinking:  StateFnThinking,
	state.StateSpeaking:  StateFnSpeaking,
}

// TerminationRequest yapısını tanımla
type TerminationRequest struct {
	CallID string `json:"callId"`
}

func RunDialogLoop(ctx context.Context, deps *Dependencies, stateManager *state.Manager, initialSt *state.CallState) {
	currentCallID := initialSt.CallID
	l := deps.Log.With().Str("call_id", currentCallID).Str("trace_id", initialSt.TraceID).Logger()

	// --- YENİ: defer ile döngü bittiğinde çağrıyı sonlandırma isteği gönder ---
	defer func() {
		// Panic durumunda da çalışması için recover ekleyebiliriz ama şimdilik basit tutalım.
		finalState, err := stateManager.Get(context.Background(), currentCallID)
		if err != nil || finalState == nil {
			l.Error().Err(err).Msg("Döngü sonu durumu alınamadı, sonlandırma isteği gönderilemiyor.")
			return
		}

		// Sadece diyalog bizim tarafımızdan sonlandırıldıysa (TERMINATED) isteği gönder.
		// Eğer dışarıdan (call.ended ile) sonlandırıldıysa (ENDED), tekrar istek atmaya gerek yok.
		if finalState.CurrentState == state.StateTerminated {
			l.Info().Msg("Diyalog sonlandı, sip-signaling'e çağrıyı kapatma isteği gönderiliyor.")

			terminationReq := TerminationRequest{CallID: currentCallID}

			// Yeni oluşturduğumuz Publisher'ı kullanıyoruz.
			err := deps.Publisher.PublishJSON(context.Background(), "call.terminate.request", terminationReq)
			if err != nil {
				l.Error().Err(err).Msg("Çağrı sonlandırma isteği yayınlanamadı.")
			}
		}
	}()
	// --- DEĞİŞİKLİK SONU ---

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

		if st.CurrentState == state.StateEnded { // Sadece StateEnded kontrolü yeterli
			l.Info().Str("final_state", string(st.CurrentState)).Msg("Diyalog döngüsü dış bir olayla (call.ended) sonlandırıldı.")
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

		// Eğer bir state fonksiyonu doğrudan sonlandırma kararı verdiyse, döngüyü kır.
		if st.CurrentState == state.StateTerminated {
			l.Info().Msg("Durum 'Terminated' olarak ayarlandı, döngü sonlandırılıyor.")
			// Son durumu kaydetmeyi unutma!
			if err := stateManager.Set(ctx, st); err != nil {
				l.Error().Err(err).Msg("Son 'Terminated' durumu güncellenemedi.")
			}
			return
		}

		if err != nil {
			if err == context.Canceled || strings.Contains(err.Error(), "context canceled") {
				l.Warn().Msg("İşlem context iptali nedeniyle durduruldu. Döngü sonlanıyor.")
				return // Context iptal olduysa döngüden çık
			}
			l.Error().Err(err).Msg("Durum işlenirken hata oluştu, sonlandırma deneniyor.")
			PlayAnnouncement(deps, l, st, "ANNOUNCE_SYSTEM_ERROR")
			st.CurrentState = state.StateTerminated
			if err := stateManager.Set(ctx, st); err != nil {
				l.Error().Err(err).Msg("Hata sonrası 'Terminated' durumu güncellenemedi.")
			}
			return // Hata sonrası döngüden çık
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
