# DOSYA: sentiric-agent-service/main.py (Genesis Mimarisi Uyumlu - NİHAİ VERSİYON)
import os
import pika
import time
import json
import structlog
import grpc

from logger_config import setup_logging
from sentiric.media.v1 import media_pb2, media_pb2_grpc
# TODO: Gelecekte user_service'i de buradan çağıracağız.
# from sentiric.user.v1 import user_pb2, user_pb2_grpc 

log = setup_logging()
RABBITMQ_URL = os.getenv("RABBITMQ_URL")
MEDIA_SERVICE_GRPC_URL = os.getenv("MEDIA_SERVICE_GRPC_URL")
# USER_SERVICE_GRPC_URL = os.getenv("USER_SERVICE_GRPC_URL") # Gelecekte
QUEUE_NAME = 'call.events'

def play_audio(call_id: str, media_info: dict, audio_path: str):
    log.info("play_audio_triggered", call_id=call_id, audio_path=audio_path)
    
    caller_rtp_addr = media_info.get("caller_rtp_addr")
    server_rtp_port = media_info.get("server_rtp_port")
    
    if not all([caller_rtp_addr, server_rtp_port is not None]):
        log.error("media_info_missing_for_play_audio", media_info=media_info)
        return

    if not MEDIA_SERVICE_GRPC_URL:
        log.error("media_service_url_not_configured")
        return

    try:
        grpc_target = MEDIA_SERVICE_GRPC_URL.replace("http://", "")
        with grpc.insecure_channel(grpc_target) as channel:
            stub = media_pb2_grpc.MediaServiceStub(channel)
            request = media_pb2.PlayAudioRequest(
                rtp_target_addr=caller_rtp_addr,
                audio_id=audio_path,
                server_rtp_port=int(server_rtp_port)
            )
            response = stub.PlayAudio(request, timeout=10)
            if response.success:
                log.info("play_audio_request_successful", message=response.message)
            else:
                log.warn("play_audio_request_failed_on_server", message=response.message)
    except grpc.RpcError as e:
        log.error("grpc_error_media_service", error=str(e), code=e.code().name, details=e.details())
    except Exception as e:
        log.error("play_audio_failed", error=str(e), exc_info=True)


def process_call_event(message_data: dict):
    call_id = message_data.get("callId")
    media_info = message_data.get("media")
    # DİKKAT: Gelen JSON'daki "dialplan_id" anahtarı, proto'daki "dialplanId"den farklı olabilir.
    # Esnek olmak için ikisini de kontrol edelim.
    dialplan = message_data.get("dialplan", {})
    action_type = dialplan.get("action", {}).get("action")
    action_data_map = dialplan.get("action", {}).get("actionData", {}).get("data", {})

    structlog.contextvars.bind_contextvars(
        dialplan_id=dialplan.get("dialplanId") or dialplan.get("dialplan_id"),
        action=action_type
    )
    log.info("processing_dialplan_action", data=action_data_map)

    if not media_info:
        log.warn("media_info_missing_in_event")
        return

    announcement_map = {
        "ANNOUNCE_SYSTEM_MAINTENANCE_TR": "audio/tr/maintenance.wav",
        "ANNOUNCE_GUEST_WELCOME_TR": "audio/tr/welcome_anonymous.wav",
        "ANNOUNCE_SYSTEM_ERROR_TR": "audio/tr/system_error.wav",
        "ANNOUNCE_DEFAULT_WELCOME_TR": "audio/tr/welcome.wav",
    }
    fallback_audio = announcement_map["ANNOUNCE_SYSTEM_ERROR_TR"]

    # --- KRİTİK DÜZELTME BURADA ---
    if action_type == "PLAY_ANNOUNCEMENT":
        announcement_id = action_data_map.get("announcement_id")
        audio_path = announcement_map.get(announcement_id, fallback_audio)
        play_audio(call_id, media_info, audio_path)

    elif action_type == "START_AI_CONVERSATION":
        announcement_id = action_data_map.get("welcome_announcement_id", "ANNOUNCE_DEFAULT_WELCOME_TR")
        audio_path = announcement_map.get(announcement_id, fallback_audio)
        play_audio(call_id, media_info, audio_path)
        log.info("ai_conversation_flow_started")
        # TODO: STT dinlemesini başlat ve LLM döngüsüne gir.

    elif action_type == "PROCESS_GUEST_CALL":
        announcement_id = action_data_map.get("welcome_announcement_id", "ANNOUNCE_GUEST_WELCOME_TR")
        audio_path = announcement_map.get(announcement_id, fallback_audio)
        play_audio(call_id, media_info, audio_path)
        log.info("processing_new_guest_caller")
        # TODO: user-service'e gRPC ile CreateUser çağrısı yaparak bu kullanıcıyı kaydet.
        # Sonra AI döngüsünü başlat.

    else:
        log.error("unknown_dialplan_action", received_action=action_type)
        play_audio(call_id, media_info, fallback_audio)


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
            process_call_event(message_data)
            
    except Exception as e:
        log.error("message_processing_error", error=str(e), exc_info=True)
    
    ch.basic_ack(delivery_tag=method.delivery_tag)


def main():
    log.info("agent-service_starting", service_name="agent-service")
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