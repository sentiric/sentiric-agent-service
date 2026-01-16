package client

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	"github.com/rs/zerolog/log"
	
	dialogv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialog/v1"
	mediav1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/media/v1"
	sipv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/sip/v1"
	sttv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/stt/v1"
	telephonyv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/telephony/v1"
	ttsv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/tts/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
	
	"github.com/sentiric/sentiric-agent-service/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type Clients struct {
	User            userv1.UserServiceClient
	TelephonyAction telephonyv1.TelephonyActionServiceClient
	Media           mediav1.MediaServiceClient
	STT             sttv1.SttGatewayServiceClient
	TTS             ttsv1.TtsGatewayServiceClient
	Dialog          dialogv1.DialogServiceClient
	Signaling       sipv1.SipSignalingServiceClient
}

func NewClients(cfg *config.Config) (*Clients, error) {
	log.Info().Msg("🔌 Servis bağlantıları kuruluyor (v1.0.1 - URL Fix Applied)...")

	// 1. User Service
	userConn, err := createConnection(cfg, cfg.UserServiceURL)
	if err != nil { return nil, err }

	// 2. Telephony Action Service
	telephonyConn, err := createConnection(cfg, cfg.TelephonyActionURL)
	if err != nil { return nil, err }

	log.Info().Msg("✅ İstemciler hazır.")

	return &Clients{
		User:            userv1.NewUserServiceClient(userConn),
		TelephonyAction: telephonyv1.NewTelephonyActionServiceClient(telephonyConn),
		Media:     nil,
		STT:       nil,
		TTS:       nil,
		Dialog:    nil,
		Signaling: nil,
	}, nil
}

func createConnection(cfg *config.Config, targetURL string) (*grpc.ClientConn, error) {
	var opts []grpc.DialOption
	
	// [FIX] URL Sanitization: "https://" veya "http://" ön eklerini kesinlikle kaldır.
	cleanTarget := targetURL
	if strings.HasPrefix(cleanTarget, "https://") {
		cleanTarget = strings.TrimPrefix(cleanTarget, "https://")
	} else if strings.HasPrefix(cleanTarget, "http://") {
		cleanTarget = strings.TrimPrefix(cleanTarget, "http://")
	}

	// ServerName (SNI) için portu ayır (örn: "user-service:12011" -> "user-service")
	serverName := strings.Split(cleanTarget, ":")[0]

	// Log: Hedef adresi kontrol et (Genişletilmiş Debug)
	log.Debug().
		Str("original_url", targetURL).
		Str("cleaned_target", cleanTarget).
		Str("sni_server_name", serverName).
		Msg("gRPC bağlantı girişimi")

	// mTLS Kontrolü
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
				ServerName:   serverName, // Kritik: IP değil Hostname olmalı
			})
			opts = append(opts, grpc.WithTransportCredentials(creds))
			log.Debug().Str("target", cleanTarget).Msg("🔒 mTLS bağlantısı aktif")
		}
	} else {
		log.Warn().Str("target", cleanTarget).Msg("⚠️ mTLS config eksik, INSECURE bağlanılıyor.")
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// [FIX] cleanTarget kullanarak bağlan
	return grpc.NewClient(cleanTarget, opts...)
}