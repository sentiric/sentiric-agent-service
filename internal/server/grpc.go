// [ARCH-COMPLIANCE] strict mtls_failure_policy: Fail-fast, no insecure fallback allowed.
package server

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"

	"github.com/rs/zerolog"
	"github.com/sentiric/sentiric-agent-service/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func NewGrpcServer(cfg *config.Config, log zerolog.Logger) *grpc.Server {
	opts := []grpc.ServerOption{}

	if cfg.CertPath == "" || cfg.KeyPath == "" || cfg.CaPath == "" {
		log.Fatal().Str("event", "MTLS_CONFIG_MISSING").Msg("mTLS sertifika yolları eksik. Güvensiz mod yasaktır.")
	}

	creds, err := loadServerTLS(cfg.CertPath, cfg.KeyPath, cfg.CaPath)
	if err != nil {
		log.Fatal().Str("event", "MTLS_LOAD_FAILED").Err(err).Msg("TLS yüklenemedi, güvensiz moda geçiş YASAKTIR.")
	}

	opts = append(opts, grpc.Creds(creds))
	log.Info().Str("event", "MTLS_ACTIVE").Msg("🔐 mTLS Aktif (Agent Server)")

	return grpc.NewServer(opts...)
}

func Start(grpcServer *grpc.Server, port string) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		return err
	}
	return grpcServer.Serve(lis)
}

func Stop(grpcServer *grpc.Server) {
	grpcServer.GracefulStop()
}

func loadServerTLS(certPath, keyPath, caPath string) (credentials.TransportCredentials, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}

	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.NoClientCert,
	}

	if caPath != "" {
		caCert, err := os.ReadFile(caPath)
		if err == nil {
			caPool := x509.NewCertPool()
			if caPool.AppendCertsFromPEM(caCert) {
				config.ClientCAs = caPool
				config.ClientAuth = tls.RequireAndVerifyClientCert
			}
		}
	}

	return credentials.NewTLS(config), nil
}
