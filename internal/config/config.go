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
	AgentServiceCertPath        string
	AgentServiceKeyPath         string
	GrpcTlsCaPath               string
	// YENİ: Yapılandırılabilir diyalog parametreleri (AGENT-IMPRV-01)
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

	// YENİ: AGENT_MAX_CONSECUTIVE_FAILURES yükleniyor (AGENT-IMPRV-01)
	maxFailuresStr := getEnvWithDefault("AGENT_MAX_CONSECUTIVE_FAILURES", "2")
	maxFailures, err := strconv.Atoi(maxFailuresStr)
	if err != nil {
		return nil, fmt.Errorf("geçersiz AGENT_MAX_CONSECUTIVE_FAILURES: %w", err)
	}

	cfg := &Config{
		Env:                         getEnvWithDefault("ENV", "production"),
		PostgresURL:                 getEnv("POSTGRES_URL"),
		RabbitMQURL:                 getEnv("RABBITMQ_URL"),
		RedisURL:                    getEnv("REDIS_URL"),
		MetricsPort:                 getEnvWithDefault("METRICS_PORT_AGENT", "9091"),
		LlmServiceURL:               getEnv("LLM_SERVICE_URL"),
		SttServiceURL:               getEnv("STT_SERVICE_URL"),
		SttServiceTargetSampleRate:  uint32(sttSampleRate),
		SttServiceLogprobThreshold:  sttLogprob,
		SttServiceNoSpeechThreshold: sttNoSpeech,
		TtsServiceGrpcURL:           getEnv("TTS_GATEWAY_URL"),
		MediaServiceGrpcURL:         getEnv("MEDIA_SERVICE_GRPC_URL"),
		UserServiceGrpcURL:          getEnv("USER_SERVICE_GRPC_URL"),
		AgentServiceCertPath:        getEnv("AGENT_SERVICE_CERT_PATH"),
		AgentServiceKeyPath:         getEnv("AGENT_SERVICE_KEY_PATH"),
		GrpcTlsCaPath:               getEnv("GRPC_TLS_CA_PATH"),
		// YENİ: Değerler atanıyor (AGENT-IMPRV-01)
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
