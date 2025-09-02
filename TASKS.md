# 🧠 Sentiric Agent Service - Görev Listesi (v5.5 - Nihai Stabilizasyon)

Bu belge, platformun tam diyalog döngüsünü tamamlamasını engelleyen son kritik "nil pointer" hatasını gidermek için gereken görevleri tanımlar.

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

### **FAZ 2: Uçtan Uca Diyalog Akışının Sağlamlaştırılması (ACİL ÖNCELİK)**

**Amaç:** Canlı testlerde tespit edilen ve diyalog döngüsünü engelleyen kritik hataları gidererek, platformun kullanıcıyla tam bir karşılıklı konuşma yapabilmesini sağlamak.

-   [x] **Görev ID: AGENT-BUG-06 - Veritabanı Bütünlüğü ve Misafir Kullanıcı Oluşturma Hatası (KRİTİK & ACİL)**
    -   **Durum:** ✅ **Tamamlandı**
    -   **Bulgular:** `agent-service`, misafir bir kullanıcı oluştururken `tenant_id` olarak hard-code edilmiş `"default"` değerini `user-service`'e gönderiyor. Veritabanında bu isimde bir tenant olmadığı için `user-service` çöküyor ve tüm diyalog akışı `ANNOUNCE_SYSTEM_ERROR` ile sonlanıyor. Bu, anonsların duyulmaması ve STT/LLM döngüsünün hiç başlamamasının **kök nedenidir.**
    -   **Çözüm Stratejisi:** `agent-service`, tenant ID'sini hard-code etmek yerine, `dialplan`'den gelen dinamik veriyi kullanmalıdır.
    -   **Kabul Kriterleri:**
        -   [ ] `internal/handler/event_handler.go` içindeki `handleProcessGuestCall` fonksiyonu, yeni kullanıcı oluştururken `tenantID` olarak `event.Dialplan.GetInboundRoute().GetTenantId()` değerini kullanmalıdır.
        -   [ ] Eğer `InboundRoute` veya `TenantId` alanı `nil` veya boş ise, bir fallback olarak `"sentiric_demo"` tenant'ını kullanmalıdır. Hard-code edilmiş `"default"` değeri tamamen kaldırılmalıdır.
        -   [ ] Düzeltme sonrası yapılan test çağrısında, `user-service` loglarında artık `violates foreign key constraint` hatasının görülmediği ve `agent-service` loglarında `Misafir kullanıcı başarıyla oluşturuldu` mesajının göründüğü doğrulanmalıdır.
    -   **Tahmini Süre:** ~1 saat

-   [x] **Görev ID: AGENT-BUG-04 - `user.identified.for_call` Olayını Yayınlama (KRİTİK)**
    -   **Durum:** ✅ **Tamamlandı**
    -   **Bulgular:** `calls` tablosundaki `user_id`, `contact_id`, `tenant_id` alanlarının `(NULL)` kalması, bu olayın yayınlanmadığını veya `cdr-service` tarafından işlenmediğini kanıtlamaktadır. Bu, raporlama ve veri bütünlüğü için kritik bir eksikliktir.
    -   **Çözüm Stratejisi:** `agent-service`, bir kullanıcıyı bulduğunda veya başarılı bir şekilde oluşturduğunda, bu bilgiyi asenkron olarak diğer servislere duyurmalıdır.
    -   **Kabul Kriterleri:**
        -   [ ] `handleProcessGuestCall` fonksiyonu, `user-service`'ten başarılı bir kullanıcı yanıtı aldığında (`mevcut kullanıcı bulundu` VEYA `yeni misafir oluşturuldu`), `user.identified.for_call` tipinde yeni bir olayı RabbitMQ'ya yayınlamalıdır.
        -   [ ] Bu olayın payload'u, `sentiric-contracts`'te tanımlandığı gibi `call_id`, `user_id`, `contact_id` ve `tenant_id` alanlarını içermelidir.
        -   [ ] Test çağrısı sonunda, `cdr-service` loglarında bu olayın işlendiğine dair bir mesaj ve `calls` tablosunda ilgili alanların doğru bir şekilde doldurulduğu doğrulanmalıdır.
    -   **Tahmini Süre:** ~1 saat
    
-   [x] **Görev ID: AGENT-BUG-02 - Yanlış Tenant ID ile Prompt Sorgulama Hatası**
    -   **Durum:** ✅ **Tamamlandı ve Doğrulandı.**

-   [x] **Görev ID: AGENT-BUG-03 - `playText` Fonksiyonunda Kapsamlı Nil Pointer Koruması (KRİTİK & ACİL)**
    -   **Durum:** ✅ **Tamamlandı**
    -   **Engelleyici Mi?:** **EVET. TAM DİYALOG AKIŞINI BLOKE EDİYOR.**
    -   **Tahmini Süre:** ~1 saat
    -   **Açıklama:** `playText` fonksiyonu, `CallState` içindeki `st.Event.Media` map'ine ve içindeki `caller_rtp_addr`, `server_rtp_port` gibi anahtarlara erişmeden önce bu map'in veya anahtarların var olup olmadığını kontrol etmiyor. Bu, servisin çökmesine ve diyalog döngüsünün tamamlanamamasına neden oluyor.
    -   **Kabul Kriterleri:**
        -   [ ] `internal/dialog/states.go` içindeki `playText` fonksiyonu, `st.Event` ve `st.Event.Media`'nın `nil` olmadığını kontrol etmelidir.
        -   [ ] Fonksiyon, `caller_rtp_addr` ve `server_rtp_port` anahtarlarının `Media` map'inde var olup olmadığını ve doğru tipte (`string`, `float64`) olduklarını güvenli bir şekilde kontrol etmelidir.
        -   [ ] Eğer bu kritik medya bilgileri eksikse, fonksiyon paniklemek yerine anlamlı bir hata logu basmalı ve `error` döndürerek diyalog döngüsünün çağrıyı güvenli bir şekilde sonlandırmasını sağlamalıdır.
        -   [ ] Düzeltme sonrası yapılan test çağrısında, `agent-service`'in artık `panic` yapmadığı, `StateWelcoming`'i tamamlayıp, sesi kullanıcıya çaldığı ve `StateListening`'e geçtiği **loglarda ve ses kaydında doğrulanmalıdır.**

-   **Görev ID: AGENT-BUG-07 - STT Sessizlik/Timeout Durumunu Yönetme**
    -   **Durum:** ⬜ **Yapılacak (Bloklandı)**
    -   **Öncelik:** **KRİTİK**
    -   **Stratejik Önem:** Bu hata, tüm diyalog akışının kopmasının ve çağrının "Sizi anlayamadım" diyerek sonlanmasının **kök nedenidir.** Bu çözülmeden platform işlevsel olamaz.
    -   **Problem Tanımı:** `agent-service`, `stt-service`'e ses akışını başlattıktan sonra, kullanıcı konuşmasa veya `stt-service`'ten hiç yanıt gelmese bile sonsuza dek bekliyor. Sonunda `streamAndTranscribe` fonksiyonu boş metin döndürüyor ve `StateFnListening` içindeki hata sayacı tetiklenerek çağrı sonlandırılıyor.
    -   **Çözüm Stratejisi:** `StateFnListening` fonksiyonu, STT'den veri gelmemesi durumuna karşı daha dayanıklı hale getirilmelidir. `streamAndTranscribe` fonksiyonu, sadece metin değil, aynı zamanda "no_speech_detected" veya "timeout" gibi durumları da döndürebilmelidir.
    -   **Bağımlılıklar:** `STT-BUG-01` (STT servisinin timeout/sessizlik durumu bildirmesi)
    -   **Kabul Kriterleri:**
        -   [ ] Kullanıcı 15-20 saniye boyunca hiç konuşmadığında, `agent-service` loglarında "STT'den ses algılanmadı/zaman aşımı" gibi bir uyarı görülmelidir.
        -   [ ] Bu durumda, `ANNOUNCE_SYSTEM_CANT_HEAR_YOU` ("Sizi duyamıyorum") anonsu çalınmalı ve servis tekrar `StateListening` durumuna dönerek kullanıcıya bir şans daha vermelidir.
        -   [ ] Çağrı, `ANNOUNCE_SYSTEM_MAX_FAILURES` anonsuyla sonlandırılmamalıdır (kullanıcı hiç konuşmadığı sürece).
    -   **Tahmini Süre:** ~4-6 Saat

-   **Görev ID: AGENT-FEAT-01 - Dinamik TTS Ses Seçimi**
    -   **Durum:** ⬜ **Yapılacak**
    -   **Öncelik:** YÜKSEK
    -   **Stratejik Önem:** Platformun, her kiracı veya senaryo için farklı ses kimlikleri sunabilmesini sağlar. Ürün esnekliği için önemlidir.
    -   **Problem Tanımı:** Test çağrısında, `dialplan`'de `tr-TR-EmelNeural` sesi tanımlı olmasına rağmen, varsayılan `edge-tts` sesi duyulmaktadır. Loglar, `agent-service`'in `voice_selector` bilgisini `tts-gateway`'e göndermediğini doğrulamaktadır.
    -   **Çözüm Stratejisi:** `playText` fonksiyonu, `SynthesizeRequest` oluştururken, `st.Event.Dialplan.Action.ActionData.Data` içinden `voice_selector` anahtarını okumalı ve isteğe eklemelidir.
    -   **Kabul Kriterleri:**
        -   [ ] `tts-gateway` loglarında, gelen isteğin `voice_selector` alanının doğru değeri (`tr-TR-EmelNeural`) içerdiği görülmelidir.
        -   [ ] Test çağrısının ses kaydı dinlendiğinde, duyulan sesin `EmelNeural` olduğu doğrulanmalıdır.
    -   **Tahmini Süre:** ~1-2 Saat

-   **Görev ID: AGENT-RAG-01 - `knowledge-service` Entegrasyonu**
    -   **Durum:** ⬜ **Planlandı**
    -   **Öncelik:** ORTA
    -   **Stratejik Önem:** Bu görev, platformu basit bir "konuşan bottan", kurumsal bilgiye sahip "akıllı bir asistana" dönüştüren en önemli adımdır.
    -   **Bağımlılıklar:** `AGENT-BUG-07`'nin çözülerek diyalog döngüsünün stabil hale gelmesi.
    -   **Kabul Kriterleri:**
        -   [ ] `StateFnThinking` fonksiyonu, `llm-service`'i çağırmadan önce `knowledge-service`'in `/api/v1/query` endpoint'ine bir HTTP isteği göndermelidir.
        -   [ ] `knowledge-service`'ten dönen sonuçlar, LLM prompt'una "Şu bilgiyi kullanarak cevapla: [SONUÇLAR]... Soru: [KULLANICI SORUSU]" formatında eklenmelidir.
        -   [ ] **Uçtan Uca Test:** Kullanıcı "VIP Check-up paketine neler dahildir?" diye sorduğunda, sistemin `sentiric_health` bilgi tabanından aldığı doğru bilgiyle cevap verdiği ses kaydı ve loglarla kanıtlanmalıdır.
    -   **Tahmini Süre:** ~1 Gün

-   [ ] **Görev ID:** `CDR-BUG-02` / `AGENT-BUG-04`
    -   **Açıklama:** `cdr-service`'in `call.started` olayında kullanıcı bilgisi aramaktan vazgeçmesini sağla. Bunun yerine, `agent-service`'in, bir misafir kullanıcıyı oluşturduktan veya mevcut bir kullanıcıyı bulduktan sonra, `user_id`, `contact_id` ve `tenant_id` içeren yeni bir `user.identified.for_call` olayı yayınlamasını sağla. `cdr-service` bu yeni olayı dinleyerek mevcut `calls` kaydını güncellemeli.
    -   **Kabul Kriterleri:**
        *   [ ] `sentiric-contracts`'e yeni `UserIdentifiedForCallEvent` mesajı eklenmeli.
        *   [ ] `agent-service`, kullanıcıyı bulduktan/oluşturduktan sonra bu olayı yayınlamalı.
        *   [ ] `cdr-service`, bu olayı dinleyip ilgili `calls` satırını `UPDATE` etmeli.
        *   [ ] Test çağrısı sonunda `calls` tablosundaki `user_id`, `contact_id` ve `tenant_id` alanlarının doğru bir şekilde doldurulduğu doğrulanmalıdır.



-   [ ] **Görev ID: AGENT-DIAG-01 - Tam Diyalog Döngüsü Sağlamlık Testi**
    -   **Durum:** ⬜ Planlandı
    -   **Bağımlılık:** `AGENT-BUG-03`'ün tamamlanmasına bağlı.
    -   **Tahmini Süre:** ~4-6 saat (hata ayıklama dahil)
    -   **Kabul Kriterleri:**
        -   [ ] Test çağrısı sırasında kullanıcıya **"Merhaba, Sentirik'e hoş geldiniz..."** karşılama anonsu **duyulmalıdır.**
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

---

YENİ GÖREV (agent-service): AGENT-REFACTOR-02 - HTTP İstemci Soyutlaması
Açıklama: STTClient ve LLMClient için ortak bir BaseHttpClient veya benzeri bir yapı oluşturarak, Generate ve Transcribe gibi metodlardaki tekrar eden http.NewRequestWithContext, httpClient.Do ve hata yönetimi mantığını merkezileştirmek.


### **FAZ 3: Gelişmiş Orkestrasyon (Sıradaki Öncelik)**

-   [ ] **Görev ID: AGENT-REFACTOR-01 - Sorumlulukların Katmanlara Ayrılması**
    -   **Durum:** ⬜ **Planlandı**
    -   **Öncelik:** DÜŞÜK
    -   **Stratejik Önem:** Kodun test edilebilirliğini ve bakımını kolaylaştırarak uzun vadeli proje sağlığını güvence altına alır.
    -   **Tahmini Süre:** ~2-3 Gün
    
-   [ ] **Görev ID: AGENT-BUG-05 - Hatalı Olay Yayınlamayı Düzeltme**
    -   **Durum:** ⬜ Planlandı
    -   **Tahmini Süre:** ~15 dakika
    -   **Açıklama:** `call.terminate.request` olayı yayınlanırken, `cdr-service`'in olayı doğru bir şekilde işlemesi için JSON payload'una `eventType` alanı eklenmelidir.
    -   **Kabul Kriterleri:**
        -   [ ] `RunDialogLoop` fonksiyonundaki `defer` bloğunda, `terminationReq` struct'ına `EventType string \`json:"eventType"\`` alanı eklenmeli ve değeri `"call.terminate.request"` olarak atanmalıdır.
        
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

**Amaç:** `agent-service`'i, sadece konuşan değil, aynı zamanda anlayan, öğrenen ve hatırlayan bir beyne dönüştürmek. Bu, RAG ve zengin olay yayınlama yeteneklerinin eklenmesiyle gerçekleştirilecektir.

-   [ ] **Görev ID: AGENT-RAG-01 - `knowledge-service` Entegrasyonu (YÜKSEK ÖNCELİK)**
    -   **Durum:** ⬜ Planlandı
    -   **Bağımlılık:** `AGENT-DIAG-01`'in (stabil diyalog döngüsü) tamamlanmasına bağlı.
    -   **Tahmini Süre:** ~1 gün
    -   **Açıklama:** Kullanıcının konuşması STT ile metne çevrildikten sonra, bu metnin bir "bilgi talebi" olup olmadığını anlamak. Eğer öyleyse, `knowledge-service`'i çağırarak ilgili bağlamı (context) almak ve bu bağlamı LLM prompt'una ekleyerek RAG akışını tamamlamak.
    -   **Kabul Kriterleri:**
        -   [ ] `internal/dialog/states.go` içindeki `StateFnThinking` fonksiyonu güncellenmelidir.
        -   [ ] Fonksiyon, STT'den gelen metni analiz etmeli (basit bir anahtar kelime kontrolü veya bir LLM çağrısı ile niyet tespiti yapılabilir).
        -   [ ] Eğer niyet "bilgi talebi" ise, `knowledge-service`'in `/api/v1/query` endpoint'ine bir HTTP isteği gönderilmelidir.
        -   [ ] `knowledge-service`'ten dönen sonuçlar, `buildLlmPrompt` fonksiyonuna yeni bir argüman olarak verilmeli ve LLM prompt'u "Bağlam: ..., Soru: ..." formatında zenginleştirilmelidir.
        -   [ ] **Uçtan Uca Test:** Kullanıcı "VIP Check-up paketine neler dahildir?" diye sorduğunda, sistemin `knowledge-service`'ten aldığı bilgiyle doğru ve detaylı bir cevap verdiği ses kaydı ve loglarla kanıtlanmalıdır.

-   [ ] **Görev ID: AGENT-EVENT-01 - Zengin Diyalog Olaylarını Yayınlama**
    -   **Durum:** ⬜ Planlandı
    -   **Bağımlılık:** `AGENT-DIAG-01`'in tamamlanmasına bağlı.
    -   **Tahmini Süre:** ~1-2 gün
    -   **Açıklama:** `cdr-service`'i ve gelecekteki analiz servislerini beslemek için, diyalog sırasında gerçekleşen önemli anlarda (`transkripsiyon tamamlandı`, `LLM yanıtı üretildi` vb.) zengin içerikli olayları RabbitMQ'ya yayınlamak.
    -   **Kabul Kriterleri:**
        -   [ ] `StateFnListening` tamamlandığında, `call.transcription.available` tipinde ve `{"text": "..."}` gövdesine sahip bir olay yayınlanmalıdır.
        -   [ ] `StateFnThinking` tamamlandığında, `call.llm.response.generated` tipinde ve `{"prompt": "...", "response": "..."}` gövdesine sahip bir olay yayınlanmalıdır.
        -   [ ] `StateFnSpeaking` başladığında, `call.tts.synthesis.started` tipinde bir olay yayınlanmalıdır.
        -   [ ] Bu olayların `cdr-service` tarafından yakalanıp `call_events` tablosuna yazıldığı doğrulanmalıdır.    

### **FAZ 4: Mimari Sağlamlaştırma ve Teknik Borç Ödeme (Yeniden Önceliklendirildi)**

-   [ ] **Görev ID: AGENT-REFACTOR-01 - Sorumlulukların Katmanlara Ayrılması (GÖZDEN GEÇİRİLDİ)**
    -   **Durum:** ⬜ Planlandı
    -   **Bulgular:** `internal/dialog/states.go` dosyası, hem diyalog akışını (durum makinesi) hem de harici servis iletişimlerini (medya oynatma, TTS/STT/LLM çağırma) yöneterek "Tek Sorumluluk Prensibi"ni ihlal etmektedir. Bu durum, kodun bakımını ve test edilebilirliğini zorlaştırmaktadır.
    -   **Çözüm Stratejisi:** Bu mantığı, "Akıllı Orkestratör" ve "Adaptör" katmanlarına ayıracağız.
    -   **Kabul Kriterleri:**
        -   [ ] `internal/orchestrator` adında yeni bir paket oluşturulmalı ve `RunDialogLoop` ile ana durum fonksiyonları (`StateFn...`) buraya taşınmalıdır. Bu katman, akışın "ne" yapılacağını yönetir.
        -   [ ] `internal/adapter` adında yeni bir paket oluşturulmalıdır.
        -   [ ] `playText`, `PlayAnnouncement`, `streamAndTranscribe` gibi medya ve AI ile ilgili tüm mantık, `adapter/media.go`, `adapter/ai.go` gibi dosyalara taşınmalıdır. Bu katman, işin "nasıl" yapılacağını yönetir.
        -   [ ] Refaktör sonrası `internal/orchestrator`'daki durum fonksiyonları, doğrudan gRPC/HTTP istemcilerini çağırmamalı, sadece `adapter` katmanındaki fonksiyonları çağırmalıdır.
        -   [ ] Mevcut uçtan uca diyalog testi, refaktör sonrası da başarıyla çalışmaya devam etmelidir.
    -   **Tahmini Süre:** ~2-3 gün