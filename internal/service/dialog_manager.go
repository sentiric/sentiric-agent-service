package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sentiric/sentiric-agent-service/internal/config"
	"github.com/sentiric/sentiric-agent-service/internal/ctxlogger"
	"github.com/sentiric/sentiric-agent-service/internal/queue"
	"github.com/sentiric/sentiric-agent-service/internal/state"
)

type DialogManager struct {
	cfg              *config.Config
	stateManager     *state.Manager
	aiOrchestrator   *AIOrchestrator
	mediaManager     *MediaManager
	templateProvider *TemplateProvider
	publisher        *queue.Publisher
}

func NewDialogManager(
	cfg *config.Config,
	sm *state.Manager,
	aio *AIOrchestrator,
	mm *MediaManager,
	tp *TemplateProvider,
	pub *queue.Publisher,
) *DialogManager {
	return &DialogManager{
		cfg:              cfg,
		stateManager:     sm,
		aiOrchestrator:   aio,
		mediaManager:     mm,
		templateProvider: tp,
		publisher:        pub,
	}
}

func (dm *DialogManager) Start(ctx context.Context, event *state.CallEvent) {
	l := ctxlogger.FromContext(ctx)

	// --- DÜZELTME (AGENT-BUG-01): Diyalog başlamadan önce kullanıcı kimliğini yayınla ---
	// Bu, CDR servisinin çağrıyı doğru kullanıcıya bağlaması için kritiktir.
	dm.publishUserIdentifiedEvent(ctx, event)

	tenantID := "sentiric_demo"
	if event.Dialplan.GetMatchedUser() != nil && event.Dialplan.GetMatchedUser().TenantId != "" {
		tenantID = event.Dialplan.GetMatchedUser().TenantId
	} else if event.Dialplan.GetInboundRoute() != nil && event.Dialplan.GetInboundRoute().TenantId != "" {
		tenantID = event.Dialplan.GetInboundRoute().TenantId
	}

	initialState := &state.CallState{
		CallID:              event.CallID,
		TraceID:             event.TraceID,
		TenantID:            tenantID,
		CurrentState:        state.StateWelcoming,
		Event:               event,
		Conversation:        []map[string]string{},
		ConsecutiveFailures: 0,
	}

	if err := dm.stateManager.Set(ctx, initialState); err != nil {
		l.Error().Err(err).Msg("Redis'e başlangıç durumu yazılamadı.")
		return
	}

	dm.runDialogLoop(ctx, initialState)
}

func (dm *DialogManager) publishUserIdentifiedEvent(ctx context.Context, event *state.CallEvent) {
	l := ctxlogger.FromContext(ctx)

	// Gelen olayda tanınmış bir kullanıcı ve ilgili iletişim bilgisi varsa, bunu sisteme bildir.
	if event.Dialplan.GetMatchedUser() != nil && event.Dialplan.GetMatchedContact() != nil {
		l.Info().Msg("Kullanıcı kimliği belirlendi, user.identified.for_call olayı yayınlanacak.")

		userIdentifiedPayload := struct {
			EventType string    `json:"eventType"`
			TraceID   string    `json:"traceId"`
			CallID    string    `json:"callId"`
			UserID    string    `json:"userId"`
			ContactID int32     `json:"contactId"`
			TenantID  string    `json:"tenantId"`
			Timestamp time.Time `json:"timestamp"`
		}{
			EventType: "user.identified.for_call",
			TraceID:   event.TraceID,
			CallID:    event.CallID,
			UserID:    event.Dialplan.GetMatchedUser().GetId(),
			ContactID: event.Dialplan.GetMatchedContact().GetId(),
			TenantID:  event.Dialplan.GetMatchedUser().GetTenantId(),
			Timestamp: time.Now().UTC(),
		}

		err := dm.publisher.PublishJSON(ctx, "user.identified.for_call", userIdentifiedPayload)
		if err != nil {
			l.Error().Err(err).Msg("user.identified.for_call olayı yayınlanamadı.")
		} else {
			l.Info().Msg("user.identified.for_call olayı başarıyla yayınlandı.")
		}
	} else {
		// Loglarda görülen uyarı. Bu durum, arayanın misafir olduğu anlamına gelir.
		l.Warn().Msg("Kullanıcı veya contact bilgisi eksik olduğu için user.identified.for_call olayı yayınlanamadı. Bu durum misafir arayanlar için normaldir.")
	}
}

func (dm *DialogManager) runDialogLoop(ctx context.Context, initialSt *state.CallState) {
	l := ctxlogger.FromContext(ctx)
	currentCallID := initialSt.CallID

	defer func() {
		dm.mediaManager.StopRecording(ctx, initialSt)

		finalState, err := dm.stateManager.Get(context.Background(), currentCallID)
		if err != nil || finalState == nil {
			l.Error().Err(err).Msg("Döngü sonu durumu alınamadı, sonlandırma isteği gönderilemiyor.")
			return
		}

		if finalState.CurrentState == state.StateTerminated {
			l.Info().Msg("Diyalog sonlandı, sip-signaling'e çağrıyı kapatma isteği gönderiliyor.")
			type TerminationRequest struct {
				EventType string    `json:"eventType"`
				CallID    string    `json:"callId"`
				Timestamp time.Time `json:"timestamp"`
			}
			terminationReq := TerminationRequest{
				EventType: "call.terminate.request",
				CallID:    currentCallID,
				Timestamp: time.Now().UTC(),
			}
			err := dm.publisher.PublishJSON(context.Background(), "call.terminate.request", terminationReq)
			if err != nil {
				l.Error().Err(err).Msg("Çağrı sonlandırma isteği yayınlanamadı.")
			}
		}
	}()

	actionName := initialSt.Event.Dialplan.Action.Action
	var initialAnnouncementID string
	if actionName == "PROCESS_GUEST_CALL" {
		initialAnnouncementID = "ANNOUNCE_GUEST_WELCOME"
	} else {
		initialAnnouncementID = "ANNOUNCE_SYSTEM_CONNECTING"
	}
	dm.mediaManager.PlayAnnouncement(ctx, initialSt, initialAnnouncementID)
	dm.mediaManager.StartRecording(ctx, initialSt)

	type DialogFunc func(context.Context, *state.CallState) (*state.CallState, error)
	stateMap := map[state.DialogState]DialogFunc{
		state.StateWelcoming: dm.stateFnWelcoming,
		state.StateListening: dm.stateFnListening,
		state.StateThinking:  dm.stateFnThinking,
		state.StateSpeaking:  dm.stateFnSpeaking,
	}

	for {
		select {
		case <-ctx.Done():
			l.Info().Msg("Context iptal edildi, diyalog döngüsü temiz bir şekilde sonlandırılıyor.")
			return
		default:
		}

		st, err := dm.stateManager.Get(ctx, currentCallID)
		if err != nil || st == nil {
			l.Error().Err(err).Msg("Döngü için durum Redis'ten alınamadı veya nil, döngü sonlandırılıyor.")
			return
		}

		if st.CurrentState == state.StateEnded || st.CurrentState == state.StateTerminated {
			l.Info().Str("final_state", string(st.CurrentState)).Msg("Diyalog döngüsü sonlandırıldı.")
			return
		}

		handlerFunc, ok := stateMap[st.CurrentState]
		if !ok {
			l.Error().Str("state", string(st.CurrentState)).Msg("Bilinmeyen durum, döngü sonlandırılıyor.")
			st.CurrentState = state.StateTerminated
		} else {
			l.Info().Str("state", string(st.CurrentState)).Msg("Diyalog döngüsü adımı işleniyor.")
			st, err = handlerFunc(ctx, st)
		}

		if err != nil {
			if err == context.Canceled || strings.Contains(err.Error(), "context canceled") {
				l.Warn().Msg("İşlem context iptali nedeniyle durduruldu. Döngü sonlanıyor.")
				st.CurrentState = state.StateEnded
			} else {
				l.Error().Err(err).Msg("Durum işlenirken hata oluştu, sonlandırma deneniyor.")
				dm.mediaManager.PlayAnnouncement(ctx, st, "ANNOUNCE_SYSTEM_ERROR")
				st.CurrentState = state.StateTerminated
			}
		}

		if err := dm.stateManager.Set(ctx, st); err != nil {
			l.Error().Err(err).Msg("Döngü içinde durum güncellenemedi, sonlandırılıyor.")
			return
		}
	}
}

func (dm *DialogManager) stateFnWelcoming(ctx context.Context, st *state.CallState) (*state.CallState, error) {
	l := ctxlogger.FromContext(ctx)
	l.Info().Msg("İlk AI yanıtı öncesi 1.5 saniye bekleniyor...")
	time.Sleep(1500 * time.Millisecond)

	prompt, err := dm.templateProvider.GetWelcomePrompt(ctx, st)
	if err != nil {
		return st, err
	}

	welcomeText, err := dm.aiOrchestrator.GenerateResponse(ctx, prompt, st)
	if err != nil {
		return st, err
	}

	st.Conversation = append(st.Conversation, map[string]string{"ai": welcomeText})

	audioURI, err := dm.aiOrchestrator.SynthesizeAndGetAudio(ctx, st, welcomeText)
	if err != nil {
		return st, err
	}
	dm.mediaManager.PlayAudio(ctx, st, audioURI)

	st.CurrentState = state.StateListening
	return st, nil
}

func (dm *DialogManager) stateFnListening(ctx context.Context, st *state.CallState) (*state.CallState, error) {
	l := ctxlogger.FromContext(ctx)
	if st.ConsecutiveFailures >= dm.cfg.AgentMaxConsecutiveFailures {
		l.Warn().Int("failures", st.ConsecutiveFailures).Int("max_failures", dm.cfg.AgentMaxConsecutiveFailures).Msg("Art arda çok fazla anlama hatası. Çağrı sonlandırılıyor.")
		dm.mediaManager.PlayAnnouncement(ctx, st, "ANNOUNCE_SYSTEM_MAX_FAILURES")
		st.CurrentState = state.StateTerminated
		return st, nil
	}

	l.Info().Msg("Kullanıcıdan ses bekleniyor (gerçek zamanlı akış modu)...")
	transcriptionResult, err := dm.aiOrchestrator.StreamAndTranscribe(ctx, st)
	if err != nil {
		dm.mediaManager.PlayAnnouncement(ctx, st, "ANNOUNCE_SYSTEM_ERROR")
		st.ConsecutiveFailures++
		st.CurrentState = state.StateListening
		return st, nil
	}

	if transcriptionResult.IsNoSpeechTimeout {
		l.Warn().Msg("STT'den ses algılanmadı (timeout). Kullanıcıya bir şans daha veriliyor.")
		dm.mediaManager.PlayAnnouncement(ctx, st, "ANNOUNCE_SYSTEM_CANT_HEAR_YOU")
		st.CurrentState = state.StateListening
		return st, nil
	}

	cleanedText := strings.TrimSpace(transcriptionResult.Text)
	isMeaningless := len(cleanedText) < 3 || strings.Contains(cleanedText, "Bu dizinin betimlemesi")

	if isMeaningless {
		l.Warn().Str("stt_result", cleanedText).Msg("STT anlamsız veya çok kısa metin döndürdü, 'anlayamadım' anonsu çalınacak.")
		dm.mediaManager.PlayAnnouncement(ctx, st, "ANNOUNCE_SYSTEM_CANT_UNDERSTAND")
		st.ConsecutiveFailures++
		st.CurrentState = state.StateListening
		return st, nil
	}

	st.ConsecutiveFailures = 0
	st.Conversation = append(st.Conversation, map[string]string{"user": cleanedText})
	st.CurrentState = state.StateThinking
	return st, nil
}

func (dm *DialogManager) stateFnThinking(ctx context.Context, st *state.CallState) (*state.CallState, error) {
	l := ctxlogger.FromContext(ctx)
	l.Info().Msg("LLM'den yanıt üretiliyor (RAG akışı)...")

	lastUserMessage := ""
	for i := len(st.Conversation) - 1; i >= 0; i-- {
		if msg, ok := st.Conversation[i]["user"]; ok {
			lastUserMessage = msg
			break
		}
	}
	if lastUserMessage == "" {
		return st, fmt.Errorf("düşünme durumu için son kullanıcı mesajı bulunamadı")
	}

	ragContext, err := dm.aiOrchestrator.QueryKnowledgeBase(ctx, lastUserMessage, st)
	if err != nil {
		return st, fmt.Errorf("knowledge base sorgulanamadı: %w", err)
	}

	prompt, err := dm.templateProvider.BuildLlmPrompt(ctx, st, ragContext)
	if err != nil {
		return st, fmt.Errorf("LLM prompt'u oluşturulamadı: %w", err)
	}

	llmRespText, err := dm.aiOrchestrator.GenerateResponse(ctx, prompt, st)
	if err != nil {
		return st, fmt.Errorf("LLM yanıtı üretilemedi: %w", err)
	}

	st.Conversation = append(st.Conversation, map[string]string{"ai": llmRespText})
	st.CurrentState = state.StateSpeaking
	return st, nil
}

func (dm *DialogManager) stateFnSpeaking(ctx context.Context, st *state.CallState) (*state.CallState, error) {
	l := ctxlogger.FromContext(ctx)
	lastAiMessage := st.Conversation[len(st.Conversation)-1]["ai"]
	l.Info().Str("text", lastAiMessage).Msg("AI yanıtı seslendiriliyor...")

	audioURI, err := dm.aiOrchestrator.SynthesizeAndGetAudio(ctx, st, lastAiMessage)
	if err != nil {
		return st, err
	}
	dm.mediaManager.PlayAudio(ctx, st, audioURI)

	time.Sleep(250 * time.Millisecond)
	st.CurrentState = state.StateListening
	return st, nil
}
