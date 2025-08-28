// File: internal/dialog/flow.go
package dialog

import (
	"context"
	"strings"

	"github.com/sentiric/sentiric-agent-service/internal/state"
)

// DialogFunc, bir diyalog durumunu işleyen fonksiyonun tipini tanımlar.
type DialogFunc func(context.Context, *Dependencies, *state.CallState) (*state.CallState, error)

// stateMap, DialogState'leri ilgili işleyici fonksiyonlarla eşleştirir.
var stateMap = map[state.DialogState]DialogFunc{
	state.StateWelcoming: StateFnWelcoming,
	state.StateListening: StateFnListening,
	state.StateThinking:  StateFnThinking,
	state.StateSpeaking:  StateFnSpeaking,
}

// RunDialogLoop, bir çağrının durum makinesini yöneten ana döngüdür.
func RunDialogLoop(ctx context.Context, deps *Dependencies, stateManager *state.Manager, initialSt *state.CallState) {
	currentCallID := initialSt.CallID
	l := deps.Log.With().Str("call_id", currentCallID).Str("trace_id", initialSt.TraceID).Logger()

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

		// BU BLOKU EKLEYİN:
		// Eğer başka bir yerden (handleCallEnded gibi) durum 'Ended' olarak set edildiyse,
		// döngüyü temiz bir şekilde sonlandır.
		if st.CurrentState == state.StateEnded || st.CurrentState == state.StateTerminated {
			l.Info().Str("final_state", string(st.CurrentState)).Msg("Diyalog döngüsü sonlandırma durumuna ulaştı.")
			return
		}

		if st.CurrentState == state.StateEnded || st.CurrentState == state.StateTerminated {
			l.Info().Str("final_state", string(st.CurrentState)).Msg("Diyalog döngüsü sonlandırma durumuna ulaştı.")
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

		if err != nil {
			if err == context.Canceled || strings.Contains(err.Error(), "context canceled") {
				l.Warn().Msg("İşlem context iptali nedeniyle durduruldu. Döngü sonlanıyor.")
				continue
			}
			l.Error().Err(err).Msg("Durum işlenirken hata oluştu, sonlandırma deneniyor.")
			// DÜZELTME: 'true' parametresi kaldırıldı.
			PlayAnnouncement(deps, l, st, "ANNOUNCE_SYSTEM_ERROR")
			st.CurrentState = state.StateTerminated
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
