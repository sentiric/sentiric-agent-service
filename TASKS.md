# 🧠 Sentiric Agent Service - Görev Listesi (v6.0 - Modüler Mimariye Geçiş)

Bu belge, agent-service'in geliştirme yol haritasını, tamamlanan görevleri ve mevcut öncelikleri tanımlar.

---
### **FAZ 1 & 2 (Tamamlandı)**

- [x] Temel Olay Orkestrasyonu, Dayanıklılık ve Veri Bütünlüğü görevleri başarıyla tamamlandı.

---

### **FAZ 3: Modüler Mimari ve Sürdürülebilirlik (Mevcut Odak)**

**Amaç:** Kod tabanını, gelecekteki büyümeye ve potansiyel mikroservis ayrıştırmalarına olanak tanıyacak şekilde, sorumlulukları net bir şekilde ayrılmış, test edilebilir ve sürdürülebilir modüler bir yapıya kavuşturmak.

-   **Görev ID: AGENT-REFACTOR-02 - Bütüncül Mimari Refactoring: Servis Katmanı Oluşturma (KRİTİK)**
    -   **Durum:** ⬜ **Yapılacak (Öncelik 1)**
    -   **Problem Tanımı:** Mevcut kod yapısında iş mantığı `dialog` ve `handler` paketleri arasında dağılmış durumda. Özellikle `dialog/states.go` dosyası, birden fazla sorumluluğu (state yönetimi, AI orkestrasyonu, medya işlemleri) üstlenerek gelecekteki geliştirmeleri zorlaştırmakta ve teknik borç oluşturmaktadır.
    -   **Çözüm Stratejisi:** Projenin iş mantığı, sorumlulukları net bir şekilde ayrılmış yeni bir `internal/service` katmanına taşınacaktır. Bu, Tek Sorumluluk Prensibi'ni uygulayarak kodun modülerliğini ve gelecekteki ayrıştırılabilirliğini artıracaktır.
    -   **Kabul Kriterleri:**
        -   [ ] **1. `service` Paketi Oluşturulacak:** `internal/service` adında yeni bir paket oluşturulacak.
        -   [ ] **2. İş Mantığı Yöneticilere Taşınacak:**
            -   [ ] `user_manager.go`: Misafir kullanıcı bulma/oluşturma mantığı `handler`'dan buraya taşınacak.
            -   [ ] `dialog_manager.go`: `dialog/flow.go` içindeki `RunDialogLoop` ve state'ler arası geçiş mantığı buraya taşınacak. `states.go` dosyası artık sadece state tanımlarını içerecek veya tamamen kaldırılacak.
            -   [ ] `ai_orchestrator.go`: STT, LLM, TTS ile ilgili tüm fonksiyonlar buraya taşınacak.
            -   [ ] `media_manager.go`: `PlayAnnouncement` ve `playText` gibi medya işlemleri buraya taşınacak.
            -   [ ] `template_provider.go`: Veritabanından şablon okuma mantığı buraya taşınacak.
        -   [ ] **3. `handler` Paketi Sadeleştirilecek:** `event_handler.go`, sadece RabbitMQ mesajını alıp deşifre etmek ve ilgili servise (örneğin `CallHandler`'a) yönlendirmekle sorumlu olacak. `call_handler.go` adında yeni bir dosya, çağrı ile ilgili olayları alıp doğru servis yöneticilerini (örneğin `UserManager`, `DialogManager`) tetikleyecek.
        -   [ ] **4. `app` Paketi Oluşturulacak:** `main.go` içindeki servis başlatma, bağımlılıkları oluşturma (dependency injection) ve graceful shutdown mantığı `internal/app/app.go` içine taşınarak `main.go` sadeleştirilecek.
        -   [ ] **5. Test Edilebilirlik Artacak:** Yeni servis yöneticileri, bağımlılıklarını interface'ler aracılığıyla alarak kolayca birim testine tabi tutulabilir hale gelecek.
        -   [ ] Refactoring sonrası mevcut tüm işlevsellik sorunsuz bir şekilde çalışmaya devam edecektir.

---

### **FAZ 4: Akıllı RAG ve Gelişmiş Görevler (Gelecek Vizyonu)**

-   [ ] **Görev ID: AGENT-RAG-01 - `knowledge-service` Entegrasyonu:**
    -   **Durum:** ⬜ **Planlandı**
    -   **Açıklama:** Kullanıcıdan gelen bilgi taleplerini, önce `knowledge-service`'e sorarak RAG bağlamı oluşturmak ve bu bağlamla zenginleştirilmiş prompt'u LLM'e göndermek. Bu görev, **FAZ 3**'teki refactoring tamamlandıktan sonra `AIOrchestrator` içinde çok daha temiz bir şekilde implemente edilecektir.