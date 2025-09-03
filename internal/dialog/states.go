// internal/dialog/states.go dosyasının TAM ve GÜNCELLENMİŞ HALİ
package dialog

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/sentiric/sentiric-agent-service/internal/client"
	"github.com/sentiric/sentiric-agent-service/internal/config"
	"github.com/sentiric/sentiric-agent-service/internal/database"
	"github.com/sentiric/sentiric-agent-service/internal/queue"
	"github.com/sentiric/sentiric-agent-service/internal/state"
	mediav1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/media/v1"
	ttsv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/tts/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// --- DEĞİŞİKLİK: streamAndTranscribe için yeni bir yanıt struct'ı ---
type TranscriptionResult struct {
	Text              string
	IsNoSpeechTimeout bool
}

// --- DEĞİŞİKLİK SONU ---

// Dependencies struct'ı aynı kalır...
type Dependencies struct {
	DB                  *sql.DB
	Config              *config.Config
	Publisher           *queue.Publisher
	MediaClient         mediav1.MediaServiceClient
	TTSClient           ttsv1.TextToSpeechServiceClient
	LLMClient           *client.LlmClient
	STTClient           *client.SttClient
	Log                 zerolog.Logger
	SttTargetSampleRate uint32
	EventsFailed        *prometheus.CounterVec
}

// StateFnWelcoming aynı kalır...
func StateFnWelcoming(ctx context.Context, deps *Dependencies, st *state.CallState) (*state.CallState, error) {
	l := deps.Log.With().Str("call_id", st.CallID).Logger()
	l.Info().Msg("İlk AI yanıtı öncesi 1.5 saniye bekleniyor...")
	time.Sleep(1500 * time.Millisecond)
	welcomeText, err := generateWelcomeText(ctx, deps, l, st)
	if err != nil {
		return st, err
	}
	st.Conversation = append(st.Conversation, map[string]string{"ai": welcomeText})
	playText(ctx, deps, l, st, welcomeText)
	st.CurrentState = state.StateListening
	return st, nil
}

// --- DEĞİŞİKLİK: StateFnListening tamamen güncellendi ---
func StateFnListening(ctx context.Context, deps *Dependencies, st *state.CallState) (*state.CallState, error) {
	l := deps.Log.With().Str("call_id", st.CallID).Logger()

	if st.ConsecutiveFailures >= 2 {
		l.Warn().Int("failures", st.ConsecutiveFailures).Msg("Art arda çok fazla anlama hatası. Çağrı sonlandırılıyor.")
		PlayAnnouncement(ctx, deps, l, st, "ANNOUNCE_SYSTEM_MAX_FAILURES")
		st.CurrentState = state.StateTerminated
		return st, nil
	}

	l.Info().Msg("Kullanıcıdan ses bekleniyor (gerçek zamanlı akış modu)...")

	transcriptionResult, err := streamAndTranscribe(ctx, deps, st)
	if err != nil {
		if err == context.Canceled || status.Code(err) == codes.Canceled {
			return st, context.Canceled
		}
		PlayAnnouncement(ctx, deps, l, st, "ANNOUNCE_SYSTEM_ERROR")
		st.ConsecutiveFailures++
		return st, nil
	}

	// --- YENİ MANTIK BURADA ---
	if transcriptionResult.IsNoSpeechTimeout {
		l.Warn().Msg("STT'den ses algılanmadı (timeout). Kullanıcıya bir şans daha veriliyor.")
		PlayAnnouncement(ctx, deps, l, st, "ANNOUNCE_SYSTEM_CANT_HEAR_YOU")
		// Durumu tekrar Listening'e ayarlayarak döngüye devam etmesini sağlıyoruz, hata sayacını artırmıyoruz.
		st.CurrentState = state.StateListening
		return st, nil
	}

	if transcriptionResult.Text == "" {
		l.Warn().Msg("STT boş metin döndürdü, 'anlayamadım' anonsu çalınacak.")
		PlayAnnouncement(ctx, deps, l, st, "ANNOUNCE_SYSTEM_CANT_UNDERSTAND")
		st.ConsecutiveFailures++
		// Durumu tekrar Listening'e ayarlayarak döngüye devam et.
		st.CurrentState = state.StateListening
		return st, nil
	}

	st.ConsecutiveFailures = 0
	st.Conversation = append(st.Conversation, map[string]string{"user": transcriptionResult.Text})
	st.CurrentState = state.StateThinking
	return st, nil
}

// --- DEĞİŞİKLİK SONU ---

// StateFnThinking aynı kalır...
func StateFnThinking(ctx context.Context, deps *Dependencies, st *state.CallState) (*state.CallState, error) {
	l := deps.Log.With().Str("call_id", st.CallID).Logger()
	l.Info().Msg("LLM'den yanıt üretiliyor...")
	prompt, err := buildLlmPrompt(ctx, deps, st)
	if err != nil {
		return st, fmt.Errorf("LLM prompt'u oluşturulamadı: %w", err)
	}
	llmRespText, err := deps.LLMClient.Generate(ctx, prompt, st.TraceID)
	if err != nil {
		return st, fmt.Errorf("LLM yanıtı üretilemedi: %w", err)
	}
	st.Conversation = append(st.Conversation, map[string]string{"ai": llmRespText})
	st.CurrentState = state.StateSpeaking
	return st, nil
}

// StateFnSpeaking aynı kalır...
func StateFnSpeaking(ctx context.Context, deps *Dependencies, st *state.CallState) (*state.CallState, error) {
	l := deps.Log.With().Str("call_id", st.CallID).Logger()
	lastAiMessage := st.Conversation[len(st.Conversation)-1]["ai"]
	l.Info().Str("text", lastAiMessage).Msg("AI yanıtı seslendiriliyor...")
	playText(ctx, deps, l, st, lastAiMessage)
	time.Sleep(250 * time.Millisecond)
	st.CurrentState = state.StateListening
	return st, nil
}

// --- DEĞİŞİKLİK: streamAndTranscribe dönüş değeri güncellendi ---
func streamAndTranscribe(ctx context.Context, deps *Dependencies, st *state.CallState) (TranscriptionResult, error) {
	l := deps.Log.With().Str("call_id", st.CallID).Logger()
	var result TranscriptionResult // Varsayılan değerler: Text="", IsNoSpeechTimeout=false

	portVal, ok := st.Event.Media["server_rtp_port"]
	if !ok {
		return result, fmt.Errorf("kritik hata: CallState içinde 'server_rtp_port' bulunamadı")
	}
	serverRtpPortFloat, ok := portVal.(float64)
	if !ok {
		l.Error().Interface("value", portVal).Msg("Kritik hata: 'server_rtp_port' beklenen float64 tipinde değil.")
		return result, fmt.Errorf("kritik hata: 'server_rtp_port' tipi geçersiz")
	}

	grpcCtx := metadata.AppendToOutgoingContext(ctx, "x-trace-id", st.TraceID)
	mediaStream, err := deps.MediaClient.RecordAudio(grpcCtx, &mediav1.RecordAudioRequest{
		ServerRtpPort:    uint32(serverRtpPortFloat),
		TargetSampleRate: &deps.SttTargetSampleRate,
	})
	if err != nil {
		return result, fmt.Errorf("media service ile stream oluşturulamadı: %w", err)
	}
	l.Info().Msg("Media-Service'ten ses akışı başlatıldı.")

	u, err := url.Parse(deps.STTClient.BaseURL())
	if err != nil {
		return result, fmt.Errorf("stt service url parse edilemedi: %w", err)
	}
	sttURL := url.URL{Scheme: "ws", Host: u.Host, Path: "/api/v1/transcribe-stream"}
	q := sttURL.Query()
	q.Set("language", getLanguageCode(st.Event))
	q.Set("logprob_threshold", fmt.Sprintf("%f", deps.Config.SttServiceLogprobThreshold))
	q.Set("no_speech_threshold", fmt.Sprintf("%f", deps.Config.SttServiceNoSpeechThreshold))
	vadLevel := "1"
	if st.Event != nil && st.Event.Dialplan != nil && st.Event.Dialplan.Action != nil && st.Event.Dialplan.Action.ActionData != nil && st.Event.Dialplan.Action.ActionData.Data != nil {
		if val, ok := st.Event.Dialplan.Action.ActionData.Data["stt_vad_level"]; ok {
			vadLevel = val
		}
	}
	q.Set("vad_aggressiveness", vadLevel)
	sttURL.RawQuery = q.Encode()

	l.Info().Str("url", sttURL.String()).Msg("STT-Service'e WebSocket bağlantısı kuruluyor...")
	wsConn, _, err := websocket.DefaultDialer.Dial(sttURL.String(), nil)
	if err != nil {
		return result, fmt.Errorf("stt service websocket bağlantısı kurulamadı: %w", err)
	}
	defer wsConn.Close()
	l.Info().Msg("STT-Service'e WebSocket bağlantısı başarılı.")

	errChan := make(chan error, 2)
	resultChan := make(chan TranscriptionResult, 1) // Artık struct döndürüyor
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		defer func() {
			wsConn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		}()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				chunk, err := mediaStream.Recv()
				if err != nil {
					if err != io.EOF && status.Code(err) != codes.Canceled {
						errChan <- fmt.Errorf("media stream'den okuma hatası: %w", err)
					}
					return
				}
				if err := wsConn.WriteMessage(websocket.BinaryMessage, chunk.AudioData); err != nil {
					if !websocket.IsCloseError(err) {
						errChan <- fmt.Errorf("websocket'e yazma hatası: %w", err)
					}
					return
				}
			}
		}
	}()

	go func() {
		defer close(resultChan)
		for {
			_, message, err := wsConn.ReadMessage()
			if err != nil {
				return
			}
			var res map[string]interface{}
			if err := json.Unmarshal(message, &res); err == nil {
				if resType, ok := res["type"].(string); ok {
					if resType == "final" {
						if text, ok := res["text"].(string); ok {
							resultChan <- TranscriptionResult{Text: text, IsNoSpeechTimeout: false}
							return
						}
					} else if resType == "no_speech_timeout" { // --- YENİ ---
						resultChan <- TranscriptionResult{Text: "", IsNoSpeechTimeout: true}
						return
					}
				}
			}
		}
	}()

	select {
	case res, ok := <-resultChan:
		if !ok {
			l.Warn().Msg("Transkript alınamadan STT bağlantısı kapandı.")
			return TranscriptionResult{Text: "", IsNoSpeechTimeout: false}, nil
		}
		l.Info().Interface("result", res).Msg("Nihai transkript sonucu alındı.")
		return res, nil
	case err := <-errChan:
		l.Error().Err(err).Msg("Akış sırasında hata oluştu.")
		return result, err
	case <-time.After(30 * time.Second):
		l.Warn().Msg("Transkripsiyon için zaman aşımına ulaşıldı.")
		return TranscriptionResult{Text: "", IsNoSpeechTimeout: true}, nil
	}
}

// --- DEĞİŞİKLİK SONU ---

// Diğer fonksiyonlar (playText, PlayAnnouncement, generateWelcomeText, buildLlmPrompt, getLanguageCode, isAllowedSpeakerURL) aynı kalır...

// (Geri kalan fonksiyonların tamamını buraya ekliyorum)

func playText(ctx context.Context, deps *Dependencies, l zerolog.Logger, st *state.CallState, textToPlay string) {
	l.Info().Str("text", textToPlay).Msg("Metin sese dönüştürülüyor...")

	var speakerURL, voiceSelector string
	var useCloning bool

	if st.Event != nil && st.Event.Dialplan != nil && st.Event.Dialplan.Action != nil && st.Event.Dialplan.Action.ActionData != nil && st.Event.Dialplan.Action.ActionData.Data != nil {
		actionData := st.Event.Dialplan.Action.ActionData.Data
		speakerURL, useCloning = actionData["speaker_wav_url"]
		voiceSelector = actionData["voice_selector"] // Bu değeri alıyoruz
	}

	mdCtx := metadata.AppendToOutgoingContext(ctx, "x-trace-id", st.TraceID)
	languageCode := getLanguageCode(st.Event)
	ttsReq := &ttsv1.SynthesizeRequest{Text: textToPlay, LanguageCode: languageCode}

	if useCloning && speakerURL != "" {
		if !isAllowedSpeakerURL(speakerURL) {
			l.Error().Str("speaker_url", speakerURL).Msg("GÜVENLİK UYARISI: İzin verilmeyen bir speaker_wav_url engellendi.")
			deps.EventsFailed.WithLabelValues(st.Event.EventType, "ssrf_attempt_blocked").Inc()
			PlayAnnouncement(ctx, deps, l, st, "ANNOUNCE_SYSTEM_ERROR")
			return
		}
		ttsReq.SpeakerWavUrl = &speakerURL
	} else if voiceSelector != "" {
		l.Info().Str("voice_selector", voiceSelector).Msg("Dinamik ses seçici kullanılıyor.")
		ttsReq.VoiceSelector = &voiceSelector // ve isteğe ekliyoruz
	}

	ttsCtx, ttsCancel := context.WithTimeout(mdCtx, 20*time.Second)
	defer ttsCancel()

	ttsResp, err := deps.TTSClient.Synthesize(ttsCtx, ttsReq)
	if err != nil {
		l.Error().Err(err).Msg("TTS Gateway'den yanıt alınamadı (muhtemelen zaman aşımı).")
		deps.EventsFailed.WithLabelValues(st.Event.EventType, "tts_gateway_failed").Inc()
		PlayAnnouncement(ctx, deps, l, st, "ANNOUNCE_SYSTEM_ERROR")
		return
	}

	if ttsResp == nil {
		l.Error().Msg("TTS Gateway'den hata dönmedi ancak yanıt boş (nil). Bu beklenmedik bir durum.")
		deps.EventsFailed.WithLabelValues(st.Event.EventType, "tts_gateway_nil_response").Inc()
		PlayAnnouncement(ctx, deps, l, st, "ANNOUNCE_SYSTEM_ERROR")
		return
	}

	audioBytes := ttsResp.GetAudioContent()
	audioURI := fmt.Sprintf("data:audio/wav;base64,%s", base64.StdEncoding.EncodeToString(audioBytes))

	if st.Event == nil || st.Event.Media == nil {
		l.Error().Msg("PlayAudio için kritik medya bilgisi eksik (st.Event.Media is nil).")
		return
	}

	rtpTargetVal, ok1 := st.Event.Media["caller_rtp_addr"]
	serverPortVal, ok2 := st.Event.Media["server_rtp_port"]
	if !ok1 || !ok2 {
		l.Error().Msg("PlayAudio için medya bilgileri eksik (caller_rtp_addr veya server_rtp_port anahtarı yok).")
		return
	}

	rtpTarget, ok1 := rtpTargetVal.(string)
	serverPortFloat, ok2 := serverPortVal.(float64)
	if !ok1 || !ok2 {
		l.Error().Interface("rtp_target", rtpTargetVal).Interface("server_port", serverPortVal).Msg("PlayAudio için medya bilgileri geçersiz formatta.")
		return
	}

	serverPort := uint32(serverPortFloat)
	playReq := &mediav1.PlayAudioRequest{RtpTargetAddr: rtpTarget, ServerRtpPort: serverPort, AudioUri: audioURI}

	playCtx, playCancel := context.WithTimeout(mdCtx, 5*time.Minute)
	defer playCancel()

	_, err = deps.MediaClient.PlayAudio(playCtx, playReq)
	if err != nil {
		stErr, ok := status.FromError(err)
		if ok && (stErr.Code() == codes.Canceled || stErr.Code() == codes.DeadlineExceeded) {
			l.Warn().Err(err).Msg("PlayAudio işlemi başka bir komutla veya zaman aşımı nedeniyle iptal edildi.")
		} else {
			l.Error().Err(err).Msg("Hata: Dinamik ses (TTS) çalınamadı")
			deps.EventsFailed.WithLabelValues(st.Event.EventType, "play_tts_audio_failed").Inc()
		}
	} else {
		l.Info().Msg("Dinamik ses (TTS) başarıyla çalındı ve tamamlandı.")
	}
}

func PlayAnnouncement(ctx context.Context, deps *Dependencies, l zerolog.Logger, st *state.CallState, announcementID string) {
	languageCode := getLanguageCode(st.Event)
	audioPath, err := database.GetAnnouncementPathFromDB(deps.DB, announcementID, st.TenantID, languageCode)
	if err != nil {
		l.Error().Err(err).Str("announcement_id", announcementID).Msg("Anons yolu alınamadı, fallback deneniyor")
		audioPath, err = database.GetAnnouncementPathFromDB(deps.DB, announcementID, "system", "en")
		if err != nil {
			l.Error().Err(err).Str("announcement_id", announcementID).Msg("KRİTİK HATA: Sistem fallback anonsu dahi yüklenemedi.")
			return
		}
	}

	audioURI := fmt.Sprintf("file://%s", audioPath)
	mediaInfo := st.Event.Media
	if mediaInfo == nil {
		l.Error().Str("announcement_id", announcementID).Msg("Anons çalınamadı: Medya bilgisi (MediaInfo) eksik.")
		return
	}
	rtpTarget := mediaInfo["caller_rtp_addr"].(string)
	serverPort := uint32(mediaInfo["server_rtp_port"].(float64))

	playCtx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(ctx, "x-trace-id", st.TraceID), 30*time.Second)
	defer cancel()

	playReq := &mediav1.PlayAudioRequest{RtpTargetAddr: rtpTarget, ServerRtpPort: serverPort, AudioUri: audioURI}

	_, err = deps.MediaClient.PlayAudio(playCtx, playReq)
	if err != nil {
		stErr, _ := status.FromError(err)
		if stErr.Code() == codes.Canceled || stErr.Code() == codes.DeadlineExceeded {
			l.Warn().Err(err).Str("announcement_id", announcementID).Msg("Anons çalma işlemi zaman aşımına uğradı veya iptal edildi.")
		} else {
			l.Error().Err(err).Str("audio_uri", audioURI).Msg("Hata: Ses çalma komutu başarısız")
			deps.EventsFailed.WithLabelValues(st.Event.EventType, "play_announcement_failed").Inc()
		}
	}
}

func generateWelcomeText(ctx context.Context, deps *Dependencies, l zerolog.Logger, st *state.CallState) (string, error) {
	languageCode := getLanguageCode(st.Event)
	var promptID string
	if st.Event != nil && st.Event.Dialplan != nil && st.Event.Dialplan.MatchedUser != nil && st.Event.Dialplan.MatchedUser.Name != nil {
		promptID = "PROMPT_WELCOME_KNOWN_USER"
	} else {
		promptID = "PROMPT_WELCOME_GUEST"
	}
	promptTemplate, err := database.GetTemplateFromDB(deps.DB, promptID, languageCode, st.TenantID)
	if err != nil {
		l.Error().Err(err).Msg("Prompt şablonu veritabanından alınamadı.")
		return "Merhaba, hoş geldiniz.", err
	}
	prompt := promptTemplate
	if st.Event != nil && st.Event.Dialplan != nil && st.Event.Dialplan.MatchedUser != nil && st.Event.Dialplan.MatchedUser.Name != nil {
		prompt = strings.Replace(prompt, "{user_name}", *st.Event.Dialplan.MatchedUser.Name, -1)
	}
	return deps.LLMClient.Generate(ctx, prompt, st.TraceID)
}

func buildLlmPrompt(ctx context.Context, deps *Dependencies, st *state.CallState) (string, error) {
	languageCode := getLanguageCode(st.Event)
	systemPrompt, err := database.GetTemplateFromDB(deps.DB, "PROMPT_SYSTEM_DEFAULT", languageCode, st.TenantID)
	if err != nil {
		deps.Log.Error().Err(err).Str("call_id", st.CallID).Msg("Sistem prompt'u alınamadı, fallback kullanılıyor.")
		systemPrompt = "Aşağıdaki diyaloğa devam et. Cevapların kısa olsun."
	}
	var promptBuilder strings.Builder
	promptBuilder.WriteString(systemPrompt)
	promptBuilder.WriteString("\n\n--- KONUŞMA GEÇMİŞİ ---\n")
	for _, msg := range st.Conversation {
		if text, ok := msg["user"]; ok {
			promptBuilder.WriteString(fmt.Sprintf("Kullanıcı: %s\n", text))
		} else if text, ok := msg["ai"]; ok {
			promptBuilder.WriteString(fmt.Sprintf("Asistan: %s\n", text))
		}
	}
	promptBuilder.WriteString("Asistan:")
	return promptBuilder.String(), nil
}

func getLanguageCode(event *state.CallEvent) string {
	if event != nil && event.Dialplan != nil {
		if event.Dialplan.MatchedUser != nil && event.Dialplan.MatchedUser.PreferredLanguageCode != nil && *event.Dialplan.MatchedUser.PreferredLanguageCode != "" {
			return *event.Dialplan.MatchedUser.PreferredLanguageCode
		}
		if event.Dialplan.GetInboundRoute() != nil && event.Dialplan.GetInboundRoute().DefaultLanguageCode != "" {
			return event.Dialplan.GetInboundRoute().DefaultLanguageCode
		}
	}
	return "tr"
}

var allowedSpeakerDomains = map[string]bool{"sentiric.github.io": true}

func isAllowedSpeakerURL(rawURL string) bool {
	u, e := url.Parse(rawURL)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && allowedSpeakerDomains[u.Hostname()]
}
