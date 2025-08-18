package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
	"github.com/go-redis/redis/v8"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/sentiric/sentiric-agent-service/internal/database"
	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
	mediav1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/media/v1"
	ttsv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/tts/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// --- Veri Yapıları (DEĞİŞİKLİK YOK) ---
type DialogState string

const (
	StateWelcoming  DialogState = "WELCOMING"
	StateListening  DialogState = "LISTENING"
	StateThinking   DialogState = "THINKING"
	StateSpeaking   DialogState = "SPEAKING"
	StateEnded      DialogState = "ENDED"
	StateTerminated DialogState = "TERMINATED"
)

type CallState struct {
	CallID         string              `json:"call_id"`
	TraceID        string              `json:"trace_id"`
	CurrentState   DialogState         `json:"current_state"`
	Event          *CallEvent          `json:"event"`
	Conversation   []map[string]string `json:"conversation"`
	LastUpdateTime time.Time           `json:"last_update_time"`
}

var allowedSpeakerDomains = map[string]bool{"sentiric.github.io": true}

func isAllowedSpeakerURL(rawURL string) bool {
	u, e := url.Parse(rawURL)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && allowedSpeakerDomains[u.Hostname()]
}

type CallEvent struct {
	EventType string
	TraceID   string
	CallID    string
	Media     map[string]interface{}
	Dialplan  *dialplanv1.ResolveDialplanResponse
	From      string
}
type LlmGenerateRequest struct {
	Prompt string `json:"prompt"`
}
type LlmGenerateResponse struct {
	Text string `json:"text"`
}
type SttTranscribeResponse struct {
	Text string `json:"text"`
}
type EventHandler struct {
	db              *sql.DB
	rdb             *redis.Client
	mediaClient     mediav1.MediaServiceClient
	userClient      userv1.UserServiceClient
	ttsClient       ttsv1.TextToSpeechServiceClient
	log             zerolog.Logger
	eventsProcessed *prometheus.CounterVec
	eventsFailed    *prometheus.CounterVec
	llmServiceURL   string
	sttServiceURL   string
}

func NewEventHandler(db *sql.DB, rdb *redis.Client, mc mediav1.MediaServiceClient, uc userv1.UserServiceClient, tc ttsv1.TextToSpeechServiceClient, llmURL, sttURL string, log zerolog.Logger, processed, failed *prometheus.CounterVec) *EventHandler {
	return &EventHandler{db: db, rdb: rdb, mediaClient: mc, userClient: uc, ttsClient: tc, llmServiceURL: llmURL, sttServiceURL: sttURL, log: log, eventsProcessed: processed, eventsFailed: failed}
}

// --- Olay ve Durum Yönetimi (DEĞİŞİKLİK YOK) ---
func (h *EventHandler) getCallState(ctx context.Context, callID string) (*CallState, error) {
	val, err := h.rdb.Get(ctx, "callstate:"+callID).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state CallState
	if err := json.Unmarshal([]byte(val), &state); err != nil {
		return nil, err
	}
	return &state, nil
}
func (h *EventHandler) setCallState(ctx context.Context, state *CallState) error {
	state.LastUpdateTime = time.Now()
	val, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return h.rdb.Set(ctx, "callstate:"+state.CallID, val, 2*time.Hour).Err()
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
	l.Info().Msg("Olay alındı")
	switch event.EventType {
	case "call.started":
		go h.handleCallStarted(l, &event)
	case "call.ended":
		go h.handleCallEnded(l, &event)
	}
}
func (h *EventHandler) handleCallStarted(l zerolog.Logger, event *CallEvent) {
	l.Info().Msg("Yeni çağrı başlıyor, durum makinesi başlatılıyor...")
	ctx := context.Background()
	state, err := h.getCallState(ctx, event.CallID)
	if err != nil {
		l.Error().Err(err).Msg("Redis'ten durum alınamadı.")
		return
	}
	if state != nil {
		l.Warn().Msg("Bu çağrı için zaten aktif bir durum makinesi var, yeni bir tane başlatılmıyor.")
		return
	}
	initialState := &CallState{CallID: event.CallID, TraceID: event.TraceID, CurrentState: StateWelcoming, Event: event, Conversation: []map[string]string{}}
	if err := h.setCallState(ctx, initialState); err != nil {
		l.Error().Err(err).Msg("Redis'e başlangıç durumu yazılamadı.")
		return
	}
	h.runDialogLoop(initialState)
}
func (h *EventHandler) handleCallEnded(l zerolog.Logger, event *CallEvent) {
	l.Info().Msg("Çağrı sonlandı, durum makinesi durduruluyor...")
	ctx := context.Background()
	state, err := h.getCallState(ctx, event.CallID)
	if err != nil || state == nil {
		l.Warn().Err(err).Msg("Sonlanan çağrı için aktif bir durum bulunamadı.")
		return
	}
	state.CurrentState = StateEnded
	if err := h.setCallState(ctx, state); err != nil {
		l.Error().Err(err).Msg("Redis'e 'Ended' durumu yazılamadı.")
	}
}

// --- Ana Diyalog Döngüsü (DEĞİŞİKLİK YOK) ---
func (h *EventHandler) runDialogLoop(initialState *CallState) {
	ctx := context.Background()
	state := initialState
	l := h.log.With().Str("call_id", state.CallID).Str("trace_id", state.TraceID).Logger()
	for state.CurrentState != StateEnded && state.CurrentState != StateTerminated {
		l = l.With().Str("state", string(state.CurrentState)).Logger()
		l.Info().Msg("Diyalog döngüsü adımı işleniyor.")
		var err error
		switch state.CurrentState {
		case StateWelcoming:
			state, err = h.stateFnWelcoming(ctx, state)
		case StateListening:
			state, err = h.stateFnListening(ctx, state)
		case StateThinking:
			state, err = h.stateFnThinking(ctx, state)
		case StateSpeaking:
			state, err = h.stateFnSpeaking(ctx, state)
		default:
			l.Error().Msg("Bilinmeyen durum, döngü sonlandırılıyor.")
			state.CurrentState = StateTerminated
		}
		if err != nil {
			l.Error().Err(err).Msg("Durum işlenirken hata oluştu, sonlandırma deneniyor.")
			h.playAnnouncement(l, state.Event, "ANNOUNCE_SYSTEM_ERROR", true)
			state.CurrentState = StateTerminated
		}
		if err := h.setCallState(ctx, state); err != nil {
			l.Error().Err(err).Msg("Döngü içinde durum güncellenemedi, sonlandırılıyor.")
			state.CurrentState = StateTerminated
		}
	}
	l.Info().Msg("Diyalog döngüsü tamamlandı.")
}

// --- Durum Fonksiyonları (DEĞİŞİKLİK YOK) ---
func (h *EventHandler) stateFnWelcoming(ctx context.Context, state *CallState) (*CallState, error) {
	l := h.log.With().Str("call_id", state.CallID).Logger()
	h.playAnnouncement(l, state.Event, "ANNOUNCE_SYSTEM_CONNECTING", true)
	welcomeText, err := h.generateWelcomeText(l, state.Event)
	if err != nil {
		return state, err
	}
	state.Conversation = append(state.Conversation, map[string]string{"ai": welcomeText})
	h.playText(l, state.Event, welcomeText, true)
	state.CurrentState = StateListening
	return state, nil
}
func (h *EventHandler) stateFnListening(ctx context.Context, state *CallState) (*CallState, error) {
	l := h.log.With().Str("call_id", state.CallID).Logger()
	l.Info().Msg("Kullanıcıdan ses bekleniyor...")
	audioData, err := h.recordAudio(ctx, state)
	if err != nil {
		return state, fmt.Errorf("ses kaydı alınamadı: %w", err)
	}
	if len(audioData) == 0 {
		l.Warn().Msg("Kullanıcı konuşmadı veya boş ses verisi alındı. Tekrar dinleniyor.")
		return state, nil
	}
	transcribedText, err := h.transcribeAudio(ctx, state, audioData)
	if err != nil {
		return state, fmt.Errorf("ses metne çevrilemedi: %w", err)
	}
	state.Conversation = append(state.Conversation, map[string]string{"user": transcribedText})
	state.CurrentState = StateThinking
	return state, nil
}
func (h *EventHandler) stateFnThinking(ctx context.Context, state *CallState) (*CallState, error) {
	l := h.log.With().Str("call_id", state.CallID).Logger()
	l.Info().Msg("LLM'den yanıt üretiliyor...")
	prompt := h.buildLlmPrompt(state.Conversation)
	llmRespText, err := h.generateLlmResponse(ctx, state, prompt)
	if err != nil {
		return state, fmt.Errorf("LLM yanıtı üretilemedi: %w", err)
	}
	state.Conversation = append(state.Conversation, map[string]string{"ai": llmRespText})
	state.CurrentState = StateSpeaking
	return state, nil
}
func (h *EventHandler) stateFnSpeaking(ctx context.Context, state *CallState) (*CallState, error) {
	l := h.log.With().Str("call_id", state.CallID).Logger()
	lastAiMessage := state.Conversation[len(state.Conversation)-1]["ai"]
	l.Info().Str("text", lastAiMessage).Msg("AI yanıtı seslendiriliyor...")
	h.playText(l, state.Event, lastAiMessage, true)
	state.CurrentState = StateListening
	return state, nil
}

// --- Yardımcı Fonksiyonlar (API Çağrıları) ---

func (h *EventHandler) recordAudio(ctx context.Context, state *CallState) ([]byte, error) {
	l := h.log.With().Str("call_id", state.CallID).Logger()
	grpcCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	grpcCtx = metadata.AppendToOutgoingContext(grpcCtx, "x-trace-id", state.TraceID)
	stream, err := h.mediaClient.RecordAudio(grpcCtx, &mediav1.RecordAudioRequest{ServerRtpPort: uint32(state.Event.Media["server_rtp_port"].(float64))})
	if err != nil {
		return nil, err
	}
	l.Info().Msg("Ses kaydı stream'i açıldı, veri bekleniyor...")
	var audioData bytes.Buffer
	silenceThreshold := 5
	silenceCounter := 0
	receivedData := false
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			l.Info().Msg("Media-service stream'i kapattı (EOF).")
			break
		}
		if err != nil {
			st, _ := status.FromError(err)
			if st.Code() == codes.Canceled || st.Code() == codes.DeadlineExceeded {
				l.Warn().Msg("Ses kaydı stream'i zaman aşımı veya iptal nedeniyle sonlandı.")
				break
			}
			return nil, fmt.Errorf("stream'den okuma hatası: %w", err)
		}
		receivedData = true
		if len(chunk.AudioData) < 20 {
			silenceCounter++
			if silenceCounter >= silenceThreshold {
				l.Info().Int("threshold", silenceThreshold).Msg("Sessizlik eşiğine ulaşıldı, kayıt durduruluyor.")
				break
			}
		} else {
			silenceCounter = 0
			audioData.Write(chunk.AudioData)
		}
	}
	if !receivedData {
		l.Warn().Msg("Hiç ses verisi alınmadı.")
	}
	return audioData.Bytes(), nil
}

var ulawToPcmTable [256]int16

func init() {
	for i := 0; i < 256; i++ {
		ulaw := ^byte(i)
		sign := (ulaw & 0x80)
		exponent := int16((ulaw >> 4) & 0x07)
		mantissa := int16(ulaw & 0x0F)
		sample := (mantissa << (exponent + 3)) | (1 << (exponent + 2))
		if sign != 0 {
			ulawToPcmTable[i] = -sample
		} else {
			ulawToPcmTable[i] = sample
		}
	}
}

// DÜZELTME: inMemoryWriteSeeker struct'ı ve metodları
type inMemoryWriteSeeker struct {
	buf []byte
	pos int
}

func (imws *inMemoryWriteSeeker) Write(p []byte) (n int, err error) {
	if imws.pos+len(p) > len(imws.buf) {
		imws.buf = append(imws.buf, make([]byte, imws.pos+len(p)-len(imws.buf))...)
	}
	n = copy(imws.buf[imws.pos:], p)
	imws.pos += n
	return n, nil
}
func (imws *inMemoryWriteSeeker) Seek(offset int64, whence int) (int64, error) {
	newPos := imws.pos
	switch whence {
	case io.SeekStart:
		newPos = int(offset)
	case io.SeekCurrent:
		newPos = imws.pos + int(offset)
	case io.SeekEnd:
		newPos = len(imws.buf) + int(offset)
	}
	if newPos < 0 {
		return 0, fmt.Errorf("negatif seek pozisyonu")
	}
	imws.pos = newPos
	return int64(newPos), nil
}
func (imws *inMemoryWriteSeeker) Bytes() []byte {
	return imws.buf
}

func createWavInMemory(pcmuData []byte) (*bytes.Buffer, error) {
	numSamples := len(pcmuData)
	pcmInts := make([]int, numSamples)
	for i, ulawByte := range pcmuData {
		pcmInts[i] = int(ulawToPcmTable[ulawByte])
	}
	format := &audio.Format{NumChannels: 1, SampleRate: 8000}
	intBuffer := &audio.IntBuffer{Format: format, Data: pcmInts, SourceBitDepth: 16}

	// DÜZELTME: io.WriteSeeker arayüzünü karşılayan özel struct'ımızı kullanıyoruz.
	out := &inMemoryWriteSeeker{buf: make([]byte, 0, 1024)}
	encoder := wav.NewEncoder(out, format.SampleRate, 16, format.NumChannels, 1)

	if err := encoder.Write(intBuffer); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}

	return bytes.NewBuffer(out.Bytes()), nil
}

func (h *EventHandler) transcribeAudio(ctx context.Context, state *CallState, audioData []byte) (string, error) {
	l := h.log.With().Str("call_id", state.CallID).Logger()
	wavBuffer, err := createWavInMemory(audioData)
	if err != nil {
		l.Error().Err(err).Msg("Bellekte WAV dosyası oluşturulamadı.")
		return "", fmt.Errorf("bellekte wav oluşturulamadı: %w", err)
	}

	var b bytes.Buffer
	writer := multipart.NewWriter(&b)

	// --- DÜZELTME BURADA ---
	// Dil parametresini form verisi olarak ekliyoruz.
	languageCode := h.getLanguageCode(state.Event)
	if err := writer.WriteField("language", languageCode); err != nil {
		return "", err
	}
	// --- BİTTİ ---

	part, err := writer.CreateFormFile("audio_file", "stream.wav")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, wavBuffer); err != nil {
		return "", err
	}
	writer.Close()

	fullSttUrl := fmt.Sprintf("%s/api/v1/transcribe", h.sttServiceURL)
	req, err := http.NewRequestWithContext(ctx, "POST", fullSttUrl, &b)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Trace-ID", state.TraceID)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		l.Error().Int("status_code", resp.StatusCode).Bytes("body", bodyBytes).Msg("STT servisi hata döndürdü.")
		return "", fmt.Errorf("STT servisi %d kodu döndürdü", resp.StatusCode)
	}
	var sttResp SttTranscribeResponse
	if err := json.NewDecoder(resp.Body).Decode(&sttResp); err != nil {
		return "", err
	}
	l.Info().Str("transcribed_text", sttResp.Text).Str("language_used", languageCode).Msg("Ses başarıyla metne çevrildi.")
	return sttResp.Text, nil
}

func (h *EventHandler) generateLlmResponse(ctx context.Context, state *CallState, prompt string) (string, error) {
	llmReqPayload := LlmGenerateRequest{Prompt: prompt}
	payloadBytes, _ := json.Marshal(llmReqPayload)
	fullLlmUrl := fmt.Sprintf("%s/generate", h.llmServiceURL)
	req, err := http.NewRequestWithContext(ctx, "POST", fullLlmUrl, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Trace-ID", state.TraceID)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("LLM servisi %d kodu döndürdü", resp.StatusCode)
	}
	var llmResp LlmGenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
		return "", err
	}
	return strings.Trim(llmResp.Text, "\" \n\r"), nil
}

// --- Diğer Yardımcı Fonksiyonlar (DEĞİŞİKLİK YOK) ---
func (h *EventHandler) getLanguageCode(event *CallEvent) string {
	if event.Dialplan.MatchedUser != nil && event.Dialplan.MatchedUser.PreferredLanguageCode != nil && *event.Dialplan.MatchedUser.PreferredLanguageCode != "" {
		return *event.Dialplan.MatchedUser.PreferredLanguageCode
	}
	if event.Dialplan.GetInboundRoute() != nil && event.Dialplan.GetInboundRoute().DefaultLanguageCode != "" {
		return event.Dialplan.GetInboundRoute().DefaultLanguageCode
	}
	return "tr"
}
func (h *EventHandler) playText(l zerolog.Logger, event *CallEvent, textToPlay string, waitForCompletion bool) {
	l.Info().Str("text", textToPlay).Msg("Metin sese dönüştürülüyor...")
	speakerURL, useCloning := event.Dialplan.Action.ActionData.Data["speaker_wav_url"]
	voiceSelector, useVoiceSelector := event.Dialplan.Action.ActionData.Data["voice_selector"]
	ctx := metadata.AppendToOutgoingContext(context.Background(), "x-trace-id", event.TraceID)
	languageCode := h.getLanguageCode(event)
	ttsReq := &ttsv1.SynthesizeRequest{Text: textToPlay, LanguageCode: languageCode}
	if useCloning && speakerURL != "" {
		if !isAllowedSpeakerURL(speakerURL) {
			l.Error().Str("speaker_url", speakerURL).Msg("GÜVENLİK UYARISI: İzin verilmeyen bir speaker_wav_url engellendi (SSRF).")
			h.eventsFailed.WithLabelValues(event.EventType, "ssrf_attempt_blocked").Inc()
			h.playAnnouncement(l, event, "ANNOUNCE_SYSTEM_ERROR", true)
			return
		}
		ttsReq.SpeakerWavUrl = &speakerURL
	} else if useVoiceSelector && voiceSelector != "" {
		ttsReq.VoiceSelector = &voiceSelector
	}
	ttsCtx, ttsCancel := context.WithTimeout(ctx, 20*time.Second)
	defer ttsCancel()
	ttsResp, err := h.ttsClient.Synthesize(ttsCtx, ttsReq)
	if err != nil {
		l.Error().Err(err).Msg("TTS Gateway'den yanıt alınamadı.")
		h.eventsFailed.WithLabelValues(event.EventType, "tts_gateway_failed").Inc()
		h.playAnnouncement(l, event, "ANNOUNCE_SYSTEM_ERROR", true)
		return
	}
	audioBytes := ttsResp.GetAudioContent()
	mediaType := "audio/mpeg"
	if ttsResp.GetEngineUsed() != "sentiric-tts-edge-service" {
		mediaType = "audio/wav"
	}
	encodedAudio := base64.StdEncoding.EncodeToString(audioBytes)
	audioURI := fmt.Sprintf("data:%s;base64,%s", mediaType, encodedAudio)
	mediaInfo := event.Media
	rtpTarget := mediaInfo["caller_rtp_addr"].(string)
	serverPort := uint32(mediaInfo["server_rtp_port"].(float64))
	playReq := &mediav1.PlayAudioRequest{RtpTargetAddr: rtpTarget, ServerRtpPort: serverPort, AudioUri: audioURI}
	playCtx, playCancel := context.WithTimeout(ctx, 30*time.Second)
	defer playCancel()
	if waitForCompletion {
		_, err = h.mediaClient.PlayAudio(playCtx, playReq)
	} else {
		go func() {
			_, err := h.mediaClient.PlayAudio(context.Background(), playReq)
			if err != nil {
				l.Error().Err(err).Msg("Hata: Arka plan TTS sesi çalınamadı")
			}
		}()
	}
	if err != nil {
		l.Error().Err(err).Msg("Hata: Dinamik ses (TTS) çalınamadı")
		h.eventsFailed.WithLabelValues(event.EventType, "play_tts_audio_failed").Inc()
	}
}
func (h *EventHandler) playAnnouncement(l zerolog.Logger, event *CallEvent, announcementIDBase string, waitForCompletion bool) {
	languageCode := h.getLanguageCode(event)
	announcementID := fmt.Sprintf("%s_%s", announcementIDBase, strings.ToUpper(languageCode))
	audioPath, err := database.GetAnnouncementPathFromDB(h.db, announcementID)
	if err != nil {
		l.Error().Err(err).Str("announcement_id", announcementID).Msg("Anons yolu alınamadı, fallback deneniyor")
		announcementID = fmt.Sprintf("%s_EN", announcementIDBase)
		audioPath, err = database.GetAnnouncementPathFromDB(h.db, announcementID)
		if err != nil {
			l.Error().Err(err).Msg("KRİTİK HATA: Sistem fallback anonsu dahi yüklenemedi.")
			return
		}
	}
	audioURI := fmt.Sprintf("file:///%s", audioPath)
	mediaInfo := event.Media
	rtpTarget := mediaInfo["caller_rtp_addr"].(string)
	serverPort := uint32(mediaInfo["server_rtp_port"].(float64))
	ctx := metadata.AppendToOutgoingContext(context.Background(), "x-trace-id", event.TraceID)
	playCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	playReq := &mediav1.PlayAudioRequest{RtpTargetAddr: rtpTarget, ServerRtpPort: serverPort, AudioUri: audioURI}
	if waitForCompletion {
		_, err = h.mediaClient.PlayAudio(playCtx, playReq)
	} else {
		go func() {
			_, err := h.mediaClient.PlayAudio(ctx, playReq)
			if err != nil {
				l.Error().Err(err).Str("audio_uri", audioURI).Msg("Hata: Arka plan anonsu çalınamadı")
			}
		}()
	}
	if err != nil {
		l.Error().Err(err).Str("audio_uri", audioURI).Msg("Hata: Ses çalma komutu başarısız")
		h.eventsFailed.WithLabelValues(event.EventType, "play_announcement_failed").Inc()
	}
}
func (h *EventHandler) generateWelcomeText(l zerolog.Logger, event *CallEvent) (string, error) {
	languageCode := h.getLanguageCode(event)
	var promptID string
	if event.Dialplan.MatchedUser != nil && event.Dialplan.MatchedUser.Name != nil {
		promptID = "PROMPT_WELCOME_KNOWN_USER"
	} else {
		promptID = "PROMPT_WELCOME_GUEST"
	}
	promptTemplate, err := database.GetTemplateFromDB(h.db, promptID, languageCode)
	if err != nil {
		l.Error().Err(err).Msg("Prompt şablonu veritabanından alınamadı.")
		return "Merhaba, hoş geldiniz.", err
	}
	prompt := promptTemplate
	if event.Dialplan.MatchedUser != nil && event.Dialplan.MatchedUser.Name != nil {
		prompt = strings.Replace(prompt, "{user_name}", *event.Dialplan.MatchedUser.Name, -1)
	}
	return h.generateLlmResponse(context.Background(), &CallState{TraceID: event.TraceID}, prompt)
}
func (h *EventHandler) buildLlmPrompt(conversation []map[string]string) string {
	var promptBuilder strings.Builder
	promptBuilder.WriteString("Aşağıdaki diyaloğa devam et:\n")
	for _, msg := range conversation {
		if text, ok := msg["user"]; ok {
			promptBuilder.WriteString(fmt.Sprintf("Kullanıcı: %s\n", text))
		} else if text, ok := msg["ai"]; ok {
			promptBuilder.WriteString(fmt.Sprintf("Asistan: %s\n", text))
		}
	}
	promptBuilder.WriteString("Asistan: ")
	return promptBuilder.String()
}
