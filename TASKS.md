# 🧠 Sentiric Agent Service - Görev Listesi (v5.3 - Uçtan Uca Akış Onarım Planı)

Bu belge, canlı testlerde tespit edilen ve platformun temel fonksiyonelliğini engelleyen kritik hataları gidermek için gereken ACİL ve ÖNCELİKLİ görevleri tanımlar.

---

### **FAZ 1: Temel Orkestrasyon Yetenekleri (Mevcut Durum - Kısmen Hatalı)**
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

### **FAZ 2: Uçtan Uca Diyalog Akışının Onarımı (ACİL ÖNCELİK)**

**Amaç:** Platformun bir çağrıyı baştan sona yönetebilmesini, kullanıcıyla karşılıklı konuşabilmesini ve bu etkileşimi doğru bir şekilde kaydedebilmesini sağlamak.

-   [ ] **Görev ID: AGENT-BUG-02 - Yanlış Tenant ID ile Prompt Sorgulama Hatası (KRİTİK & ACİL)**
    -   **Durum:** ⬜ **Yapılacak (İLK GÖREV)**
    -   **Engelleyici Mi?:** **EVET. TÜM PLATFORMUN ÇALIŞMASINI BLOKE EDİYOR.**
    -   **Tahmini Süre:** ~1 saat
    -   **Açıklama:** `StateWelcoming` durumunda, veritabanından `PROMPT_WELCOME_GUEST` şablonu "default" tenant'ı için aranıyor, ancak bu şablon "system" tenant'ı altında kayıtlı. Bu tutarsızlık, diyalog döngüsünün anında çökmesine, boş ses kayıtlarına ve hatalı çağrı sürelerine neden olan **ana sorundur.**
    -   **Kabul Kriterleri:**
        -   [ ] `internal/database/postgres.go` içindeki `GetTemplateFromDB` fonksiyonu, hem istekle gelen `tenant_id`'yi hem de fallback olarak `'system'` tenant'ını arayacak şekilde (`WHERE id = $1 AND language_code = $2 AND (tenant_id = $3 OR tenant_id = 'system') ORDER BY tenant_id DESC LIMIT 1`) güncellenmelidir.
        -   [ ] Bu düzeltme sonrasında yapılan test çağrısında, `agent-service` loglarında "şablon bulunamadı" hatasının **görülmediği** ve durum makinesinin `StateWelcoming`'den sonra `StateListening`'e geçtiği **doğrulanmalıdır.**

-   [ ] **Görev ID: AGENT-DIAG-01 - Tam Diyalog Döngüsü Sağlamlık Testi**
    -   **Durum:** ⬜ Planlandı
    -   **Bağımlılık:** `AGENT-BUG-02`'nin tamamlanmasına bağlı.
    -   **Tahmini Süre:** ~4-6 saat (hata ayıklama dahil)
    -   **Açıklama:** `AGENT-BUG-02` düzeltildikten sonra, tam bir diyalog döngüsünü (Karşılama -> Dinleme -> Anlama -> Konuşma) test etmek ve ortaya çıkacak yeni sorunları tespit edip gidermek.
    -   **Kabul Kriterleri:**
        -   [ ] Test çağrısı sırasında kullanıcıya "Merhaba, Sentiric'e hoş geldiniz..." gibi bir karşılama anonsu **duyulmalıdır.**
        -   [ ] Kullanıcı konuştuğunda, `stt-service`'in bu konuşmayı metne çevirdiği loglarda **görülmelidir.**
        -   [ ] `agent-service`'in, bu metinle `llm-service`'e istek attığı loglarda **görülmelidir.**
        -   [ ] `agent-service`'in, LLM yanıtını `tts-gateway`'e gönderdiği ve dönen ses verisini `media-service`'e çaldırdığı **doğrulanmalıdır.**
        -   [ ] Döngünün en az 2 tur (kullanıcı konuşur, sistem cevap verir, kullanıcı tekrar konuşur, sistem tekrar cevap verir) tamamladığı kanıtlanmalıdır.

-   [ ] **Görev ID: AGENT-011 - Çağrı Kaydı Bütünlüğünün Sağlanması**
    -   **Durum:** ⬜ Planlandı
    -   **Bağımlılık:** `AGENT-DIAG-01`'in tamamlanmasına bağlı.
    -   **Açıklama:** Diyalog döngüsü başarılı olduğunda, çağrı kaydının tüm sesleri (karşılama, kullanıcı, AI yanıtları) içerdiğini ve `cdr-service`'in bu kaydın URL'ini aldığını doğrulamak.
    -   **Kabul Kriterleri:**
        -   [ ] Test çağrısı sonunda MinIO'ya kaydedilen `.wav` dosyası indirildiğinde, içinde hem sistemin hem de kullanıcının seslerinin olduğu **duyulmalıdır.**
        -   [ ] `media-service`, kayıt tamamlandığında `call.recording.available` olayını RabbitMQ'ya yayınlamalıdır. (Bu `MEDIA-004` görevidir).
        -   [ ] `cdr-service`, bu olayı dinleyerek `calls` tablosundaki ilgili kaydın `recording_url` alanını güncellemelidir. (Bu `CDR-005` görevidir).


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