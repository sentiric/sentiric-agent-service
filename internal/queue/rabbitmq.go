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
	dlxName        = "sentiric_events.failed"
	dlqName        = "sentiric.agent_service.failed"
	ghostBufSize   = 1000
)

type GhostMessage struct {
	RoutingKey  string
	ContentType string
	Body        []byte
}

type RabbitMQ struct {
	url    string
	log    zerolog.Logger
	conn   *amqp091.Connection
	ch     *amqp091.Channel
	mu     sync.RWMutex
	buffer []GhostMessage
	bufMu  sync.Mutex
}

func NewRabbitMQ(url string, log zerolog.Logger) *RabbitMQ {
	return &RabbitMQ{
		url:    url,
		log:    log,
		buffer: make([]GhostMessage, 0, ghostBufSize),
	}
}

func (m *RabbitMQ) PublishJSON(ctx context.Context, routingKey string, body interface{}) error {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		m.log.Error().Str("event", "RMQ_JSON_ERROR").Err(err).Msg("Mesaj JSON'a çevrilemedi.")
		return err
	}

	m.mu.RLock()
	ch := m.ch
	m.mu.RUnlock()

	if ch != nil && !ch.IsClosed() {
		err := ch.PublishWithContext(ctx, exchangeName, routingKey, false, false, amqp091.Publishing{
			ContentType:  "application/json",
			Body:         jsonBody,
			DeliveryMode: amqp091.Persistent,
		})
		if err == nil {
			return nil
		}
	}

	m.log.Warn().Str("event", "RMQ_GHOST_MODE").Str("routing_key", routingKey).Msg("RabbitMQ çevrimdışı. Mesaj RAM tamponuna (Ghost Buffer) alınıyor.")

	m.bufMu.Lock()
	defer m.bufMu.Unlock()
	if len(m.buffer) >= ghostBufSize {
		m.buffer = m.buffer[1:] // FIFO Drop
	}
	m.buffer = append(m.buffer, GhostMessage{
		RoutingKey:  routingKey,
		ContentType: "application/json",
		Body:        jsonBody,
	})
	return nil
}

// [ARCH-COMPLIANCE] Protobuf mesaj gönderimi için eklendi
func (m *RabbitMQ) PublishProtobuf(ctx context.Context, routingKey string, body []byte) error {
	m.mu.RLock()
	ch := m.ch
	m.mu.RUnlock()

	if ch != nil && !ch.IsClosed() {
		err := ch.PublishWithContext(ctx, exchangeName, routingKey, false, false, amqp091.Publishing{
			ContentType:  "application/protobuf",
			Body:         body,
			DeliveryMode: amqp091.Persistent,
		})
		if err == nil {
			return nil
		}
	}

	m.log.Warn().Str("event", "RMQ_GHOST_MODE").Str("routing_key", routingKey).Msg("RabbitMQ offline. Mesaj Ghost Buffer'a alınıyor.")

	m.bufMu.Lock()
	defer m.bufMu.Unlock()
	if len(m.buffer) >= ghostBufSize {
		m.buffer = m.buffer[1:] // FIFO Drop
	}
	m.buffer = append(m.buffer, GhostMessage{
		RoutingKey:  routingKey,
		ContentType: "application/protobuf",
		Body:        body,
	})
	return nil
}

func (m *RabbitMQ) flushBuffer(ctx context.Context) {
	m.bufMu.Lock()
	defer m.bufMu.Unlock()

	if len(m.buffer) == 0 {
		return
	}

	m.mu.RLock()
	ch := m.ch
	m.mu.RUnlock()

	if ch == nil || ch.IsClosed() {
		return
	}

	successCount := 0
	for _, msg := range m.buffer {
		err := ch.PublishWithContext(ctx, exchangeName, msg.RoutingKey, false, false, amqp091.Publishing{
			ContentType:  msg.ContentType,
			Body:         msg.Body,
			DeliveryMode: amqp091.Persistent,
		})
		if err != nil {
			break
		}
		successCount++
	}

	if successCount > 0 {
		m.buffer = m.buffer[successCount:]
		m.log.Info().Str("event", "RMQ_GHOST_FLUSH").Int("count", successCount).Msg("Ghost Buffer'daki mesajlar başarıyla RabbitMQ'ya aktarıldı.")
	}
}

func (m *RabbitMQ) Start(ctx context.Context, handlerFunc func([]byte), wg *sync.WaitGroup) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn, err := amqp091.Dial(m.url)
		if err != nil {
			m.log.Warn().Str("event", "RMQ_RECONNECT_WAIT").Err(err).Msg("RabbitMQ bağlantısı koptu veya yok, 5 saniye sonra tekrar denenecek...")
			time.Sleep(5 * time.Second)
			continue
		}

		ch, err := conn.Channel()
		if err != nil {
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		m.mu.Lock()
		m.conn = conn
		m.ch = ch
		m.mu.Unlock()

		m.log.Info().Str("event", "RMQ_CONNECTED").Msg("✅ RabbitMQ bağlantısı sağlandı.")

		if err := m.setupTopology(ch); err != nil {
			m.log.Error().Str("event", "RMQ_TOPOLOGY_FAIL").Err(err).Msg("Topoloji kurulamadı")
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		m.flushBuffer(ctx)
		m.consume(ctx, ch, handlerFunc, wg)

		m.mu.Lock()
		m.conn = nil
		m.ch = nil
		m.mu.Unlock()
	}
}

func (m *RabbitMQ) setupTopology(ch *amqp091.Channel) error {
	_ = ch.ExchangeDeclare(exchangeName, "topic", true, false, false, false, nil)
	_ = ch.ExchangeDeclare(dlxName, "topic", true, false, false, false, nil)
	_, _ = ch.QueueDeclare(dlqName, true, false, false, false, nil)
	_ = ch.QueueBind(dlqName, "#", dlxName, false, nil)

	args := amqp091.Table{"x-dead-letter-exchange": dlxName}
	q, err := ch.QueueDeclare(agentQueueName, true, false, false, false, args)
	if err != nil {
		return err
	}
	return ch.QueueBind(q.Name, "#", exchangeName, false, nil)
}

func (m *RabbitMQ) consume(ctx context.Context, ch *amqp091.Channel, handlerFunc func([]byte), wg *sync.WaitGroup) {
	_ = ch.Qos(10, 0, false)
	msgs, err := ch.Consume(agentQueueName, "", false, false, false, false, nil)
	if err != nil {
		m.log.Error().Str("event", "RMQ_CONSUME_FAIL").Err(err).Msg("Consume başlatılamadı")
		return
	}

	m.log.Info().Str("event", "RMQ_CONSUMING").Str("queue", agentQueueName).Msg("Kuyruk dinleniyor, mesajlar bekleniyor...")

	for {
		select {
		case <-ctx.Done():
			m.log.Info().Str("event", "RMQ_CONSUMER_STOP").Msg("Tüketici döngüsü durduruluyor.")
			return
		case d, ok := <-msgs:
			if !ok {
				m.log.Info().Str("event", "RMQ_CHANNEL_CLOSED").Msg("RabbitMQ mesaj kanalı kapandı, yeniden bağlanılacak.")
				return
			}
			wg.Add(1)
			go func(msg amqp091.Delivery) {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						m.log.Error().Str("event", "RMQ_PANIC_RECOVERY").Interface("panic", r).Msg("CRITICAL: Message handler panikledi! Mesaj Nack ediliyor.")
						_ = msg.Nack(false, false)
					}
				}()

				handlerFunc(msg.Body)
				_ = msg.Ack(false)
			}(d)
		}
	}
}
