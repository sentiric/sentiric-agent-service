// sentiric-agent-service/internal/queue/rabbitmq.go
package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
)

const (
	exchangeName   = "sentiric_events"
	agentQueueName = "sentiric.agent_service.events"
)

type Publisher struct {
	ch  *amqp091.Channel
	log zerolog.Logger
}

func NewPublisher(ch *amqp091.Channel, log zerolog.Logger) *Publisher {
	return &Publisher{ch: ch, log: log}
}

func (p *Publisher) PublishJSON(ctx context.Context, routingKey string, body interface{}) error {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		p.log.Error().Err(err).Msg("Mesaj JSON'a çevrilemedi.")
		return err
	}

	// DEĞİŞİKLİK: Bu logu INFO'dan DEBUG'a çekiyoruz.
	p.log.Debug().Str("routing_key", routingKey).Bytes("payload", jsonBody).Msg("RabbitMQ'ya olay yayınlanıyor...")

	err = p.ch.PublishWithContext(
		ctx,
		exchangeName,
		routingKey,
		false,
		false,
		amqp091.Publishing{
			ContentType:  "application/json",
			Body:         jsonBody,
			DeliveryMode: amqp091.Persistent,
		},
	)
	if err != nil {
		p.log.Error().Err(err).Str("routing_key", routingKey).Msg("RabbitMQ'ya mesaj yayınlanamadı.")
		return err
	}
	return nil
}

// DEĞİŞİKLİK: Fonksiyon artık context alıyor.
func Connect(ctx context.Context, url string, log zerolog.Logger) (*amqp091.Channel, <-chan *amqp091.Error, error) {
	var conn *amqp091.Connection
	var err error

    // --- GÜNCELLEME: Heartbeat ayarı eklendi ---
    config := amqp091.Config{
        Heartbeat: 60 * time.Second, // 60 saniyelik heartbeat
        Locale:    "en_US",
    }
    // --- GÜNCELLEME SONU ---

	for i := 0; i < 10; i++ {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		default:
		}

		conn, err = amqp091.DialConfig(url, config) // Dial yerine DialConfig kullan
		if err == nil {
			log.Info().Msg("RabbitMQ bağlantısı başarılı.")
			ch, chErr := conn.Channel()
			if chErr != nil {
				return nil, nil, fmt.Errorf("RabbitMQ kanalı oluşturulamadı: %w", chErr)
			}
			closeChan := make(chan *amqp091.Error)
			conn.NotifyClose(closeChan)
			return ch, closeChan, nil
		}
		log.Warn().Err(err).Int("attempt", i+1).Int("max_attempts", 10).Msg("RabbitMQ'ya bağlanılamadı, 5 saniye sonra tekrar denenecek...")

		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	return nil, nil, fmt.Errorf("maksimum deneme (%d) sonrası RabbitMQ'ya bağlanılamadı: %w", 10, err)
	
}

func StartConsumer(ctx context.Context, ch *amqp091.Channel, handlerFunc func([]byte), log zerolog.Logger, wg *sync.WaitGroup) {
	err := ch.ExchangeDeclare(
		exchangeName,
		"topic",
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

	err = ch.QueueBind(
		q.Name,
		"#",
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
