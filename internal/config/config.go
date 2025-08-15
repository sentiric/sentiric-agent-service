// ========== FILE: sentiric-agent-service/internal/config/config.go ==========
package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Env                  string
	PostgresURL          string
	RabbitMQURL          string
	MetricsPort          string
	LlmServiceURL        string // <-- BU SATIR EKLENDİ
	TtsServiceGrpcURL    string
	MediaServiceGrpcURL  string
	UserServiceGrpcURL   string
	AgentServiceCertPath string
	AgentServiceKeyPath  string
	GrpcTlsCaPath        string
}

func Load() (*Config, error) {
	godotenv.Load()

	cfg := &Config{
		Env:                  getEnvWithDefault("ENV", "production"),
		PostgresURL:          getEnv("POSTGRES_URL"),
		RabbitMQURL:          getEnv("RABBITMQ_URL"),
		MetricsPort:          getEnvWithDefault("METRICS_PORT_AGENT", "9091"),
		LlmServiceURL:        getEnv("LLM_SERVICE_URL"), // <-- BU SATIR EKLENDİ
		TtsServiceGrpcURL:    getEnv("TTS_GATEWAY_URL"),
		MediaServiceGrpcURL:  getEnv("MEDIA_SERVICE_GRPC_URL"),
		UserServiceGrpcURL:   getEnv("USER_SERVICE_GRPC_URL"),
		AgentServiceCertPath: getEnv("AGENT_SERVICE_CERT_PATH"),
		AgentServiceKeyPath:  getEnv("AGENT_SERVICE_KEY_PATH"),
		GrpcTlsCaPath:        getEnv("GRPC_TLS_CA_PATH"),
	}

	if cfg.PostgresURL == "" || cfg.RabbitMQURL == "" || cfg.MediaServiceGrpcURL == "" || cfg.UserServiceGrpcURL == "" || cfg.TtsServiceGrpcURL == "" || cfg.LlmServiceURL == "" {
		return nil, fmt.Errorf("kritik altyapı URL'leri eksik (Postgres, RabbitMQ, Media, User, TTS Gateway, LLM)")
	}

	return cfg, nil
}

func getEnv(key string) string {
	return os.Getenv(key)
}

func getEnvWithDefault(key, defaultValue string) string {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue
	}
	return val
}
