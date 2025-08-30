# 🧠 Sentiric Agent Service - Görev Listesi (v5.2 - Uçtan Uca Akış Onarımı)

Bu belge, `agent-service`'in geliştirme yol haritasını ve canlı testlerde tespit edilen kritik hataların giderilmesi için gereken acil görevleri tanımlar.

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

### **FAZ 2: Uçtan Uca Diyalog Akışının Sağlamlaştırılması (ACİL ÖNCELİK)**

**Amaç:** Canlı testlerde tespit edilen ve diyalog döngüsünün başlamasını engelleyen kritik hataları gidererek, platformun ilk sesli yanıtını başarıyla vermesini sağlamak.

-   [ ] **Görev ID: AGENT-BUG-02 - Yanlış Tenant ID ile Prompt Sorgulama Hatası (KRİTİK & ACİL)**
    -   **Durum:** ⬜ **Yapılacak (Sıradaki)**
    -   **Engelleyici Mi?:** **EVET.** Bu hata, tüm diyalog akışını engellemektedir.
    -   **Tahmini Süre:** ~1-2 saat
    -   **Açıklama:** `StateWelcoming` durumunda, `generateWelcomeText` fonksiyonu `database.GetTemplateFromDB`'yi çağırırken "default" tenant_id'sini kullanıyor. Ancak "Genesis Bloğu" (`02_core_data.sql`) bu prompt'ları "system" tenant'ı altında oluşturuyor. Bu tutarsızlık, şablonun bulunamamasına ve diyalog döngüsünün çökmesine neden oluyor.
    -   **Kabul Kriterleri:**
        -   [ ] `internal/database/postgres.go` içindeki `GetTemplateFromDB` fonksiyonu, sadece belirtilen `tenant_id`'yi değil, aynı zamanda fallback olarak `system` (veya `default`) tenant'ını da arayacak şekilde (`(tenant_id = $3 OR tenant_id = 'system') ORDER BY tenant_id DESC LIMIT 1`) güncellenmelidir.
        -   [ ] Alternatif olarak, `internal/dialog/states.go` içindeki `generateWelcomeText` fonksiyonu, `CallState`'ten gelen `TenantID`'yi doğru bir şekilde `GetTemplateFromDB`'ye iletmelidir. **En doğru çözüm veritabanı sorgusunu daha esnek hale getirmektir.**
        -   [ ] Düzeltme yapıldıktan sonra, yeni bir test çağrısında `agent-service`'in artık "şablon bulunamadı" hatası vermediği ve diyalog akışına devam ettiği loglarda doğrulanmalıdır.

-   [ ] **Görev ID: AGENT-011 - Çağrı Kaydı URL'ini Loglama ve Olayını Yayınlama (Öncelik Yükseltildi)**
    -   **Durum:** ⬜ **Planlandı**
    -   **Bağımlılık:** `MEDIA-004`'e (`media-service`'in S3 URL'ini dönmesi) bağlı.
    -   **Açıklama:** Çağrı kaydı (`StartRecording`) başarılı olduğunda, `media-service`'ten dönülecek olan S3 URL'ini `cdr-service` gibi diğer servislerin kullanabilmesi için loglamak ve `call.recording.started` gibi bir olayla yayınlamak.
    -   **Kabul Kriterleri:**
        -   [ ] `agent-service` loglarında "Çağrı kaydı başlatılıyor... uri=s3:///..." logunun, `media-service`'ten gelen gerçek ve tam URL'i içerdiği doğrulanmalıdır.
        -   [ ] (Opsiyonel ama önerilir) `call.recording.available` olayı, `agent-service` tarafından dinlenmeli ve bu olay geldiğinde `calls` tablosundaki ilgili kaydın `recording_url` alanı güncellenmelidir. Bu iş `cdr-service`'in de sorumluluğu olabilir.

---
### **FAZ 3: Gelişmiş Orkestrasyon (Sıradaki Öncelik)**

**Amaç:** Platformu, karmaşık ve çok adımlı iş akışlarını yönetebilen, daha zeki bir sisteme dönüştürmek.

-   [x] **Görev ID: AGENT-010 - Kullanıcı Kimliği Olayını Yayınlama (KRİTİK DÜZELTME)**
    -   **Açıklama:** Misafir bir kullanıcı `user-service` üzerinde oluşturulduktan sonra, `cdr-service` gibi diğer servisleri bilgilendirmek için `user.created.for_call` tipinde yeni bir olay yayınlandı.
    -   **Durum:** ✅ **Tamamlandı**
    -   **Not:** Bu, `cdr-service`'in çağrı kaydını doğru `user_id` ve `contact_id` ile güncellemesini sağlayarak yarış durumu (race condition) sorununu kökünden çözer.

-   [ ] **Görev ID: AGENT-011 - Çağrı Kaydı URL'ini Loglama ve Olayını Yayınlama**
    -   **Açıklama:** Çağrı kaydı tamamlandığında, `media-service`'ten gelecek `call.recording.available` olayını dinleyerek veya geçici olarak URL'i tahmin ederek loglama ve raporlama yeteneği ekle.
    -   **Durum:** ⬜ Planlandı (MEDIA-004'e bağımlı).
        
-   [ ] **Görev ID: AGENT-003 - Akıllı AI Orkestratörü**
    -   **Açıklama:** Gelen görevin türüne göre en uygun (hızlı/ucuz/kaliteli) LLM veya TTS motorunu dinamik olarak seçme yeteneği ekle.
    -   **Durum:** ⬜ Planlandı.

-   [ ] **Görev ID: AGENT-004 - SAGA Pattern Uygulaması**
    -   **Açıklama:** `ADR-003`'te tanımlandığı gibi, çok adımlı işlemlerde veri bütünlüğünü garanti altına almak için SAGA orkestrasyon mantığını implemente et.
    -   **Durum:** ⬜ Planlandı.

-   [ ] **Görev ID: AGENT-008 - Anlaşılır Hata Yönetimi**
    -   **Açıklama:** `ANNOUNCE_SYSTEM_ERROR` yerine, hatanın kaynağına göre daha spesifik anonslar çal (örn: `ANNOUNCE_TTS_UNAVAILABLE`, `ANNOUNCE_LLM_TIMEOUT`).
    -   **Durum:** ⬜ Planlandı.