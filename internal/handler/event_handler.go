// File: internal/handler/event_handler.go
package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	// <-- YENİ: redis importu eklendi
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/sentiric/sentiric-agent-service/internal/client"
	"github.com/sentiric/sentiric-agent-service/internal/config"
	"github.com/sentiric/sentiric-agent-service/internal/database"
	"github.com/sentiric/sentiric-agent-service/internal/dialog"
	"github.com/sentiric/sentiric-agent-service/internal/queue"
	"github.com/sentiric/sentiric-agent-service/internal/state"
	mediav1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/media/v1"
	ttsv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/tts/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type EventHandler struct {
	stateManager    *state.Manager
	publisher       *queue.Publisher
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
	pub *queue.Publisher,
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
		Publisher:           pub,
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
		publisher:       pub,
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
		h.handleCallStarted(l, &event)
	case "call.ended":
		go h.handleCallEnded(l, &event)
	}
}

func (h *EventHandler) handleCallStarted(l zerolog.Logger, event *state.CallEvent) {
	// --- YENİ: Yinelenen Çağrı İşlemesini Engelleme (DISTRIBUTED LOCK) ---
	lockKey := "active_agent_lock:" + event.CallID
	// SetNX (Set if Not Exists) atomik bir operasyondur.
	// Bu anahtarı ilk oluşturan true alır, diğerleri false.
	// 5 dakikalık bir TTL (Time To Live) ekleyerek, olası bir çökme durumunda
	// anahtarın sonsuza dek Redis'te kalmasını engelliyoruz.
	wasSet, err := h.stateManager.RedisClient().SetNX(context.Background(), lockKey, event.TraceID, 5*time.Minute).Result()
	if err != nil {
		l.Error().Err(err).Msg("Redis'e lock anahtarı yazılamadı.")
		return
	}

	if !wasSet {
		// Bu anahtar zaten vardı, yani başka bir işlem bu çağrıyı yönetiyor.
		l.Warn().Msg("Bu çağrı için zaten aktif bir agent süreci var. Yinelenen 'call.started' olayı görmezden geliniyor.")
		return
	}
	// --- KİLİTLEME MANTIĞI SONU ---

	if event.Dialplan == nil || event.Dialplan.Action == nil {
		l.Error().Msg("Dialplan veya action bilgisi eksik, çağrı işlenemiyor.")
		h.eventsFailed.WithLabelValues(event.EventType, "missing_dialplan_action").Inc()
		return
	}

	actionName := event.Dialplan.Action.Action
	l = l.With().Str("action", actionName).Logger()
	l.Info().Msg("Dialplan eylemine göre çağrı yönlendiriliyor.")

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

// =============================================================
// === BU FONKSİYON TAMAMEN YENİLENDİ ===
// =============================================================
func (h *EventHandler) handleProcessGuestCall(l zerolog.Logger, event *state.CallEvent) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	l.Info().Msg("Misafir kullanıcı akışı başlatıldı: Önce bul, yoksa oluştur.")

	callerNumber := event.From
	if strings.Contains(callerNumber, "<") {
		parts := strings.Split(callerNumber, "<")
		if len(parts) > 1 {
			uriPart := strings.Split(parts[1], "@")[0]
			uriPart = strings.TrimPrefix(uriPart, "sip:")
			callerNumber = uriPart
		}
	}
	l = l.With().Str("caller_number", callerNumber).Logger()
	l.Info().Msg("Arayan numara parse edildi.")

	tenantID := "default"
	if event.Dialplan.GetInboundRoute() != nil {
		tenantID = event.Dialplan.GetInboundRoute().TenantId
	}

	// 1. ADIM: Önce kullanıcıyı bu numarayla bulmayı dene
	findCtx, findCancel := context.WithTimeout(metadata.AppendToOutgoingContext(ctx, "x-trace-id", event.TraceID), 10*time.Second)
	defer findCancel()

	findUserReq := &userv1.FindUserByContactRequest{
		ContactType:  "phone",
		ContactValue: callerNumber,
	}

	foundUserRes, err := h.userClient.FindUserByContact(findCtx, findUserReq)

	if err == nil && foundUserRes.User != nil {
		// KULLANICI BULUNDU!
		l.Info().Str("user_id", foundUserRes.User.Id).Msg("Mevcut kullanıcı bulundu, oluşturma adımı atlanıyor.")
		event.Dialplan.MatchedUser = foundUserRes.User
		// Kullanıcının arama yaptığı contact bilgisini de bulup ekleyelim.
		for _, contact := range foundUserRes.User.Contacts {
			if contact.ContactValue == callerNumber {
				event.Dialplan.MatchedContact = contact
				break
			}
		}

	} else {
		// KULLANICI BULUNAMADI, şimdi oluştur.
		st, _ := status.FromError(err)
		if st.Code() == codes.NotFound {
			l.Info().Msg("Kullanıcı bulunamadı, yeni bir misafir kullanıcı oluşturulacak.")

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

			createdUserRes, createErr := h.userClient.CreateUser(createCtx, createUserReq)
			if createErr != nil {
				l.Error().Err(createErr).Msg("User-service'de misafir kullanıcı oluşturulamadı.")
				h.eventsFailed.WithLabelValues(event.EventType, "guest_user_creation_failed").Inc()
				playInitialAnnouncement(ctx, h.dialogDeps, l, &state.CallState{Event: event, TenantID: tenantID, TraceID: event.TraceID}, "ANNOUNCE_SYSTEM_ERROR")
				return
			}
			l.Info().Str("user_id", createdUserRes.User.Id).Msg("Misafir kullanıcı başarıyla oluşturuldu.")
			event.Dialplan.MatchedUser = createdUserRes.User
			if len(createdUserRes.User.Contacts) > 0 {
				event.Dialplan.MatchedContact = createdUserRes.User.Contacts[0]
			}
		} else {
			// FindUserByContact başka bir hata döndürdü
			l.Error().Err(err).Msg("Kullanıcı aranırken beklenmedik bir hata oluştu.")
			h.eventsFailed.WithLabelValues(event.EventType, "find_user_failed").Inc()
			playInitialAnnouncement(ctx, h.dialogDeps, l, &state.CallState{Event: event, TenantID: tenantID, TraceID: event.TraceID}, "ANNOUNCE_SYSTEM_ERROR")
			return
		}
	}

	// ===== YENİ EKLENECEK KOD BAŞLANGICI =====
	// CDR'daki yarış durumunu çözmek için bu olayı yayınlıyoruz.
	if event.Dialplan.GetMatchedUser() != nil && event.Dialplan.GetMatchedContact() != nil {
		l.Info().Msg("Kullanıcı kimliği belirlendi, user.created.for_call olayı yayınlanacak.")

		userIdentifiedPayload := struct {
			EventType string `json:"eventType"`
			TraceID   string `json:"traceId"`
			CallID    string `json:"callId"`
			UserID    string `json:"userId"`
			ContactID int32  `json:"contactId"`
			TenantID  string `json:"tenantId"`
		}{
			EventType: "user.created.for_call",
			TraceID:   event.TraceID,
			CallID:    event.CallID,
			UserID:    event.Dialplan.GetMatchedUser().GetId(),
			ContactID: event.Dialplan.GetMatchedContact().GetId(),
			TenantID:  event.Dialplan.GetMatchedUser().GetTenantId(),
		}

		publishErr := h.publisher.PublishJSON(ctx, "user.created.for_call", userIdentifiedPayload)
		if publishErr != nil {
			l.Error().Err(publishErr).Msg("user.created.for_call olayı yayınlanamadı.")
			// Bu kritik bir hata, ancak akışı durdurmuyoruz, sadece logluyoruz.
			// CDR kaydı eksik kalacak ama çağrı devam edebilir.
		} else {
			l.Info().Msg("user.created.for_call olayı başarıyla yayınlandı.")
		}
	} else {
		l.Warn().Msg("Kullanıcı veya contact bilgisi eksik olduğu için user.created.for_call olayı yayınlanamadı. CDR kaydı eksik olabilir.")
	}
	// ===== YENİ EKLENECEK KOD SONU =====

	// Akışın sonunda, kullanıcı ya bulundu ya da oluşturuldu. Standart diyalog akışını başlat.
	h.handleStartAIConversation(l, event)
}

func (h *EventHandler) handleStartAIConversation(l zerolog.Logger, event *state.CallEvent) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-ctx.Done()
		cancel()
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

	playInitialAnnouncement(ctx, h.dialogDeps, l, initialState, "ANNOUNCE_SYSTEM_CONNECTING")
	dialog.RunDialogLoop(ctx, h.dialogDeps, h.stateManager, initialState)
}

func (h *EventHandler) handleCallEnded(l zerolog.Logger, event *state.CallEvent) {
	l.Info().Msg("Çağrı sonlandırma olayı işleniyor.")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// --- YENİ: Çağrı bittiğinde Distributed Lock'u temizle ---
	lockKey := "active_agent_lock:" + event.CallID
	if err := h.stateManager.RedisClient().Del(ctx, lockKey).Err(); err != nil {
		l.Error().Err(err).Msg("Redis'ten lock anahtarı silinemedi.")
	} else {
		l.Info().Msg("Aktif agent lock'ı başarıyla temizlendi.")
	}
	// --- TEMİZLEME SONU ---

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

func playInitialAnnouncement(ctx context.Context, deps *dialog.Dependencies, l zerolog.Logger, st *state.CallState, announcementID string) {
	languageCode := "tr"
	if st.Event != nil && st.Event.Dialplan != nil {
		// Düzeltme: MatchedUser veya InboundRoute'dan dil kodunu al
		if st.Event.Dialplan.MatchedUser != nil && st.Event.Dialplan.MatchedUser.PreferredLanguageCode != nil {
			languageCode = *st.Event.Dialplan.MatchedUser.PreferredLanguageCode
		} else if st.Event.Dialplan.GetInboundRoute() != nil && st.Event.Dialplan.GetInboundRoute().DefaultLanguageCode != "" {
			languageCode = st.Event.Dialplan.GetInboundRoute().DefaultLanguageCode
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
