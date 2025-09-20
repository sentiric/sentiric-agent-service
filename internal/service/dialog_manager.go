// sentiric-agent-service/internal/service/dialog_manager.go
package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sentiric/sentiric-agent-service/internal/config"
	"github.com/sentiric/sentiric-agent-service/internal/constants"
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
	dm.publishUserIdentifiedEvent(ctx, event)
	tenantID := "sentiric_demo"
	if event.Dialplan != nil {
		if event.Dialplan.MatchedUser != nil && event.Dialplan.MatchedUser.TenantID != "" {
			tenantID = event.Dialplan.MatchedUser.TenantID
		} else if event.Dialplan.TenantID != "" {
			tenantID = event.Dialplan.TenantID
		}
	}
	initialState := &state.CallState{
		CallID:              event.CallID,
		TraceID:             event.TraceID,
		TenantID:            tenantID,
		CurrentState:        constants.StateWelcoming,
		Event:               event,
		Conversation:        []map[string]string{},
		ConsecutiveFailures: 0,
	}
	if err := dm.stateManager.Set(ctx, initialState); err != nil {
		l.Error().Err(err).Msg("Redis'e başlangıç durumu yazılamadı.")
		return
	}
	go dm.runDialogLoop(ctx, initialState)
}

func (dm *DialogManager) publishUserIdentifiedEvent(ctx context.Context, event *state.CallEvent) {
	l := ctxlogger.FromContext(ctx)
	if event.Dialplan != nil && event.Dialplan.MatchedUser != nil && event.Dialplan.MatchedContact != nil {
		l.Info().Str("user_id", event.Dialplan.MatchedUser.ID).Msg("Kullanıcı kimliği belirlendi, olay yayınlanıyor.")
		user := event.Dialplan.MatchedUser
		contact := event.Dialplan.MatchedContact
		userIdentifiedPayload := struct {
			EventType string    `json:"eventType"`
			TraceID   string    `json:"traceId"`
			CallID    string    `json:"callId"`
			UserID    string    `json:"userId"`
			ContactID int32     `json:"contactId"`
			TenantID  string    `json:"tenantId"`
			Timestamp time.Time `json:"timestamp"`
		}{
			EventType: string(constants.EventTypeUserIdentifiedForCall),
			TraceID:   event.TraceID,
			CallID:    event.CallID,
			UserID:    user.ID,
			ContactID: contact.ID,
			TenantID:  user.TenantID,
			Timestamp: time.Now().UTC(),
		}
		err := dm.publisher.PublishJSON(ctx, string(constants.EventTypeUserIdentifiedForCall), userIdentifiedPayload)
		if err != nil {
			l.Error().Err(err).Msg("user.identified.for_call olayı yayınlanamadı.")
		} else {
			l.Debug().Msg("user.identified.for_call olayı başarıyla yayınlandı.")
		}
	} else {
		l.Info().Msg("Misafir araması, user.identified.for_call olayı yayınlanmıyor.")
	}
}

func (dm *DialogManager) runDialogLoop(ctx context.Context, initialSt *state.CallState) {
	l := ctxlogger.FromContext(ctx)
	currentCallID := initialSt.CallID

	dm.mediaManager.StartRecording(ctx, initialSt)

	defer func() {
		dm.mediaManager.StopRecording(context.Background(), initialSt)
		l.Info().Msg("Diyalog döngüsü sonlandı, kaynaklar temizleniyor.")
	}()

	type DialogFunc func(context.Context, *state.CallState) (*state.CallState, error)
	stateMap := map[constants.DialogState]DialogFunc{
		constants.StateWelcoming: dm.stateFnWelcoming,
		constants.StateListening: dm.stateFnListening,
		constants.StateThinking:  dm.stateFnThinking,
		constants.StateSpeaking:  dm.stateFnSpeaking,
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
		if st.CurrentState == constants.StateEnded || st.CurrentState == constants.StateTerminated {
			l.Info().Str("final_state", string(st.CurrentState)).Msg("Diyalog döngüsü sonlandırıldı.")
			return
		}
		handlerFunc, ok := stateMap[st.CurrentState]
		if !ok {
			l.Error().Str("state", string(st.CurrentState)).Msg("Bilinmeyen durum, döngü sonlandırılıyor.")
			st.CurrentState = constants.StateTerminated
		} else {
			l.Info().Str("state", string(st.CurrentState)).Msg("Diyalog durumuna giriliyor.")
			st, err = handlerFunc(ctx, st)
		}
		if err != nil {
			if err == context.Canceled || strings.Contains(err.Error(), "context canceled") {
				l.Warn().Msg("İşlem context iptali nedeniyle durduruldu. Döngü sonlanıyor.")
				st.CurrentState = constants.StateEnded
			} else {
				l.Error().Err(err).Msg("Durum işlenirken hata oluştu, sonlandırma deneniyor.")
				dm.mediaManager.PlayAnnouncement(ctx, st, constants.AnnounceSystemError)
				st.CurrentState = constants.StateTerminated
				dm.publishTerminationRequest(ctx, st.CallID)
			}
		}
		if err := dm.stateManager.Set(ctx, st); err != nil {
			l.Error().Err(err).Msg("Döngü içinde durum güncellenemedi, sonlandırılıyor.")
			return
		}
	}
}

func (dm *DialogManager) publishTerminationRequest(ctx context.Context, callID string) {
	l := ctxlogger.FromContext(ctx)
	l.Info().Msg("Çağrıyı kapatma isteği gönderiliyor...")
	type TerminationRequest struct {
		EventType string    `json:"eventType"`
		CallID    string    `json:"callId"`
		Timestamp time.Time `json:"timestamp"`
	}
	terminationReq := TerminationRequest{
		EventType: string(constants.EventTypeCallTerminateRequest),
		CallID:    callID,
		Timestamp: time.Now().UTC(),
	}
	err := dm.publisher.PublishJSON(ctx, string(constants.EventTypeCallTerminateRequest), terminationReq)
	if err != nil {
		l.Error().Err(err).Msg("Çağrı sonlandırma isteği yayınlanamadı.")
	}
}

func (dm *DialogManager) stateFnWelcoming(ctx context.Context, st *state.CallState) (*state.CallState, error) {
	l := ctxlogger.FromContext(ctx)
	l.Info().Msg("İlk AI karşılama yanıtı üretiliyor...")
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
	st.CurrentState = constants.StateListening
	return st, nil
}

func (dm *DialogManager) stateFnListening(ctx context.Context, st *state.CallState) (*state.CallState, error) {
	l := ctxlogger.FromContext(ctx)
	if st.ConsecutiveFailures >= dm.cfg.AgentMaxConsecutiveFailures {
		l.Warn().Int("failures", st.ConsecutiveFailures).Int("max_failures", dm.cfg.AgentMaxConsecutiveFailures).Msg("Art arda çok fazla anlama hatası. Çağrı sonlandırılıyor.")
		dm.mediaManager.PlayAnnouncement(ctx, st, constants.AnnounceSystemMaxFailures)
		dm.publishTerminationRequest(ctx, st.CallID)
		st.CurrentState = constants.StateTerminated
		return st, nil
	}
	l.Info().Msg("Kullanıcıdan ses bekleniyor (gerçek zamanlı akış modu)...")
	transcriptionResult, err := dm.aiOrchestrator.StreamAndTranscribe(ctx, st)
	if err != nil {
		dm.mediaManager.PlayAnnouncement(ctx, st, constants.AnnounceSystemError)
		st.ConsecutiveFailures++
		st.CurrentState = constants.StateListening
		return st, nil
	}
	if transcriptionResult.IsNoSpeechTimeout {
		l.Warn().Msg("STT'den ses algılanmadı (timeout). Kullanıcıya bir şans daha veriliyor.")
		dm.mediaManager.PlayAnnouncement(ctx, st, constants.AnnounceSystemCantHearYou)
		st.CurrentState = constants.StateListening
		return st, nil
	}
	cleanedText := strings.TrimSpace(transcriptionResult.Text)
	isMeaningless := len(cleanedText) < 3 || strings.Contains(cleanedText, "Bu dizinin betimlemesi")
	if isMeaningless {
		l.Warn().Str("stt_result", cleanedText).Msg("STT anlamsız veya çok kısa metin döndürdü, 'anlayamadım' anonsu çalınacak.")
		dm.mediaManager.PlayAnnouncement(ctx, st, constants.AnnounceSystemCantUnderstand)
		st.ConsecutiveFailures++
		st.CurrentState = constants.StateListening
		return st, nil
	}
	l.Info().Str("transcribed_text", cleanedText).Msg("Kullanıcıdan gelen ses metne çevrildi.")
	st.ConsecutiveFailures = 0
	st.Conversation = append(st.Conversation, map[string]string{"user": cleanedText})
	st.CurrentState = constants.StateThinking
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

	if dm.detectTerminationIntent(lastUserMessage) {
		l.Info().Str("user_message", lastUserMessage).Msg("Sonlandırma niyeti algılandı. Veda ediliyor ve çağrı sonlandırılıyor.")

		dm.mediaManager.PlayAnnouncement(ctx, st, constants.AnnounceSystemGoodbye)

		dm.publishTerminationRequest(ctx, st.CallID)

		st.CurrentState = constants.StateTerminated
		return st, nil
	}

	var ragContext string
	var err error

	if dm.shouldTriggerRAG(lastUserMessage) {
		ragContext, err = dm.aiOrchestrator.QueryKnowledgeBase(ctx, lastUserMessage, st)
		if err != nil {
			return st, fmt.Errorf("knowledge base sorgulanamadı: %w", err)
		}
	} else {
		l.Debug().Str("user_message", lastUserMessage).Msg("Basit niyet algılandı, RAG sorgusu atlanıyor.")
	}

	prompt, err := dm.templateProvider.BuildLlmPrompt(ctx, st, ragContext)
	if err != nil {
		return st, fmt.Errorf("LLM prompt'u oluşturulamadı: %w", err)
	}

	llmRespText, err := dm.aiOrchestrator.GenerateResponse(ctx, prompt, st)
	if err != nil {
		return st, fmt.Errorf("LLM yanıtı üretilemedi: %w", err)
	}

	l.Info().Str("llm_response", llmRespText).Msg("LLM yanıtı başarıyla üretildi.")
	st.Conversation = append(st.Conversation, map[string]string{"ai": llmRespText})
	st.CurrentState = constants.StateSpeaking
	return st, nil
}

func (dm *DialogManager) detectTerminationIntent(text string) bool {
	lowerText := strings.ToLower(text)
	terminationKeywords := []string{
		"kapat", "kapatıyorum", "görüşürüz", "hoşça kal", "bay bay",
		"yeterli", "tamamdır", "teşekkürler", "teşekkür ederim",
	}

	for _, keyword := range terminationKeywords {
		if strings.Contains(lowerText, keyword) {
			return true
		}
	}
	return false
}

func (dm *DialogManager) shouldTriggerRAG(text string) bool {
	lowerText := strings.ToLower(text)
	nonRagKeywords := []string{
		"merhaba", "selam", "alo", "iyi günler",
	}
	for _, keyword := range nonRagKeywords {
		if strings.Contains(lowerText, keyword) {
			if len(strings.Fields(lowerText)) <= 3 {
				return false
			}
		}
	}
	questionKeywords := []string{
		"nedir", "nasıl", "ne kadar", "hangi", "kimdir", "nerede",
		"bilgi alabilir miyim", "anlatır mısın",
	}
	for _, keyword := range questionKeywords {
		if strings.Contains(lowerText, keyword) {
			return true
		}
	}
	if len(strings.Fields(lowerText)) > 3 {
		return true
	}
	return false
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
	st.CurrentState = constants.StateListening
	return st, nil
}
