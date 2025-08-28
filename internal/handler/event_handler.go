// File: internal/handler/event_handler.go
package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt" // playInitialAnnouncement için fmt eklendi
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/sentiric/sentiric-agent-service/internal/client"
	"github.com/sentiric/sentiric-agent-service/internal/config"
	"github.com/sentiric/sentiric-agent-service/internal/database" // playInitialAnnouncement için database eklendi
	"github.com/sentiric/sentiric-agent-service/internal/dialog"
	"github.com/sentiric/sentiric-agent-service/internal/state"
	mediav1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/media/v1"
	ttsv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/tts/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
	"google.golang.org/grpc/metadata"
)

// activeCallContexts global değişkeni tamamen kaldırıldı.

type EventHandler struct {
	stateManager    *state.Manager
	dialogDeps      *dialog.Dependencies
	log             zerolog.Logger
	eventsProcessed *prometheus.CounterVec
	eventsFailed    *prometheus.CounterVec
	userClient      userv1.UserServiceClient
}

func NewEventHandler(
	db *sql.DB,
	cfg *config.Config,
	sm *state.Manager,
	mc mediav1.MediaServiceClient,
	uc userv1.UserServiceClient,
	tc ttsv1.TextToSpeechServiceClient,
	llmC *client.LlmClient,
	sttC *client.SttClient,
	log zerolog.Logger,
	processed, failed *prometheus.CounterVec,
	sttSampleRate uint32,
) *EventHandler {
	dialogDeps := &dialog.Dependencies{
		DB:                  db,
		Config:              cfg,
		MediaClient:         mc,
		TTSClient:           tc,
		LLMClient:           llmC,
		STTClient:           sttC,
		Log:                 log,
		SttTargetSampleRate: sttSampleRate,
		EventsFailed:        failed,
	}
	return &EventHandler{
		stateManager:    sm,
		dialogDeps:      dialogDeps,
		log:             log,
		eventsProcessed: processed,
		eventsFailed:    failed,
		userClient:      uc,
	}
}

func (h *EventHandler) HandleRabbitMQMessage(body []byte) {
	var event state.CallEvent
	if err := json.Unmarshal(body, &event); err != nil {
		h.log.Error().Err(err).Bytes("raw_message", body).Msg("Hata: Mesaj JSON formatında değil")
		h.eventsFailed.WithLabelValues("unknown", "json_unmarshal").Inc()
		return
	}
	h.eventsProcessed.WithLabelValues(event.EventType).Inc()
	l := h.log.With().Str("call_id", event.CallID).Str("trace_id", event.TraceID).Str("event_type", event.EventType).Logger()
	l.Info().Msg("Olay alındı")

	switch event.EventType {
	case "call.started":
		// handleCallStarted artık kendi goroutine'ini yönetiyor
		h.handleCallStarted(l, &event)
	case "call.ended":
		// handleCallEnded kısa sürdüğü için goroutine içinde çağrılabilir
		go h.handleCallEnded(l, &event)
	}
}

func (h *EventHandler) handleCallStarted(l zerolog.Logger, event *state.CallEvent) {
	if event.Dialplan == nil || event.Dialplan.Action == nil {
		l.Error().Msg("Dialplan veya action bilgisi eksik, çağrı işlenemiyor.")
		h.eventsFailed.WithLabelValues(event.EventType, "missing_dialplan_action").Inc()
		return
	}

	actionName := event.Dialplan.Action.Action
	l = l.With().Str("action", actionName).Logger()
	l.Info().Msg("Dialplan eylemine göre çağrı yönlendiriliyor.")

	// Context tanımı buradan kaldırıldı. Her goroutine kendi context'ini yönetecek.

	switch actionName {
	case "PROCESS_GUEST_CALL":
		go h.handleProcessGuestCall(l, event)
	case "START_AI_CONVERSATION":
		go h.handleStartAIConversation(l, event)
	default:
		l.Error().Msg("Bilinmeyen veya desteklenmeyen dialplan eylemi.")
		h.eventsFailed.WithLabelValues(event.EventType, "unsupported_action").Inc()
	}
}

func (h *EventHandler) handleProcessGuestCall(l zerolog.Logger, event *state.CallEvent) {
	// Bu fonksiyon artık kendi context'ini oluşturuyor.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	l.Info().Msg("Misafir kullanıcı oluşturma akışı başlatıldı.")

	callerNumber := event.From
	if strings.Contains(callerNumber, "<") {
		parts := strings.Split(callerNumber, "<")
		if len(parts) > 1 {
			uriPart := strings.Split(parts[1], "@")[0]
			uriPart = strings.TrimPrefix(uriPart, "sip:")
			callerNumber = uriPart
		}
	}
	l.Info().Str("caller_number", callerNumber).Msg("Arayan numara parse edildi.")

	tenantID := "default"
	if event.Dialplan.GetInboundRoute() != nil {
		tenantID = event.Dialplan.GetInboundRoute().TenantId
	}

	createCtx, createCancel := context.WithTimeout(metadata.AppendToOutgoingContext(ctx, "x-trace-id", event.TraceID), 10*time.Second)
	defer createCancel()

	createUserReq := &userv1.CreateUserRequest{
		TenantId: tenantID,
		UserType: "caller",
		InitialContact: &userv1.CreateUserRequest_InitialContact{
			ContactType:  "phone",
			ContactValue: callerNumber,
		},
	}

	createdUser, err := h.userClient.CreateUser(createCtx, createUserReq)
	if err != nil {
		l.Error().Err(err).Msg("User-service'de misafir kullanıcı oluşturulamadı.")
		h.eventsFailed.WithLabelValues(event.EventType, "guest_user_creation_failed").Inc()
		// Hata durumunda, kullanıcıya bir hata anonsu çal ve akışı bitir.
		playInitialAnnouncement(ctx, h.dialogDeps, l, &state.CallState{Event: event, TenantID: tenantID, TraceID: event.TraceID}, "ANNOUNCE_SYSTEM_ERROR")
		return
	}

	l.Info().Str("user_id", createdUser.User.Id).Msg("Misafir kullanıcı başarıyla oluşturuldu.")
	event.Dialplan.MatchedUser = createdUser.User

	// Misafir oluşturulduktan sonra, asıl diyalog akışını başlat.
	h.handleStartAIConversation(l, event)
}

func (h *EventHandler) handleStartAIConversation(l zerolog.Logger, event *state.CallEvent) {
	// Bu fonksiyon, diyalog döngüsünün tüm ömrü boyunca yaşayacak olan ana context'i oluşturur.
	ctx, cancel := context.WithCancel(context.Background())

	// Diyalog döngüsü bittiğinde (ctx.Done() sinyali geldiğinde) context'i temizlemek
	// için bir goroutine başlatıyoruz. Bu, kaynak sızıntısını önler.
	go func() {
		<-ctx.Done() // Diyalog döngüsünün bitmesini bekle
		cancel()     // Kaynakları serbest bırak
		l.Info().Str("call_id", event.CallID).Msg("Diyalog context'i ve kaynakları başarıyla temizlendi.")
	}()

	st, err := h.stateManager.Get(ctx, event.CallID)
	if err != nil {
		l.Error().Err(err).Msg("Redis'ten durum alınamadı.")
		return
	}
	if st != nil {
		l.Warn().Msg("Bu çağrı için zaten aktif bir Redis durumu var, yeni bir tane başlatılmıyor.")
		return
	}

	tenantID := "default"
	if event.Dialplan != nil {
		if event.Dialplan.TenantId != "" {
			tenantID = event.Dialplan.TenantId
		} else if event.Dialplan.InboundRoute != nil {
			tenantID = event.Dialplan.InboundRoute.TenantId
		}
	}

	initialState := &state.CallState{
		CallID: event.CallID, TraceID: event.TraceID, TenantID: tenantID,
		CurrentState: state.StateWelcoming, Event: event, Conversation: []map[string]string{},
	}

	if err := h.stateManager.Set(ctx, initialState); err != nil {
		l.Error().Err(err).Msg("Redis'e başlangıç durumu yazılamadı.")
		return
	}

	// Kullanıcıya anında geri bildirim ver: "bağlanıyor" anonsu
	playInitialAnnouncement(ctx, h.dialogDeps, l, initialState, "ANNOUNCE_SYSTEM_CONNECTING")

	// Artık ana diyalog döngüsünü başlatabiliriz. Bu fonksiyon bloklayıcıdır ve
	// çağrı bitene kadar (veya hata alana kadar) çalışacaktır.
	dialog.RunDialogLoop(ctx, h.dialogDeps, h.stateManager, initialState)
}

func (h *EventHandler) handleCallEnded(l zerolog.Logger, event *state.CallEvent) {
	l.Info().Msg("Çağrı sonlandırma olayı işleniyor.")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	st, err := h.stateManager.Get(ctx, event.CallID)
	if err != nil {
		l.Error().Err(err).Msg("Sonlanan çağrı için durum Redis'ten alınamadı.")
		return
	}
	if st == nil {
		l.Warn().Msg("Sonlanan çağrı için aktif bir durum bulunamadı, işlem atlanıyor.")
		return
	}

	st.CurrentState = state.StateEnded
	if err := h.stateManager.Set(ctx, st); err != nil {
		l.Error().Err(err).Msg("Redis'e 'Ended' durumu yazılamadı.")
	} else {
		l.Info().Msg("Çağrı durumu Redis'te 'Ended' olarak güncellendi.")
	}
}

// Bu yardımcı fonksiyon, kullanıcıya anında bir sesli geri bildirim vermek için kullanılır.
func playInitialAnnouncement(ctx context.Context, deps *dialog.Dependencies, l zerolog.Logger, st *state.CallState, announcementID string) {
	// Bu fonksiyon, dialog/states.go'daki PlayAnnouncement'ın bir kopyasıdır.
	// Kod tekrarını önlemek için bu mantık gelecekte ortak bir yardımcı pakete taşınabilir.
	languageCode := "tr"
	if st.Event != nil && st.Event.Dialplan != nil && st.Event.Dialplan.GetInboundRoute() != nil {
		route := st.Event.Dialplan.GetInboundRoute()
		if route.DefaultLanguageCode != "" {
			languageCode = route.DefaultLanguageCode
		}
	}

	audioPath, err := database.GetAnnouncementPathFromDB(deps.DB, announcementID, st.TenantID, languageCode)
	if err != nil {
		l.Error().Err(err).Str("announcement_id", announcementID).Msg("Başlangıç anonsu yolu alınamadı")
		return
	}

	audioURI := fmt.Sprintf("file://%s", audioPath)
	mediaInfo := st.Event.Media
	rtpTargetVal, ok1 := mediaInfo["caller_rtp_addr"]
	serverPortVal, ok2 := mediaInfo["server_rtp_port"]

	if !ok1 || !ok2 {
		l.Error().Msg("Başlangıç anonsu için medya bilgileri eksik (caller_rtp_addr veya server_rtp_port)")
		return
	}

	rtpTarget, ok1 := rtpTargetVal.(string)
	serverPortFloat, ok2 := serverPortVal.(float64)

	if !ok1 || !ok2 {
		l.Error().Msg("Başlangıç anonsu için medya bilgileri geçersiz formatta.")
		return
	}

	serverPort := uint32(serverPortFloat)

	playCtx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(ctx, "x-trace-id", st.TraceID), 30*time.Second)
	defer cancel()

	playReq := &mediav1.PlayAudioRequest{RtpTargetAddr: rtpTarget, ServerRtpPort: serverPort, AudioUri: audioURI}

	_, err = deps.MediaClient.PlayAudio(playCtx, playReq)
	if err != nil {
		l.Error().Err(err).Str("announcement_id", announcementID).Msg("Başlangıç anonsu çalınamadı.")
	} else {
		l.Info().Str("announcement_id", announcementID).Msg("Başlangıç anonsu başarıyla çalındı.")
	}
}
