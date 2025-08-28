// File: sentiric-agent-service/internal/client/stt_client.go

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
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
		// Genel timeout'u kaldırıyoruz
		httpClient: &http.Client{},
		baseURL:    baseURL,
		log:        log.With().Str("client", "stt").Logger(),
	}
}

func (c *SttClient) BaseURL() string {
	return c.baseURL
}

func (c *SttClient) Transcribe(ctx context.Context, audioData []byte, language, traceID string) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("language", language); err != nil {
		return "", fmt.Errorf("dil alanı yazılamadı: %w", err)
	}

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="audio_file"; filename="stream.wav"`)
	h.Set("Content-Type", "audio/wav")
	part, err := writer.CreatePart(h)
	if err != nil {
		return "", fmt.Errorf("form part'ı oluşturulamadı: %w", err)
	}
	if _, err := io.Copy(part, bytes.NewReader(audioData)); err != nil {
		return "", fmt.Errorf("ses verisi forma kopyalanamadı: %w", err)
	}
	writer.Close()

	url := fmt.Sprintf("%s/api/v1/transcribe", c.baseURL)
	c.log.Info().Str("url", url).Int("audio_size_kb", len(audioData)/1024).Msg("STT'ye transkripsiyon isteği gönderiliyor...")

	// --- YENİ: İsteğe özel zaman aşımı ---
	// STT işlemi daha uzun sürebilir, 60 saniye verelim.
	reqCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	// --- DEĞİŞİKLİK SONU ---

	req, err := http.NewRequestWithContext(reqCtx, "POST", url, &body) // reqCtx'i kullan
	if err != nil {
		return "", fmt.Errorf("STT isteği oluşturulamadı: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Trace-ID", traceID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.log.Error().Err(err).Msg("STT isteği başarısız oldu (muhtemelen zaman aşımı).")
		return "", fmt.Errorf("STT isteği başarısız oldu: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		c.log.Error().Int("status_code", resp.StatusCode).Bytes("body", bodyBytes).Str("url", url).Msg("STT servisi hata döndürdü")
		return "", fmt.Errorf("STT servisi %d durum kodu döndürdü", resp.StatusCode)
	}

	var sttResp SttTranscribeResponse
	if err := json.NewDecoder(resp.Body).Decode(&sttResp); err != nil {
		return "", fmt.Errorf("STT yanıtı çözümlenemedi: %w", err)
	}

	c.log.Info().Str("transcribed_text", sttResp.Text).Msg("Ses başarıyla metne çevrildi.")

	return sttResp.Text, nil
}
