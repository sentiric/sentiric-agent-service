# 🧠 Sentiric Agent Service - Görev Listesi (v6.4 - Veri Bütünlüğü ve Dayanıklılık)

Bu belge, agent-service'in geliştirme yol haritasını, tamamlanan görevleri ve mevcut öncelikleri tanımlar.

---
### **FAZ 6.3: KRİTİK HATA DÜZELTME VE REGRESYON TESTİ (Mevcut Odak)**

**Amaç:** Canlı test loglarında tespit edilen ve platformun temel diyalog akışını bozan kritik veri bütünlüğü sorunlarını gidermek.

-   **Görev ID: AGENT-BUG-03 - fix(events): Tanınan Kullanıcı Kimliği Olayı Regresyonunu Düzelt**
    -   **Durum:** ⬜ **Yapılacak (Öncelik 1 - KRİTİK)**
    -   **Bağımlılık:** `sentiric-sip-signaling-service`'teki `SIG-FEAT-01` görevinin tamamlanmış olması.
    -   **Açıklama:** Loglar, `call.started` olayının zenginleştirilmiş `dialplan` verisiyle gelmediğini ve bu nedenle servisin `WRN Kullanıcı veya contact bilgisi eksik olduğu için user.identified.for_call olayı yayınlanamadı.` hatasını verdiğini doğrulamaktadır. Bu, `cdr-service`'in çağrıyı doğru kullanıcıyla eşleştirmesini engellemektedir. Bu görev, zenginleştirilmiş `call.started` olayını doğru işlemeyi hedefler.
    -   **Kabul Kriterleri:**
        -   [ ] `DialogManager`'ın `Start` metoduna `publishUserIdentifiedEvent` adında yeni bir özel fonksiyon eklenmelidir.
        -   [ ] Bu fonksiyon, gelen `call.started` olayındaki `dialplan.matchedUser` ve `dialplan.matchedContact` alanlarının varlığını kontrol etmelidir.
        -   [ ] Eğer bu alanlar doluysa, `user.identified.for_call` olayını doğru `userID`, `contactID` ve `tenantID` ile RabbitMQ'ya yayınlamalıdır.
        -   [ ] Düzeltme sonrası yapılan test aramasında, loglarda artık bu uyarı mesajının **görülmediği** ve `user.identified.for_call` olayının yayınlandığı görülmelidir.

-   **Görev ID: AGENT-BUG-04 - fix(prompting): Kişiselleştirilmiş Karşılama Regresyonunu Düzelt**
    -   **Durum:** ⬜ **Yapılacak (Öncelik 2 - YÜKSEK)**
    -   **Bağımlılık:** `AGENT-BUG-03`'ün tamamlanmış olması.
    -   **Açıklama:** `agent-service`'in kullanıcıyı tanıyamama sorunu çözüldükten sonra, loglarda `Misafir kullanıcı için karşılama prompt'u hazırlanıyor.` mesajı yerine tanınan kullanıcı için doğru prompt'un hazırlandığı görülmelidir.
    -   **Kabul Kriterleri:**
        -   [ ] Tanınan bir kullanıcı aradığında, `agent-service` loglarında "Tanınan kullanıcı için karşılama prompt'u hazırlanıyor." mesajı ve kullanıcının adının geçtiği görülmelidir.
        -   [ ] `llm-service`'e gönderilen prompt'un `PROMPT_WELCOME_KNOWN_USER` şablonundan türetildiği doğrulanmalıdır.

-   **Görev ID: AGENT-CLEANUP-01 - refactor(events): Kullanılmayan `call.answered` Olay İşleyicisini Kaldır**
    -   **Durum:** ⬜ **Yapılacak (Öncelik 3)**
    -   **Bağımlılık:** `sentiric-sip-signaling-service`'teki `SIG-CLEANUP-01` görevinin tamamlanmış olması.
    -   **Açıklama:** Loglarda, servisin `call.answered` olayını `Bilinmeyen olay türü, görmezden geliniyor.` mesajıyla işlediği görülmektedir. Bu kod bloğu gereksizdir ve kaldırılmalıdır.
    -   **Kabul Kriterleri:**
        -   [ ] `internal/handler/event_handler.go` dosyasındaki `HandleRabbitMQMessage` fonksiyonundan `case constants.EventTypeCallAnswered:` bloğu tamamen kaldırılmalıdır.
---
### **GELECEK FAZLAR: Gelişmiş Diyalog Yönetimi**

**Amaç:** Agent'ın diyalog yeteneklerini insan benzeri bir seviyeye taşımak, gereksiz kaynak kullanımını önlemek ve sistemi daha yapılandırılabilir hale getirmek.

-   **Görev ID: AGENT-FEAT-02 - Niyet Tanıma ve Akıllı RAG Tetikleme**
    -   **Durum:** ⬜ **Yapılacak (Öncelik 1)**
    -   **Açıklama:** Şu anda her kullanıcı mesajında `knowledge-service` sorgulanmaktadır. Bu, "merhaba" gibi basit ifadeler için gereksizdir. Kullanıcının niyetinin bir "bilgi talebi" mi yoksa "selamlaşma/kapanış" mı olduğunu tespit eden bir mekanizma eklenmelidir.
    -   **Kabul Kriterleri:**
        -   [ ] `AIOrchestrator` içinde, kullanıcının son mesajını analiz eden bir `DetectIntent` metodu oluşturulmalıdır.
        -   [ ] `DialogManager`'ın `stateFnThinking` adımı, sadece niyet `bilgi_talebi` olduğunda RAG akışını tetiklemelidir.

-   **Görev ID: AGENT-FEAT-03 - Diyalog Sonlandırma Yeteneği**
    -   **Durum:** ⬜ **Planlandı**
    -   **Açıklama:** Kullanıcı "görüşmeyi bitir", "kapat", "teşekkürler, yeterli" gibi ifadeler kullandığında agent'ın bunu anlayıp diyaloğu sonlandırması gerekir.
    -   **Kabul Kriterleri:**
        -   [ ] `DetectIntent` metodu, `kapanis` niyetini de tanıyabilmelidir.
        -   [ ] `DialogManager`, `kapanis` niyeti algılandığında, uygun bir veda anonsu (`ANNOUNCE_SYSTEM_GOODBYE`) çaldıktan sonra çağrı durumunu `StateTerminated` olarak ayarlamalıdır.