package handler

import (
	"context"
	"sync"
	"time"

	"github.com/sentiric/sentiric-agent-service/internal/ctxlogger"
	"github.com/sentiric/sentiric-agent-service/internal/service"
	"github.com/sentiric/sentiric-agent-service/internal/state"
)

type CallHandler struct {
	UserManager   *service.UserManager
	DialogManager *service.DialogManager
	StateManager  *state.Manager
	dialogWg      sync.WaitGroup
}

func NewCallHandler(um *service.UserManager, dm *service.DialogManager, sm *state.Manager) *CallHandler {
	return &CallHandler{
		UserManager:   um,
		DialogManager: dm,
		StateManager:  sm,
	}
}

func (h *CallHandler) WaitOnDialogs() {
	h.dialogWg.Wait()
}

func (h *CallHandler) HandleCallStarted(ctx context.Context, event *state.CallEvent) {
	h.dialogWg.Add(1)
	defer h.dialogWg.Done()

	l := ctxlogger.FromContext(ctx)

	lockKey := "active_agent_lock:" + event.CallID
	wasSet, err := h.StateManager.RedisClient().SetNX(context.Background(), lockKey, event.TraceID, 5*time.Minute).Result()
	if err != nil {
		l.Error().Err(err).Msg("Redis'e lock anahtarı yazılamadı.")
		return
	}

	if !wasSet {
		l.Warn().Msg("Bu çağrı için zaten aktif bir agent süreci var. Yinelenen 'call.started' olayı görmezden geliniyor.")
		return
	}

	if event.Dialplan == nil || event.Dialplan.Action == nil {
		l.Error().Msg("Dialplan veya action bilgisi eksik, çağrı işlenemiyor.")
		return
	}

	actionName := event.Dialplan.Action.Action
	l = l.With().Str("action", actionName).Logger()
	ctx = ctxlogger.ToContext(ctx, l)
	l.Info().Msg("Dialplan eylemine göre çağrı yönlendiriliyor.")

	switch actionName {
	case "PROCESS_GUEST_CALL":
		h.handleProcessGuestCall(ctx, event)
	case "START_AI_CONVERSATION":
		h.handleStartAIConversation(ctx, event)
	default:
		l.Error().Msg("Bilinmeyen veya desteklenmeyen dialplan eylemi.")
	}
}

func (h *CallHandler) HandleCallEnded(ctx context.Context, event *state.CallEvent) {
	l := ctxlogger.FromContext(ctx)
	l.Info().Msg("Çağrı sonlandırma olayı işleniyor.")

	lockKey := "active_agent_lock:" + event.CallID
	if err := h.StateManager.RedisClient().Del(ctx, lockKey).Err(); err != nil {
		l.Error().Err(err).Msg("Redis'ten lock anahtarı silinemedi.")
	} else {
		l.Info().Msg("Aktif agent lock'ı başarıyla temizlendi.")
	}

	stateKey := "callstate:" + event.CallID
	if err := h.StateManager.RedisClient().Del(ctx, stateKey).Err(); err != nil {
		l.Error().Err(err).Msg("Redis'ten 'callstate' anahtarı silinemedi.")
	} else {
		l.Info().Msg("Çağrı durumu 'callstate' kaydı Redis'ten başarıyla silindi.")
	}
}

func (h *CallHandler) handleProcessGuestCall(ctx context.Context, event *state.CallEvent) {
	l := ctxlogger.FromContext(ctx)
	l.Info().Msg("Misafir kullanıcı akışı başlatıldı: Önce bul, yoksa oluştur.")

	user, contact, err := h.UserManager.FindOrCreateGuest(ctx, event)
	if err != nil {
		l.Error().Err(err).Msg("Misafir kullanıcı bulunamadı veya oluşturulamadı.")
		return
	}

	event.Dialplan.MatchedUser = service.ConvertUserToPayload(user)
	event.Dialplan.MatchedContact = service.ConvertContactToPayload(contact)

	h.handleStartAIConversation(ctx, event)
}

func (h *CallHandler) handleStartAIConversation(ctx context.Context, event *state.CallEvent) {
	l := ctxlogger.FromContext(ctx)

	st, err := h.StateManager.Get(ctx, event.CallID)
	if err != nil {
		l.Error().Err(err).Msg("Redis'ten durum alınamadı.")
		return
	}
	if st != nil {
		l.Warn().Msg("Bu çağrı için zaten aktif bir Redis durumu var, yeni bir tane başlatılmıyor.")
		return
	}

	h.DialogManager.Start(ctx, event)
}
