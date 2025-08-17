// ========== FILE: sentiric-agent-service/internal/queue/rabbitmq.go (Durable Queue Mimarisi) ==========
package queue

import (
	"context"
	"sync"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
)

const (
	exchangeName = "sentiric_events"
	// Agent için kalıcı ve bilinen bir kuyruk adı tanımlıyoruz.
	agentQueueName = "sentiric.agent_service.events"
)

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
		exchangeName, // name
		"fanout",     // type
		true,         // durable
		false,        // auto-deleted
		false,        // internal
		false,        // no-wait
		nil,          // arguments
	)
	if err != nil {
		log.Fatal().Err(err).Str("exchange", exchangeName).Msg("Exchange deklare edilemedi")
	}

	// YENİ MİMARİ: Geçici ve isimsiz kuyruk yerine, kalıcı ve bilinen bir kuyruk oluşturuyoruz.
	q, err := ch.QueueDeclare(
		agentQueueName, // name: Artık bilinen bir ismimiz var.
		true,           // durable: RabbitMQ yeniden başlasa bile kuyruk kaybolmaz.
		false,          // delete when unused: Tüketici olmasa bile silinmez.
		false,          // exclusive: Birden fazla agent instance'ı aynı kuyruğu dinleyebilir.
		false,          // no-wait
		nil,            // arguments
	)
	if err != nil {
		log.Fatal().Err(err).Msg("Kalıcı agent kuyruğu oluşturulamadı")
	}

	err = ch.QueueBind(
		q.Name,       // queue name
		"",           // routing key
		exchangeName, // exchange
		false,
		nil,
	)
	if err != nil {
		log.Fatal().Err(err).Str("queue", q.Name).Str("exchange", exchangeName).Msg("Kuyruk exchange'e bağlanamadı")
	}

	log.Info().Str("queue", q.Name).Str("exchange", exchangeName).Msg("Kalıcı kuyruk başarıyla exchange'e bağlandı.")

	// Prefetch ayarı, bir worker'ın aynı anda çok fazla mesaj alıp bellekte şişmesini engeller.
	// Bir seferde sadece 1 mesaj alacak, onu işleyip onayladıktan sonra yenisini isteyecek.
	err = ch.Qos(1, 0, false)
	if err != nil {
		log.Fatal().Err(err).Msg("QoS ayarı yapılamadı.")
	}

	msgs, err := ch.Consume(
		q.Name, // queue
		"",     // consumer
		false,  // auto-ack: DEĞİŞTİRİLDİ! Mesajları manuel onaylayacağız.
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
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
				// Handler'ı çalıştır. Handler'ın panic yapması durumuna karşı koruma eklenebilir.
				handlerFunc(msg.Body)
				// Handler başarıyla tamamlandıktan sonra mesajı RabbitMQ'dan silmesi için onay gönder.
				_ = msg.Ack(false)
			}(d)
		}
	}
}
