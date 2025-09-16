# 🧠 Sentiric Agent Service - Görev Listesi (v6.5 - Veri Bütünlüğü ve Dayanıklılık)

Bu belge, agent-service'in geliştirme yol haritasını, tamamlanan görevleri ve mevcut öncelikleri tanımlar.

---

### **FAZ 1: Veri Bütünlüğü ve Regresyon Düzeltmesi (Mevcut Odak)**

**Amaç:** Platformun temel diyalog akışını bozan kritik veri bütünlüğü sorunlarını gidermek.

-   **Görev ID: AGENT-FIX-01 - Zenginleştirilmiş `call.started` Olayını İşleme**
    -   **Durum:** x **Yapılacak (Öncelik 1 - KRİTİK)**
    -   **Bağımlılık:** `sentiric-sip-signaling-service`'teki `SIG-FIX-01` görevinin tamamlanmış olması.
    -   **Problem:** Loglar, servisin `call.started` olayından kullanıcı bilgisini alamadığını ve bu nedenle `user.identified.for_call` olayını yayınlayamadığını doğrulamaktadır. Bu, `cdr-service`'in çağrıyı doğru kullanıcıyla eşleştirmesini engeller.
    -   **Çözüm:**
        -   [x] `internal/service/dialog_manager.go` dosyasındaki `Start` metodunda veya ilgili bir başlangıç fonksiyonunda, `user.identified.for_call` olayını yayınlayan yeni bir mantık eklenmelidir.
        -   [x] Bu mantık, gelen `call.started` olayındaki `dialplan_resolution` alanının ve içindeki `matchedUser` ile `matchedContact` alanlarının varlığını kontrol etmelidir.
        -   [x] Eğer bu alanlar doluysa, `user.identified.for_call` olayını doğru `userID`, `contactID` ve `tenantID` ile RabbitMQ'ya yayınlamalıdır.
        -   [x] Düzeltme sonrası yapılan test aramasında, `WRN Kullanıcı veya contact bilgisi eksik...` uyarısının **görülmediği** ve `user.identified.for_call` olayının yayınlandığı loglardan doğrulanmalıdır.

-   **Görev ID: AGENT-CLEANUP-01 - Kullanılmayan `call.answered` Olay İşleyicisini Kaldır**
    -   **Durum:** x **Yapılacak (Öncelik 2 - DÜŞÜK)**
    -   **Bağımlılık:** `sentiric-sip-signaling-service`'teki `SIG-CLEANUP-01` görevinin tamamlanmış olması.
    -   **Problem:** Loglarda, servisin `call.answered` olayını "Bilinmeyen olay türü" mesajıyla işlediği görülmektedir. Bu kod bloğu gereksizdir.
    -   **Çözüm:**
        -   [x] `internal/handler/event_handler.go` dosyasındaki `HandleRabbitMQMessage` fonksiyonundan `case constants.EventTypeCallAnswered:` bloğu tamamen kaldırılmalıdır.

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