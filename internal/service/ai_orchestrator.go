package service

import (
	"context"
	"encoding/base64" // DÜZELTME: 'b64' yerine standart paket adı kullanılıyor.
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sentiric/sentiric-agent-service/internal/client"
	"github.com/sentiric/sentiric-agent-service/internal/config"
	"github.com/sentiric/sentiric-agent-service/internal/ctxlogger"
	"github.com/sentiric/sentiric-agent-service/internal/state"
	mediav1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/media/v1"
	ttsv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/tts/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TranscriptionResult, STT servisinden dönen sonucu taşır.
type TranscriptionResult struct {
	Text              string
	IsNoSpeechTimeout bool
}

// AIOrchestrator, STT, LLM ve TTS servisleri ile olan tüm etkileşimleri yönetir.
type AIOrchestrator struct {
	cfg         *config.Config
	llmClient   *client.LlmClient
	sttClient   *client.SttClient
	ttsClient   ttsv1.TextToSpeechServiceClient
	mediaClient mediav1.MediaServiceClient
}

// NewAIOrchestrator, yeni bir AIOrchestrator örneği oluşturur.
func NewAIOrchestrator(
	cfg *config.Config,
	llmC *client.LlmClient,
	sttC *client.SttClient,
	ttsC ttsv1.TextToSpeechServiceClient,
	mediaC mediav1.MediaServiceClient,
) *AIOrchestrator {
	return &AIOrchestrator{
		cfg:         cfg,
		llmClient:   llmC,
		sttClient:   sttC,
		ttsClient:   ttsC,
		mediaClient: mediaC,
	}
}

// GenerateResponse, bir prompt alıp LLM'den yanıt üretir.
func (a *AIOrchestrator) GenerateResponse(ctx context.Context, prompt string, callState *state.CallState) (string, error) {
	return a.llmClient.Generate(ctx, prompt, callState.TraceID)
}

// SynthesizeAndGetAudio, metni sese dönüştürür ve base64-encoded audio URI'si döndürür.
func (a *AIOrchestrator) SynthesizeAndGetAudio(ctx context.Context, callState *state.CallState, textToPlay string) (string, error) {
	l := ctxlogger.FromContext(ctx)
	l.Info().Str("text", textToPlay).Msg("Metin sese dönüştürülüyor...")

	var speakerURL, voiceSelector string
	var useCloning bool

	if callState.Event != nil && callState.Event.Dialplan != nil && callState.Event.Dialplan.Action != nil && callState.Event.Dialplan.Action.ActionData != nil && callState.Event.Dialplan.Action.ActionData.Data != nil {
		actionData := callState.Event.Dialplan.Action.ActionData.Data
		speakerURL, useCloning = actionData["speaker_wav_url"]
		voiceSelector = actionData["voice_selector"]
	}

	mdCtx := metadata.AppendToOutgoingContext(ctx, "x-trace-id", callState.TraceID)
	languageCode := getLanguageCode(callState.Event)
	ttsReq := &ttsv1.SynthesizeRequest{Text: textToPlay, LanguageCode: languageCode}

	if useCloning && speakerURL != "" {
		if !isAllowedSpeakerURL(speakerURL, a.cfg.AgentAllowedSpeakerDomains) {
			err := fmt.Errorf("güvenlik uyarisi: İzin verilmeyen bir speaker_wav_url engellendi: %s", speakerURL)
			l.Error().Err(err).Send()
			return "", err
		}
		ttsReq.SpeakerWavUrl = &speakerURL
	} else if voiceSelector != "" {
		l.Info().Str("voice_selector", voiceSelector).Msg("Dinamik ses seçici kullanılıyor.")
		ttsReq.VoiceSelector = &voiceSelector
	}

	ttsCtx, ttsCancel := context.WithTimeout(mdCtx, 20*time.Second)
	defer ttsCancel()

	ttsResp, err := a.ttsClient.Synthesize(ttsCtx, ttsReq)
	if err != nil {
		l.Error().Err(err).Msg("TTS Gateway'den yanıt alınamadı (muhtemelen zaman aşımı).")
		return "", err
	}
	if ttsResp == nil {
		err := fmt.Errorf("TTS Gateway'den hata dönmedi ancak yanıt boş (nil)")
		l.Error().Err(err).Msg("Bu beklenmedik bir durum.")
		return "", err
	}

	audioBytes := ttsResp.GetAudioContent()
	// DÜZELTME: 'b64' yerine 'base64' kullanılıyor.
	audioURI := fmt.Sprintf("data:audio/wav;base64,%s", base64.StdEncoding.EncodeToString(audioBytes))
	return audioURI, nil
}

// StreamAndTranscribe, media-service'ten ses akışı alıp STT servisine gönderir ve sonucu döndürür.
func (a *AIOrchestrator) StreamAndTranscribe(ctx context.Context, callState *state.CallState) (TranscriptionResult, error) {
	l := ctxlogger.FromContext(ctx)
	var result TranscriptionResult

	portVal, ok := callState.Event.Media["server_rtp_port"]
	if !ok {
		return result, fmt.Errorf("kritik hata: CallState içinde 'server_rtp_port' bulunamadı")
	}
	serverRtpPortFloat, ok := portVal.(float64)
	if !ok {
		l.Error().Interface("value", portVal).Msg("Kritik hata: 'server_rtp_port' beklenen float64 tipinde değil.")
		return result, fmt.Errorf("kritik hata: 'server_rtp_port' tipi geçersiz")
	}

	grpcCtx := metadata.AppendToOutgoingContext(ctx, "x-trace-id", callState.TraceID)
	mediaStream, err := a.mediaClient.RecordAudio(grpcCtx, &mediav1.RecordAudioRequest{
		ServerRtpPort:    uint32(serverRtpPortFloat),
		TargetSampleRate: &a.cfg.SttServiceTargetSampleRate,
	})
	if err != nil {
		return result, fmt.Errorf("media service ile stream oluşturulamadı: %w", err)
	}
	l.Info().Msg("Media-Service'ten ses akışı başlatıldı.")

	u, err := url.Parse(a.sttClient.BaseURL())
	if err != nil {
		return result, fmt.Errorf("stt service url parse edilemedi: %w", err)
	}

	scheme := "wss"
	if u.Scheme == "http" {
		scheme = "ws"
	}

	sttURL := url.URL{Scheme: scheme, Host: u.Host, Path: "/api/v1/transcribe-stream"}
	q := sttURL.Query()
	q.Set("language", getLanguageCode(callState.Event))
	q.Set("logprob_threshold", fmt.Sprintf("%f", a.cfg.SttServiceLogprobThreshold))
	q.Set("no_speech_threshold", fmt.Sprintf("%f", a.cfg.SttServiceNoSpeechThreshold))
	vadLevel := "1"
	if callState.Event.Dialplan.Action.ActionData != nil && callState.Event.Dialplan.Action.ActionData.Data != nil {
		if val, ok := callState.Event.Dialplan.Action.ActionData.Data["stt_vad_level"]; ok {
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
	resultChan := make(chan TranscriptionResult, 1)
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()

	go func() {
		defer func() {
			wsConn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		}()
		for {
			select {
			case <-streamCtx.Done():
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
					switch resType {
					case "final":
						if text, ok := res["text"].(string); ok {
							resultChan <- TranscriptionResult{Text: text, IsNoSpeechTimeout: false}
							return
						}
					case "no_speech_timeout":
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

// --- Yardımcı Fonksiyonlar ---

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

func isAllowedSpeakerURL(rawURL, allowedDomainsCSV string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}

	allowedDomains := strings.Split(allowedDomainsCSV, ",")
	domainMap := make(map[string]bool)
	for _, domain := range allowedDomains {
		trimmedDomain := strings.TrimSpace(domain)
		if trimmedDomain != "" {
			domainMap[trimmedDomain] = true
		}
	}
	return domainMap[u.Hostname()]
}
