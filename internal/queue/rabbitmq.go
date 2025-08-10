package queue

import (
	"context"
	"sync" // WaitGroup için eklendi
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
)

// Connect, RabbitMQ'ya yeniden deneme mekanizması ile bağlanır.
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

// DÜZELTME: StartConsumer artık bir WaitGroup pointer'ı alıyor.
// StartConsumer, belirtilen kuyruktan mesajları tüketmeye başlar ve bunları işleyiciye gönderir.
func StartConsumer(ctx context.Context, ch *amqp091.Channel, queueName string, handlerFunc func([]byte), log zerolog.Logger, wg *sync.WaitGroup) {
	_, err := ch.QueueDeclare(queueName, true, false, false, false, nil)
	if err != nil {
		log.Fatal().Err(err).Msg("Kuyruk deklare edilemedi")
	}

	msgs, err := ch.Consume(queueName, "", true, false, false, false, nil)
	if err != nil {
		log.Fatal().Err(err).Msg("Mesajlar tüketilemedi")
	}

	log.Info().Str("queue", queueName).Msg("Kuyruk dinleniyor, mesajlar bekleniyor...")

	for {
		select {
		case <-ctx.Done():
			// Kapatma sinyali geldiğinde döngüden çık.
			log.Info().Msg("Tüketici döngüsü durduruluyor, yeni mesajlar alınmayacak.")
			return
		case d, ok := <-msgs:
			// Kanal kapandıysa döngüden çık.
			if !ok {
				log.Info().Msg("RabbitMQ mesaj kanalı kapandı.")
				return
			}
			// Yeni bir iş başladığında WaitGroup'u artır.
			wg.Add(1)
			go func(msg amqp091.Delivery) {
				// İş bittiğinde WaitGroup'u azalt.
				defer wg.Done()
				handlerFunc(msg.Body)
			}(d)
		}
	}
}
