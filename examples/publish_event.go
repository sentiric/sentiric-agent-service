package main

import (
	"context"
	"encoding/json"
	"log"
    // "time" importu kaldırıldı

	"github.com/rabbitmq/amqp091-go"
)

func main() {
	// Docker içinden erişim için host adını 'rabbitmq' olarak varsayıyoruz.
    // Eğer localhost'tan çalıştırıyorsanız ve port forward yoksa hata alabilirsiniz.
    // Local test için: "amqp://sentiric:sentiric_pass@localhost:5672/%2f"
	url := "amqp://sentiric:sentiric_pass@localhost:5672/%2f"
	
	conn, err := amqp091.Dial(url)
	if err != nil {
		log.Fatalf("RabbitMQ bağlantı hatası: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Kanal açma hatası: %v", err)
	}
	defer ch.Close()

	event := map[string]interface{}{
		"eventType": "call.started",
		"callId":    "test-call-agent-1",
		"traceId":   "trace-agent-1",
		"fromUri":   "905551234567",
		"mediaInfo": map[string]interface{}{
			"callerRtpAddr": "1.2.3.4:12345",
			"serverRtpPort": 10000,
		},
		"dialplanResolution": map[string]interface{}{
			"action": map[string]string{
				"action": "START_AI_CONVERSATION",
			},
			"matchedUser": map[string]string{
				"id": "user-123",
				"tenantId": "demo",
			},
		},
	}

	body, _ := json.Marshal(event)

	err = ch.PublishWithContext(context.Background(), "sentiric_events", "call.started", false, false, amqp091.Publishing{
		ContentType: "application/json",
		Body:        body,
	})

	if err != nil {
		log.Fatalf("Yayın hatası: %v", err)
	}
	log.Println("Olay yayınlandı!")
}