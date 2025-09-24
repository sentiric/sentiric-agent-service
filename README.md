# 🧠 Sentiric Agent Service

[![Status](https://img.shields.io/badge/status-active-success.svg)]()
[![Language](https://img.shields.io/badge/language-Go-blue.svg)]()
[![Protocol](https://img.shields.io/badge/protocol-gRPC_&_RabbitMQ-green.svg)]()

**Sentiric Agent Service**, Sentiric platformunun **merkezi asenkron beynidir.** Yüksek performans, eşzamanlılık ve sağlamlık için **Go** ile yazılmıştır. Görevi, `RabbitMQ` üzerinden gelen olayları dinlemek ve bu olaylara göre platformdaki diğer uzman servisleri (`media`, `user`, `llm` vb.) yöneterek iş akışlarını hayata geçirmektir.

Bu servis, platformun asenkron iş mantığını yürüten ana işçisidir (worker).

## 🎯 Temel Sorumluluklar

*   **Olay Tüketimi:** `call.events` gibi RabbitMQ kuyruklarını dinleyerek `call.started` gibi olayları tüketir.
*   **İş Akışı Orkestrasyonu:** Gelen olayın içerdiği `dialplan` kararına göre bir dizi eylemi yönetir. Örneğin:
    *   Bir kullanıcıyı `user-service`'e kaydeder.
    *   Bir anonsu `media-service`'e çaldırır.
    *   **RAG (Retrieval-Augmented Generation):** Bir yapay zeka diyaloğu başlatmak için önce `knowledge-service`'i sorgular, ardından bu bilgiyle zenginleştirilmiş bir isteği `llm-service`'e gönderir.
*   **Servis İstemcisi:** Platformdaki diğer tüm uzman mikroservisler için birincil istemci (client) olarak görev yapar. İletişim için gRPC (iç servisler) ve HTTP/REST (AI servisleri) kullanır.
*   **Durum Yönetimi:** Uzun süren diyalogların durumunu yönetmek için Redis kullanır.

## 🛠️ Teknoloji Yığını

*   **Dil:** Go
*   **Asenkron İletişim:** RabbitMQ (`amqp091-go` kütüphanesi)
*   **Servisler Arası İletişim:**
    *   **gRPC:** İç, yüksek performanslı servislere (`media`, `user`, `tts-gateway`, `knowledge`) bağlanmak için.
    *   **HTTP/REST:** Dış veya bağımlılıkları izole edilmiş AI servislerine (`llm-service`, `stt-service`) bağlanmak için.
*   **Veritabanı Erişimi:** PostgreSQL (`pgx` kütüphanesi)
*   **Gözlemlenebilirlik:** Prometheus metrikleri ve `zerolog` ile yapılandırılmış loglama.

## 🔌 API Etkileşimleri

Bu servis bir sunucu değil, bir **istemci ve tüketicidir.** Dışarıya bir port açmaz.

*   **Gelen (Tüketici):**
    *   `RabbitMQ`: Ana iş akışını tetikleyen olayları alır.
*   **Giden (İstemci):**
    *   `sentiric-media-service` (gRPC): Medya işlemlerini yönetmek için.
    *   `sentiric-user-service` (gRPC): Kullanıcı işlemlerini yönetmek için.
    *   `sentiric-knowledge-service` (gRPC): RAG için kurumsal bilgi tabanını sorgulamak.
    *   `sentiric-tts-gateway-service` (gRPC): Metni sese çevirmek için.
    *   `sentiric-llm-service` (HTTP/REST): Yapay zeka metin üretimi için.
    *   `sentiric-stt-service` (HTTP/WebSocket): Sesi metne çevirmek için.
    *   `PostgreSQL`: Anons yolları gibi konfigürasyon verilerini okumak için.
    *   `Redis`: Çağrı durumunu yönetmek için.

## 🚀 Yerel Geliştirme

1.  **Bağımlılıkları Yükleyin:**
2.  **Ortam Değişkenlerini Ayarlayın:** `.env.example` dosyasını `.env` olarak kopyalayın ve gerekli değişkenleri doldurun.
3.  **Servisi Çalıştırın:**

## 🤝 Katkıda Bulunma

Katkılarınızı bekliyoruz! Lütfen projenin ana [Sentiric Governance](https://github.com/sentiric/sentiric-governance) reposundaki kodlama standartlarına ve katkıda bulunma rehberine göz atın.

---
## 🏛️ Anayasal Konum

Bu servis, [Sentiric Anayasası'nın (v11.0)](https://github.com/sentiric/sentiric-governance/blob/main/docs/blueprint/Architecture-Overview.md) **Zeka & Orkestrasyon Katmanı**'nda yer alan merkezi bir bileşendir.