package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Env                         string
	PostgresURL                 string
	RabbitMQURL                 string
	RedisURL                    string
	MetricsPort                 string
	LlmServiceURL               string
	SttServiceURL               string
	SttServiceTargetSampleRate  uint32
	SttServiceLogprobThreshold  float64
	SttServiceNoSpeechThreshold float64
	TtsServiceGrpcURL           string
	MediaServiceGrpcURL         string
	UserServiceGrpcURL          string
	KnowledgeServiceGrpcURL     string // gRPC için
	KnowledgeServiceURL         string // YENİ: HTTP için
	KnowledgeServiceTopK        int    // YENİ: Yapılandırılabilir RAG parametresi
	AgentServiceCertPath        string
	AgentServiceKeyPath         string
	GrpcTlsCaPath               string
	AgentMaxConsecutiveFailures int
	AgentAllowedSpeakerDomains  string
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

	maxFailuresStr := getEnvWithDefault("AGENT_MAX_CONSECUTIVE_FAILURES", "2")
	maxFailures, err := strconv.Atoi(maxFailuresStr)
	if err != nil {
		return nil, fmt.Errorf("geçersiz AGENT_MAX_CONSECUTIVE_FAILURES: %w", err)
	}

	knowledgeTopKStr := getEnvWithDefault("KNOWLEDGE_SERVICE_TOP_K", "3")
	knowledgeTopK, err := strconv.Atoi(knowledgeTopKStr)
	if err != nil {
		return nil, fmt.Errorf("geçersiz KNOWLEDGE_SERVICE_TOP_K: %w", err)
	}

	cfg := &Config{
		Env:                         getEnvWithDefault("ENV", "production"),
		PostgresURL:                 getEnv("POSTGRES_URL"),
		RabbitMQURL:                 getEnv("RABBITMQ_URL"),
		RedisURL:                    getEnv("REDIS_URL"),
		MetricsPort:                 getEnvWithDefault("AGENT_SERVICE_METRICS_PORT", "12032"),
		LlmServiceURL:               getEnv("LLM_SERVICE_HTTP_URL"),
		SttServiceURL:               getEnv("STT_SERVICE_HTTP_URL"),
		SttServiceTargetSampleRate:  uint32(sttSampleRate),
		SttServiceLogprobThreshold:  sttLogprob,
		SttServiceNoSpeechThreshold: sttNoSpeech,
		TtsServiceGrpcURL:           getEnv("TTS_GATEWAY_GRPC_URL"),
		MediaServiceGrpcURL:         getEnv("MEDIA_SERVICE_GRPC_URL"),
		UserServiceGrpcURL:          getEnv("USER_SERVICE_GRPC_URL"),
		KnowledgeServiceGrpcURL:     getEnv("KNOWLEDGE_SERVICE_GRPC_URL"),
		KnowledgeServiceURL:         getEnv("KNOWLEDGE_SERVICE_HTTP_PORT"),
		KnowledgeServiceTopK:        knowledgeTopK,
		AgentServiceCertPath:        getEnv("AGENT_SERVICE_CERT_PATH"),
		AgentServiceKeyPath:         getEnv("AGENT_SERVICE_KEY_PATH"),
		GrpcTlsCaPath:               getEnv("GRPC_TLS_CA_PATH"),
		AgentMaxConsecutiveFailures: maxFailures,
		AgentAllowedSpeakerDomains:  getEnvWithDefault("AGENT_ALLOWED_SPEAKER_DOMAINS", "sentiric.github.io"),
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
