// ========== FILE: sentiric-agent-service/internal/handler/event_handler.go ==========
package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/sentiric/sentiric-agent-service/internal/database"
	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
	mediav1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/media/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
	"google.golang.org/grpc/metadata"
)

type CallEvent struct {
	EventType string                             `json:"eventType"`
	TraceID   string                             `json:"traceId"`
	CallID    string                             `json:"callId"`
	Media     map[string]interface{}             `json:"media"`
	Dialplan  dialplanv1.ResolveDialplanResponse `json:"dialplan"`
	From      string                             `json:"from"`
}

type LlmRequest struct {
	Prompt string `json:"prompt"`
}

type LlmResponse struct {
	Text string `json:"text"`
}

type TtsRequest struct {
	Text          string  `json:"text"`
	SpeakerWavUrl *string `json:"speaker_wav_url,omitempty"`
}

type TtsResponse struct {
	AudioPath string `json:"audio_path"`
}

type EventHandler struct {
	db              *sql.DB
	mediaClient     mediav1.MediaServiceClient
	userClient      userv1.UserServiceClient
	httpClient      *http.Client
	llmServiceURL   string
	ttsServiceURL   string
	log             zerolog.Logger
	eventsProcessed *prometheus.CounterVec
	eventsFailed    *prometheus.CounterVec
}

func NewEventHandler(db *sql.DB, mc mediav1.MediaServiceClient, uc userv1.UserServiceClient, llmURL, ttsURL string, log zerolog.Logger, processed, failed *prometheus.CounterVec) *EventHandler {
	return &EventHandler{
		db:              db,
		mediaClient:     mc,
		userClient:      uc,
		httpClient:      &http.Client{Timeout: 15 * time.Second},
		llmServiceURL:   llmURL,
		ttsServiceURL:   ttsURL,
		log:             log,
		eventsProcessed: processed,
		eventsFailed:    failed,
	}
}

func (h *EventHandler) HandleRabbitMQMessage(body []byte) {
	var event CallEvent
	if err := json.Unmarshal(body, &event); err != nil {
		h.log.Error().Err(err).Bytes("raw_message", body).Msg("Hata: Mesaj JSON formatında değil")
		h.eventsFailed.WithLabelValues("unknown", "json_unmarshal").Inc()
		return
	}

	h.eventsProcessed.WithLabelValues(event.EventType).Inc()
	l := h.log.With().Str("call_id", event.CallID).Str("trace_id", event.TraceID).Str("event_type", event.EventType).Logger()
	l.Info().RawJSON("event_data", body).Msg("Olay alındı")

	if event.EventType == "call.started" {
		go h.handleCallStarted(l, &event)
	}
}

func (h *EventHandler) handleCallStarted(l zerolog.Logger, event *CallEvent) {
	l.Info().Msg("Yeni çağrı işleniyor...")
	if event.Dialplan.Action == nil {
		l.Error().Msg("Hata: Dialplan Action boş.")
		h.eventsFailed.WithLabelValues(event.EventType, "nil_dialplan_action").Inc()
		h.playAnnouncement(l, event, "ANNOUNCE_SYSTEM_ERROR_TR")
		return
	}
	action := event.Dialplan.Action.Action
	l = l.With().Str("action", action).Str("dialplan_id", event.Dialplan.DialplanId).Logger()

	switch action {
	case "PLAY_ANNOUNCEMENT":
		h.handlePlayAnnouncement(l, event)
	case "START_AI_CONVERSATION":
		h.handleStartAIConversation(l, event)
	case "PROCESS_GUEST_CALL":
		h.handleProcessGuestCall(l, event)
	default:
		l.Error().Str("received_action", action).Msg("Bilinmeyen eylem")
		h.eventsFailed.WithLabelValues(event.EventType, "unknown_action").Inc()
		h.playAnnouncement(l, event, "ANNOUNCE_SYSTEM_ERROR_TR")
	}
}

func (h *EventHandler) handlePlayAnnouncement(l zerolog.Logger, event *CallEvent) {
	announcementID := event.Dialplan.Action.ActionData.Data["announcement_id"]
	l.Info().Str("announcement_id", announcementID).Msg("Anons çalma eylemi işleniyor")
	h.playAnnouncement(l, event, announcementID)
}

func (h *EventHandler) handleStartAIConversation(l zerolog.Logger, event *CallEvent) {
	announcementID := event.Dialplan.Action.ActionData.Data["welcome_announcement_id"]
	l.Info().Str("announcement_id", announcementID).Msg("AI Konuşma başlatma eylemi işleniyor (karşılama anonsu)")
	h.playAnnouncement(l, event, announcementID)
	go h.startDialogLoop(l, event)
}

func (h *EventHandler) handleProcessGuestCall(l zerolog.Logger, event *CallEvent) {
	callerID := extractCallerID(event.From)
	tenantID := event.Dialplan.TenantId
	if callerID != "" && tenantID != "" {
		h.createGuestUser(l, event, callerID, tenantID)
	} else {
		l.Error().Str("caller_id", callerID).Str("tenant_id", tenantID).Msg("Misafir kullanıcı oluşturulamadı, bilgi eksik.")
		h.eventsFailed.WithLabelValues(event.EventType, "missing_guest_info").Inc()
	}
	announcementID := event.Dialplan.Action.ActionData.Data["welcome_announcement_id"]
	l.Info().Str("announcement_id", announcementID).Msg("Misafir karşılama eylemi işleniyor")
	h.playAnnouncement(l, event, announcementID)
	go h.startDialogLoop(l, event)
}

func (h *EventHandler) startDialogLoop(l zerolog.Logger, event *CallEvent) {
	time.Sleep(5 * time.Second)
	l.Info().Msg("Yapay zeka diyalog döngüsü başlatılıyor...")
	respText, err := h.generateLlmResponse(l, "Merhaba, nasılsınız? Lütfen kısa bir yanıt verin.")
	if err != nil {
		l.Error().Err(err).Msg("Hata: LLM'den yanıt alınamadı")
		h.eventsFailed.WithLabelValues(event.EventType, "llm_initial_response_failed").Inc()
		h.playAnnouncement(l, event, "ANNOUNCE_SYSTEM_ERROR_TR")
		return
	}
	l.Info().Str("llm_response", respText).Msg("LLM yanıtı alındı")
	h.playText(l, event, respText)
}

func (h *EventHandler) playText(l zerolog.Logger, event *CallEvent, textToPlay string) {
	l.Info().Str("text", textToPlay).Msg("Metin sese dönüştürülüyor...")
	speakerURL, ok := event.Dialplan.Action.ActionData.Data["speaker_wav_url"]
	ttsReq := TtsRequest{Text: textToPlay}
	if ok && speakerURL != "" {
		l.Info().Str("speaker_url", speakerURL).Msg("Dinamik referans sesi kullanılacak.")
		ttsReq.SpeakerWavUrl = &speakerURL
	} else {
		l.Info().Msg("Varsayılan referans sesi kullanılacak.")
	}
	reqBody, err := json.Marshal(ttsReq)
	if err != nil {
		l.Error().Err(err).Msg("TTS istek gövdesi oluşturulamadı.")
		h.eventsFailed.WithLabelValues(event.EventType, "tts_request_marshal_failed").Inc()
		h.playAnnouncement(l, event, "ANNOUNCE_SYSTEM_ERROR_TR")
		return
	}
	req, err := http.NewRequest("POST", h.ttsServiceURL+"/api/v1/synthesize", bytes.NewBuffer(reqBody))
	if err != nil {
		l.Error().Err(err).Msg("TTS HTTP isteği oluşturulamadı.")
		h.eventsFailed.WithLabelValues(event.EventType, "tts_http_request_failed").Inc()
		h.playAnnouncement(l, event, "ANNOUNCE_SYSTEM_ERROR_TR")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.httpClient.Do(req)
	if err != nil {
		l.Error().Err(err).Msg("TTS servisine istek başarısız.")
		h.eventsFailed.WithLabelValues(event.EventType, "tts_service_request_failed").Inc()
		h.playAnnouncement(l, event, "ANNOUNCE_SYSTEM_ERROR_TR")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		l.Error().Str("status", resp.Status).Str("body", string(bodyBytes)).Msg("TTS servisi hata döndü.")
		h.eventsFailed.WithLabelValues(event.EventType, "tts_service_error_response").Inc()
		h.playAnnouncement(l, event, "ANNOUNCE_SYSTEM_ERROR_TR")
		return
	}
	var ttsResp TtsResponse
	if err := json.NewDecoder(resp.Body).Decode(&ttsResp); err != nil {
		l.Error().Err(err).Msg("TTS yanıtı çözümlenemedi.")
		h.eventsFailed.WithLabelValues(event.EventType, "tts_response_unmarshal_failed").Inc()
		h.playAnnouncement(l, event, "ANNOUNCE_SYSTEM_ERROR_TR")
		return
	}
	l.Info().Str("audio_path", ttsResp.AudioPath).Msg("Dinamik ses dosyası çalınacak.")
	h.playDynamicAudio(l, event, ttsResp.AudioPath)
}

func (h *EventHandler) playDynamicAudio(l zerolog.Logger, event *CallEvent, audioPath string) {
	mediaInfo := event.Media
	rtpTarget, _ := mediaInfo["caller_rtp_addr"].(string)
	serverPort, _ := mediaInfo["server_rtp_port"].(float64)
	if rtpTarget == "" || serverPort == 0 {
		l.Error().Str("rtp_target", rtpTarget).Float64("server_port", serverPort).Msg("Geçersiz medya bilgisi, dinamik ses çalınamıyor.")
		h.eventsFailed.WithLabelValues(event.EventType, "invalid_media_info").Inc()
		return
	}
	ctx := metadata.AppendToOutgoingContext(context.Background(), "x-trace-id", event.TraceID)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := h.mediaClient.PlayAudio(ctx, &mediav1.PlayAudioRequest{
		RtpTargetAddr: rtpTarget,
		AudioId:       audioPath,
		ServerRtpPort: uint32(serverPort),
	})
	if err != nil {
		l.Error().Err(err).Str("audio_path", audioPath).Msg("Hata: Dinamik ses çalınamadı")
		h.eventsFailed.WithLabelValues(event.EventType, "play_dynamic_audio_failed").Inc()
	} else {
		l.Info().Str("audio_path", audioPath).Msg("Dinamik ses çalma komutu gönderildi")
	}
}

func (h *EventHandler) playAnnouncement(l zerolog.Logger, event *CallEvent, announcementID string) {
	audioPath, err := database.GetAnnouncementPathFromDB(h.db, announcementID)
	if err != nil {
		l.Error().Err(err).Str("announcement_id", announcementID).Msg("Anons yolu alınamadı, fallback kullanılıyor")
		h.eventsFailed.WithLabelValues(event.EventType, "get_announcement_failed").Inc()
		audioPath = "audio/tr/system_error.wav"
	}
	mediaInfo := event.Media
	rtpTarget, _ := mediaInfo["caller_rtp_addr"].(string)
	serverPort, _ := mediaInfo["server_rtp_port"].(float64)
	if rtpTarget == "" || serverPort == 0 {
		l.Error().Str("rtp_target", rtpTarget).Float64("server_port", serverPort).Msg("Geçersiz medya bilgisi, ses çalınamıyor.")
		h.eventsFailed.WithLabelValues(event.EventType, "invalid_media_info").Inc()
		return
	}
	ctx := metadata.AppendToOutgoingContext(context.Background(), "x-trace-id", event.TraceID)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err = h.mediaClient.PlayAudio(ctx, &mediav1.PlayAudioRequest{
		RtpTargetAddr: rtpTarget,
		AudioId:       audioPath,
		ServerRtpPort: uint32(serverPort),
	})
	if err != nil {
		l.Error().Err(err).Str("audio_path", audioPath).Msg("Hata: Ses çalınamadı")
		h.eventsFailed.WithLabelValues(event.EventType, "play_announcement_failed").Inc()
	} else {
		l.Info().Str("audio_path", audioPath).Msg("Ses çalma komutu gönderildi")
	}
}

// DÜZELTME: Fonksiyonun imzası ve içeriği yeni CreateUserRequest'e göre güncellendi.
func (h *EventHandler) createGuestUser(l zerolog.Logger, event *CallEvent, callerID, tenantID string) {
	l.Info().Str("caller_id", callerID).Str("tenant_id", tenantID).Msg("Misafir kullanıcı oluşturuluyor...")
	ctx := metadata.AppendToOutgoingContext(context.Background(), "x-trace-id", event.TraceID)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	guestName := "Guest Caller"
	_, err := h.userClient.CreateUser(ctx, &userv1.CreateUserRequest{
		TenantId: tenantID,
		UserType: "caller",
		Name:     &guestName,
		InitialContact: &userv1.CreateUserRequest_InitialContact{
			ContactType:  "phone",
			ContactValue: callerID,
		},
	})

	if err != nil {
		l.Error().Err(err).Msg("Hata: Misafir kullanıcı oluşturulamadı")
		h.eventsFailed.WithLabelValues("process_guest_call", "create_guest_user_failed").Inc()
	} else {
		l.Info().Str("caller_id", callerID).Msg("Misafir kullanıcı başarıyla oluşturuldu")
	}
}

func (h *EventHandler) generateLlmResponse(l zerolog.Logger, prompt string) (string, error) {
	reqBody, err := json.Marshal(LlmRequest{Prompt: prompt})
	if err != nil {
		return "", fmt.Errorf("istek gövdesi oluşturulamadı: %w", err)
	}
	req, err := http.NewRequestWithContext(context.Background(), "POST", h.llmServiceURL+"/generate", bytes.NewBuffer(reqBody))
	if err != nil {
		return "", fmt.Errorf("HTTP isteği oluşturulamadı: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("LLM servisine istek başarısız: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("LLM servisi hata döndü: %s - %s", resp.Status, string(bodyBytes))
	}
	var llmResp LlmResponse
	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
		return "", fmt.Errorf("LLM yanıtı çözümlenemedi: %w", err)
	}
	return llmResp.Text, nil
}

func extractCallerID(fromURI string) string {
	re := regexp.MustCompile(`sip:(\+?\d+)@`)
	matches := re.FindStringSubmatch(fromURI)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}
