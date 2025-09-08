# 🧠 Sentiric Agent Service - Görev Listesi (v6.1 - RAG Entegrasyonu)

Bu belge, agent-service'in geliştirme yol haritasını, tamamlanan görevleri ve mevcut öncelikleri tanımlar.

---
### **FAZ 1, 2 & 3 (Tamamlandı)**

- [x] Temel Olay Orkestrasyonu, Dayanıklılık, Veri Bütünlüğü ve Modüler Mimari görevleri başarıyla tamamlandı.

---

### **FAZ 4: Akıllı RAG Entegrasyonu (Mevcut Odak)**

**Amaç:** Agent'a, LLM'e gitmeden önce `knowledge-service` üzerinden kurumsal bilgi tabanını sorgulama yeteneği kazandırarak, daha doğru, bağlamsal ve güvenilir yanıtlar üretmesini sağlamak.

-   **Görev ID: AGENT-RAG-01 - `knowledge-service` Entegrasyonu (KRİTİK)**
    -   **Durum:** ⬜ **Yapılacak (Öncelik 1)**
    -   **Açıklama:** Kullanıcıdan gelen bilgi taleplerini, önce `knowledge-service`'e sorarak RAG bağlamı oluşturmak ve bu bağlamla zenginleştirilmiş prompt'u LLM'e göndermek.
    -   **Kabul Kriterleri:**
        -   [ ] `internal/client` paketi içine `knowledge-service` için yeni bir gRPC istemcisi eklenmelidir.
        -   [ ] `AIOrchestrator`, `knowledge-service`'i sorgulamak için yeni bir `QueryKnowledgeBase` metoduna sahip olmalıdır.
        -   [ ] `TemplateProvider`, RAG sonuçlarını işlemek için `PROMPT_SYSTEM_RAG` adında yeni bir şablonu veritabanından okuyabilmeli ve zenginleştirilmiş prompt'u oluşturabilmelidir.
        -   [ ] `DialogManager` içindeki `stateFnThinking` metodu, LLM'i çağırmadan önce `AIOrchestrator` aracılığıyla `knowledge-service`'i sorgulamalıdır.
        -   [ ] Eğer `knowledge-service`'ten anlamlı bir sonuç dönerse, LLM'e RAG şablonu ile zenginleştirilmiş prompt gönderilmelidir.
        -   [ ] Eğer `knowledge-service` hata dönerse veya sonuç bulamazsa, diyalog kesilmemeli, standart (non-RAG) prompt ile devam etmelidir (graceful degradation).

---