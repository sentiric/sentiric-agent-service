package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// Config, servis için tüm yapılandırma değerlerini tutar.
type Config struct {
	Env                  string
	PostgresURL          string
	RabbitMQURL          string
	QueueName            string
	LlmServiceURL        string
	TtsServiceURL        string
	MediaServiceGrpcURL  string
	UserServiceGrpcURL   string
	AgentServiceCertPath string
	AgentServiceKeyPath  string
	GrpcTlsCaPath        string
	MetricsPort          string
}

// Load, ortam değişkenlerini doğru öncelik sırasıyla yükler.
func Load() (*Config, error) {
	// 1. Önce .env dosyasını yüklemeyi dene (yerel geliştirme için).
	//    Eğer bulunamazsa hata vermez, devam eder. Bu `go run` için.
	godotenv.Load()

	// 2. RUNNING_IN_DOCKER ortam değişkeni varsa (docker-compose'dan gelir),
	//    .env.docker dosyasını yükleyerek yerel ayarları geçersiz kıl.
	// if os.Getenv("RUNNING_IN_DOCKER") == "true" {
	// 	godotenv.Load(".env.docker")
	// }
	// NOT: Bu yaklaşım yerine docker-compose'da doğrudan env_file kullanmak daha temiz.
	// Bu yüzden bu bloğu yorumda bırakıyorum. Mevcut docker-compose.yml'niz doğru.

	cfg := &Config{
		Env:                  getEnvWithDefault("ENV", "production"),
		PostgresURL:          getEnv("POSTGRES_URL"),
		RabbitMQURL:          getEnv("RABBITMQ_URL"),
		QueueName:            getEnvWithDefault("AGENT_QUEUE_NAME", "call.events"),
		MediaServiceGrpcURL:  getEnv("MEDIA_SERVICE_GRPC_URL"),
		UserServiceGrpcURL:   getEnv("USER_SERVICE_GRPC_URL"),
		AgentServiceCertPath: getEnv("AGENT_SERVICE_CERT_PATH"),
		AgentServiceKeyPath:  getEnv("AGENT_SERVICE_KEY_PATH"),
		GrpcTlsCaPath:        getEnv("GRPC_TLS_CA_PATH"),
		MetricsPort:          getEnvWithDefault("METRICS_PORT", "9091"),
		LlmServiceURL:        getEnv("LLM_SERVICE_URL"),
		TtsServiceURL:        getEnv("TTS_SERVICE_URL"),
	}

	if cfg.PostgresURL == "" || cfg.RabbitMQURL == "" || cfg.MediaServiceGrpcURL == "" || cfg.UserServiceGrpcURL == "" {
		return nil, fmt.Errorf("kritik ortam değişkenleri eksik. Lütfen ilgili .env dosyasını kontrol edin")
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

func ensureHTTP(url string) string {
	if url != "" && !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return "http://" + url
	}
	return url
}
