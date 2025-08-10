package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config struct'ı, ham ve türetilmiş yapılandırma değerlerini tutar.
type Config struct {
	Env         string
	PostgresURL string
	RabbitMQURL string
	QueueName   string
	MetricsPort string

	// Ham HTTP servis yapılandırması
	LlmServiceHost       string
	LlmServicePort       string
	LlmServiceTlsEnabled bool
	TtsServiceHost       string
	TtsServicePort       string
	TtsServiceTlsEnabled bool

	// Kod içinde dinamik olarak oluşturulan tam URL'ler
	LlmServiceURL string
	TtsServiceURL string

	// gRPC ve mTLS ayarları
	MediaServiceGrpcURL  string
	UserServiceGrpcURL   string
	AgentServiceCertPath string
	AgentServiceKeyPath  string
	GrpcTlsCaPath        string
}

// buildURL, host, port ve tls ayarlarından tam bir URL oluşturur.
func buildURL(host, port string, tlsEnabled bool) string {
	scheme := "http"
	if tlsEnabled {
		scheme = "https"
	}
	// Port boşsa, URL'e ekleme. Bu, 80/443 gibi varsayılan portları destekler.
	if port != "" {
		return fmt.Sprintf("%s://%s:%s", scheme, host, port)
	}
	return fmt.Sprintf("%s://%s", scheme, host)
}

// Load, tüm yapılandırmayı ortamdan yükler ve işler.
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

		MediaServiceGrpcURL:  getEnv("MEDIA_SERVICE_GRPC_URL"),
		UserServiceGrpcURL:   getEnv("USER_SERVICE_GRPC_URL"),
		AgentServiceCertPath: getEnv("AGENT_SERVICE_CERT_PATH"),
		AgentServiceKeyPath:  getEnv("AGENT_SERVICE_KEY_PATH"),
		GrpcTlsCaPath:        getEnv("GRPC_TLS_CA_PATH"),
	}

	// Akıllı URL'leri oluştur
	cfg.LlmServiceURL = buildURL(cfg.LlmServiceHost, cfg.LlmServicePort, cfg.LlmServiceTlsEnabled)
	cfg.TtsServiceURL = buildURL(cfg.TtsServiceHost, cfg.TtsServicePort, cfg.TtsServiceTlsEnabled)

	// Kritik değişkenlerin varlığını kontrol et
	if cfg.PostgresURL == "" || cfg.RabbitMQURL == "" || cfg.MediaServiceGrpcURL == "" || cfg.UserServiceGrpcURL == "" {
		return nil, fmt.Errorf("kritik altyapı URL'leri eksik (Postgres, RabbitMQ, Media, User)")
	}
	if cfg.LlmServiceHost == "" || cfg.TtsServiceHost == "" {
		return nil, fmt.Errorf("kritik AI servis HOST tanımlamaları eksik (LLM, TTS)")
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
