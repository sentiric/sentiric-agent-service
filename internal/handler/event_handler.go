package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/sentiric/sentiric-agent-service/internal/database"
	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
	mediav1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/media/v1"
	ttsv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/tts/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
	"google.golang.org/grpc/metadata"
)

var allowedSpeakerDomains = map[string]bool{
	"sentiric.github.io": true,
}

func isAllowedSpeakerURL(rawURL string) bool {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return false
	}
	return allowedSpeakerDomains[parsedURL.Hostname()]
}

type CallEvent struct {
	EventType string                             `json:"eventType"`
	TraceID   string                             `json:"traceId"`
	CallID    string                             `json:"callId"`
	Media     map[string]interface{}             `json:"media"`
	Dialplan  dialplanv1.ResolveDialplanResponse `json:"dialplan"`
	From      string                             `json:"from"`
}

type LlmGenerateRequest struct {
	Prompt string `json:"prompt"`
}
type LlmGenerateResponse struct {
	Text string `json:"text"`
}

type EventHandler struct {
	db              *sql.DB
	mediaClient     mediav1.MediaServiceClient
	userClient      userv1.UserServiceClient
	ttsClient       ttsv1.TextToSpeechServiceClient
	log             zerolog.Logger
	eventsProcessed *prometheus.CounterVec
	eventsFailed    *prometheus.CounterVec
	llmServiceURL   string
}

func NewEventHandler(db *sql.DB, mc mediav1.MediaServiceClient, uc userv1.UserServiceClient, tc ttsv1.TextToSpeechServiceClient, llmURL string, log zerolog.Logger, processed, failed *prometheus.CounterVec) *EventHandler {
	return &EventHandler{
		db: db, mediaClient: mc, userClient: uc, ttsClient: tc, llmServiceURL: llmURL,
		log: log, eventsProcessed: processed, eventsFailed: failed,
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
	if event.Dialplan.Action == nil || event.Dialplan.Action.ActionData == nil {
		l.Error().Msg("Hata: Dialplan Action veya ActionData boş.")
		h.eventsFailed.WithLabelValues(event.EventType, "nil_dialplan_action").Inc()
		h.playAnnouncement(l, event, "ANNOUNCE_SYSTEM_ERROR")
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
		h.playAnnouncement(l, event, "ANNOUNCE_SYSTEM_ERROR")
	}
}

func (h *EventHandler) handlePlayAnnouncement(l zerolog.Logger, event *CallEvent) {
	announcementID, ok := event.Dialplan.Action.ActionData.Data["announcement_id"]
	if !ok {
		l.Error().Msg("Dialplan action_data içinde 'announcement_id' bulunamadı.")
		h.eventsFailed.WithLabelValues(event.EventType, "missing_parameter").Inc()
		h.playAnnouncement(l, event, "ANNOUNCE_SYSTEM_ERROR")
		return
	}
	l.Info().Str("announcement_id", announcementID).Msg("Anons çalma eylemi işleniyor")
	h.playAnnouncement(l, event, announcementID)
}

func (h *EventHandler) handleStartAIConversation(l zerolog.Logger, event *CallEvent) {
	l.Info().Msg("AI Konuşma başlatılıyor (Dinamik Karşılama)...")

	// --- NİHAİ DÜZELTME BAŞLANGICI ---
	var promptID string
	var languageCode string

	// Dil kodunu kullanıcı profilinden veya varsayılan olarak al
	if event.Dialplan.MatchedUser != nil && event.Dialplan.MatchedUser.PreferredLanguageCode != nil {
		languageCode = *event.Dialplan.MatchedUser.PreferredLanguageCode
	} else {
		languageCode = "tr" // Varsayılan dil
	}

	// Prompt ID'sini belirle (artık sonunda dil kodu yok)
	if event.Dialplan.MatchedUser != nil && event.Dialplan.MatchedUser.Name != nil {
		promptID = "PROMPT_WELCOME_KNOWN_USER"
	} else {
		promptID = "PROMPT_WELCOME_GUEST"
	}
	l = l.With().Str("prompt_id", promptID).Str("language_code", languageCode).Logger()

	// Prompt şablonunu veritabanından DİL KODU ile birlikte çek
	promptTemplate, err := database.GetTemplateFromDB(h.db, promptID, languageCode)
	if err != nil {
		l.Error().Err(err).Msg("Prompt şablonu veritabanından alınamadı.")
		h.playAnnouncement(l, event, "ANNOUNCE_SYSTEM_ERROR")
		return
	}
	// --- NİHAİ DÜZELTME SONU ---

	prompt := promptTemplate
	if event.Dialplan.MatchedUser != nil && event.Dialplan.MatchedUser.Name != nil {
		prompt = strings.Replace(prompt, "{user_name}", *event.Dialplan.MatchedUser.Name, -1)
	}

	llmReqPayload := LlmGenerateRequest{Prompt: prompt}
	payloadBytes, _ := json.Marshal(llmReqPayload)
	fullLlmUrl := fmt.Sprintf("%s/generate", h.llmServiceURL)
	req, err := http.NewRequestWithContext(context.Background(), "POST", fullLlmUrl, bytes.NewBuffer(payloadBytes))
	if err != nil {
		l.Error().Err(err).Msg("LLM isteği oluşturulamadı.")
		h.playAnnouncement(l, event, "ANNOUNCE_SYSTEM_ERROR")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Trace-ID", event.TraceID)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		l.Error().Err(err).Msg("LLM servisinden yanıt alınamadı.")
		h.playAnnouncement(l, event, "ANNOUNCE_SYSTEM_ERROR")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		l.Error().Int("status_code", resp.StatusCode).Msg("LLM servisi hata döndürdü.")
		h.playAnnouncement(l, event, "ANNOUNCE_SYSTEM_ERROR")
		return
	}
	var llmResp LlmGenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
		l.Error().Err(err).Msg("LLM yanıtı parse edilemedi.")
		h.playAnnouncement(l, event, "ANNOUNCE_SYSTEM_ERROR")
		return
	}
	welcomeText := strings.Trim(llmResp.Text, "\"")
	l.Info().Str("llm_response", welcomeText).Msg("LLM'den dinamik karşılama metni alındı.")
	h.playText(l, event, welcomeText)
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
	h.handleStartAIConversation(l, event)
}

func (h *EventHandler) playText(l zerolog.Logger, event *CallEvent, textToPlay string) {
	l.Info().Str("text", textToPlay).Msg("Metin sese dönüştürülüyor...")
	speakerURL, useCloning := event.Dialplan.Action.ActionData.Data["speaker_wav_url"]
	voiceSelector, useVoiceSelector := event.Dialplan.Action.ActionData.Data["voice_selector"]
	ctx := metadata.AppendToOutgoingContext(context.Background(), "x-trace-id", event.TraceID)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var languageCode string
	if event.Dialplan.MatchedUser != nil && event.Dialplan.MatchedUser.PreferredLanguageCode != nil {
		languageCode = *event.Dialplan.MatchedUser.PreferredLanguageCode
	} else {
		languageCode = "tr"
	}

	ttsReq := &ttsv1.SynthesizeRequest{Text: textToPlay, LanguageCode: languageCode}
	if useCloning && speakerURL != "" {
		if !isAllowedSpeakerURL(speakerURL) {
			l.Error().Str("speaker_url", speakerURL).Msg("GÜVENLİK UYARISI: İzin verilmeyen bir speaker_wav_url engellendi (SSRF).")
			h.eventsFailed.WithLabelValues(event.EventType, "ssrf_attempt_blocked").Inc()
			h.playAnnouncement(l, event, "ANNOUNCE_SYSTEM_ERROR")
			return
		}
		ttsReq.SpeakerWavUrl = &speakerURL
		l.Info().Str("speaker_url", speakerURL).Msg("Ses klonlama isteği hazırlanıyor.")
	} else if useVoiceSelector && voiceSelector != "" {
		ttsReq.VoiceSelector = &voiceSelector
		l.Info().Str("voice_selector", voiceSelector).Msg("Özel ses seçici isteği hazırlanıyor.")
	} else {
		l.Info().Msg("Varsayılan ses sentezleme isteği hazırlanıyor.")
	}

	ttsResp, err := h.ttsClient.Synthesize(ctx, ttsReq)
	if err != nil {
		l.Error().Err(err).Msg("TTS Gateway'den yanıt alınamadı.")
		h.eventsFailed.WithLabelValues(event.EventType, "tts_gateway_failed").Inc()
		h.playAnnouncement(l, event, "ANNOUNCE_SYSTEM_ERROR")
		return
	}
	audioBytes := ttsResp.GetAudioContent()
	l.Info().Str("engine_used", ttsResp.GetEngineUsed()).Int("audio_size_bytes", len(audioBytes)).Msg("TTS Gateway'den ses başarıyla alındı.")

	var mediaType string
	if ttsResp.GetEngineUsed() == "sentiric-tts-edge-service" {
		mediaType = "audio/mpeg"
	} else {
		mediaType = "audio/wav"
	}
	encodedAudio := base64.StdEncoding.EncodeToString(audioBytes)
	audioURI := fmt.Sprintf("data:%s;base64,%s", mediaType, encodedAudio)

	mediaInfo := event.Media
	rtpTarget, ok1 := mediaInfo["caller_rtp_addr"].(string)
	serverPortFloat, ok2 := mediaInfo["server_rtp_port"].(float64)
	if !ok1 || !ok2 || rtpTarget == "" || serverPortFloat == 0 {
		l.Error().Interface("media_info", mediaInfo).Msg("Geçersiz veya eksik medya bilgisi, dinamik ses çalınamıyor.")
		h.eventsFailed.WithLabelValues(event.EventType, "invalid_media_info_for_tts").Inc()
		return
	}
	serverPort := uint32(serverPortFloat)

	mediaCtx, mediaCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer mediaCancel()
	_, err = h.mediaClient.PlayAudio(mediaCtx, &mediav1.PlayAudioRequest{
		RtpTargetAddr: rtpTarget,
		ServerRtpPort: serverPort,
		AudioUri:      audioURI,
	})
	if err != nil {
		l.Error().Err(err).Msg("Hata: Dinamik ses (TTS) çalınamadı")
		h.eventsFailed.WithLabelValues(event.EventType, "play_tts_audio_failed").Inc()
	} else {
		l.Info().Msg("Dinamik ses (TTS) çalma komutu media-service'e gönderildi.")
	}
}

func (h *EventHandler) playAnnouncement(l zerolog.Logger, event *CallEvent, announcementID string) {
	var languageCode string
	if event.Dialplan.MatchedUser != nil && event.Dialplan.MatchedUser.PreferredLanguageCode != nil {
		languageCode = *event.Dialplan.MatchedUser.PreferredLanguageCode
	} else {
		languageCode = "tr"
	}
	tenantID := event.Dialplan.TenantId
	audioPath, err := database.GetAnnouncementPathFromDB(h.db, announcementID, tenantID, languageCode)
	if err != nil {
		l.Error().Err(err).Str("announcement_id", announcementID).Msg("Anons yolu alınamadı, fallback kullanılıyor")
		h.eventsFailed.WithLabelValues(event.EventType, "get_announcement_failed").Inc()
		audioPath, err = database.GetAnnouncementPathFromDB(h.db, "ANNOUNCE_SYSTEM_ERROR", "system", "en")
		if err != nil {
			l.Error().Err(err).Msg("KRİTİK HATA: Sistem fallback anonsu dahi yüklenemedi. Ses çalınamayacak.")
			return
		}
	}
	audioURI := fmt.Sprintf("file:///%s", audioPath)
	mediaInfo := event.Media
	rtpTarget, ok1 := mediaInfo["caller_rtp_addr"].(string)
	serverPortFloat, ok2 := mediaInfo["server_rtp_port"].(float64)
	if !ok1 || !ok2 || rtpTarget == "" || serverPortFloat == 0 {
		l.Error().Interface("media_info", mediaInfo).Msg("Geçersiz veya eksik medya bilgisi, ses çalınamıyor.")
		h.eventsFailed.WithLabelValues(event.EventType, "invalid_media_info").Inc()
		return
	}
	serverPort := uint32(serverPortFloat)
	ctx := metadata.AppendToOutgoingContext(context.Background(), "x-trace-id", event.TraceID)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err = h.mediaClient.PlayAudio(ctx, &mediav1.PlayAudioRequest{
		RtpTargetAddr: rtpTarget,
		ServerRtpPort: serverPort,
		AudioUri:      audioURI,
	})
	if err != nil {
		l.Error().Err(err).Str("audio_uri", audioURI).Msg("Hata: Ses çalınamadı")
		h.eventsFailed.WithLabelValues(event.EventType, "play_announcement_failed").Inc()
	} else {
		l.Info().Str("audio_uri", audioURI).Msg("Ses çalma komutu gönderildi")
	}
}

func (h *EventHandler) createGuestUser(l zerolog.Logger, event *CallEvent, callerID, tenantID string) {
	l.Info().Str("caller_id", callerID).Str("tenant_id", tenantID).Msg("Misafir kullanıcı oluşturuluyor...")
	ctx := metadata.AppendToOutgoingContext(context.Background(), "x-trace-id", event.TraceID)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

func extractCallerID(fromURI string) string {
	re := regexp.MustCompile(`sip:(\+?\d+)@`)
	matches := re.FindStringSubmatch(fromURI)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}
