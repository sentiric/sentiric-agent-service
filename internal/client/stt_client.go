package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

type SttTranscribeResponse struct {
	Text string `json:"text"`
}

type SttClient struct {
	httpClient *http.Client
	baseURL    string
	log        zerolog.Logger
}

func NewSttClient(baseURL string, log zerolog.Logger) *SttClient {
	return &SttClient{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		baseURL:    baseURL,
		log:        log.With().Str("client", "stt").Logger(),
	}
}

func (c *SttClient) Transcribe(ctx context.Context, audioData []byte, language, traceID string) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("language", language); err != nil {
		return "", fmt.Errorf("failed to write language field: %w", err)
	}
	part, err := writer.CreateFormFile("audio_file", "stream.wav")
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := io.Copy(part, bytes.NewReader(audioData)); err != nil {
		return "", fmt.Errorf("failed to copy audio data to form: %w", err)
	}
	writer.Close()

	url := fmt.Sprintf("%s/api/v1/transcribe", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, &body)
	if err != nil {
		return "", fmt.Errorf("failed to create stt request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Trace-ID", traceID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("stt request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		c.log.Error().Int("status_code", resp.StatusCode).Bytes("body", bodyBytes).Msg("STT service returned an error")
		return "", fmt.Errorf("STT service returned status %d", resp.StatusCode)
	}

	var sttResp SttTranscribeResponse
	if err := json.NewDecoder(resp.Body).Decode(&sttResp); err != nil {
		return "", fmt.Errorf("failed to decode stt response: %w", err)
	}

	c.log.Info().Str("transcribed_text", sttResp.Text).Msg("Audio transcribed successfully")
	return sttResp.Text, nil
}
