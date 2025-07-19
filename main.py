import os
import pika
import time
import threading
import json
import requests
import base64
from fastapi import FastAPI
import uvicorn

# --- Ayarlar ---
RABBITMQ_HOST = os.getenv("RABBITMQ_HOST", "rabbitmq")
RABBITMQ_USER = os.getenv("RABBITMQ_USER", "sentiric")
RABBITMQ_PASS = os.getenv("RABBITMQ_PASS", "sentiric_pass")
MEDIA_SERVICE_URL = os.getenv("MEDIA_SERVICE_URL", "http://media-service:3003")
QUEUE_NAME = 'call.events'
WELCOME_MESSAGE_TEXT = "Merhaba, Sentiric platformuna hoş geldiniz."

# --- FastAPI Uygulaması ---
app = FastAPI(title="Sentiric Agent Service", version="0.1.0")

@app.get("/health")
def read_health():
    return {"status": "ok", "service": "Agent Service"}

# --- TTS Fonksiyonu ---
def text_to_speech_file(text: str) -> str | None:
    """
    Verilen metni sese dönüştürür ve base64 formatında döndürür.
    Bu örnekte basit ve ücretsiz bir TTS API'si kullanıyoruz.
    """
    try:
        print(f"--> TTS API'sine istek gönderiliyor: '{text}'")
        tts_api_url = f"https://api.streamelements.com/kappa/v2/speech?voice=Brian&text={requests.utils.quote(text)}"
        response = requests.get(tts_api_url, timeout=10)
        
        # DÜZELTME: Hem "audio/mpeg" hem de "audio/mp3" formatlarını kabul et.
        content_type = response.headers.get("Content-Type", "").lower()
        if response.status_code == 200 and ("audio/mpeg" in content_type or "audio/mp3" in content_type):
            audio_base64 = base64.b64encode(response.content).decode('utf-8')
            print(f"✅ TTS API'sinden ses verisi başarıyla alındı (Content-Type: {content_type}, Base64 boyutu: {len(audio_base64)}).")
            return audio_base64
        else:
            print(f"❌ TTS API hatası: Status {response.status_code}, Content-Type: {content_type}")
            return None
    except Exception as e:
        print(f"❌ TTS API'sine bağlanırken hata oluştu: {e}")
        return None

# --- RabbitMQ Tüketici Fonksiyonu ---
def consume_events():
    """RabbitMQ'dan olayları dinleyen ve yeniden bağlanma mantığı içeren ana fonksiyon."""
    print("[Agent Service] Olay dinleme thread'i başlatıldı.")
    while True:
        try:
            print(f"[Agent Service] RabbitMQ'ya bağlanmaya çalışılıyor... Host: {RABBITMQ_HOST}")
            credentials = pika.PlainCredentials(RABBITMQ_USER, RABBITMQ_PASS)
            connection_params = pika.ConnectionParameters(
                host=RABBITMQ_HOST,
                credentials=credentials,
                heartbeat=30,
                blocked_connection_timeout=60
            )
            
            connection = pika.BlockingConnection(connection_params)
            channel = connection.channel()
            
            channel.queue_declare(queue=QUEUE_NAME, durable=True)
            print(f"✅ [Agent Service] RabbitMQ'ya başarıyla bağlandı. '{QUEUE_NAME}' kuyruğu dinleniyor...")

            def callback(ch, method, properties, body):
                try:
                    message_data = json.loads(body.decode())
                    print("\n--- 🧠 Yeni Olay Alındı! ---")
                    
                    if message_data.get('eventType') == 'call.started':
                        print("--> 'call.started' olayı tespit edildi. Karşılama süreci başlıyor...")
                        
                        # 1. Adım: TTS API'sini kullanarak karşılama sesini üret.
                        audio_data_base64 = text_to_speech_file(WELCOME_MESSAGE_TEXT)
                        
                        if audio_data_base64:
                            # 2. Adım: Media Service'e sesi dinletmesi için komut gönder.
                            media_info = message_data.get('media', {})
                            rtp_port = media_info.get('port')
                            
                            if rtp_port:
                                play_audio_payload = {
                                    "rtp_port": rtp_port,
                                    "audio_data_base64": audio_data_base64,
                                    "format": "mp3_base64"
                                }
                                try:
                                    print(f"--> 🔊 Media Service'e {rtp_port} portundan sesi çalması için komut gönderiliyor...")
                                    requests.post(f"{MEDIA_SERVICE_URL}/play-audio", json=play_audio_payload, timeout=5)
                                except Exception as e:
                                    print(f"❌ Media Service'e komut gönderilirken hata: {e}")
                    
                except Exception as e:
                    print(f"HATA: Mesaj işlenirken bir sorun oluştu: {e}")
                
                ch.basic_ack(delivery_tag=method.delivery_tag)

            channel.basic_consume(queue=QUEUE_NAME, on_message_callback=callback)
            channel.start_consuming()

        except pika.exceptions.AMQPConnectionError as e:
            print(f"❌ RabbitMQ bağlantı hatası: {e}. 5 saniye sonra tekrar denenecek...")
            time.sleep(5)
        except Exception as e:
            print(f"❌ Beklenmedik bir hata oluştu: {e}. 5 saniye sonra tekrar denenecek...")
            time.sleep(5)

# --- Ana Başlatma Bloğu ---
if __name__ == "__main__":
    # 1. RabbitMQ dinleyicisini ayrı bir arka plan thread'inde başlat.
    listener_thread = threading.Thread(target=consume_events, daemon=True)
    listener_thread.start()
    
    # 2. FastAPI/Uvicorn web sunucusunu ana thread'de başlat.
    uvicorn.run(app, host="0.0.0.0", port=8000)