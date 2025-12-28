// ========== DOSYA: sentiric-agent-service/internal/client/llm_client.go (DÜZELTİLMİŞ) ==========
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// OpenAI Uyumlu İstek Yapısı
type OpenAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OpenAIChatRequest struct {
	Model       string              `json:"model"`
	Messages    []OpenAIChatMessage `json:"messages"`
	Temperature float64             `json:"temperature"`
	MaxTokens   int                 `json:"max_tokens"`
	Stream      bool                `json:"stream"`
}

// OpenAI Uyumlu Yanıt Yapısı
type OpenAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type LlmClient struct {
	httpClient *http.Client
	baseURL    string
	log        zerolog.Logger
}

func NewLlmClient(rawBaseURL string, log zerolog.Logger) *LlmClient {
	finalBaseURL := rawBaseURL
	// Şema kontrolü
	if !strings.HasPrefix(rawBaseURL, "http://") && !strings.HasPrefix(rawBaseURL, "https://") {
		finalBaseURL = "http://" + rawBaseURL
	}

	return &LlmClient{
		httpClient: &http.Client{},
		baseURL:    finalBaseURL,
		log:        log.With().Str("client", "llm").Logger(),
	}
}

func (c *LlmClient) Generate(ctx context.Context, prompt, traceID string) (string, error) {
	// DÜZELTME: OpenAI Compatible Endpoint
	url := fmt.Sprintf("%s/v1/chat/completions", c.baseURL)
	
	// Prompt'u OpenAI formatına çevir
	payload := OpenAIChatRequest{
		Model: "default", // Llama service bunu profilden seçecek
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: prompt},
		},
		Temperature: 0.7,
		MaxTokens:   256,
		Stream:      false,
	}

	payloadBytes, _ := json.Marshal(payload)
	c.log.Debug().Str("url", url).Int("prompt_size", len(prompt)).Msg("LLM'e (OpenAI API) istek gönderiliyor...")
	
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second) // Timeout artırıldı (LLM yavaş olabilir)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "POST", url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return "", fmt.Errorf("LLM isteği oluşturulamadı: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Trace-ID", traceID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.log.Error().Err(err).Msg("LLM isteği başarısız oldu (Bağlantı hatası).")
		return "", fmt.Errorf("LLM isteği başarısız oldu: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		c.log.Error().Int("status_code", resp.StatusCode).Bytes("body", bodyBytes).Msg("LLM servisi hata döndürdü")
		return "", fmt.Errorf("LLM servisi %d durum kodu döndürdü", resp.StatusCode)
	}

	var llmResp OpenAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
		return "", fmt.Errorf("LLM yanıtı çözümlenemedi: %w", err)
	}

	if len(llmResp.Choices) == 0 {
		return "", fmt.Errorf("LLM boş yanıt döndü")
	}

	text := llmResp.Choices[0].Message.Content
	cleanedText := strings.Trim(text, "\" \n\r")
	
	c.log.Debug().Int("response_size", len(cleanedText)).Msg("LLM'den yanıt başarıyla alındı.")
	return cleanedText, nil
}