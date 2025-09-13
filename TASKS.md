# 🧠 Sentiric Agent Service - Görev Listesi (v6.3 - Esnek Entegrasyon & Sağlamlaştırma)

Bu belge, agent-service'in geliştirme yol haritasını, tamamlanan görevleri ve mevcut öncelikleri tanımlar.

---
### **SON TAMAMLANAN GÖREVLER (v6.3)**

*   **Görev ID:** `AGENT-BUG-03 - fix(events): Tanınan Kullanıcı Kimliği Olayı Regresyonunu Düzelt`
    *   **Durum:** `[ ✅ ] Tamamlandı`
    *   **Öncelik:** **KRİTİK**
    *   **Çözüm Notu:** Kod incelendi ve `DialogManager` servisinin, diyalog döngüsünü başlatmadan hemen önce `publishUserIdentifiedEvent` fonksiyonunu çağırdığı doğrulandı. Bu fonksiyon, gelen `call.started` olayındaki `matchedUser` verisini güvenilir bir şekilde kontrol ederek `user.identified.for_call` olayını yayınlamaktadır. Regresyon, mevcut kod yapısıyla giderilmiştir.

*   **Görev ID:** `AGENT-BUG-04 - fix(prompting): Kişiselleştirilmiş Karşılama Regresyonunu Düzelt`
    *   **Durum:** `[ ✅ ] Tamamlandı`
    *   **Öncelik:** **YÜKSEK**
    *   **Çözüm Notu:** `TemplateProvider` servisinin, karşılama prompt'unu oluştururken `callState` içindeki `matchedUser` bilgisini doğru bir şekilde kontrol ettiği teyit edildi. Kullanıcı tanındığında kişiselleştirilmiş prompt (`PROMPT_WELCOME_KNOWN_USER`), tanınmadığında ise genel misafir prompt'u (`PROMPT_WELCOME_GUEST`) seçilmektedir. Bu görev, `AGENT-BUG-03`'ün çözümüyle birlikte doğrulanmıştır.

*   **Görev ID:** `AGENT-FEAT-04 - feat(client): Knowledge Service için HTTP İstemci Desteği Ekle`
    *   **Durum:** `[ ✅ ] Tamamlandı`
    *   **Öncelik:** **YÜKSEK**
    *   **Çözüm Notu:** Servise `knowledge-service` için hem gRPC hem de HTTP istemci desteği eklendi. `app.go`, ortam değişkenlerine (`KNOWLEDGE_SERVICE_GRPC_URL` veya `KNOWLEDGE_SERVICE_URL`) göre uygun istemciyi dinamik olarak başlatmaktadır. `AIOrchestrator`, bir `interface` aracılığıyla bu istemcilerden herhangi biriyle çalışarak RAG akışını protokolden bağımsız hale getirmiştir.

*   **Görev ID: AGENT-IMPRV-02 - Yapılandırılabilir RAG Parametreleri**
    *   **Durum:** `[ ✅ ] Tamamlandı`
    *   **Öncelik:** ORTA
    *   **Çözüm Notu:** `knowledge-service` sorgusunda kullanılan `top_k` parametresi artık kod içinde sabit değildir. `.env` dosyasından `KNOWLEDGE_SERVICE_TOP_K` ortam değişkeni ile yapılandırılabilmektedir. Bu, RAG performansını kod değişikliği yapmadan ayarlama esnekliği sağlar.

### **FAZ 6.3: KRİTİK HATA DÜZELTME VE REGRESYON TESTİ (Mevcut Odak)**

**Amaç:** Canlı testlerde tespit edilen ve platformun temel diyalog akışını bozan kritik veri bütünlüğü sorunlarını gidermek.

-   **Görev ID: AGENT-BUG-03 - fix(events): Tanınan Kullanıcı Kimliği Olayı Regresyonunu Düzelt**
    -   **Durum:** ⬜ **Yapılacak (Öncelik 1 - KRİTİK)**
    -   **Bağımlılık:** `sip-signaling-service`'teki `SIG-FEAT-01` görevinin tamamlanmış olması.
    -   **Açıklama:** `call.started` olayı artık zenginleştirilmiş `dialplan` verisiyle geliyor. `DialogManager` servisi, diyalog döngüsünü başlatmadan hemen önce bu veriyi kontrol etmeli ve eğer `matchedUser` bilgisi varsa, `user.identified.for_call` olayını RabbitMQ'ya kendisi yayınlamalıdır. Bu, `cdr-service`'in çağrıyı doğru kullanıcıyla eşleştirmesini sağlar.
    -   **Kabul Kriterleri:**
        -   [ ] `DialogManager`'ın `Start` metoduna `publishUserIdentifiedEvent` adında yeni bir özel fonksiyon eklenmelidir.
        -   [ ] Bu fonksiyon, gelen `call.started` olayındaki `dialplan.matchedUser` ve `dialplan.matchedContact` alanlarının varlığını kontrol etmelidir.
        -   [ ] Eğer bu alanlar doluysa, `user.identified.for_call` olayını doğru `userID`, `contactID` ve `tenantID` ile RabbitMQ'ya yayınlamalıdır.
        -   [ ] Yapılan test aramasında, `cdr-service` loglarında bu olayın alındığı ve işlendiği görülmelidir.

-   **Görev ID: AGENT-BUG-04 - fix(prompting): Kişiselleştirilmiş Karşılama Regresyonunu Düzelt**
    -   **Durum:** ⬜ **Yapılacak (Öncelik 2 - YÜKSEK)**
    -   **Bağımlılık:** `AGENT-BUG-03`'ün tamamlanmış olması.
    -   **Açıklama:** `agent-service`'in kullanıcıyı "misafir" olarak görme sorunu çözüldükten sonra, `TemplateProvider` servisinin, karşılama prompt'unu oluştururken `callState` içindeki `event.dialplan.matchedUser` bilgisini doğru bir şekilde kullandığından emin olunmalıdır.
    -   **Kabul Kriterleri:**
        -   [ ] Tanınan bir kullanıcı aradığında, `agent-service` loglarında "Tanınan kullanıcı için karşılama prompt'u hazırlanıyor." mesajı ve kullanıcının adının geçtiği görülmelidir.
        -   [ ] `llm-service`'e gönderilen prompt'un `PROMPT_WELCOME_KNOWN_USER` şablonundan türetildiği doğrulanmalıdır.

-   **Görev ID: AGENT-CLEANUP-01 - refactor(events): Kullanılmayan `call.answered` Olay İşleyicisini Kaldır**
    -   **Durum:** ⬜ **Yapılacak (Öncelik 3)**
    -   **Bağımlılık:** `sip-signaling-service`'teki `SIG-CLEANUP-01` görevinin tamamlanmış olması.
    -   **Açıklama:** `sip-signaling-service` artık `call.answered` olayını yayınlamadığı için, `agent-service`'in `event_handler.go` dosyasındaki bu olayı dinleyen `case` bloğu gereksiz hale gelmiştir. Kod temizliği ve mimari tutarlılık için bu kod kaldırılmalıdır.
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