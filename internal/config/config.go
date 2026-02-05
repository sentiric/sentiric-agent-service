// sentiric-agent-service/internal/config/config.go
package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog/log"
)

type Config struct {
	Env         string
	LogLevel    string
	PostgresURL string
	RabbitMQURL string
	RedisURL    string
	MetricsPort string

	// Hedef Servisler
	UserServiceURL     string
	TelephonyActionURL string
	B2buaServiceURL    string // YENİ EKLENDİ

	// Security
	CertPath string
	KeyPath  string
	CaPath   string

	// Diğer ayarlar
	AgentMaxConsecutiveFailures int
	BucketName                  string
}

func Load() (*Config, error) {
	godotenv.Load()

	maxFailuresStr := getEnvWithDefault("AGENT_MAX_CONSECUTIVE_FAILURES", "3")
	maxFailures, _ := strconv.Atoi(maxFailuresStr)

	return &Config{
		Env:         getEnvWithDefault("ENV", "production"),
		LogLevel:    getEnvWithDefault("LOG_LEVEL", "info"),
		PostgresURL: GetEnvOrFail("POSTGRES_URL"),

		RabbitMQURL: GetEnvOrFail("RABBITMQ_URL"),
		RedisURL:    GetEnvOrFail("REDIS_URL"),

		MetricsPort: getEnvWithDefault("AGENT_SERVICE_METRICS_PORT", "12032"),

		UserServiceURL:     getEnvWithDefault("USER_SERVICE_TARGET_GRPC_URL", "user-service:12011"),
		TelephonyActionURL: getEnvWithDefault("TELEPHONY_ACTION_TARGET_GRPC_URL", "telephony-action-service:13111"),
		B2buaServiceURL:    getEnvWithDefault("B2BUA_SERVICE_TARGET_GRPC_URL", "b2bua-service:13081"), // YENİ EKLENDİ

		CertPath: GetEnvOrFail("AGENT_SERVICE_CERT_PATH"),
		KeyPath:  GetEnvOrFail("AGENT_SERVICE_KEY_PATH"),
		CaPath:   GetEnvOrFail("GRPC_TLS_CA_PATH"),

		AgentMaxConsecutiveFailures: maxFailures,
		BucketName:                  getEnvWithDefault("BUCKET_NAME", "sentiric"),
	}, nil
}

func GetEnvOrFail(key string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		log.Fatal().Str("variable", key).Msg("Gerekli ortam değişkeni eksik")
	}
	return value
}

func getEnvWithDefault(key, fallback string) string {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	return val
}
