// sentiric-agent-service/internal/config/config.go
package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Env         string
	LogLevel    string
	PostgresURL string
	RabbitMQURL string
	RedisURL    string
	MetricsPort string

	// --- DEĞİŞİKLİK BURADA: Artık tüm bağımlılıklar için _TARGET_ URL'lerini okuyoruz ---
	LlmServiceURL           string
	SttServiceURL           string
	KnowledgeServiceGrpcURL string // Bu, RAG'in hem gRPC hem HTTP desteklemesi için kalabilir
	KnowledgeServiceURL     string
	TtsServiceGrpcURL       string
	MediaServiceGrpcURL     string
	UserServiceGrpcURL      string
	SipSignalingGrpcURL     string
	// --- DEĞİŞİKLİK SONA ERDİ ---

	SttServiceTargetSampleRate     uint32
	SttServiceLogprobThreshold     float64
	SttServiceNoSpeechThreshold    float64
	SttServiceStreamTimeoutSeconds int
	KnowledgeServiceTopK           int
	AgentServiceCertPath           string
	AgentServiceKeyPath            string
	GrpcTlsCaPath                  string
	AgentMaxConsecutiveFailures    int
	AgentAllowedSpeakerDomains     string
	BucketName                     string
}

func Load() (*Config, error) {
	godotenv.Load()

	sttSampleRateStr := getEnvWithDefault("STT_SERVICE_TARGET_SAMPLE_RATE", "16000")
	sttSampleRate, err := strconv.ParseUint(sttSampleRateStr, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("geçersiz STT_SERVICE_TARGET_SAMPLE_RATE: %w", err)
	}

	sttLogprobStr := getEnvWithDefault("STT_SERVICE_LOGPROB_THRESHOLD", "-1.0")
	sttLogprob, err := strconv.ParseFloat(sttLogprobStr, 64)
	if err != nil {
		return nil, fmt.Errorf("geçersiz STT_SERVICE_LOGPROB_THRESHOLD: %w", err)
	}

	sttNoSpeechStr := getEnvWithDefault("STT_SERVICE_NO_SPEECH_THRESHOLD", "0.75")
	sttNoSpeech, err := strconv.ParseFloat(sttNoSpeechStr, 64)
	if err != nil {
		return nil, fmt.Errorf("geçersiz STT_SERVICE_NO_SPEECH_THRESHOLD: %w", err)
	}

	sttTimeoutStr := getEnvWithDefault("STT_SERVICE_STREAM_TIMEOUT_SECONDS", "30")
	sttTimeout, err := strconv.Atoi(sttTimeoutStr)
	if err != nil {
		return nil, fmt.Errorf("geçersiz STT_SERVICE_STREAM_TIMEOUT_SECONDS: %w", err)
	}

	maxFailuresStr := getEnvWithDefault("AGENT_MAX_CONSECUTIVE_FAILURES", "2")
	maxFailures, err := strconv.Atoi(maxFailuresStr)
	if err != nil {
		return nil, fmt.Errorf("geçersiz AGENT_MAX_CONSECUTIVE_FAILURES: %w", err)
	}

	knowledgeTopKStr := getEnvWithDefault("KNOWLEDGE_QUERY_DEFAULT_TOP_K", "3")
	knowledgeTopK, err := strconv.Atoi(knowledgeTopKStr)
	if err != nil {
		return nil, fmt.Errorf("geçersiz KNOWLEDGE_QUERY_DEFAULT_TOP_K: %w", err)
	}

	cfg := &Config{
		Env:         getEnvWithDefault("ENV", "production"),
		LogLevel:    getEnvWithDefault("LOG_LEVEL", "info"),
		PostgresURL: getEnv("POSTGRES_URL"),
		RabbitMQURL: getEnv("RABBITMQ_URL"),
		RedisURL:    getEnv("REDIS_URL"),
		MetricsPort: getEnvWithDefault("AGENT_SERVICE_METRICS_PORT", "12032"),

		// --- DEĞİŞİKLİK BURADA: _TARGET_ değişkenlerini okuyoruz ---
		LlmServiceURL:           getEnv("LLM_SERVICE_TARGET_HTTP_URL"),
		SttServiceURL:           getEnv("STT_SERVICE_TARGET_HTTP_URL"),
		KnowledgeServiceGrpcURL: getEnv("KNOWLEDGE_QUERY_SERVICE_TARGET_GRPC_URL"), // Hibrit RAG desteği için
		KnowledgeServiceURL:     getEnv("KNOWLEDGE_QUERY_SERVICE_TARGET_HTTP_URL"), // Hibrit RAG desteği için
		TtsServiceGrpcURL:       getEnv("TTS_GATEWAY_SERVICE_TARGET_GRPC_URL"),
		MediaServiceGrpcURL:     getEnv("MEDIA_SERVICE_TARGET_GRPC_URL"),
		UserServiceGrpcURL:      getEnv("USER_SERVICE_TARGET_GRPC_URL"),
		SipSignalingGrpcURL:     getEnv("SIP_SIGNALING_SERVICE_TARGET_GRPC_URL"),
		// --- DEĞİŞİKLİK SONA ERDİ ---

		SttServiceTargetSampleRate:     uint32(sttSampleRate),
		SttServiceLogprobThreshold:     sttLogprob,
		SttServiceNoSpeechThreshold:    sttNoSpeech,
		SttServiceStreamTimeoutSeconds: sttTimeout,
		KnowledgeServiceTopK:           knowledgeTopK,
		AgentServiceCertPath:           getEnv("AGENT_SERVICE_CERT_PATH"),
		AgentServiceKeyPath:            getEnv("AGENT_SERVICE_KEY_PATH"),
		GrpcTlsCaPath:                  getEnv("GRPC_TLS_CA_PATH"),
		AgentMaxConsecutiveFailures:    maxFailures,
		AgentAllowedSpeakerDomains:     getEnvWithDefault("AGENT_ALLOWED_SPEAKER_DOMAINS", "sentiric.github.io"),
		BucketName:                     getEnv("BUCKET_NAME"),
	}

	return cfg, nil
}

func getEnv(key string) string {
	return os.Getenv(key)
}

func getEnvWithDefault(key, fallback string) string {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	return val
}
