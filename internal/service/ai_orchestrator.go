// sentiric-agent-service/internal/service/ai_orchestrator.go
package service

import (
	"context"
	"encoding/base64"
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
	knowledgev1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/knowledge/v1"
	mediav1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/media/v1"
	ttsv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/tts/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type KnowledgeClientInterface interface {
	Query(ctx context.Context, req *knowledgev1.QueryRequest) (*knowledgev1.QueryResponse, error)
}

type TranscriptionResult struct {
	Text              string
	IsNoSpeechTimeout bool
}

type AIOrchestrator struct {
	cfg             *config.Config
	llmClient       *client.LlmClient
	sttClient       *client.SttClient
	// DÜZELTME: İstemci tipi güncellendi
	ttsClient       ttsv1.TtsGatewayServiceClient
	mediaClient     mediav1.MediaServiceClient
	knowledgeClient KnowledgeClientInterface
}

func NewAIOrchestrator(
	cfg *config.Config,
	llmC *client.LlmClient,
	sttC *client.SttClient,
	// DÜZELTME: İstemci tipi güncellendi
	ttsC ttsv1.TtsGatewayServiceClient,
	mediaC mediav1.MediaServiceClient,
	knowC KnowledgeClientInterface,
) *AIOrchestrator {
	return &AIOrchestrator{
		cfg:             cfg,
		llmClient:       llmC,
		sttClient:       sttC,
		ttsClient:       ttsC,
		mediaClient:     mediaC,
		knowledgeClient: knowC,
	}
}

func (a *AIOrchestrator) QueryKnowledgeBase(ctx context.Context, query string, callState *state.CallState) (string, error) {
	l := ctxlogger.FromContext(ctx)
	if a.knowledgeClient == nil {
		l.Warn().Msg("Knowledge service istemcisi yapılandırılmamış, RAG sorgulaması atlanıyor.")
		return "", nil
	}
	l.Debug().Str("query", query).Msg("Knowledge base sorgulanıyor...")
	req := &knowledgev1.QueryRequest{
		TenantId: callState.TenantID,
		Query:    query,
		TopK:     int32(a.cfg.KnowledgeServiceTopK),
	}
	queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	res, err := a.knowledgeClient.Query(queryCtx, req)
	if err != nil {
		st, ok := status.FromError(err)
		if ok && (st.Code() == codes.NotFound || st.Code() == codes.Unavailable) {
			l.Warn().Err(err).Msg("Knowledge service'e ulaşılamadı veya sonuç bulunamadı, RAG olmadan devam edilecek.")
			return "", nil
		}
		l.Error().Err(err).Msg("Knowledge base sorgulanırken beklenmedik bir hata oluştu.")
		return "", err
	}
	if len(res.GetResults()) == 0 {
		l.Info().Msg("Knowledge base'de ilgili sonuç bulunamadı.")
		return "", nil
	}
	var contextBuilder strings.Builder
	contextBuilder.WriteString("İlgili Bilgiler:\n")
	for i, result := range res.GetResults() {
		contextBuilder.WriteString(fmt.Sprintf("%d. %s\n", i+1, result.GetContent()))
	}
	contextStr := contextBuilder.String()
	l.Info().Int("result_count", len(res.GetResults())).Msg("Knowledge base'den sonuçlar başarıyla alındı.")
	return contextStr, nil
}

func (a *AIOrchestrator) GenerateResponse(ctx context.Context, prompt string, callState *state.CallState) (string, error) {
	return a.llmClient.Generate(ctx, prompt, callState.TraceID)
}

func (a *AIOrchestrator) SynthesizeAndGetAudio(ctx context.Context, callState *state.CallState, textToPlay string) (string, error) {
	l := ctxlogger.FromContext(ctx)
	l.Debug().Str("text", textToPlay).Msg("Metin sese dönüştürülüyor...")
	
	// Varsayılanlar
	voiceID := "default" 
	
	if callState.Event.Dialplan != nil && callState.Event.Dialplan.Action != nil && callState.Event.Dialplan.Action.ActionData != nil && callState.Event.Dialplan.Action.ActionData.Data != nil {
		actionData := callState.Event.Dialplan.Action.ActionData.Data
		if val, ok := actionData["voice_selector"]; ok && val != "" {
			voiceID = val
			l.Debug().Str("voice_id", voiceID).Msg("Dialplan'dan gelen ses seçimi kullanılıyor.")
		}
	}

	mdCtx := metadata.AppendToOutgoingContext(ctx, "x-trace-id", callState.TraceID)
	languageCode := GetLanguageCode(callState.Event)
	
	// --- GÜNCEL KONTRAK YAPISI ---
	// TtsGatewayService.Synthesize çağrısı
	ttsReq := &ttsv1.SynthesizeRequest{
		Text:     textToPlay,
		TextType: ttsv1.TextType_TEXT_TYPE_TEXT, // Düz metin
		VoiceId:  voiceID,
		AudioConfig: &ttsv1.AudioConfig{
			AudioFormat:      ttsv1.AudioFormat_AUDIO_FORMAT_WAV,
			SampleRateHertz: 8000, // Telekom standardı
			VolumeGainDb:    0.0,
		},
		Prosody: &ttsv1.ProsodyConfig{
			Rate:    1.0,
			Pitch:   1.0,
			Emotion: "neutral",
		},
		// PreferredProvider boş bırakılırsa Gateway en uygun olanı seçer (Coqui, Edge, MMS vb.)
		PreferredProvider: "", 
	}

	ttsCtx, ttsCancel := context.WithTimeout(mdCtx, 20*time.Second)
	defer ttsCancel()
	
	ttsResp, err := a.ttsClient.Synthesize(ttsCtx, ttsReq)
	if err != nil {
		l.Error().Err(err).Msg("TTS Gateway'den yanıt alınamadı veya hata döndü.")
		return "", err
	}
	
	audioBytes := ttsResp.GetAudioContent()
	if len(audioBytes) == 0 {
		return "", fmt.Errorf("TTS Gateway boş ses verisi döndürdü")
	}

	audioURI := fmt.Sprintf("data:audio/wav;base64,%s", base64.StdEncoding.EncodeToString(audioBytes))
	return audioURI, nil
}

func (a *AIOrchestrator) StreamAndTranscribe(ctx context.Context, callState *state.CallState) (TranscriptionResult, error) {
	l := ctxlogger.FromContext(ctx)
	var result TranscriptionResult

	if callState.Event == nil || callState.Event.Media == nil {
		return result, fmt.Errorf("kritik hata: StreamAndTranscribe için medya bilgisi (MediaInfo) bulunamadı")
	}
	serverRtpPort := callState.Event.Media.ServerRtpPort

	grpcCtx := metadata.AppendToOutgoingContext(ctx, "x-trace-id", callState.TraceID)
	mediaStream, err := a.mediaClient.RecordAudio(grpcCtx, &mediav1.RecordAudioRequest{
		ServerRtpPort:    uint32(serverRtpPort),
		TargetSampleRate: &a.cfg.SttServiceTargetSampleRate,
	})
	if err != nil {
		return result, fmt.Errorf("media service ile stream oluşturulamadı: %w", err)
	}
	l.Debug().Msg("Media-Service'ten ses akışı başlatıldı.")

	sttURL, err := a.buildSttUrl(ctx, callState)
	if err != nil {
		return result, err
	}

	l.Debug().Str("url", sttURL.String()).Msg("STT-Service'e WebSocket bağlantısı kuruluyor...")
	var wsConn *websocket.Conn
	maxRetries := 3
	retryDelay := 2 * time.Second
	for i := 0; i < maxRetries; i++ {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
		wsConn, _, err = websocket.DefaultDialer.Dial(sttURL.String(), nil)
		if err == nil {
			break
		}
		l.Warn().Err(err).Int("attempt", i+1).Msg("STT-Service'e WebSocket bağlantısı kurulamadı, tekrar denenecek...")
		time.Sleep(retryDelay)
	}
	if err != nil {
		return result, fmt.Errorf("%d deneme sonrası STT-Service'e bağlanılamadı: %w", maxRetries, err)
	}
	defer wsConn.Close()
	l.Info().Msg("STT-Service'e WebSocket bağlantısı başarılı.")

	resultChan := make(chan TranscriptionResult, 1)
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()

	go a.listenToStt(streamCtx, wsConn, resultChan)

	for {
		chunk, err := mediaStream.Recv()
		if err == io.EOF || status.Code(err) == codes.Canceled {
			l.Info().Msg("Media stream sonlandı.")
			wsConn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			break
		}
		if err != nil {
			l.Error().Err(err).Msg("Media stream'den okuma hatası.")
			return result, err
		}
		if err := wsConn.WriteMessage(websocket.BinaryMessage, chunk.AudioData); err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				l.Error().Err(err).Msg("WebSocket'e yazma hatası.")
			}
			break
		}
	}

	select {
	case res, ok := <-resultChan:
		if !ok {
			l.Warn().Msg("Transkript alınamadan STT dinleyici kanalı kapandı.")
			return TranscriptionResult{Text: "", IsNoSpeechTimeout: true}, nil
		}
		l.Info().Str("transcribed_text", res.Text).Bool("is_timeout", res.IsNoSpeechTimeout).Msg("Nihai transkript sonucu alındı.")
		return res, nil
	case <-time.After(time.Duration(a.cfg.SttServiceStreamTimeoutSeconds) * time.Second):
		l.Warn().Msg("Genel transkripsiyon zaman aşımına ulaşıldı.")
		return TranscriptionResult{Text: "", IsNoSpeechTimeout: true}, nil
	}
}

func (a *AIOrchestrator) buildSttUrl(ctx context.Context, callState *state.CallState) (*url.URL, error) {
	l := ctxlogger.FromContext(ctx)
	baseURL := a.sttClient.BaseURL()
	u, err := url.Parse(baseURL)
	if err != nil {
		l.Error().Err(err).Str("invalid_base_url", baseURL).Msg("STT Service base URL'i parse edilemedi.")
		return nil, fmt.Errorf("stt service url parse edilemedi: %w", err)
	}
	scheme := "wss"
	if u.Scheme == "http" {
		scheme = "ws"
	}
	sttURL := url.URL{Scheme: scheme, Host: u.Host, Path: "/api/v1/transcribe-stream"}
	q := sttURL.Query()
	q.Set("language", GetLanguageCode(callState.Event))
	q.Set("logprob_threshold", fmt.Sprintf("%f", a.cfg.SttServiceLogprobThreshold))
	q.Set("no_speech_threshold", fmt.Sprintf("%f", a.cfg.SttServiceNoSpeechThreshold))
	vadLevel := "1"
	if callState.Event.Dialplan != nil && callState.Event.Dialplan.Action != nil && callState.Event.Dialplan.Action.ActionData != nil && callState.Event.Dialplan.Action.ActionData.Data != nil {
		if val, ok := callState.Event.Dialplan.Action.ActionData.Data["stt_vad_level"]; ok {
			vadLevel = val
		}
	}
	q.Set("vad_aggressiveness", vadLevel)
	sttURL.RawQuery = q.Encode()
	return &sttURL, nil
}

func (a *AIOrchestrator) listenToStt(ctx context.Context, wsConn *websocket.Conn, resultChan chan<- TranscriptionResult) {
	defer close(resultChan)
	for {
		select {
		case <-ctx.Done():
			return
		default:
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
	}
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