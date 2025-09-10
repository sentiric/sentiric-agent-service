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