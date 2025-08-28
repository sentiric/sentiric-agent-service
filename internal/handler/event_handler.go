// File: internal/handler/event_handler.go
package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/sentiric/sentiric-agent-service/internal/client"
	"github.com/sentiric/sentiric-agent-service/internal/config"
	"github.com/sentiric/sentiric-agent-service/internal/database"
	"github.com/sentiric/sentiric-agent-service/internal/dialog"
	"github.com/sentiric/sentiric-agent-service/internal/state"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"

	mediav1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/media/v1"
	ttsv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/tts/v1"

	"google.golang.org/grpc/metadata"
)

// var activeCallContexts = struct {
// 	sync.RWMutex
// 	m map[string]context.CancelFunc
// }{m: make(map[string]context.CancelFunc)}

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
		go h.handleCallStarted(l, &event)
	case "call.ended":
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

	// ctx, cancel := context.WithCancel(context.Background())
	// activeCallContexts.Lock()
	// if _, exists := activeCallContexts.m[event.CallID]; exists {
	// 	l.Warn().Msg("Bu çağrı için zaten aktif bir context var, yeni goroutine başlatılmıyor.")
	// 	activeCallContexts.Unlock()
	// 	cancel()
	// 	return
	// }
	// activeCallContexts.m[event.CallID] = cancel
	// activeCallContexts.Unlock()
	// defer func() {
	// 	activeCallContexts.Lock()
	// 	delete(activeCallContexts.m, event.CallID)
	// 	activeCallContexts.Unlock()
	// 	cancel()
	// 	l.Info().Msg("Çağrı context'i temizlendi.")
	// }()

	// Bunun yerine basit bir context kullanın.
	// Her handler kendi kısa ömürlü context'ini yönetmeli.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute) // Örneğin 2 dk'lık genel timeout
	defer cancel()

	switch actionName {
	case "PROCESS_GUEST_CALL":
		// Artık handleProcessGuestCall'a goroutine'siz, doğrudan context'i vererek çağırın.
		h.handleProcessGuestCall(ctx, l, event)
	case "START_AI_CONVERSATION":
		// Artık handleStartAIConversation'a goroutine'siz, doğrudan context'i vererek çağırın.
		h.handleStartAIConversation(ctx, l, event)
	default:
		l.Error().Msg("Bilinmeyen veya desteklenmeyen dialplan eylemi.")
		h.eventsFailed.WithLabelValues(event.EventType, "unsupported_action").Inc()
	}
}

func (h *EventHandler) handleProcessGuestCall(ctx context.Context, l zerolog.Logger, event *state.CallEvent) {
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

	createCtx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(ctx, "x-trace-id", event.TraceID), 10*time.Second)
	defer cancel()

	// DÜZELTME: CreateUserRequest yapısı, `user.proto` v1.8.3 ile tam uyumlu hale getirildi.
	createUserReq := &userv1.CreateUserRequest{
		TenantId: tenantID,
		UserType: "caller",
		InitialContact: &userv1.CreateUserRequest_InitialContact{ // Doğru alan adı ve iç içe tip
			ContactType:  "phone",      // Doğru alan adı
			ContactValue: callerNumber, // Doğru alan adı
		},
	}

	createdUser, err := h.userClient.CreateUser(createCtx, createUserReq)
	if err != nil {
		l.Error().Err(err).Msg("User-service'de misafir kullanıcı oluşturulamadı.")
		h.eventsFailed.WithLabelValues(event.EventType, "guest_user_creation_failed").Inc()
		return
	}

	l.Info().Str("user_id", createdUser.User.Id).Msg("Misafir kullanıcı başarıyla oluşturuldu.")

	event.Dialplan.MatchedUser = createdUser.User

	h.handleStartAIConversation(ctx, l, event)
}

func (h *EventHandler) handleStartAIConversation(ctx context.Context, l zerolog.Logger, event *state.CallEvent) {
	st, err := h.stateManager.Get(ctx, event.CallID)
	if err != nil {
		if ctx.Err() == context.Canceled {
			l.Info().Msg("handleStartAIConversation context iptal edildiği için durduruldu.")
			return
		}
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
		if ctx.Err() == context.Canceled {
			l.Info().Msg("Başlangıç durumu yazılırken context iptal edildi.")
			return
		}
		l.Error().Err(err).Msg("Redis'e başlangıç durumu yazılamadı.")
		return
	}

	// --- YENİ ADIM: ANINDA GERİ BİLDİRİM ---
	l.Info().Msg("Kullanıcıya 'bağlanıyor' anonsu çalınıyor...")
	// `PlayAnnouncement` fonksiyonunu dialog paketinden handler'a taşıyabilir veya
	// burada geçici olarak yeniden yazabiliriz.
	// Bu, AI düşünürken kullanıcının hatta olduğunu anlamasını sağlar.
	playInitialAnnouncement(ctx, h.dialogDeps, l, initialState, "ANNOUNCE_SYSTEM_CONNECTING")
	// --- BİTTİ ---

	// Ana diyalog döngüsünü bir goroutine içinde başlat
	go dialog.RunDialogLoop(ctx, h.dialogDeps, h.stateManager, initialState)
}

// Bu yardımcı fonksiyonu event_handler.go dosyasının sonuna ekleyin
func playInitialAnnouncement(ctx context.Context, deps *dialog.Dependencies, l zerolog.Logger, st *state.CallState, announcementID string) {
	// Bu fonksiyon, dialog/states.go'daki PlayAnnouncement'ın bir kopyasıdır.
	// Kod tekrarını önlemek için bu mantığı ortak bir yere taşımak en iyisidir.
	languageCode := "tr" // Varsayılan veya event'ten gelen dil
	if st.Event.Dialplan != nil && st.Event.Dialplan.GetInboundRoute() != nil {
		languageCode = st.Event.Dialplan.GetInboundRoute().DefaultLanguageCode
	}

	audioPath, err := database.GetAnnouncementPathFromDB(deps.DB, announcementID, st.TenantID, languageCode)
	if err != nil {
		l.Error().Err(err).Str("announcement_id", announcementID).Msg("Başlangıç anonsu yolu alınamadı")
		return
	}

	audioURI := fmt.Sprintf("file://%s", audioPath)
	mediaInfo := st.Event.Media
	rtpTarget := mediaInfo["caller_rtp_addr"].(string)
	serverPort := uint32(mediaInfo["server_rtp_port"].(float64))

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

// handleCallEnded fonksiyonunu GÜNCELLEYİN:
func (h *EventHandler) handleCallEnded(l zerolog.Logger, event *state.CallEvent) {
	l.Info().Msg("Çağrı sonlandırma olayı işleniyor.")

	// Context'i arka planda oluştur, çünkü bu işlem kısa sürecek.
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

	// Sadece durumu güncelle. Aktif olan goroutine bunu bir sonraki döngüde görecek.
	st.CurrentState = state.StateEnded
	if err := h.stateManager.Set(ctx, st); err != nil {
		l.Error().Err(err).Msg("Redis'e 'Ended' durumu yazılamadı.")
	} else {
		l.Info().Msg("Çağrı durumu Redis'te 'Ended' olarak güncellendi.")
	}
}
