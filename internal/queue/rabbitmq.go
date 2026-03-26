// [ARCH-COMPLIANCE] Added explicit RabbitMQ publisher channel reconnect policies
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
	dlxName        = "sentiric_events.failed"
	dlqName        = "sentiric.agent_service.failed"
)

type Publisher struct {
	conn *amqp091.Connection
	ch   *amqp091.Channel
	mu   sync.RWMutex
	log  zerolog.Logger
}

func NewPublisher(conn *amqp091.Connection, log zerolog.Logger) *Publisher {
	p := &Publisher{
		conn: conn,
		log:  log,
	}
	_ = p.reconnectChannel()
	return p
}

func (p *Publisher) reconnectChannel() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ch != nil && !p.ch.IsClosed() {
		return nil
	}
	ch, err := p.conn.Channel()
	if err != nil {
		p.log.Error().Str("event", "RMQ_CH_RECONNECT_FAIL").Err(err).Msg("RabbitMQ channel oluşturulamadı")
		return err
	}
	p.ch = ch
	return nil
}

func (p *Publisher) PublishJSON(ctx context.Context, routingKey string, body interface{}) error {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		p.log.Error().Str("event", "RMQ_JSON_ERROR").Err(err).Msg("Mesaj JSON'a çevrilemedi.")
		return err
	}

	p.log.Debug().Str("event", "RMQ_PUBLISH").Str("routing_key", routingKey).Msg("RabbitMQ'ya olay yayınlanıyor...")

	for i := 0; i < 2; i++ {
		p.mu.RLock()
		ch := p.ch
		p.mu.RUnlock()

		if ch == nil || ch.IsClosed() {
			if err := p.reconnectChannel(); err != nil {
				continue
			}
			p.mu.RLock()
			ch = p.ch
			p.mu.RUnlock()
		}

		err = ch.PublishWithContext(
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
		if err == nil {
			return nil
		}
		p.log.Warn().Str("event", "RMQ_PUBLISH_RETRY").Err(err).Msg("Publish başarısız, channel reconnect denenecek")
		_ = p.reconnectChannel()
	}

	p.log.Error().Str("event", "RMQ_PUBLISH_FAILED").Err(err).Str("routing_key", routingKey).Msg("RabbitMQ'ya mesaj yayınlanamadı.")
	return err
}

func Connect(ctx context.Context, url string, log zerolog.Logger) (*amqp091.Connection, <-chan *amqp091.Error, error) {
	var conn *amqp091.Connection
	var err error

	config := amqp091.Config{
		Heartbeat: 60 * time.Second,
		Locale:    "en_US",
	}

	for i := 0; i < 10; i++ {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		default:
		}

		conn, err = amqp091.DialConfig(url, config)
		if err == nil {
			log.Info().Str("event", "RMQ_CONNECTED").Msg("RabbitMQ bağlantısı başarılı.")
			closeChan := make(chan *amqp091.Error)
			conn.NotifyClose(closeChan)
			return conn, closeChan, nil
		}
		log.Warn().Str("event", "RMQ_CONNECTION_RETRY").Err(err).Int("attempt", i+1).Int("max_attempts", 10).Msg("RabbitMQ'ya bağlanılamadı, 5 saniye sonra tekrar denenecek...")

		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	return nil, nil, fmt.Errorf("maksimum deneme (%d) sonrası RabbitMQ'ya bağlanılamadı: %w", 10, err)
}

func StartConsumer(ctx context.Context, conn *amqp091.Connection, handlerFunc func([]byte), log zerolog.Logger, wg *sync.WaitGroup) {
	ch, err := conn.Channel()
	if err != nil {
		log.Fatal().Str("event", "RMQ_CONS_CH_FAIL").Err(err).Msg("RabbitMQ tüketici kanalı oluşturulamadı")
	}

	err = ch.ExchangeDeclare(
		exchangeName,
		"topic",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatal().Str("event", "RMQ_EXCHANGE_FAIL").Err(err).Str("exchange", exchangeName).Msg("Exchange deklare edilemedi")
	}

	_ = ch.ExchangeDeclare(dlxName, "topic", true, false, false, false, nil)
	_, _ = ch.QueueDeclare(dlqName, true, false, false, false, nil)
	_ = ch.QueueBind(dlqName, "#", dlxName, false, nil)

	args := amqp091.Table{
		"x-dead-letter-exchange": dlxName,
	}

	q, err := ch.QueueDeclare(
		agentQueueName,
		true,
		false,
		false,
		false,
		args,
	)
	if err != nil {
		log.Fatal().Str("event", "RMQ_QUEUE_FAIL").Err(err).Msg("Kalıcı agent kuyruğu oluşturulamadı")
	}

	err = ch.QueueBind(
		q.Name,
		"#",
		exchangeName,
		false,
		nil,
	)
	if err != nil {
		log.Fatal().Str("event", "RMQ_BIND_FAIL").Err(err).Str("queue", q.Name).Msg("Kuyruk exchange'e bağlanamadı")
	}

	log.Info().Str("event", "RMQ_QUEUE_BOUND").Str("queue", q.Name).Msg("Kalıcı kuyruk başarıyla exchange'e bağlandı.")

	err = ch.Qos(10, 0, false)
	if err != nil {
		log.Fatal().Str("event", "RMQ_QOS_FAIL").Err(err).Msg("QoS ayarı yapılamadı.")
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
		log.Fatal().Str("event", "RMQ_CONSUME_FAIL").Err(err).Msg("Mesajlar tüketilemedi")
	}

	log.Info().Str("event", "RMQ_CONSUMING").Str("queue", q.Name).Msg("Kuyruk dinleniyor, mesajlar bekleniyor...")

	for {
		select {
		case <-ctx.Done():
			log.Info().Str("event", "RMQ_CONSUMER_STOP").Msg("Tüketici döngüsü durduruluyor, yeni mesajlar alınmayacak.")
			return
		case d, ok := <-msgs:
			if !ok {
				log.Info().Str("event", "RMQ_CHANNEL_CLOSED").Msg("RabbitMQ mesaj kanalı kapandı.")
				return
			}
			wg.Add(1)
			go func(msg amqp091.Delivery) {
				defer wg.Done()

				defer func() {
					if r := recover(); r != nil {
						log.Error().Str("event", "RMQ_PANIC_RECOVERY").Interface("panic_info", r).Msg("CRITICAL: Message handler panikledi! Zehirli mesaj Nack ediliyor.")
						if err := msg.Nack(false, false); err != nil {
							log.Error().Str("event", "RMQ_NACK_FAIL").Err(err).Msg("Zehirli mesaj Nack edilemedi.")
						}
					}
				}()

				handlerFunc(msg.Body)

				if err := msg.Ack(false); err != nil {
					log.Error().Str("event", "RMQ_ACK_FAIL").Err(err).Msg("Mesaj Ack edilemedi.")
				}
			}(d)
		}
	}
}
