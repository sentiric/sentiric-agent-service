# 🧠 Sentiric Agent Service - Görev Listesi

Bu belge, `agent-service`'in geliştirme yol haritasını ve önceliklerini tanımlar.

---

### Faz 1: Temel Orkestrasyon Yetenekleri (Mevcut Durum)

Bu faz, servisin temel olayları dinleyip basit, önceden tanımlanmış eylemleri tetikleyebilmesini hedefler.

-   [x] **RabbitMQ Tüketicisi:** `call.started` olaylarını dinleme yeteneği.
-   [x] **gRPC İstemcileri:** `user-service` ve `media-service` için güvenli (mTLS) istemcilerin oluşturulması.
-   [x] **Temel Eylem Yönetimi:** `dialplan` kararına göre `PlayAudio` veya `CreateUser` gibi temel gRPC çağrılarını yapabilme.
-   [x] **HTTP İstemcisi:** `llm-service` ve `tts-service`'e basit REST istekleri atabilme.

- [ ] **Görev ID: AGT-015 - AI Kararıyla Çağrıyı Sonlandırma (KRİTİK)**
    -   **Açıklama:** Diyalog döngüsünün belirli bir noktasında (örneğin, kullanıcı vedalaştığında, art arda anlama hatası olduğunda veya işlem tamamlandığında) çağrıyı proaktif olarak sonlandırma yeteneği ekle. Bu, `sip-signaling-service`'in yeni eklenen uzaktan sonlandırma özelliğini kullanacak.
    -   **Bağımlılık:** `sip-signaling-service` (Görev `SIG-005`)
    -   **Teknik Gereksinimler:**
        -   `agent-service`'in RabbitMQ bağlantısını kullanarak `sentiric_events` exchange'ine mesaj yayınlayabilen bir fonksiyon oluşturulmalı.
        -   Bu fonksiyon, `call.terminate.request` routing key'ini kullanarak aşağıdaki formatta bir JSON mesajı göndermelidir:
            ```json
            {
              "callId": "sonlandırılacak-çağrının-id'si"
            }
            ```
        -   Diyalog yöneticisi (`dialogue_manager.rs`), `State::TERMINATED` veya benzeri bir son duruma ulaştığında bu yeni fonksiyonu çağırmalıdır.
    -   **Kabul Kriterleri:**
        -   [ ] `agent-service`, diyalog akışını sonlandırma kararı aldığında RabbitMQ'ya doğru formatta ve doğru routing key ile bir `call.terminate.request` olayı yayınlamalıdır.
        -   [ ] `sip-signaling-service` loglarında bu isteğin alındığı ve bir `BYE` paketinin gönderildiği görülmelidir.
        -   [ ] Kullanıcının telefonu (SIP istemcisi) çağrının sonlandığını görmelidir.
        -   [ ] Çağrı sonlandıktan sonra `agent-service`'in `call.ended` olayını işlemesi ve state'i temizlemesi mevcut akışı bozmamalıdır.
        
- [ ] **Görev ID: AGENT-007 - Çağrı Sonlandırma İsteği Yayınlama (KRİTİK)**
    -   **Açıklama:** Bir diyalog, `StateTerminated` durumuna ulaştığında (örneğin art arda anlama hatası nedeniyle), `RabbitMQ`'ya `call.terminate.request` tipinde bir olay yayınla. Bu olay, sonlandırılacak `call_id`'yi içermelidir.
    -   **Kabul Kriterleri:**
        -   [ ] `RunDialogLoop` içinde, döngü sonlandığında bu yeni olay `RabbitMQ`'ya gönderilmelidir.
        -   [ ] Olay, `sip-signaling-service`'in işleyebileceği standart bir formata sahip olmalıdır.

- [ ] **Görev ID: AGENT-008 - Misafir Kullanıcı Oluşturma Mantığı (`PROCESS_GUEST_CALL`)**
    -   **Açıklama:** `dialplan`'den `PROCESS_GUEST_CALL` eylemi geldiğinde, `agent-service`'in bu "misafir" arayan için `user-service` üzerinde yeni bir kullanıcı ve iletişim kanalı oluşturmasını sağlayan mantığı implemente et.
    -   **Kabul Kriterleri:**
        -   [ ] `agent-service`, `call.started` olayındaki `from` bilgisini ayrıştırarak arayanın numarasını almalıdır.
        -   [ ] `user-service`'in `CreateUser` RPC'sini, `tenant_id` (dialplan'den gelen), `user_type='caller'` ve arayanın numarası ile çağırmalıdır.
        -   [ ] Kullanıcı oluşturulduktan sonra, standart `START_AI_CONVERSATION` akışına devam edilmelidir.
        
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