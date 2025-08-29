# 🧠 Sentiric Agent Service - Görev Listesi (v5.1 - Dayanıklı Akış ve Akıllı Sonlandırma)

Bu belge, `agent-service`'in geliştirme yol haritasını, tamamlanan görevleri ve bir sonraki öncelikleri tanımlar.

---

### **FAZ 1: Temel Orkestrasyon Yetenekleri (Mevcut Durum)**

**Amaç:** Servisin temel olayları dinleyip, diğer servisleri yöneterek basit bir diyalog akışını baştan sona yürütebilmesini sağlamak.

-   [x] **Görev ID: AGENT-CORE-01 - Olay Tüketimi ve Servis İstemcileri**
    -   **Açıklama:** `call.started` ve `call.ended` olaylarını RabbitMQ'dan dinleme ve `user`, `media`, `tts`, `stt`, `llm` servisleri için istemcileri (gRPC/HTTP) oluşturma.
    -   **Durum:** ✅ **Tamamlandı**

-   [x] **Görev ID: AGENT-CORE-02 - Misafir Kullanıcı Oluşturma (`PROCESS_GUEST_CALL`)**
    -   **Açıklama:** `dialplan`'den `PROCESS_GUEST_CALL` eylemi geldiğinde, arayan için otomatik olarak `user-service` üzerinde bir kullanıcı kaydı oluşturma.
    -   **Durum:** ✅ **Tamamlandı**

-   [x] **Görev ID: AGENT-CORE-03 - Temel Durum Makinesi ve Diyalog Döngüsü**
    -   **Açıklama:** Her çağrı için `WELCOMING` -> `LISTENING` -> `THINKING` -> `SPEAKING` durumlarını yöneten, Redis tabanlı bir durum makinesi ve `RunDialogLoop` implementasyonu.
    -   **Durum:** ✅ **Tamamlandı**

-   [x] **Görev ID: AGENT-CORE-04 - Anında Sesli Geri Bildirim**
    -   **Açıklama:** AI'ın ilk yanıtı hazırlanırken kullanıcının "ölü hava" duymasını engellemek için, çağrı başlar başlamaz bir "bağlanıyor" anonsu çalma yeteneği.
    -   **Durum:** ✅ **Tamamlandı**

-   [x] **Görev ID: AGENT-CORE-05 - Yarış Durumuna Karşı Dayanıklılık (Race Condition Fix)**
    -   **Açıklama:** `call.started` ve `call.ended` olayları aynı anda geldiğinde, `context canceled` hatası oluşmasını engelleyen, Redis tabanlı, daha dayanıklı bir durum yönetimi mimarisi.
    -   **Durum:** ✅ **Tamamlandı**

-   [x] **Görev ID: AGENT-BUG-01 - Çağrı Kaydı Tenant ID Düzeltmesi**
    -   **Açıklama:** Çağrı kaydı S3 yolunu oluştururken, `dialplan`'in `tenant_id`'si yerine çağrının geldiği `inbound_route`'un `tenant_id`'sini kullanarak veri izolasyonunu sağlama.
    -   **Durum:** ✅ **Tamamlandı**

---

### **FAZ 2: Akıllı ve Güvenli Diyalog Yönetimi (Tamamlandı)**

**Amaç:** Servisi, hataları yönetebilen, zaman aşımlarına duyarlı ve diyalog akışını akıllıca sonlandırabilen, üretime hazır bir orkestratöre dönüştürmek.

-   [x] **Görev ID: AGENT-006 - Zaman Aşımlı ve Dayanıklı İstemciler (KRİTİK)**
    -   **Açıklama:** Harici AI servislerine (STT, LLM, TTS) yapılan tüm gRPC ve HTTP çağrılarına makul zaman aşımları (timeout) eklendi.
    -   **Durum:** ✅ **Tamamlandı**
    -   **Kabul Kriterleri:**
        -   [x] Tüm harici istemci çağrıları `context.WithTimeout` ile sarıldı (örn: LLM için 20s, TTS için 20s, STT için 60s).
        -   [x] Bir servis zaman aşımına uğradığında veya hata döndürdüğünde, bu durum loglandı ve diyalog döngüsü güvenli bir şekilde sonlandırıldı.
        -   [x] Hata durumunda, `media-service` üzerinden `ANNOUNCE_SYSTEM_ERROR` anonsu çalınarak `StateTerminated` durumuna geçildi.

-   [x] **Görev ID: AGENT-007 - AI Kararıyla Çağrıyı Sonlandırma (KRİTİK)**
    -   **Açıklama:** Diyalog döngüsünün belirli bir noktasında (örn: kullanıcı vedalaştığında veya işlem tamamlandığında) çağrıyı proaktif olarak sonlandırma yeteneği eklendi.
    -   **Bağımlılık:** `sip-signaling-service`'in `call.terminate.request` olayını dinlemesi.
    -   **Durum:** ✅ **Tamamlandı**
    -   **Kabul Kriterleri:**
        -   [x] `RunDialogLoop` içinde, `StateTerminated` durumuna ulaşıldığında, `RabbitMQ`'ya `call.terminate.request` tipinde ve `{"callId": "..."}` gövdesine sahip bir olay yayınlandı.
        -   [x] Bu olay, `sentiric_events` exchange'ine ve `call.terminate.request` routing key'ine gönderildi.

-   [x] **Görev ID: AGENT-009 - Sonsuz Döngü Kırma Mekanizması**
    -   **Açıklama:** `StateListening` durumunda, art arda belirli sayıda (örn: 2 kez) STT'den boş metin dönmesi veya anlama hatası yaşanması durumunda, bir hata anonsu çalıp çağrıyı sonlandıran bir sayaç mekanizması eklendi.
    -   **Durum:** ✅ **Tamamlandı**
    -   **Kabul Kriterleri:**
        -   [x] `CallState` yapısına `consecutive_failures` adında bir sayaç eklendi.
        -   [x] `StateFnListening` içinde, STT'den boş metin döndüğünde veya hata alındığında bu sayaç artırıldı.
        -   [x] Sayaç belirlenen eşiğe ulaştığında, `ANNOUNCE_SYSTEM_MAX_FAILURES` anonsu çalınarak durum `StateTerminated`'e set edildi.
        -   [x] Başarılı bir transkripsiyon olduğunda sayaç sıfırlandı.

---

### **FAZ 3: Gelişmiş Orkestrasyon (Sıradaki Öncelik)**

**Amaç:** Platformu, karmaşık ve çok adımlı iş akışlarını yönetebilen, daha zeki bir sisteme dönüştürmek.

-   [ ] **Görev ID: AGENT-003 - Akıllı AI Orkestratörü**
    -   **Açıklama:** Gelen görevin türüne göre en uygun (hızlı/ucuz/kaliteli) LLM veya TTS motorunu dinamik olarak seçme yeteneği ekle.
    -   **Durum:** ⬜ Planlandı.

-   [ ] **Görev ID: AGENT-004 - SAGA Pattern Uygulaması**
    -   **Açıklama:** `ADR-003`'te tanımlandığı gibi, çok adımlı işlemlerde veri bütünlüğünü garanti altına almak için SAGA orkestrasyon mantığını implemente et.
    -   **Durum:** ⬜ Planlandı.

-   [ ] **Görev ID: AGENT-008 - Anlaşılır Hata Yönetimi**
    -   **Açıklama:** `ANNOUNCE_SYSTEM_ERROR` yerine, hatanın kaynağına göre daha spesifik anonslar çal (örn: `ANNOUNCE_TTS_UNAVAILABLE`, `ANNOUNCE_LLM_TIMEOUT`).
    -   **Durum:** ⬜ Planlandı.