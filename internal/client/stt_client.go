// ========== DOSYA: sentiric-agent-service/internal/client/stt_client.go (TAM VE DOĞRU İÇERİK) ==========
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
	"strings"
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

func NewSttClient(rawBaseURL string, log zerolog.Logger) *SttClient {
	finalBaseURL := rawBaseURL
	// --- DEĞİŞİKLİK: Eğer URL'de şema yoksa, varsayılan olarak http ekle. ---
	if !strings.HasPrefix(rawBaseURL, "http://") && !strings.HasPrefix(rawBaseURL, "https://") {
		finalBaseURL = "http://" + rawBaseURL
	}
	// --- DEĞİŞİKLİK SONU ---

	return &SttClient{
		httpClient: &http.Client{},
		baseURL:    finalBaseURL, // Düzeltilmiş URL'i kullan
		log:        log.With().Str("client", "stt").Logger(),
	}
}

func (c *SttClient) BaseURL() string {
	return c.baseURL
}

func (c *SttClient) Transcribe(ctx context.Context, audioData []byte, language, traceID string) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("language", language)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="audio_file"; filename="stream.wav"`)
	h.Set("Content-Type", "audio/wav")
	part, _ := writer.CreatePart(h)
	_, _ = io.Copy(part, bytes.NewReader(audioData))
	writer.Close()

	url := fmt.Sprintf("%s/api/v1/transcribe", c.baseURL)
	c.log.Info().Str("url", url).Int("audio_size_kb", len(audioData)/1024).Msg("STT'ye transkripsiyon isteği gönderiliyor...")

	reqCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "POST", url, &body)
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
