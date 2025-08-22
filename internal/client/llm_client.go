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
	baseURL    string
	log        zerolog.Logger
}

func NewLlmClient(baseURL string, log zerolog.Logger) *LlmClient {
	return &LlmClient{
		httpClient: &http.Client{Timeout: 20 * time.Second},
		baseURL:    baseURL,
		log:        log.With().Str("client", "llm").Logger(),
	}
}

func (c *LlmClient) Generate(ctx context.Context, prompt, traceID string) (string, error) {
	payload := LlmGenerateRequest{Prompt: prompt}
	payloadBytes, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/generate", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create llm request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Trace-ID", traceID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		c.log.Error().Int("status_code", resp.StatusCode).Bytes("body", bodyBytes).Msg("LLM service returned an error")
		return "", fmt.Errorf("LLM service returned status %d", resp.StatusCode)
	}

	var llmResp LlmGenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
		return "", fmt.Errorf("failed to decode llm response: %w", err)
	}

	// Trim quotes and whitespace that models sometimes add
	return strings.Trim(llmResp.Text, "\" \n\r"), nil
}
