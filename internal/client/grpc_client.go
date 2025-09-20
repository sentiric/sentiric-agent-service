//  sentiric-agent-service/internal/client/grpc_client.go

package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	// "net/http" // ARTIK GEREKLİ DEĞİL
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
	conn, err := createSecureGrpcClient(cfg, cfg.MediaServiceGrpcURL)
	if err != nil {
		return nil, fmt.Errorf("media service istemcisi için bağlantı oluşturulamadı: %w", err)
	}
	return mediav1.NewMediaServiceClient(conn), nil
}

func NewUserServiceClient(cfg *config.Config) (userv1.UserServiceClient, error) {
	conn, err := createSecureGrpcClient(cfg, cfg.UserServiceGrpcURL)
	if err != nil {
		return nil, fmt.Errorf("user service istemcisi için bağlantı oluşturulamadı: %w", err)
	}
	return userv1.NewUserServiceClient(conn), nil
}

func NewTTSServiceClient(cfg *config.Config) (ttsv1.TextToSpeechServiceClient, error) {
	conn, err := createSecureGrpcClient(cfg, cfg.TtsServiceGrpcURL)
	if err != nil {
		return nil, fmt.Errorf("tts gateway istemcisi için bağlantı oluşturulamadı: %w", err)
	}
	return ttsv1.NewTextToSpeechServiceClient(conn), nil
}

func NewKnowledgeServiceClient(cfg *config.Config) (knowledgev1.KnowledgeServiceClient, error) {
	if cfg.KnowledgeServiceGrpcURL == "" {
		return nil, nil
	}
	conn, err := createSecureGrpcClient(cfg, cfg.KnowledgeServiceGrpcURL)
	if err != nil {
		return nil, fmt.Errorf("knowledge service istemcisi için bağlantı oluşturulamadı: %w", err)
	}
	return knowledgev1.NewKnowledgeServiceClient(conn), nil
}

// DÜZELTİLMİŞ FONKSİYON
func createSecureGrpcClient(cfg *config.Config, addr string) (*grpc.ClientConn, error) {
	// --- KALDIRILAN BÖLÜM ---
	// Artık burada manuel bir HTTP health check döngüsü yok.
	// Docker Compose'daki `depends_on` ve `healthcheck` direktifleri,
	// servisin TCP portu açmasını beklemek için yeterlidir.
	// gRPC'nin kendi `WithBlock` ve timeout mekanizması, bağlantı için daha doğru bir yöntemdir.
	// --- KALDIRILAN BÖLÜM SONU ---
	
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
    // Bağlantı için 15 saniyelik bir zaman aşımı ekliyoruz.
    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second) 
	defer cancel()

	// WithBlock() seçeneği, bağlantı kurulana kadar bekler veya zaman aşımına uğrar.
    conn, err := grpc.DialContext(ctx, target, grpc.WithTransportCredentials(creds), grpc.WithBlock())

	if err != nil {
		return nil, fmt.Errorf("gRPC sunucusuna (%s) bağlanılamadı: %w", addr, err)
	}

	return conn, nil
}