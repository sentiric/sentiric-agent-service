import os
import pika
import time
import json
import structlog
from logger_config import setup_logging

# --- Global Yapılandırma ---
log = setup_logging() # Loglamayı en başta kur ve logger'ı al
RABBITMQ_URL = os.getenv("RABBITMQ_URL", "amqp://sentiric:sentiric_pass@rabbitmq:5672/%2f")
QUEUE_NAME = 'call.events'

def main():
    """RabbitMQ'dan olayları dinleyen ve yeniden bağlanma mantığı içeren ana fonksiyon."""
    log.info("service_starting", service_name="agent-service")
    
    while True:
        try:
            log.info("rabbitmq_connecting", host=RABBITMQ_URL.split('@')[-1])
            connection = pika.BlockingConnection(pika.URLParameters(RABBITMQ_URL))
            channel = connection.channel()
            
            channel.queue_declare(queue=QUEUE_NAME, durable=True)
            log.info(
                "rabbitmq_connection_successful",
                queue_name=QUEUE_NAME,
                details="Listening for events..."
            )

            def callback(ch, method, properties, body):
                # Her mesaj için bir context oluştur, bu loglara otomatik eklenecek
                structlog.contextvars.clear_contextvars()
                
                try:
                    message_data = json.loads(body.decode())
                    # Loglara, bu mesaja özel callId gibi bilgileri bağla
                    structlog.contextvars.bind_contextvars(
                        call_id=message_data.get('callId'),
                        event_type=message_data.get('eventType')
                    )

                    log.info("event_received", event_data=message_data)
                    
                    # Burada gelecekte iş mantığı olacak
                    # Örn: if message_data.get('eventType') == 'call.started':
                    #         handle_new_call(message_data)
                    
                except json.JSONDecodeError as e:
                    log.error("json_decode_error", error=str(e), raw_body=body.decode(errors='ignore'))
                except Exception as e:
                    log.error("message_processing_error", error=str(e), exc_info=True)
                
                # Mesajın işlendiğini RabbitMQ'ya bildir
                ch.basic_ack(delivery_tag=method.delivery_tag)

            channel.basic_consume(queue=QUEUE_NAME, on_message_callback=callback)
            channel.start_consuming()

        except pika.exceptions.AMQPConnectionError as e:
            log.error("rabbitmq_connection_error", error=str(e), action="retrying_in_5_seconds")
            time.sleep(5)
        except Exception as e:
            log.error("unexpected_error", error=str(e), exc_info=True, action="retrying_in_5_seconds")
            time.sleep(5)

if __name__ == '__main__':
    main()