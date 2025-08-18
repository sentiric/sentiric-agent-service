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
	RedisURL             string // YENİ
	MetricsPort          string
	LlmServiceURL        string
	SttServiceURL        string // YENİ
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
		RedisURL:             getEnv("REDIS_URL"), // YENİ
		MetricsPort:          getEnvWithDefault("METRICS_PORT_AGENT", "9091"),
		LlmServiceURL:        getEnv("LLM_SERVICE_URL"),
		SttServiceURL:        getEnv("STT_SERVICE_URL"), // YENİ
		TtsServiceGrpcURL:    getEnv("TTS_GATEWAY_URL"),
		MediaServiceGrpcURL:  getEnv("MEDIA_SERVICE_GRPC_URL"),
		UserServiceGrpcURL:   getEnv("USER_SERVICE_GRPC_URL"),
		AgentServiceCertPath: getEnv("AGENT_SERVICE_CERT_PATH"),
		AgentServiceKeyPath:  getEnv("AGENT_SERVICE_KEY_PATH"),
		GrpcTlsCaPath:        getEnv("GRPC_TLS_CA_PATH"),
	}

	if cfg.PostgresURL == "" || cfg.RabbitMQURL == "" || cfg.RedisURL == "" || cfg.MediaServiceGrpcURL == "" || cfg.UserServiceGrpcURL == "" || cfg.TtsServiceGrpcURL == "" || cfg.LlmServiceURL == "" || cfg.SttServiceURL == "" {
		return nil, fmt.Errorf("kritik altyapı URL'leri eksik (Postgres, RabbitMQ, Redis, Media, User, TTS, LLM, STT)")
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
