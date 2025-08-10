// AÇIKLAMA: Bu paket, diğer servislere gRPC istemci bağlantıları oluşturmaktan sorumludur.
package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sentiric/sentiric-agent-service/internal/config"
	mediav1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/media/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// NewMediaServiceClient, Media servisi için bir gRPC istemcisi oluşturur.
func NewMediaServiceClient(cfg *config.Config) (mediav1.MediaServiceClient, error) {
	conn, err := createGrpcClient(cfg, cfg.MediaServiceGrpcURL)
	if err != nil {
		return nil, fmt.Errorf("media service istemcisi için bağlantı oluşturulamadı: %w", err)
	}
	return mediav1.NewMediaServiceClient(conn), nil
}

// NewUserServiceClient, User servisi için bir gRPC istemcisi oluşturur.
func NewUserServiceClient(cfg *config.Config) (userv1.UserServiceClient, error) {
	conn, err := createGrpcClient(cfg, cfg.UserServiceGrpcURL)
	if err != nil {
		return nil, fmt.Errorf("user service istemcisi için bağlantı oluşturulamadı: %w", err)
	}
	return userv1.NewUserServiceClient(conn), nil
}

// createGrpcClient, verilen adrese güvenli bir gRPC istemci bağlantısı kurar.
func createGrpcClient(cfg *config.Config, addr string) (*grpc.ClientConn, error) {
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

	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caCertPool,
		ServerName:   strings.Split(addr, ":")[0],
		MinVersion:   tls.VersionTLS12,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(creds), grpc.WithBlock())
	if err != nil {
		return nil, fmt.Errorf("gRPC sunucusuna (%s) bağlanılamadı: %w", addr, err)
	}

	return conn, nil
}
