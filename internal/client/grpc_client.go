package client

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	"github.com/rs/zerolog/log"
	
	// --- CONTRACT IMPORTS (EKSİKSİZ) ---
	dialogv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialog/v1"
	mediav1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/media/v1"
	sipv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/sip/v1"
	sttv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/stt/v1"
	telephonyv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/telephony/v1" // EKLENDİ
	ttsv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/tts/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"           // EKLENDİ
	
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
	log.Info().Msg("🔌 Servis bağlantıları kuruluyor...")

	// 1. User Service
	userConn, err := createConnection(cfg, cfg.UserServiceURL)
	if err != nil { return nil, err }

	// 2. Telephony Action Service
	telephonyConn, err := createConnection(cfg, cfg.TelephonyActionURL)
	if err != nil { return nil, err }

	// Not: Diğer servisler (Media, STT vb.) şu an Agent tarafından doğrudan kullanılmıyor
	// (Telephony Action üzerinden yönetiliyor). Ancak struct bütünlüğü için nil bırakabiliriz
	// veya ileride lazım olursa buraya ekleyebiliriz.

	log.Info().Msg("✅ İstemciler hazır.")

	return &Clients{
		User:            userv1.NewUserServiceClient(userConn),
		TelephonyAction: telephonyv1.NewTelephonyActionServiceClient(telephonyConn),
		// Diğer alanlar opsiyonel/ileriye dönük:
		Media:     nil,
		STT:       nil,
		TTS:       nil,
		Dialog:    nil,
		Signaling: nil,
	}, nil
}

func createConnection(cfg *config.Config, targetURL string) (*grpc.ClientConn, error) {
	var opts []grpc.DialOption

	// mTLS Kontrolü
	if cfg.CertPath != "" && cfg.KeyPath != "" && cfg.CaPath != "" {
		// Dosyaların varlığını kontrol et
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

			serverName := strings.Split(targetURL, ":")[0]
			creds := credentials.NewTLS(&tls.Config{
				Certificates: []tls.Certificate{clientCert},
				RootCAs:      caPool,
				ServerName:   serverName,
			})
			opts = append(opts, grpc.WithTransportCredentials(creds))
			log.Debug().Str("target", targetURL).Msg("🔒 mTLS bağlantısı hazırlanıyor")
		}
	} else {
		log.Warn().Str("target", targetURL).Msg("⚠️ mTLS config eksik, INSECURE bağlanılıyor.")
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// Lazy Connection
	return grpc.NewClient(targetURL, opts...)
}