// File: internal/dialog/states.go
package dialog

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/sentiric/sentiric-agent-service/internal/client"
	"github.com/sentiric/sentiric-agent-service/internal/database"
	"github.com/sentiric/sentiric-agent-service/internal/state"
	mediav1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/media/v1"
	ttsv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/tts/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Dependencies, diyalog fonksiyonlarının ihtiyaç duyduğu tüm bağımlılıkları içeren bir yapıdır.
type Dependencies struct {
	DB                  *sql.DB
	MediaClient         mediav1.MediaServiceClient
	TTSClient           ttsv1.TextToSpeechServiceClient
	LLMClient           *client.LlmClient
	STTClient           *client.SttClient
	Log                 zerolog.Logger
	SttTargetSampleRate uint32
	EventsFailed        *prometheus.CounterVec
}

func StateFnWelcoming(ctx context.Context, deps *Dependencies, st *state.CallState) (*state.CallState, error) {
	l := deps.Log.With().Str("call_id", st.CallID).Logger()
	welcomeText, err := generateWelcomeText(ctx, deps, l, st)
	if err != nil {
		return st, err
	}
	st.Conversation = append(st.Conversation, map[string]string{"ai": welcomeText})
	playText(ctx, deps, l, st, welcomeText)
	st.CurrentState = state.StateListening
	return st, nil
}

func StateFnListening(ctx context.Context, deps *Dependencies, st *state.CallState) (*state.CallState, error) {
	l := deps.Log.With().Str("call_id", st.CallID).Logger()
	l.Info().Msg("Kullanıcıdan ses bekleniyor...")

	audioData, err := recordAudio(ctx, deps, st)
	if err != nil {
		// Eğer context ana döngü tarafından iptal edildiyse, hatayı yukarı taşı
		if err == context.Canceled || status.Code(err) == codes.Canceled {
			return st, context.Canceled
		}
		// Diğer hatalar loglanır ama döngüyü kırmaz
		l.Error().Err(err).Msg("Ses kaydı sırasında bir hata oluştu, tekrar dinleniyor.")
		return st, nil // Hata durumunda tekrar dinlemeye geç
	}
	if len(audioData) == 0 {
		l.Warn().Msg("Kullanıcı konuşmadı veya boş ses verisi alındı. Tekrar dinleniyor.")
		return st, nil
	}

	languageCode := getLanguageCode(st.Event)
	transcribedText, err := deps.STTClient.Transcribe(ctx, audioData, languageCode, st.TraceID)
	if err != nil {
		return st, fmt.Errorf("ses metne çevrilemedi: %w", err)
	}
	if transcribedText == "" {
		l.Warn().Msg("STT boş metin döndürdü, tekrar dinleniyor.")
		return st, nil
	}

	st.Conversation = append(st.Conversation, map[string]string{"user": transcribedText})
	st.CurrentState = state.StateThinking
	return st, nil
}

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

func StateFnSpeaking(ctx context.Context, deps *Dependencies, st *state.CallState) (*state.CallState, error) {
	l := deps.Log.With().Str("call_id", st.CallID).Logger()
	lastAiMessage := st.Conversation[len(st.Conversation)-1]["ai"]
	l.Info().Str("text", lastAiMessage).Msg("AI yanıtı seslendiriliyor...")
	playText(ctx, deps, l, st, lastAiMessage)
	st.CurrentState = state.StateListening
	return st, nil
}

// --- Helper Functions ---

var allowedSpeakerDomains = map[string]bool{"sentiric.github.io": true}

func isAllowedSpeakerURL(rawURL string) bool {
	u, e := url.Parse(rawURL)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && allowedSpeakerDomains[u.Hostname()]
}

func playText(ctx context.Context, deps *Dependencies, l zerolog.Logger, st *state.CallState, textToPlay string) {
	l.Info().Str("text", textToPlay).Msg("Metin sese dönüştürülüyor...")
	speakerURL, useCloning := st.Event.Dialplan.Action.ActionData.Data["speaker_wav_url"]
	voiceSelector, useVoiceSelector := st.Event.Dialplan.Action.ActionData.Data["voice_selector"]

	mdCtx := metadata.AppendToOutgoingContext(ctx, "x-trace-id", st.TraceID)
	languageCode := getLanguageCode(st.Event)
	ttsReq := &ttsv1.SynthesizeRequest{Text: textToPlay, LanguageCode: languageCode}

	if useCloning && speakerURL != "" {
		if !isAllowedSpeakerURL(speakerURL) {
			l.Error().Str("speaker_url", speakerURL).Msg("GÜVENLİK UYARISI: İzin verilmeyen bir speaker_wav_url engellendi.")
			deps.EventsFailed.WithLabelValues(st.Event.EventType, "ssrf_attempt_blocked").Inc()
			PlayAnnouncement(deps, l, st, "ANNOUNCE_SYSTEM_ERROR")
			return
		}
		ttsReq.SpeakerWavUrl = &speakerURL
	} else if useVoiceSelector && voiceSelector != "" {
		ttsReq.VoiceSelector = &voiceSelector
	}

	ttsCtx, ttsCancel := context.WithTimeout(mdCtx, 20*time.Second)
	defer ttsCancel()
	ttsResp, err := deps.TTSClient.Synthesize(ttsCtx, ttsReq)
	if err != nil {
		l.Error().Err(err).Msg("TTS Gateway'den yanıt alınamadı.")
		deps.EventsFailed.WithLabelValues(st.Event.EventType, "tts_gateway_failed").Inc()
		PlayAnnouncement(deps, l, st, "ANNOUNCE_SYSTEM_ERROR")
		return
	}

	audioBytes := ttsResp.GetAudioContent()
	mediaType := "audio/mpeg"
	if ttsResp.GetEngineUsed() != "sentiric-tts-edge-service" {
		mediaType = "audio/wav"
	}
	encodedAudio := base64.StdEncoding.EncodeToString(audioBytes)
	audioURI := fmt.Sprintf("data:%s;base64,%s", mediaType, encodedAudio)

	mediaInfo := st.Event.Media
	rtpTarget := mediaInfo["caller_rtp_addr"].(string)
	serverPort := uint32(mediaInfo["server_rtp_port"].(float64))
	playReq := &mediav1.PlayAudioRequest{RtpTargetAddr: rtpTarget, ServerRtpPort: serverPort, AudioUri: audioURI}

	playCtx, playCancel := context.WithTimeout(mdCtx, 5*time.Minute)
	defer playCancel()

	_, err = deps.MediaClient.PlayAudio(playCtx, playReq)

	if err != nil {
		stErr, ok := status.FromError(err)
		if ok && stErr.Code() == codes.Canceled {
			l.Info().Msg("PlayAudio işlemi başka bir komutla veya çağrı sonlandığı için iptal edildi.")
		} else {
			l.Error().Err(err).Msg("Hata: Dinamik ses (TTS) çalınamadı")
			deps.EventsFailed.WithLabelValues(st.Event.EventType, "play_tts_audio_failed").Inc()
		}
	} else {
		l.Info().Msg("Dinamik ses (TTS) başarıyla çalındı ve tamamlandı.")
	}
}

// #############################################################################
// ###                      NİHAİ VE DÜZELTİLMİŞ FONKSİYON                     ###
// #############################################################################
func recordAudio(ctx context.Context, deps *Dependencies, st *state.CallState) ([]byte, error) {
	l := deps.Log.With().Str("call_id", st.CallID).Logger()
	grpcCtx := metadata.AppendToOutgoingContext(ctx, "x-trace-id", st.TraceID)

	stream, err := deps.MediaClient.RecordAudio(grpcCtx, &mediav1.RecordAudioRequest{
		ServerRtpPort:    uint32(st.Event.Media["server_rtp_port"].(float64)),
		TargetSampleRate: &deps.SttTargetSampleRate,
	})
	if err != nil {
		return nil, fmt.Errorf("media service ile stream oluşturulamadı: %w", err)
	}

	l.Info().Uint32("target_sample_rate", deps.SttTargetSampleRate).Msg("Ses kaydı stream'i açıldı, VAD döngüsü başlıyor...")

	const listeningTimeout = 15 * time.Second
	const silenceThresholdDuration = 1*time.Second + 200*time.Millisecond // 1.2 saniye

	// Fonksiyonun her durumda sonlanmasını garantilemek için genel bir zaman aşımı
	ctxWithTimeout, cancel := context.WithTimeout(ctx, listeningTimeout)
	defer cancel()

	var audioData bytes.Buffer
	var lastAudioTime = time.Now()
	var speechStarted = false

	for {
		// Ana context'in iptal edilip edilmediğini her döngünün başında kontrol et
		if err := ctxWithTimeout.Err(); err != nil {
			if audioData.Len() > 0 {
				l.Info().Msg("Genel zaman aşımına ulaşıldı, kayıt tamamlandı.")
				return audioData.Bytes(), nil
			}
			l.Warn().Msg("Dinleme zaman aşımına uğradı, kullanıcı hiç konuşmadı.")
			return nil, nil // Bu bir hata değil, sadece zaman aşımı
		}

		// Sessizlik süresinin dolup dolmadığını kontrol et
		if speechStarted && time.Since(lastAudioTime) > silenceThresholdDuration {
			l.Info().Msg("Sessizlik eşiğine ulaşıldı, kayıt tamamlandı.")
			return audioData.Bytes(), nil
		}

		chunk, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				l.Info().Msg("Media-service stream'i kapattı (EOF).")
				return audioData.Bytes(), nil
			}
			stErr, _ := status.FromError(err)
			if stErr.Code() == codes.Canceled {
				return nil, context.Canceled // Ana döngü tarafından iptal edildi
			}
			// Diğer tüm gRPC hataları loglanır ama akışı kırmaz, bir sonraki denemeyi bekler
			l.Warn().Err(err).Msg("Stream'den okuma hatası, görmezden geliniyor.")
			time.Sleep(20 * time.Millisecond) // Hata durumunda kısa bir bekleme
			continue
		}

		if chunk != nil && len(chunk.AudioData) > 0 {
			audioData.Write(chunk.AudioData)
			lastAudioTime = time.Now()
			if !speechStarted {
				l.Info().Msg("Konuşma aktivitesi tespit edildi.")
				speechStarted = true
			}
		} else {
			// Boş chunk veya hiç chunk gelmemesi durumunda kısa bir bekleme
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// #############################################################################

func getLanguageCode(event *state.CallEvent) string {
	if event.Dialplan != nil && event.Dialplan.MatchedUser != nil && event.Dialplan.MatchedUser.PreferredLanguageCode != nil && *event.Dialplan.MatchedUser.PreferredLanguageCode != "" {
		return *event.Dialplan.MatchedUser.PreferredLanguageCode
	}
	if event.Dialplan != nil && event.Dialplan.GetInboundRoute() != nil && event.Dialplan.GetInboundRoute().DefaultLanguageCode != "" {
		return event.Dialplan.GetInboundRoute().DefaultLanguageCode
	}
	return "tr"
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

func generateWelcomeText(ctx context.Context, deps *Dependencies, l zerolog.Logger, st *state.CallState) (string, error) {
	languageCode := getLanguageCode(st.Event)
	var promptID string
	if st.Event.Dialplan.MatchedUser != nil && st.Event.Dialplan.MatchedUser.Name != nil {
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
	if st.Event.Dialplan.MatchedUser != nil && st.Event.Dialplan.MatchedUser.Name != nil {
		prompt = strings.Replace(prompt, "{user_name}", *st.Event.Dialplan.MatchedUser.Name, -1)
	}
	return deps.LLMClient.Generate(ctx, prompt, st.TraceID)
}

func PlayAnnouncement(deps *Dependencies, l zerolog.Logger, st *state.CallState, announcementID string) {
	languageCode := getLanguageCode(st.Event)
	audioPath, err := database.GetAnnouncementPathFromDB(deps.DB, announcementID, st.TenantID, languageCode)
	if err != nil {
		l.Error().Err(err).Str("announcement_id", announcementID).Msg("Anons yolu alınamadı, fallback deneniyor")
		audioPath, err = database.GetAnnouncementPathFromDB(deps.DB, announcementID, st.TenantID, "en")
		if err != nil {
			l.Error().Err(err).Msg("KRİTİK HATA: Sistem fallback anonsu dahi yüklenemedi.")
			return
		}
	}

	audioURI := fmt.Sprintf("file://%s", audioPath)
	mediaInfo := st.Event.Media
	rtpTarget := mediaInfo["caller_rtp_addr"].(string)
	serverPort := uint32(mediaInfo["server_rtp_port"].(float64))
	ctx := metadata.AppendToOutgoingContext(context.Background(), "x-trace-id", st.TraceID)
	playCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	playReq := &mediav1.PlayAudioRequest{RtpTargetAddr: rtpTarget, ServerRtpPort: serverPort, AudioUri: audioURI}

	_, err = deps.MediaClient.PlayAudio(playCtx, playReq)
	if err != nil {
		stErr, _ := status.FromError(err)
		if stErr.Code() == codes.Canceled {
			l.Info().Str("announcement_id", announcementID).Msg("Anons başka bir komutla iptal edildi.")
		} else {
			l.Error().Err(err).Str("audio_uri", audioURI).Msg("Hata: Ses çalma komutu başarısız")
			deps.EventsFailed.WithLabelValues(st.Event.EventType, "play_announcement_failed").Inc()
		}
	}
}
