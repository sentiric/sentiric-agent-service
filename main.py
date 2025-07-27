import os
import pika
import time
import json
import structlog
import grpc

from logger_config import setup_logging
# Üretilen gRPC kodlarını kurduğumuz paketten import ediyoruz
from sentiric.media.v1 import media_pb2, media_pb2_grpc

# --- Global Yapılandırma ---
log = setup_logging()
RABBITMQ_URL = os.getenv("RABBITMQ_URL")
MEDIA_SERVICE_GRPC_URL = os.getenv("MEDIA_SERVICE_GRPC_URL")
QUEUE_NAME = 'call.events'

def play_welcome_message(call_id: str, media_info: dict):
    """
    Belirli bir çağrı için karşılama mesajını media-service aracılığıyla çalar.
    """
    log.info("welcome_message_flow_started", call_id=call_id)
    
    if not MEDIA_SERVICE_GRPC_URL:
        log.error("media_service_url_not_configured")
        return

    try:
        # TODO: Bu ses dosyasının media-service konteynerinde var olduğundan emin olmalıyız.
        welcome_audio_id = "assets/welcome_tr.wav" 
        log.info("preparing_to_play_audio", audio_id=welcome_audio_id)

        # Media servisine gRPC ile bağlan
        # Not: gRPC URL'indeki 'http://' prefix'ini kaldırıyoruz.
        grpc_target = MEDIA_SERVICE_GRPC_URL.replace("http://", "")
        with grpc.insecure_channel(grpc_target) as channel:
            stub = media_pb2_grpc.MediaServiceStub(channel)
            
            request = media_pb2.PlayAudioRequest(
                call_id=call_id,
                rtp_port=media_info.get("port"),
                audio_id=welcome_audio_id
            )
            
            log.info("sending_play_audio_request", rtp_port=media_info.get("port"))
            response = stub.PlayAudio(request, timeout=10)
            
            if response.success:
                log.info("play_audio_request_successful")
            else:
                log.warn("play_audio_request_failed_on_server")

    except grpc.RpcError as e:
        log.error("grpc_error_media_service", error=str(e), code=e.code().name, details=e.details())
    except Exception as e:
        log.error("play_welcome_message_failed", error=str(e), exc_info=True)

def callback(ch, method, properties, body):
    structlog.contextvars.clear_contextvars()
    
    try:
        message_data = json.loads(body.decode())
        structlog.contextvars.bind_contextvars(
            call_id=message_data.get('callId'),
            event_type=message_data.get('eventType')
        )
        log.info("event_received", event_data=message_data)
        
        if message_data.get('eventType') == 'call.started':
            media_info = message_data.get("media")
            if media_info and media_info.get("port"):
                play_welcome_message(message_data.get('callId'), media_info)
            else:
                log.warn("media_info_missing_in_event", event_data=message_data)
        
    except json.JSONDecodeError as e:
        log.error("json_decode_error", error=str(e), raw_body=body.decode(errors='ignore'))
    except Exception as e:
        log.error("message_processing_error", error=str(e), exc_info=True)
    
    ch.basic_ack(delivery_tag=method.delivery_tag)

def main():
    log.info("service_starting", service_name="agent-service")
    
    if not RABBITMQ_URL:
        log.critical("rabbitmq_url_not_configured_exiting")
        return

    while True:
        try:
            log.info("rabbitmq_connecting")
            connection = pika.BlockingConnection(pika.URLParameters(RABBITMQ_URL))
            channel = connection.channel()
            
            channel.queue_declare(queue=QUEUE_NAME, durable=True)
            log.info("rabbitmq_connection_successful", queue_name=QUEUE_NAME)

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