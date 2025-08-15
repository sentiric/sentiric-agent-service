// ========== FILE: sentiric-agent-service/internal/queue/rabbitmq.go ==========
package queue

import (
	"context"
	"sync"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
)

const exchangeName = "sentiric_events"

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

// DÜZELTME: Artık kullanılmayan 'queueName' parametresini fonksiyondan kaldırıyoruz.
func StartConsumer(ctx context.Context, ch *amqp091.Channel, handlerFunc func([]byte), log zerolog.Logger, wg *sync.WaitGroup) {
	// 1. Olayların yayınlandığı fanout exchange'in var olduğundan emin ol.
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

	// 2. Bu servis için özel, sunucu tarafından isimlendirilen geçici bir kuyruk oluştur.
	q, err := ch.QueueDeclare(
		"",    // name: boş bırakınca RabbitMQ rastgele bir isim verir
		false, // durable: bağlantı kapanınca silinsin
		true,  // delete when unused: tüketici kalmayınca silinsin
		true,  // exclusive: sadece bu bağlantı tarafından kullanılabilir
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		log.Fatal().Err(err).Msg("Özel tüketici kuyruğu oluşturulamadı")
	}

	// 3. Oluşturduğumuz özel kuyruğu, ana exchange'e bağla (bind).
	err = ch.QueueBind(
		q.Name,       // queue name
		"",           // routing key: fanout için önemsiz
		exchangeName, // exchange
		false,
		nil,
	)
	if err != nil {
		log.Fatal().Err(err).Str("queue", q.Name).Str("exchange", exchangeName).Msg("Kuyruk exchange'e bağlanamadı")
	}

	log.Info().Str("queue", q.Name).Str("exchange", exchangeName).Msg("Kuyruk başarıyla exchange'e bağlandı.")

	msgs, err := ch.Consume(
		q.Name, // queue
		"",     // consumer
		true,   // auto-ack
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
	)
	if err != nil {
		log.Fatal().Err(err).Msg("Mesajlar tüketilemedi")
	}

	log.Info().Str("exchange", exchangeName).Msg("Exchange dinleniyor, mesajlar bekleniyor...")

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
			}(d)
		}
	}
}
