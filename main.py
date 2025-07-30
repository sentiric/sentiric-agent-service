# DOSYA: sentiric-agent-service/main.py (DİNAMİK VERİTABANI BAĞLANTILI - NİHAİ VERSİYON)
import os
import pika
import time
import json
import structlog
import grpc
import psycopg2
from psycopg2.extras import RealDictCursor

from logger_config import setup_logging
from sentiric.media.v1 import media_pb2, media_pb2_grpc
from sentiric.user.v1 import user_pb2, user_pb2_grpc

# --- Konfigürasyon ---
log = setup_logging()
RABBITMQ_URL = os.getenv("RABBITMQ_URL")
DATABASE_URL = os.getenv("DATABASE_URL")
MEDIA_SERVICE_GRPC_URL = os.getenv("MEDIA_SERVICE_GRPC_URL")
USER_SERVICE_GRPC_URL = os.getenv("USER_SERVICE_GRPC_URL")
QUEUE_NAME = 'call.events'

# --- Veritabanı Bağlantısı ---
db_conn = None

def get_db_connection():
    """Veritabanı bağlantısını yönetir, gerekirse yeniden kurar."""
    global db_conn
    if db_conn is None or db_conn.closed != 0:
        try:
            log.info("database_connecting")
            db_conn = psycopg2.connect(DATABASE_URL)
            log.info("database_connection_successful")
        except psycopg2.OperationalError as e:
            log.error("database_connection_failed", error=str(e))
            db_conn = None
    return db_conn

def get_announcement_path(announcement_id: str) -> str:
    """Verilen anons ID'si için veritabanından ses dosyasının yolunu alır."""
    conn = get_db_connection()
    fallback_path = "audio/tr/system_error.wav"
    if not conn:
        log.error("get_announcement_path_db_unavailable", fallback_used=fallback_path)
        return fallback_path
    
    try:
        with conn.cursor() as cur:
            cur.execute("SELECT audio_path FROM announcements WHERE id = %s", (announcement_id,))
            result = cur.fetchone()
            if result:
                return result[0]
            else:
                log.warn("announcement_id_not_found_in_db", announcement_id=announcement_id)
                # Fallback olarak sistem hatası anonsunu veritabanından çekmeyi dene
                cur.execute("SELECT audio_path FROM announcements WHERE id = 'ANNOUNCE_SYSTEM_ERROR_TR'")
                fallback_result = cur.fetchone()
                return fallback_result[0] if fallback_result else fallback_path
    except Exception as e:
        log.error("get_announcement_path_query_failed", error=str(e))
        return fallback_path

# --- gRPC Çağrıları ---

def play_audio(call_id: str, media_info: dict, audio_path: str):
    """Media Service'e ses çalma komutu gönderir."""
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

def create_guest_user(caller_id: str, tenant_id: str):
    """User Service'i çağırarak yeni bir misafir kullanıcı oluşturur."""
    log.info("creating_guest_user", caller_id=caller_id, tenant_id=tenant_id)
    if not USER_SERVICE_GRPC_URL:
        log.error("user_service_url_not_configured")
        return

    try:
        grpc_target = USER_SERVICE_GRPC_URL.replace("http://", "")
        with grpc.insecure_channel(grpc_target) as channel:
            stub = user_pb2_grpc.UserServiceStub(channel)
            request = user_pb2.CreateUserRequest(
                id=caller_id,
                tenant_id=tenant_id,
                user_type="caller",
                name="Guest Caller" # İsim opsiyonel, varsayılan bir değer atayabiliriz
            )
            response = stub.CreateUser(request, timeout=10)
            if response.user and response.user.id == caller_id:
                log.info("guest_user_creation_successful", user_id=response.user.id)
            else:
                log.warn("guest_user_creation_failed_on_server", response=str(response))
    except grpc.RpcError as e:
        log.error("grpc_error_user_service", error=str(e), code=e.code().name, details=e.details())
    except Exception as e:
        log.error("create_guest_user_failed", error=str(e), exc_info=True)


# --- Ana İş Mantığı ---

def process_call_event(message_data: dict):
    call_id = message_data.get("callId")
    media_info = message_data.get("media")
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

    # ARTIK STATİK MAP YOK! HER ŞEY VERİTABANINDAN GELECEK.

    if action_type == "PLAY_ANNOUNCEMENT":
        announcement_id = action_data_map.get("announcement_id")
        audio_path = get_announcement_path(announcement_id)
        play_audio(call_id, media_info, audio_path)

    elif action_type == "START_AI_CONVERSATION":
        announcement_id = action_data_map.get("welcome_announcement_id", "ANNOUNCE_DEFAULT_WELCOME_TR")
        audio_path = get_announcement_path(announcement_id)
        play_audio(call_id, media_info, audio_path)
        log.info("ai_conversation_flow_started")
        # TODO: STT dinlemesini başlat ve LLM döngüsüne gir.

    elif action_type == "PROCESS_GUEST_CALL":
        tenant_id = dialplan.get("tenantId")
        from_uri = message_data.get("from", "")
        # Arayan numarayı SIP URI'sinden çıkarmak için basit bir regex veya split
        caller_id_match = from_uri.split('<sip:')[1].split('@')[0] if '<sip:' in from_uri else None
        
        if caller_id_match and tenant_id:
            create_guest_user(caller_id_match, tenant_id)
        else:
            log.error("cannot_create_guest_user_missing_info", caller_id=caller_id_match, tenant_id=tenant_id)

        announcement_id = action_data_map.get("welcome_announcement_id", "ANNOUNCE_GUEST_WELCOME_TR")
        audio_path = get_announcement_path(announcement_id)
        play_audio(call_id, media_info, audio_path)
        log.info("processing_new_guest_caller_flow_complete")
        # TODO: AI döngüsünü başlat.

    else:
        log.error("unknown_dialplan_action", received_action=action_type)
        audio_path = get_announcement_path("ANNOUNCE_SYSTEM_ERROR_TR")
        play_audio(call_id, media_info, audio_path)


def callback(ch, method, properties, body):
    """RabbitMQ'dan gelen mesajları işleyen ana fonksiyon."""
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
    if not all([RABBITMQ_URL, DATABASE_URL]):
        log.critical("rabbitmq_or_database_url_not_configured_exiting")
        return

    # İlk veritabanı bağlantısını kurmayı dene
    get_db_connection()

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