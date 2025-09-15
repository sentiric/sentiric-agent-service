// File: sentiric-agent-service/internal/client/llm_client.go

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

type LlmGenerateRequest struct {
	Prompt string `json:"prompt"`
}

type LlmGenerateResponse struct {
	Text string `json:"text"`
}

type LlmClient struct {
	httpClient *http.Client
	baseURL    string // Bu artık "http://llm-service:16010" gibi tam bir URL olacak
	log        zerolog.Logger
}

func NewLlmClient(rawBaseURL string, log zerolog.Logger) *LlmClient {
	finalBaseURL := rawBaseURL
	// Eğer URL'de şema yoksa, varsayılan olarak http ekle.
	if !strings.HasPrefix(rawBaseURL, "http://") && !strings.HasPrefix(rawBaseURL, "https://") {
		finalBaseURL = "http://" + rawBaseURL
	}

	return &LlmClient{
		httpClient: &http.Client{},
		baseURL:    finalBaseURL, // Düzeltilmiş URL'i kullan
		log:        log.With().Str("client", "llm").Logger(),
	}
}

func (c *LlmClient) Generate(ctx context.Context, prompt, traceID string) (string, error) {
	// URL oluşturma mantığını basitleştiriyoruz, çünkü baseURL artık tam.
	url := fmt.Sprintf("%s/generate", c.baseURL)

	payload := LlmGenerateRequest{Prompt: prompt}
	payloadBytes, _ := json.Marshal(payload)

	c.log.Info().Str("url", url).Int("prompt_size", len(prompt)).Msg("LLM'e istek gönderiliyor...")
	c.log.Debug().Str("prompt", prompt).Msg("Gönderilen tam LLM prompt'u")

	// --- YENİ: İsteğe özel zaman aşımı ---
	// Çağıran yerden gelen ana context'i 20 saniyelik bir timeout ile sarmalıyoruz.
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	// --- DEĞİŞİKLİK SONU ---

	req, err := http.NewRequestWithContext(reqCtx, "POST", url, bytes.NewBuffer(payloadBytes)) // reqCtx'i kullan
	if err != nil {
		return "", fmt.Errorf("LLM isteği oluşturulamadı: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Trace-ID", traceID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Zaman aşımı hatası burada yakalanacak (context.DeadlineExceeded)
		c.log.Error().Err(err).Msg("LLM isteği başarısız oldu (muhtemelen zaman aşımı).")
		return "", fmt.Errorf("LLM isteği başarısız oldu: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		c.log.Error().Int("status_code", resp.StatusCode).Bytes("body", bodyBytes).Msg("LLM servisi hata döndürdü")
		return "", fmt.Errorf("LLM servisi %d durum kodu döndürdü", resp.StatusCode)
	}

	var llmResp LlmGenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
		return "", fmt.Errorf("LLM yanıtı çözümlenemedi: %w", err)
	}

	cleanedText := strings.Trim(llmResp.Text, "\" \n\r")
	c.log.Info().Int("response_size", len(cleanedText)).Str("response_text", cleanedText).Msg("LLM'den yanıt başarıyla alındı.")

	return cleanedText, nil
}
