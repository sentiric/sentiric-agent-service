package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings" // Bu import'u eklemeyi unutma
	"time"

	"github.com/rs/zerolog"
	knowledgev1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/knowledge/v1"
)

// KnowledgeClient, knowledge-service'e HTTP istekleri gönderir.
type KnowledgeClient struct {
	httpClient *http.Client
	baseURL    string
	log        zerolog.Logger
}

func NewKnowledgeClient(rawBaseURL string, log zerolog.Logger) *KnowledgeClient {
	finalBaseURL := rawBaseURL
	// Eğer URL'de şema (http:// veya https://) yoksa, varsayılan olarak http ekle.
	if !strings.HasPrefix(rawBaseURL, "http://") && !strings.HasPrefix(rawBaseURL, "https://") {
		finalBaseURL = "http://" + rawBaseURL
	}

	return &KnowledgeClient{
		httpClient: &http.Client{},
		baseURL:    finalBaseURL, // Düzeltilmiş ve şema içeren URL'i kullan
		log:        log.With().Str("client", "knowledge-http").Logger(),
	}
}

// Query, bilgi tabanına bir sorgu gönderir.
func (c *KnowledgeClient) Query(ctx context.Context, req *knowledgev1.QueryRequest) (*knowledgev1.QueryResponse, error) {
	payloadBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("knowledge service isteği JSON'a çevrilemedi: %w", err)
	}

	// URL oluşturma mantığı basitleşti, çünkü baseURL artık tam bir URL.
	url := fmt.Sprintf("%s/api/v1/query", c.baseURL)
	c.log.Info().Str("url", url).Str("query", req.Query).Msg("Knowledge Service'e (HTTP) sorgu gönderiliyor...")

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, "POST", url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("knowledge service isteği oluşturulamadı: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.log.Error().Err(err).Msg("Knowledge service (HTTP) isteği başarısız oldu.")
		return nil, fmt.Errorf("knowledge service (HTTP) isteği başarısız oldu: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		c.log.Error().Int("status_code", resp.StatusCode).Bytes("body", bodyBytes).Msg("Knowledge service hata döndürdü")
		return nil, fmt.Errorf("knowledge service %d durum kodu döndürdü", resp.StatusCode)
	}

	var knowledgeResp knowledgev1.QueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&knowledgeResp); err != nil {
		return nil, fmt.Errorf("knowledge service yanıtı çözümlenemedi: %w", err)
	}

	return &knowledgeResp, nil
}
