# 🧠 Sentiric Agent Service - Görev Listesi

Bu belge, `agent-service`'in geliştirme yol haritasını ve önceliklerini tanımlar.

---

### Faz 1: Temel Orkestrasyon Yetenekleri (Mevcut Durum)

Bu faz, servisin temel olayları dinleyip basit, önceden tanımlanmış eylemleri tetikleyebilmesini hedefler.

-   [x] **RabbitMQ Tüketicisi:** `call.started` olaylarını dinleme yeteneği.
-   [x] **gRPC İstemcileri:** `user-service` ve `media-service` için güvenli (mTLS) istemcilerin oluşturulması.
-   [x] **Temel Eylem Yönetimi:** `dialplan` kararına göre `PlayAudio` veya `CreateUser` gibi temel gRPC çağrılarını yapabilme.
-   [x] **HTTP İstemcisi:** `llm-service` ve `tts-service`'e basit REST istekleri atabilme.

-   [ ] **Görev ID: AGENT-007 - Çağrı Sonlandırma İsteği Yayınlama**
    - **Açıklama:** Bir diyalog TERMINATED durumuna ulaştığında, RabbitMQ'ya call.terminate.request tipinde bir olay yayınla. Bu olay, sonlandırılacak call_id'yi içermelidir.

-   [ ] **Görev ID: AGENT-006 - Zaman Aşımlı ve Dayanıklı İstemciler (KRİTİK)**
    -   **Açıklama:** Harici AI servislerine (STT, LLM, TTS) yapılan tüm gRPC ve HTTP çağrılarına makul zaman aşımları (timeout) ekle.
    -   **Kabul Kriterleri:**
        -   [ ] Tüm istemci çağrıları `context.WithTimeout` ile sarılmalı (örn: 15 saniye).
        -   [ ] Bir servis zaman aşımına uğradığında veya hata döndürdüğünde, bu durum loglanmalı ve diyalog döngüsü güvenli bir şekilde sonlandırılmalı.
        -   [ ] Hata durumunda, `media-service` üzerinden `ANNOUNCE_SYSTEM_ERROR` anonsu çalınmalı.

---

### Faz 2: Akıllı Diyalog Yönetimi (Sıradaki Öncelik)

Bu faz, servisi basit bir eylem tetikleyiciden, tam bir diyalog yöneticisine dönüştürmeyi hedefler.

-   [ ] **Görev ID: AGENT-001 - Durum Makinesi (State Machine) Entegrasyonu**
    -   **Açıklama:** Her bir aktif çağrının durumunu (`WELCOMING`, `LISTENING`, `EXECUTING_TASK`) yönetmek için Redis tabanlı bir durum makinesi implemente et.
    -   **Durum:** ⬜ Planlandı.

-   [ ] **Görev ID: AGENT-002 - Tam Diyalog Döngüsü**
    -   **Açıklama:** STT -> LLM -> TTS döngüsünü tam olarak implemente et. `media-service`'ten gelen ses verisini `stt-service`'e gönder, dönen metni `llm-service`'e gönder, dönen yanıtı `tts-gateway` ile sese çevir ve `media-service`'e geri çal.
    -   **Durum:** ⬜ Planlandı.

-   [ ] **Görev ID: AGENT-003 - Akıllı AI Orkestratörü**
    -   **Açıklama:** Gelen görevin türüne ve aciliyetine göre en uygun (hızlı/ucuz/kaliteli) LLM veya TTS motorunu dinamik olarak seçme yeteneği ekle.
    -   **Durum:** ⬜ Planlandı.

---

### Faz 3: Veri Bütünlüğü ve Dayanıklılık

Bu faz, servisi kurumsal düzeyde güvenilir ve hataya dayanıklı hale getirmeyi hedefler.

-   [ ] **Görev ID: AGENT-004 - SAGA Pattern Uygulaması**
    -   **Açıklama:** `ADR-003`'te tanımlandığı gibi, çok adımlı işlemlerde (örn: ödemeli randevu) veri bütünlüğünü garanti altına almak için SAGA orkestrasyon mantığını implemente et.
    -   **Durum:** ⬜ Planlandı.

-   [ ] **Görev ID: AGENT-005 - Gelişmiş Hata Yönetimi**
    -   **Açıklama:** gRPC/HTTP istemcilerine yeniden deneme (retry) ve devre kesici (circuit breaker) mekanizmaları ekle.
    -   **Durum:** ⬜ Planlandı.