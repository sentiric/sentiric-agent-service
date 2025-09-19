//  sentiric-agent-service/internal/client/grpc_client.go
package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http" // YENİ: HTTP istemcisi için import
	"os"
	"strings"
	"time"

	"github.com/sentiric/sentiric-agent-service/internal/config"
	knowledgev1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/knowledge/v1"
	mediav1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/media/v1"
	ttsv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/tts/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)


func NewMediaServiceClient(cfg *config.Config) (mediav1.MediaServiceClient, error) {
	conn, err := createSecureGrpcClient(cfg, cfg.MediaServiceGrpcURL, "") // Media service'in health endpoint'i yok, bu yüzden boş bırakıyoruz.
	if err != nil {
		return nil, fmt.Errorf("media service istemcisi için bağlantı oluşturulamadı: %w", err)
	}
	return mediav1.NewMediaServiceClient(conn), nil
}

func NewUserServiceClient(cfg *config.Config) (userv1.UserServiceClient, error) {
	conn, err := createSecureGrpcClient(cfg, cfg.UserServiceGrpcURL, "") // User service'in health endpoint'i yok.
	if err != nil {
		return nil, fmt.Errorf("user service istemcisi için bağlantı oluşturulamadı: %w", err)
	}
	return userv1.NewUserServiceClient(conn), nil
}

func NewTTSServiceClient(cfg *config.Config) (ttsv1.TextToSpeechServiceClient, error) {
	conn, err := createSecureGrpcClient(cfg, cfg.TtsServiceGrpcURL, "") // TTS Gateway'in health endpoint'i yok.
	if err != nil {
		return nil, fmt.Errorf("tts gateway istemcisi için bağlantı oluşturulamadı: %w", err)
	}
	return ttsv1.NewTextToSpeechServiceClient(conn), nil
}

func NewKnowledgeServiceClient(cfg *config.Config) (knowledgev1.KnowledgeServiceClient, error) {
	if cfg.KnowledgeServiceGrpcURL == "" {
		return nil, nil
	}
	// YENİ: Health check URL'ini de gönderiyoruz.
	conn, err := createSecureGrpcClient(cfg, cfg.KnowledgeServiceGrpcURL, cfg.KnowledgeServiceURL+"/health")
	if err != nil {
		return nil, fmt.Errorf("knowledge service istemcisi için bağlantı oluşturulamadı: %w", err)
	}
	return knowledgev1.NewKnowledgeServiceClient(conn), nil
}

func createSecureGrpcClient(cfg *config.Config, addr string, healthCheckURL string) (*grpc.ClientConn, error) {
	// --- YENİ: HEALTH CHECK MANTIĞI ---
	if healthCheckURL != "" {
		maxRetries := 12 // 12 * 5s = 60 saniye toplam bekleme
		retryDelay := 5 * time.Second
		httpClient := &http.Client{Timeout: 3 * time.Second}
		
		fmt.Printf("gRPC bağlantısı kurulmadan önce '%s' adresinin sağlıklı olması bekleniyor...\n", healthCheckURL)
		for i := 0; i < maxRetries; i++ {
			resp, err := httpClient.Get(healthCheckURL)
			if err == nil && resp.StatusCode == http.StatusOK {
				fmt.Printf("'%s' başarıyla yanıt verdi (status 200 OK).\n", healthCheckURL)
				resp.Body.Close()
				break // Sağlıklı, döngüden çık.
			}
			
			if resp != nil {
				resp.Body.Close()
			}

			if i == maxRetries-1 {
				return nil, fmt.Errorf("maksimum deneme (%d) sonrası servis '%s' sağlıklı duruma geçemedi", maxRetries, healthCheckURL)
			}
			
			fmt.Printf("Servis '%s' henüz hazır değil (deneme %d/%d). %v saniye sonra tekrar denenecek.\n", healthCheckURL, i+1, maxRetries, retryDelay.Seconds())
			time.Sleep(retryDelay)
		}
	}
	// --- HEALTH CHECK MANTIĞI SONU ---

	// --- Mevcut gRPC Bağlantı Mantığı ---
	clientCert, err := tls.LoadX509KeyPair(cfg.AgentServiceCertPath, cfg.AgentServiceKeyPath)
	if err != nil {
		return nil, fmt.Errorf("istemci sertifikası yüklenemedi: %w", err)
	}

	caCert, err := os.ReadFile(cfg.GrpcTlsCaPath)
	if err != nil {
		return nil, fmt.Errorf("CA sertifikası okunamadı: %w", err)
	}
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("CA sertifikası havuza eklenemedi")
	}

	serverName := strings.Split(addr, ":")[0]
	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caCertPool,
		ServerName:   serverName,
		MinVersion:   tls.VersionTLS12,
	})

	target := fmt.Sprintf("passthrough:///%s", addr)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second) // Timeout'u biraz artırdık.
	defer cancel()

	conn, err := grpc.DialContext(ctx, target, grpc.WithTransportCredentials(creds), grpc.WithBlock())
	if err != nil {
		return nil, fmt.Errorf("gRPC sunucusuna (%s) bağlanılamadı: %w", addr, err)
	}

	return conn, nil
}