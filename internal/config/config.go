// ========== FILE: sentiric-agent-service/internal/config/config.go ==========
package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Env                  string
	PostgresURL          string
	RabbitMQURL          string
	QueueName            string
	MetricsPort          string
	LlmServiceHost       string
	LlmServicePort       string
	LlmServiceTlsEnabled bool
	TtsServiceHost       string
	TtsServicePort       string
	TtsServiceTlsEnabled bool
	LlmServiceURL        string
	TtsServiceURL        string
	TtsServiceGrpcURL    string // YENİ
	MediaServiceGrpcURL  string
	UserServiceGrpcURL   string
	AgentServiceCertPath string
	AgentServiceKeyPath  string
	GrpcTlsCaPath        string
}

func buildURL(host, port string, tlsEnabled bool) string {
	if host == "" { // Eğer host tanımlı değilse, boş URL döndür.
		return ""
	}
	scheme := "http"
	if tlsEnabled {
		scheme = "https"
	}
	if port != "" {
		return fmt.Sprintf("%s://%s:%s", scheme, host, port)
	}
	return fmt.Sprintf("%s://%s", scheme, host)
}

func Load() (*Config, error) {
	godotenv.Load()

	llmTls, _ := strconv.ParseBool(getEnv("LLM_SERVICE_TLS_ENABLED"))
	ttsTls, _ := strconv.ParseBool(getEnv("TTS_SERVICE_TLS_ENABLED"))

	cfg := &Config{
		Env:         getEnvWithDefault("ENV", "production"),
		PostgresURL: getEnv("POSTGRES_URL"),
		RabbitMQURL: getEnv("RABBITMQ_URL"),
		QueueName:   getEnvWithDefault("AGENT_QUEUE_NAME", "call.events"),
		MetricsPort: getEnvWithDefault("METRICS_PORT", "9091"),

		LlmServiceHost:       getEnv("LLM_SERVICE_HOST"),
		LlmServicePort:       getEnv("LLM_SERVICE_PORT"),
		LlmServiceTlsEnabled: llmTls,

		TtsServiceHost:       getEnv("TTS_SERVICE_HOST"),
		TtsServicePort:       getEnv("TTS_SERVICE_PORT"),
		TtsServiceTlsEnabled: ttsTls,
		TtsServiceGrpcURL:    getEnv("TTS_SERVICE_GRPC_URL"), // YENİ

		MediaServiceGrpcURL:  getEnv("MEDIA_SERVICE_GRPC_URL"),
		UserServiceGrpcURL:   getEnv("USER_SERVICE_GRPC_URL"),
		AgentServiceCertPath: getEnv("AGENT_SERVICE_CERT_PATH"),
		AgentServiceKeyPath:  getEnv("AGENT_SERVICE_KEY_PATH"),
		GrpcTlsCaPath:        getEnv("GRPC_TLS_CA_PATH"),
	}

	cfg.LlmServiceURL = buildURL(cfg.LlmServiceHost, cfg.LlmServicePort, cfg.LlmServiceTlsEnabled)
	cfg.TtsServiceURL = buildURL(cfg.TtsServiceHost, cfg.TtsServicePort, cfg.TtsServiceTlsEnabled)

	// DÜZELTME: Kritik kontrolü daha esnek hale getiriyoruz.
	// Sadece gerçekten vazgeçilmez olanları kontrol et.
	if cfg.PostgresURL == "" || cfg.RabbitMQURL == "" || cfg.MediaServiceGrpcURL == "" || cfg.UserServiceGrpcURL == "" {
		return nil, fmt.Errorf("kritik altyapı URL'leri eksik (Postgres, RabbitMQ, Media, User)")
	}
	if cfg.LlmServiceHost == "" {
		return nil, fmt.Errorf("kritik AI servis HOST tanımlaması eksik (LLM)")
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
