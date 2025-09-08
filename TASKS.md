# 🧠 Sentiric Agent Service - Görev Listesi (v6.2 - RAG Sonrası Vizyon)

Bu belge, agent-service'in geliştirme yol haritasını, tamamlanan görevleri ve mevcut öncelikleri tanımlar.

---
### **FAZ 1, 2 & 3 (Tamamlandı)**

- [x] Temel Olay Orkestrasyonu, Dayanıklılık, Veri Bütünlüğü ve Modüler Mimari görevleri başarıyla tamamlandı.

---

### **FAZ 4: Akıllı RAG Entegrasyonu (Tamamlandı)**

**Amaç:** Agent'a, LLM'e gitmeden önce `knowledge-service` üzerinden kurumsal bilgi tabanını sorgulama yeteneği kazandırarak, daha doğru, bağlamsal ve güvenilir yanıtlar üretmesini sağlamak.

-   [x] **Görev ID: AGENT-RAG-01 - `knowledge-service` Entegrasyonu**

---

### **FAZ 5: Gelişmiş Diyalog Yönetimi ve Verimlilik (Mevcut Odak)**

**Amaç:** Agent'ın diyalog yeteneklerini insan benzeri bir seviyeye taşımak, gereksiz kaynak kullanımını önlemek ve sistemi daha yapılandırılabilir hale getirmek.

-   **Görev ID: AGENT-FEAT-02 - Niyet Tanıma ve RAG Tetikleme**
    -   **Durum:** ⬜ **Yapılacak (Öncelik 1)**
    -   **Açıklama:** Şu anda her kullanıcı mesajında `knowledge-service` sorgulanmaktadır. Bu, "merhaba" gibi basit ifadeler için gereksizdir. Kullanıcının niyetinin bir "bilgi talebi" mi yoksa "selamlaşma/kapanış" mı olduğunu tespit eden bir mekanizma eklenmelidir.
    -   **Kabul Kriterleri:**
        -   [ ] `AIOrchestrator` içinde, kullanıcının son mesajını analiz eden bir `DetectIntent` metodu oluşturulmalıdır.
        -   [ ] Bu metot, basit anahtar kelime analizi veya küçük bir LLM çağrısı ile ` bilgi_talebi`, `selamlasma`, `kapanis` gibi niyetleri belirlemelidir.
        -   [ ] `DialogManager`'ın `stateFnThinking` adımı, sadece niyet `bilgi_talebi` olduğunda RAG akışını tetiklemelidir.
        -   [ ] Diğer niyetler için standart, geçmişe dayalı sohbet yanıtları üretilmelidir. Bu, gereksiz `knowledge-service` sorgularını önleyecektir.

-   **Görev ID: AGENT-FEAT-03 - Diyalog Sonlandırma Yeteneği**
    -   **Durum:** ⬜ **Planlandı**
    -   **Açıklama:** Kullanıcı "görüşmeyi bitir", "kapat", "teşekkürler, yeterli" gibi ifadeler kullandığında agent'ın bunu anlayıp diyaloğu sonlandırması gerekir.
    -   **Kabul Kriterleri:**
        -   [ ] `DetectIntent` metodu, `kapanis` niyetini de tanıyabilmelidir.
        -   [ ] `DialogManager`, `kapanis` niyeti algılandığında, uygun bir veda anonsu (`ANNOUNCE_SYSTEM_GOODBYE`) çaldıktan sonra çağrı durumunu `StateTerminated` olarak ayarlamalıdır.

-   **Görev ID: AGENT-IMPRV-02 - Yapılandırılabilir RAG Parametreleri**
    -   **Durum:** ⬜ **Planlandı**
    -   **Açıklama:** `knowledge-service` sorgusunda kullanılan `top_k=3` gibi parametreler kod içinde sabitlenmiştir. Bu, esnekliği azaltır.
    -   **Kabul Kriterleri:**
        -   [ ] `.env` dosyasına `KNOWLEDGE_SERVICE_TOP_K` gibi yeni ortam değişkenleri eklenmelidir.
        -   [ ] `config.go` bu değişkenleri okumalıdır.
        -   [ ] `AIOrchestrator`, bu parametreleri yapılandırmadan alarak `knowledge-service`'i sorgulamalıdır.
        