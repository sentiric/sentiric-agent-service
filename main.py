import os
import pika
import time
import threading
import json
from fastapi import FastAPI
import uvicorn

# --- RabbitMQ Bağlantı Bilgileri ---
RABBITMQ_HOST = os.getenv("RABBITMQ_HOST", "rabbitmq")
RABBITMQ_USER = os.getenv("RABBITMQ_USER", "sentiric")
RABBITMQ_PASS = os.getenv("RABBITMQ_PASS", "sentiric_pass")
QUEUE_NAME = 'call.events'

# --- FastAPI Uygulaması ---
app = FastAPI(title="Sentiric Agent Service", version="0.1.0")

@app.get("/health")
def read_health():
    """Servisin ayakta olup olmadığını kontrol etmek için basit bir endpoint."""
    return {"status": "ok", "service": "Agent Service"}

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
                    print(f"  Olay Tipi: {message_data.get('eventType')}")
                    print(f"  Çağrı ID: {message_data.get('callId')}")
                    print("  --> AI şimdi bu olaya göre bir aksiyon almalı (örn: karşılama mesajı üret).")
                    print("--------------------------")
                except json.JSONDecodeError:
                    print(f"⚠️ Alınan mesaj JSON formatında değil: {body.decode()}")
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