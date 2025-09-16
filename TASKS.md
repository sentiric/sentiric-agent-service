# 🧠 Sentiric Agent Service - Görev Listesi (v6.5 - Veri Bütünlüğü ve Dayanıklılık)

Bu belge, agent-service'in geliştirme yol haritasını, tamamlanan görevleri ve mevcut öncelikleri tanımlar.

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