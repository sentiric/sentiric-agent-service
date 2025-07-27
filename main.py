import os
import pika
import time
import json
import structlog
import grpc

from logger_config import setup_logging
from sentiric.media.v1 import media_pb2, media_pb2_grpc

log = setup_logging()
RABBITMQ_URL = os.getenv("RABBITMQ_URL")
MEDIA_SERVICE_GRPC_URL = os.getenv("MEDIA_SERVICE_GRPC_URL")
QUEUE_NAME = 'call.events'

def play_welcome_message(call_id: str, media_info: dict):
    log.info("welcome_message_flow_started", call_id=call_id)
    
    # Hem arayanın adresini HEM DE bizim portumuzu event'ten alıyoruz
    caller_rtp_addr = media_info.get("caller_rtp_addr")
    server_rtp_port = media_info.get("server_rtp_port")
    
    if not caller_rtp_addr or server_rtp_port is None:
        log.error("caller_rtp_addr_or_server_rtp_port_missing_in_event", media_info=media_info)
        return

    if not MEDIA_SERVICE_GRPC_URL:
        log.error("media_service_url_not_configured")
        return

    try:
        welcome_audio_id = "assets/welcome_tr.wav" 
        log.info("preparing_to_play_audio", audio_id=welcome_audio_id)

        grpc_target = MEDIA_SERVICE_GRPC_URL.replace("http://", "")
        with grpc.insecure_channel(grpc_target) as channel:
            stub = media_pb2_grpc.MediaServiceStub(channel)
            
            # gRPC isteğine `server_rtp_port`'u da ekliyoruz
            request = media_pb2.PlayAudioRequest(
                rtp_target_addr=caller_rtp_addr,
                audio_id=welcome_audio_id,
                server_rtp_port=int(server_rtp_port) # Port numarasını integer'a çeviriyoruz
            )
            
            log.info("sending_play_audio_request", target_addr=caller_rtp_addr, server_port=server_rtp_port)
            response = stub.PlayAudio(request, timeout=10)
            
            if response.success:
                log.info("play_audio_request_successful", message=response.message)
            else:
                log.warn("play_audio_request_failed_on_server", message=response.message)

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
            if media_info:
                play_welcome_message(message_data.get('callId'), media_info)
            else:
                log.warn("media_info_missing", event_data=message_data)
    except Exception as e:
        log.error("message_processing_error", error=str(e), exc_info=True)
    
    ch.basic_ack(delivery_tag=method.delivery_tag)

def main():
    log.info("agent-service", service_name="agent-service")
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