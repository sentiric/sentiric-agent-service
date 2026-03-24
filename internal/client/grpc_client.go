// Dosya: sentiric-agent-service/internal/client/grpc_client.go
package client

import (
	"context"
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
	"google.golang.org/grpc/metadata"
)

type Clients struct {
	User            userv1.UserServiceClient
	TelephonyAction telephonyv1.TelephonyActionServiceClient
	B2BUA           sipv1.B2BUAServiceClient
}

func NewClients(cfg *config.Config) (*Clients, error) {
	log.Info().Msg("🔌 Servis bağlantıları (mTLS/Trace destekli) kuruluyor...")

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

	log.Info().Msg("✅ İstemciler ve Trace Interceptor'lar hazır.")

	return &Clients{
		User:            userv1.NewUserServiceClient(userConn),
		TelephonyAction: telephonyv1.NewTelephonyActionServiceClient(telephonyConn),
		B2BUA:           sipv1.NewB2BUAServiceClient(b2buaConn),
	}, nil
}

// createConnection: Trace Propagation Interceptor ile gRPC bağlantısı oluşturur.
func createConnection(cfg *config.Config, targetURL string) (*grpc.ClientConn, error) {
	var opts []grpc.DialOption

	cleanTarget := strings.TrimPrefix(strings.TrimPrefix(targetURL, "https://"), "http://")
	serverName := strings.Split(cleanTarget, ":")[0]

	// [CRITICAL UPDATE] Trace ID Propagation Interceptor
	opts = append(opts, grpc.WithUnaryInterceptor(tracePropagationInterceptor))
	opts = append(opts, grpc.WithStreamInterceptor(streamTracePropagationInterceptor))

	// [ARCH-COMPLIANCE] constraints.yaml: grpc_communication zorunluluğu.
	// Insecure mode fallback (güvensiz yama) KESİNLİKLE YASAKTIR. Fail-fast uygulanır.
	if cfg.CertPath == "" || cfg.KeyPath == "" || cfg.CaPath == "" {
		return nil, fmt.Errorf("[ARCH-COMPLIANCE] mTLS sertifika yolları eksik yapılandırılmış. Insecure mod yasaktır")
	}

	if _, err := os.Stat(cfg.CertPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("[ARCH-COMPLIANCE] mTLS sertifika dosyası bulunamadı: %s. Insecure mod yasaktır", cfg.CertPath)
	}

	clientCert, err := tls.LoadX509KeyPair(cfg.CertPath, cfg.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("cert load error: %w", err)
	}
	caCert, err := os.ReadFile(cfg.CaPath)
	if err != nil {
		return nil, fmt.Errorf("ca load error: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to append CA")
	}

	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caCertPool,
		ServerName:   serverName,
	})

	opts = append(opts, grpc.WithTransportCredentials(creds))

	return grpc.NewClient(cleanTarget, opts...)
}

// tracePropagationInterceptor: Unary çağrılar için Trace ID taşır.
func tracePropagationInterceptor(
	ctx context.Context,
	method string,
	req, reply interface{},
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption,
) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		md = metadata.New(nil)
	}

	if traceIDs := md.Get("x-trace-id"); len(traceIDs) > 0 {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-trace-id", traceIDs[0])
	}

	return invoker(ctx, method, req, reply, cc, opts...)
}

// streamTracePropagationInterceptor: Stream çağrılar için Trace ID taşır.
func streamTracePropagationInterceptor(
	ctx context.Context,
	desc *grpc.StreamDesc,
	cc *grpc.ClientConn,
	method string,
	streamer grpc.Streamer,
	opts ...grpc.CallOption,
) (grpc.ClientStream, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		md = metadata.New(nil)
	}

	if traceIDs := md.Get("x-trace-id"); len(traceIDs) > 0 {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-trace-id", traceIDs[0])
	}

	return streamer(ctx, desc, cc, method, opts...)
}
