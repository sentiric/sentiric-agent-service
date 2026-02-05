// sentiric-agent-service/internal/client/grpc_client.go
package client

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	"github.com/rs/zerolog/log"

	sipv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/sip/v1"
	telephonyv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/telephony/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"

	"github.com/sentiric/sentiric-agent-service/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type Clients struct {
	User            userv1.UserServiceClient
	TelephonyAction telephonyv1.TelephonyActionServiceClient
	B2BUA           sipv1.B2BUAServiceClient
}

func NewClients(cfg *config.Config) (*Clients, error) {
	log.Info().Msg("🔌 Servis bağlantıları kuruluyor...")

	// 1. User Service
	userConn, err := createConnection(cfg, cfg.UserServiceURL)
	if err != nil {
		return nil, fmt.Errorf("user-service bağlantısı başarısız: %w", err)
	}

	// 2. Telephony Action Service
	telephonyConn, err := createConnection(cfg, cfg.TelephonyActionURL)
	if err != nil {
		return nil, fmt.Errorf("telephony-action-service bağlantısı başarısız: %w", err)
	}

	// 3. B2BUA Service
	b2buaConn, err := createConnection(cfg, cfg.B2buaServiceURL)
	if err != nil {
		return nil, fmt.Errorf("b2bua-service bağlantısı başarısız: %w", err)
	}

	log.Info().Msg("✅ İstemciler hazır.")

	return &Clients{
		User:            userv1.NewUserServiceClient(userConn),
		TelephonyAction: telephonyv1.NewTelephonyActionServiceClient(telephonyConn),
		B2BUA:           sipv1.NewB2BUAServiceClient(b2buaConn),
	}, nil
}

func createConnection(cfg *config.Config, targetURL string) (*grpc.ClientConn, error) {
	var opts []grpc.DialOption

	cleanTarget := strings.TrimPrefix(strings.TrimPrefix(targetURL, "https://"), "http://")
	serverName := strings.Split(cleanTarget, ":")[0]

	if cfg.CertPath != "" && cfg.KeyPath != "" && cfg.CaPath != "" {
		if _, err := os.Stat(cfg.CertPath); os.IsNotExist(err) {
			log.Warn().Str("path", cfg.CertPath).Msg("Sertifika dosyası bulunamadı, INSECURE moduna geçiliyor.")
			opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
		} else {
			clientCert, err := tls.LoadX509KeyPair(cfg.CertPath, cfg.KeyPath)
			if err != nil {
				return nil, fmt.Errorf("cert load error: %w", err)
			}
			caCert, err := os.ReadFile(cfg.CaPath)
			if err != nil {
				return nil, fmt.Errorf("ca load error: %w", err)
			}
			caPool := x509.NewCertPool()
			if !caPool.AppendCertsFromPEM(caCert) {
				return nil, fmt.Errorf("failed to append CA")
			}

			creds := credentials.NewTLS(&tls.Config{
				Certificates: []tls.Certificate{clientCert},
				RootCAs:      caPool,
				ServerName:   serverName,
			})
			opts = append(opts, grpc.WithTransportCredentials(creds))
		}
	} else {
		log.Warn().Msg("mTLS config eksik, INSECURE bağlanılıyor.")
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	return grpc.NewClient(cleanTarget, opts...)
}
