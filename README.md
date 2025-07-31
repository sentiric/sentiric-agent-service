# 🧠 Sentiric Agent Service

**Açıklama:** Bu servis, Sentiric platformunun **merkezi orkestrasyon beynidir.** Yüksek performans, eşzamanlılık ve sağlamlık için **Go** ile yazılmıştır. Görevi, `RabbitMQ` üzerinden gelen olayları dinlemek ve bu olaylara göre platformdaki diğer uzman servisleri (`media`, `user`, `llm` vb.) yöneterek iş akışlarını hayata geçirmektir.

Bu servis, platformun asenkron iş mantığını yürüten ana işçisidir (worker).

## 🎯 Temel Sorumluluklar

*   **Olay Tüketimi:** `call.events` gibi RabbitMQ kuyruklarını dinleyerek `call.started` gibi olayları tüketir.
*   **İş Akışı Orkestrasyonu:** Gelen olayın içerdiği `dialplan` kararına göre bir dizi eylemi yönetir. Örneğin:
    *   Bir kullanıcıyı `user-service`'e kaydeder.
    *   Bir anonsu `media-service`'e çaldırır.
    *   Bir yapay zeka diyaloğu başlatmak için `llm-service`'e istek gönderir.
*   **Servis İstemcisi:** Platformdaki diğer tüm uzman mikroservisler için birincil istemci (client) olarak görev yapar. İletişim için gRPC (iç servisler) ve HTTP/REST (AI servisleri) kullanır.
*   **Durum Yönetimi (Gelecek):** Uzun süren diyalogların durumunu yönetmek için Redis veya benzeri bir in-memory veritabanı ile entegre olacaktır.

## 🛠️ Teknoloji Yığını

*   **Dil:** Go
*   **Asenkron İletişim:** RabbitMQ (`amqp091-go` kütüphanesi)
*   **Servisler Arası İletişim:**
    *   **gRPC:** İç, yüksek performanslı servislere (`media`, `user`, `dialplan`) bağlanmak için.
    *   **HTTP/REST:** Dış veya bağımlılıkları izole edilmiş AI servislerine (`llm-service`) bağlanmak için.
*   **Veritabanı Erişimi:** PostgreSQL (`pgx` kütüphanesi)

## 🔌 API Etkileşimleri

Bu servis bir sunucu değil, bir **istemci ve tüketicidir.** Dışarıya bir port açmaz.

*   **Gelen (Consumer Of):**
    *   `RabbitMQ`: Ana iş akışını tetikleyen olayları alır.
*   **Giden (Client Of):**
    *   `sentiric-media-service` (gRPC): Medya işlemlerini yönetmek için.
    *   `sentiric-user-service` (gRPC): Kullanıcı işlemlerini yönetmek için.
    *   `sentiric-llm-service` (HTTP/REST): Yapay zeka metin üretimi için.
    *   `PostgreSQL`: Anons yolları gibi konfigürasyon verilerini okumak için.

## 🚀 Yerel Geliştirme (Local Development)

### Önkoşullar
*   Go (versiyon 1.22+)
*   Docker & Docker Compose (bağımlı servisleri çalıştırmak için)

### Kurulum ve Çalıştırma
1.  **Bağımlılıkları Yükleyin:**
    Projenin ana dizininde `go mod tidy` komutunu çalıştırarak gerekli tüm Go modüllerini indirin.
    ```bash
    go mod tidy
    ```

2.  **Ortam Değişkenlerini Ayarlayın:**
    `.env.example` dosyasını `.env` olarak kopyalayın. Platformun diğer tüm servisleri (`sentiric-infrastructure` ile) Docker üzerinde çalışıyorsa, `localhost` adresleri doğru olacaktır.
    ```bash
    cp .env.example .env
    ```

3.  **Servisi Çalıştırın:**
    Platformun geri kalanı Docker'da çalışırken, `agent-service`'i doğrudan yerel makinenizde çalıştırarak hızlıca test edebilirsiniz:
    ```bash
    go run .
    ```

## 🐳 Docker ile Çalıştırma

Bu servis, `sentiric-infrastructure` reposundaki merkezi `docker-compose.yml` dosyası aracılığıyla platformun bir parçası olarak çalıştırılmak üzere tasarlanmıştır. `Dockerfile`, üretim için optimize edilmiş, minimal bir `scratch` imajı oluşturur.

## 🤝 Katkıda Bulunma

Katkılarınızı bekliyoruz! Lütfen projenin ana [Sentiric Governance](https://github.com/sentiric/sentiric-governance) reposundaki kodlama standartlarına ve katkıda bulunma rehberine göz atın.