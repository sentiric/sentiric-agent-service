// File: sentiric-agent-service/internal/queue/rabbitmq.go

// ========== FILE: sentiric-agent-service/internal/queue/rabbitmq.go (Publisher Eklendi) ==========
package queue

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
)

const (
	exchangeName   = "sentiric_events"
	agentQueueName = "sentiric.agent_service.events"
)

// --- YENİ: Publisher yapısı ---
type Publisher struct {
	ch  *amqp091.Channel
	log zerolog.Logger
}

func NewPublisher(ch *amqp091.Channel, log zerolog.Logger) *Publisher {
	return &Publisher{ch: ch, log: log}
}

// --- YENİ: Mesaj yayınlama fonksiyonu ---
func (p *Publisher) PublishJSON(ctx context.Context, routingKey string, body interface{}) error {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		p.log.Error().Err(err).Msg("Mesaj JSON'a çevrilemedi.")
		return err
	}

	p.log.Info().Str("routing_key", routingKey).Bytes("payload", jsonBody).Msg("RabbitMQ'ya olay yayınlanıyor...")

	err = p.ch.PublishWithContext(
		ctx,
		exchangeName, // exchange
		routingKey,   // routing key
		false,        // mandatory
		false,        // immediate
		amqp091.Publishing{
			ContentType:  "application/json",
			Body:         jsonBody,
			DeliveryMode: amqp091.Persistent, // Mesajın kalıcı olmasını sağla
		},
	)
	if err != nil {
		p.log.Error().Err(err).Str("routing_key", routingKey).Msg("RabbitMQ'ya mesaj yayınlanamadı.")
		return err
	}
	return nil
}

func Connect(url string, log zerolog.Logger) (*amqp091.Channel, <-chan *amqp091.Error) {
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
			closeChan := make(chan *amqp091.Error)
			conn.NotifyClose(closeChan)
			return ch, closeChan
		}
		log.Warn().Err(err).Int("attempt", i+1).Int("max_attempts", 10).Msg("RabbitMQ'ya bağlanılamadı, 5 saniye sonra tekrar denenecek...")
		time.Sleep(5 * time.Second)
	}
	log.Fatal().Err(err).Msgf("Maksimum deneme (%d) sonrası RabbitMQ'ya bağlanılamadı", 10)
	return nil, nil
}

func StartConsumer(ctx context.Context, ch *amqp091.Channel, handlerFunc func([]byte), log zerolog.Logger, wg *sync.WaitGroup) {
	err := ch.ExchangeDeclare(
		exchangeName,
		"topic", // DİKKAT: Artık "topic" exchange kullanıyoruz, "fanout" değil.
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatal().Err(err).Str("exchange", exchangeName).Msg("Exchange deklare edilemedi")
	}

	q, err := ch.QueueDeclare(
		agentQueueName,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("Kalıcı agent kuyruğu oluşturulamadı")
	}

	// Agent'ın dinleyeceği "routing key"leri bağlıyoruz.
	// "#" wildcard'ı, tüm olayları dinlemesini sağlar.
	err = ch.QueueBind(
		q.Name,
		"#", // Tüm olayları dinle
		exchangeName,
		false,
		nil,
	)
	if err != nil {
		log.Fatal().Err(err).Str("queue", q.Name).Str("exchange", exchangeName).Msg("Kuyruk exchange'e bağlanamadı")
	}

	log.Info().Str("queue", q.Name).Str("exchange", exchangeName).Msg("Kalıcı kuyruk başarıyla exchange'e bağlandı.")

	err = ch.Qos(1, 0, false)
	if err != nil {
		log.Fatal().Err(err).Msg("QoS ayarı yapılamadı.")
	}

	msgs, err := ch.Consume(
		q.Name,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("Mesajlar tüketilemedi")
	}

	log.Info().Str("queue", q.Name).Msg("Kuyruk dinleniyor, mesajlar bekleniyor...")

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Tüketici döngüsü durduruluyor, yeni mesajlar alınmayacak.")
			return
		case d, ok := <-msgs:
			if !ok {
				log.Info().Msg("RabbitMQ mesaj kanalı kapandı.")
				return
			}
			wg.Add(1)
			go func(msg amqp091.Delivery) {
				defer wg.Done()
				handlerFunc(msg.Body)
				_ = msg.Ack(false)
			}(d)
		}
	}
}
