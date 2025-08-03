// DOSYA: sentiric-agent-service/main.go (NİHAİ v4.3)

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	dialplanv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/dialplan/v1"
	mediav1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/media/v1"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
)

const queueName = "call.events"

// ... (Veri yapıları aynı kalacak) ...
type CallEvent struct {
	EventType string                             `json:"eventType"`
	CallID    string                             `json:"callId"`
	Media     map[string]interface{}             `json:"media"`
	Dialplan  dialplanv1.ResolveDialplanResponse `json:"dialplan"`
	From      string                             `json:"from"`
}

type LlmRequest struct {
	Prompt string `json:"prompt"`
}

type LlmResponse struct {
	Text string `json:"text"`
}

type AgentService struct {
	db            *sql.DB
	mediaClient   mediav1.MediaServiceClient
	userClient    userv1.UserServiceClient
	httpClient    *http.Client
	llmServiceURL string
}

func main() {
	zerolog.TimeFieldFormat = time.RFC3339
	log.Logger = log.Output(os.Stderr).With().Timestamp().Str("service", "agent-service-go").Logger()

	// .env dosyasını yüklemeye çalış, hata olursa sadece uyar ve devam et
	// Docker ortamında değişkenler zaten enjekte edildiği için bu hata vermemeli.
	godotenv.Load()

	log.Info().Msg("Sentiric Agent Service (Go) başlatılıyor...")

	db := connectToDBWithRetry(getEnv("POSTGRES_URL"))
	defer db.Close()

	rabbitCh := connectToRabbitMQWithRetry(getEnv("RABBITMQ_URL"))
	defer rabbitCh.Close()

	agent := &AgentService{
		db:            db,
		mediaClient:   createMediaServiceClient(),
		userClient:    createUserServiceClient(),
		httpClient:    &http.Client{Timeout: 15 * time.Second},
		llmServiceURL: getEnv("LLM_SERVICE_URL"),
	}

	// ... (main fonksiyonunun geri kalanı aynı) ...
	msgs, err := rabbitCh.Consume(queueName, "", true, false, false, false, nil)
	if err != nil {
		log.Fatal().Err(err).Msg("Mesajlar tüketilemedi")
	}

	log.Info().Msg("[*] Mesajlar bekleniyor...")
	forever := make(chan bool)
	go func() {
		for d := range msgs {
			agent.handleRabbitMQMessage(d.Body)
		}
	}()
	go func() {
		errChan := <-rabbitCh.NotifyClose(make(chan *amqp091.Error))
		log.Fatal().Err(errChan).Msg("RabbitMQ bağlantısı koptu. Uygulama yeniden başlatılmalı.")
	}()
	<-forever
}

// ... (handle... fonksiyonları ve yardımcı fonksiyonlar aynı kalacak) ...
func (agent *AgentService) handleRabbitMQMessage(body []byte) {
	var event CallEvent
	if err := json.Unmarshal(body, &event); err != nil {
		log.Error().Err(err).Bytes("raw_message", body).Msg("Hata: Mesaj JSON formatında değil")
		return
	}

	l := log.With().Str("call_id", event.CallID).Str("event_type", event.EventType).Logger()
	l.Info().RawJSON("event_data", body).Msg("Olay alındı")

	if event.EventType == "call.started" {
		go agent.handleCallStarted(l, &event)
	}
}

func (agent *AgentService) handleCallStarted(l zerolog.Logger, event *CallEvent) {
	l.Info().Msg("Yeni çağrı işleniyor...")

	if event.Dialplan.Action == nil {
		l.Error().Msg("Hata: Dialplan Action boş.")
		return
	}
	action := event.Dialplan.Action.Action
	l = l.With().Str("action", action).Str("dialplan_id", event.Dialplan.DialplanId).Logger()

	switch action {
	case "PLAY_ANNOUNCEMENT":
		agent.handlePlayAnnouncement(l, event)
	case "START_AI_CONVERSATION":
		agent.handleStartAIConversation(l, event)
	case "PROCESS_GUEST_CALL":
		agent.handleProcessGuestCall(l, event)
	default:
		l.Error().Str("received_action", action).Msg("Bilinmeyen eylem")
		agent.playAnnouncement(l, event, "ANNOUNCE_SYSTEM_ERROR_TR")
	}
}

func (agent *AgentService) handlePlayAnnouncement(l zerolog.Logger, event *CallEvent) {
	announcementID := event.Dialplan.Action.ActionData.Data["announcement_id"]
	l.Info().Str("announcement_id", announcementID).Msg("Anons çalma eylemi işleniyor")
	agent.playAnnouncement(l, event, announcementID)
}

func (agent *AgentService) handleStartAIConversation(l zerolog.Logger, event *CallEvent) {
	announcementID := event.Dialplan.Action.ActionData.Data["welcome_announcement_id"]
	l.Info().Str("announcement_id", announcementID).Msg("AI Konuşma başlatma eylemi işleniyor")
	agent.playAnnouncement(l, event, announcementID)
	agent.startDialogLoop(l, event)
}

func (agent *AgentService) handleProcessGuestCall(l zerolog.Logger, event *CallEvent) {
	callerID := extractCallerID(event.From)
	tenantID := event.Dialplan.TenantId
	if callerID != "" && tenantID != "" {
		agent.createGuestUser(l, callerID, tenantID)
	} else {
		l.Error().Str("caller_id", callerID).Str("tenant_id", tenantID).Msg("Misafir kullanıcı oluşturulamadı, bilgi eksik.")
	}

	announcementID := event.Dialplan.Action.ActionData.Data["welcome_announcement_id"]
	l.Info().Str("announcement_id", announcementID).Msg("Misafir karşılama eylemi işleniyor")
	agent.playAnnouncement(l, event, announcementID)
	agent.startDialogLoop(l, event)
}

func (agent *AgentService) startDialogLoop(l zerolog.Logger, event *CallEvent) {
	l.Info().Msg("Yapay zeka diyalog döngüsü başlatılıyor...")
	respText, err := agent.generateLlmResponse(l, "Merhaba, nasılsınız?")
	if err != nil {
		l.Error().Err(err).Msg("Hata: LLM'den yanıt alınamadı")
		return
	}
	l.Info().Str("llm_response", respText).Msg("LLM yanıtı alındı")
}

func (agent *AgentService) playAnnouncement(l zerolog.Logger, event *CallEvent, announcementID string) {
	audioPath, err := agent.getAnnouncementPathFromDB(l, announcementID)
	if err != nil {
		l.Error().Err(err).Str("announcement_id", announcementID).Msg("Anons yolu alınamadı, fallback kullanılıyor")
		audioPath = "audio/tr/system_error.wav"
	}

	mediaInfo := event.Media
	rtpTarget, _ := mediaInfo["caller_rtp_addr"].(string)
	serverPort, _ := mediaInfo["server_rtp_port"].(float64)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err = agent.mediaClient.PlayAudio(ctx, &mediav1.PlayAudioRequest{
		RtpTargetAddr: rtpTarget,
		AudioId:       audioPath,
		ServerRtpPort: uint32(serverPort),
	})

	if err != nil {
		l.Error().Err(err).Str("audio_path", audioPath).Msg("Hata: Ses çalınamadı")
	} else {
		l.Info().Str("audio_path", audioPath).Msg("Ses çalma komutu gönderildi")
	}
}

func (agent *AgentService) getAnnouncementPathFromDB(l zerolog.Logger, announcementID string) (string, error) {
	var audioPath string
	query := "SELECT audio_path FROM announcements WHERE id = $1"
	err := agent.db.QueryRow(query, announcementID).Scan(&audioPath)
	if err != nil {
		if err == sql.ErrNoRows {
			l.Warn().Str("announcement_id", announcementID).Msg("Veritabanında anons bulunamadı. Fallback deneniyor.")
			err = agent.db.QueryRow(query, "ANNOUNCE_SYSTEM_ERROR_TR").Scan(&audioPath)
			if err != nil {
				return "", fmt.Errorf("fallback anonsu bile bulunamadı: %w", err)
			}
			return audioPath, nil
		}
		return "", fmt.Errorf("anons sorgusu başarısız: %w", err)
	}
	return audioPath, nil
}

func (agent *AgentService) createGuestUser(l zerolog.Logger, callerID, tenantID string) {
	l.Info().Str("caller_id", callerID).Str("tenant_id", tenantID).Msg("Misafir kullanıcı oluşturuluyor...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	name := "Guest Caller"
	_, err := agent.userClient.CreateUser(ctx, &userv1.CreateUserRequest{
		Id:       callerID,
		TenantId: tenantID,
		UserType: "caller",
		Name:     &name,
	})

	if err != nil {
		l.Error().Err(err).Msg("Hata: Misafir kullanıcı oluşturulamadı")
	} else {
		l.Info().Str("caller_id", callerID).Msg("Misafir kullanıcı başarıyla oluşturuldu")
	}
}

func (agent *AgentService) generateLlmResponse(l zerolog.Logger, prompt string) (string, error) {
	reqBody, err := json.Marshal(LlmRequest{Prompt: prompt})
	if err != nil {
		return "", fmt.Errorf("istek gövdesi oluşturulamadı: %w", err)
	}

	req, err := http.NewRequest("POST", agent.llmServiceURL+"/generate", bytes.NewBuffer(reqBody))
	if err != nil {
		return "", fmt.Errorf("HTTP isteği oluşturulamadı: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := agent.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("LLM servisine istek başarısız: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("LLM servisi hata döndü: %s - %s", resp.Status, string(bodyBytes))
	}

	var llmResp LlmResponse
	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
		return "", fmt.Errorf("LLM yanıtı çözümlenemedi: %w", err)
	}

	return llmResp.Text, nil
}

// --- Kurulum ve Bağlantı Fonksiyonları ---
func getEnv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		log.Fatal().Str("variable", key).Msg("Kritik ortam değişkeni bulunamadı")
	}
	return val
}

func connectToDBWithRetry(url string) *sql.DB {
	var db *sql.DB
	var err error
	for i := 0; i < 10; i++ {
		db, err = sql.Open("pgx", url)
		if err == nil {
			if err = db.Ping(); err == nil {
				log.Info().Msg("Veritabanı bağlantısı başarılı.")
				return db
			}
		}
		if i == 9 {
			log.Fatal().Err(err).Msg("Maksimum deneme sonrası veritabanına bağlanılamadı")
		}
		log.Warn().Err(err).Int("attempt", i+1).Int("max_attempts", 10).Msg("Veritabanına bağlanılamadı, 5 saniye sonra tekrar denenecek...")
		time.Sleep(5 * time.Second)
	}
	return nil
}

func connectToRabbitMQWithRetry(url string) *amqp091.Channel {
	var conn *amqp091.Connection
	var err error
	for i := 0; i < 10; i++ {
		conn, err = amqp091.Dial(url)
		if err == nil {
			log.Info().Msg("RabbitMQ bağlantısı başarılı.")
			ch, err := conn.Channel()
			if err != nil {
				log.Fatal().Err(err).Msg("RabbitMQ kanalı oluşturulamadı")
			}
			_, err = ch.QueueDeclare(queueName, true, false, false, false, nil)
			if err != nil {
				log.Fatal().Err(err).Msg("Kuyruk deklare edilemedi")
			}
			return ch
		}
		if i == 9 {
			log.Fatal().Err(err).Msg("Maksimum deneme sonrası RabbitMQ'ya bağlanılamadı")
		}
		log.Warn().Err(err).Int("attempt", i+1).Int("max_attempts", 10).Msg("RabbitMQ'ya bağlanılamadı, 5 saniye sonra tekrar denenecek...")
		time.Sleep(5 * time.Second)
	}
	return nil
}

func createGrpcClient(addrEnvVar, certEnvVar, keyEnvVar string) *grpc.ClientConn {
	addr := getEnv(addrEnvVar)
	certPath := getEnv(certEnvVar)
	keyPath := getEnv(keyEnvVar)
	caPath := getEnv("GRPC_TLS_CA_PATH")

	clientCert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		log.Fatal().Err(err).Str("path", certPath).Msg("İstemci sertifikası yüklenemedi")
	}

	caCert, err := ioutil.ReadFile(caPath)
	if err != nil {
		log.Fatal().Err(err).Str("path", caPath).Msg("CA sertifikası okunamadı")
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caCertPool,
		ServerName:   strings.Split(addr, ":")[0],
	})

	var conn *grpc.ClientConn
	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn, err = grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(creds), grpc.WithBlock())
		if err == nil {
			log.Info().Str("address", addr).Msg("Güvenli gRPC bağlantısı başarılı.")
			return conn
		}
		if i == 9 {
			log.Fatal().Err(err).Str("address", addr).Msg("Maksimum deneme sonrası gRPC sunucusuna bağlanılamadı")
		}
		log.Warn().Err(err).Str("address", addr).Int("attempt", i+1).Int("max_attempts", 10).Msg("gRPC sunucusuna bağlanılamadı, 5 saniye sonra tekrar...")
		time.Sleep(5 * time.Second)
	}
	return nil
}

func createMediaServiceClient() mediav1.MediaServiceClient {
	conn := createGrpcClient("MEDIA_SERVICE_GRPC_URL", "AGENT_SERVICE_CERT_PATH", "AGENT_SERVICE_KEY_PATH")
	return mediav1.NewMediaServiceClient(conn)
}

func createUserServiceClient() userv1.UserServiceClient {
	conn := createGrpcClient("USER_SERVICE_GRPC_URL", "AGENT_SERVICE_CERT_PATH", "AGENT_SERVICE_KEY_PATH")
	return userv1.NewUserServiceClient(conn)
}

func extractCallerID(fromURI string) string {
	re := regexp.MustCompile(`sip:(\+?\d+)@`)
	matches := re.FindStringSubmatch(fromURI)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}
