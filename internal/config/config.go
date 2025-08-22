package config

import (
	"fmt"
	"os"

	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Env           string
	PostgresURL   string
	RabbitMQURL   string
	RedisURL      string
	MetricsPort   string
	LlmServiceURL string
	SttServiceURL string
	// YENİ: STT'nin beklediği örnekleme oranını config'den alıyoruz.
	SttServiceTargetSampleRate uint32
	TtsServiceGrpcURL          string
	MediaServiceGrpcURL        string
	UserServiceGrpcURL         string
	AgentServiceCertPath       string
	AgentServiceKeyPath        string
	GrpcTlsCaPath              string
}

func Load() (*Config, error) {
	godotenv.Load()

	sttSampleRateStr := getEnvWithDefault("STT_SERVICE_TARGET_SAMPLE_RATE", "16000")
	sttSampleRate, err := strconv.ParseUint(sttSampleRateStr, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("geçersiz STT_SERVICE_TARGET_SAMPLE_RATE: %w", err)
	}

	cfg := &Config{
		Env:                        getEnvWithDefault("ENV", "production"),
		PostgresURL:                getEnv("POSTGRES_URL"),
		RabbitMQURL:                getEnv("RABBITMQ_URL"),
		RedisURL:                   getEnv("REDIS_URL"),
		MetricsPort:                getEnvWithDefault("METRICS_PORT_AGENT", "9091"),
		LlmServiceURL:              getEnv("LLM_SERVICE_URL"),
		SttServiceURL:              getEnv("STT_SERVICE_URL"),
		SttServiceTargetSampleRate: uint32(sttSampleRate), // YENİ
		TtsServiceGrpcURL:          getEnv("TTS_GATEWAY_URL"),
		MediaServiceGrpcURL:        getEnv("MEDIA_SERVICE_GRPC_URL"),
		UserServiceGrpcURL:         getEnv("USER_SERVICE_GRPC_URL"),
		AgentServiceCertPath:       getEnv("AGENT_SERVICE_CERT_PATH"),
		AgentServiceKeyPath:        getEnv("AGENT_SERVICE_KEY_PATH"),
		GrpcTlsCaPath:              getEnv("GRPC_TLS_CA_PATH"),
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
