# 🧠 Sentiric Agent Service - Görev Listesi (v5.6 - Diyalog Stabilizasyonu)

Bu belge, agent-service'in canlı testlerde tespit edilen kritik hatalarını gidermek ve platformun temel diyalog akışını kararlı hale getirmek için gereken görevleri tanımlar.

---

### **FAZ 1: Temel Orkestrasyon (Tamamlanmış Görevler)**
*   [x] **AGENT-CORE-01**: Olay Tüketimi ve Servis İstemcileri
*   [x] **AGENT-CORE-02**: Misafir Kullanıcı Oluşturma
*   [x] **AGENT-CORE-03**: Temel Durum Makinesi
*   [x] **AGENT-CORE-04**: Anında Sesli Geri Bildirim
*   [x] **AGENT-CORE-05**: Yarış Durumuna Karşı Dayanıklılık
*   [x] **AGENT-BUG-01**: Çağrı Kaydı Tenant ID Düzeltmesi
*   [x] **AGENT-BUG-06**: Veritabanı Bütünlüğü ve Misafir Kullanıcı Oluşturma Hatası
*   [x] **AGENT-BUG-04**: `user.identified.for_call` Olayını Yayınlama
*   [x] **AGENT-BUG-03**: `playText` Fonksiyonunda Kapsamlı Nil Pointer Koruması

---

### **FAZ 2: Akıllı Diyalog ve Veri Zenginleştirme (Mevcut Odak)**

**Amaç:** Kırık diyalog döngüsünü onarmak, platformu daha zeki hale getirmek ve mimariyi iyileştirmek.

-   **Görev ID: AGENT-BUG-07 - STT Sessizlik/Timeout Durumunu Yönetme**
    -   **Durum:** ✅ **Tamamlandı**
    -   **Öncelik:** **KRİTİK**
    -   **Stratejik Önem:** Bu hata, tüm diyalog akışının kopmasının ve çağrının "Sizi anlayamadım" diyerek sonlanmasının **kök nedenidir.** Bu çözülmeden platform işlevsel olamaz.
    -   **Problem Tanımı:** `agent-service`, `stt-service`'e ses akışını başlattıktan sonra, kullanıcı konuşmasa veya `stt-service`'ten hiç yanıt gelmese bile sonsuza dek bekliyor. Sonunda `streamAndTranscribe` fonksiyonu boş metin döndürüyor ve `StateFnListening` içindeki hata sayacı tetiklenerek çağrı sonlandırılıyor.
    -   **Çözüm Stratejisi:** `StateFnListening` fonksiyonu, STT'den veri gelmemesi durumuna karşı daha dayanıklı hale getirilmelidir. `streamAndTranscribe` fonksiyonu, sadece metin değil, aynı zamanda "no_speech_detected" veya "timeout" gibi durumları da döndürebilmelidir.
    -   **Bağımlılıklar:** `STT-BUG-01` (STT servisinin timeout/sessizlik durumu bildirmesi)
    -   **Kabul Kriterleri:**
        -   [x] Kullanıcı 15-20 saniye boyunca hiç konuşmadığında, `agent-service` loglarında "STT'den ses algılanmadı/zaman aşımı" gibi bir uyarı görülmelidir.
        -   [x] Bu durumda, `ANNOUNCE_SYSTEM_CANT_HEAR_YOU` ("Sizi duyamıyorum") anonsu çalınmalı ve servis tekrar `StateListening` durumuna dönerek kullanıcıya bir şans daha vermelidir.
        -   [x] Çağrı, `ANNOUNCE_SYSTEM_MAX_FAILURES` anonsuyla sonlandırılmamalıdır (kullanıcı hiç konuşmadığı sürece).
    -   **Tahmini Süre:** ~4-6 Saat

-   **Görev ID: AGENT-FEAT-01 - Dinamik TTS Ses Seçimi**
    -   **Durum:** ✅ **Tamamlandı**
    -   **Öncelik:** YÜKSEK
    -   **Stratejik Önem:** Platformun, her kiracı veya senaryo için farklı ses kimlikleri sunabilmesini sağlar. Ürün esnekliği için önemlidir.
    -   **Problem Tanımı:** Test çağrısında, `dialplan`'de `tr-TR-EmelNeural` sesi tanımlı olmasına rağmen, varsayılan `edge-tts` sesi duyulmaktadır. Loglar, `agent-service`'in `voice_selector` bilgisini `tts-gateway`'e göndermediğini doğrulamaktadır.
    -   **Çözüm Stratejisi:** `playText` fonksiyonu, `SynthesizeRequest` oluştururken, `st.Event.Dialplan.Action.ActionData.Data` içinden `voice_selector` anahtarını okumalı ve isteğe eklemelidir.
    -   **Kabul Kriterleri:**
        -   [x] `tts-gateway` loglarında, gelen isteğin `voice_selector` alanının doğru değeri (`tr-TR-EmelNeural`) içerdiği görülmelidir.
        -   [x] Test çağrısının ses kaydı dinlendiğinde, duyulan sesin `EmelNeural` olduğu doğrulanmalıdır.
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

---

### **FAZ 3: Mimarî İyileştirme (Teknik Borç)**

-   **Görev ID: AGENT-REFACTOR-01 - Sorumlulukların Katmanlara Ayrılması**
    -   **Durum:** ⬜ **Planlandı**
    -   **Öncelik:** DÜŞÜK
    -   **Stratejik Önem:** Kodun test edilebilirliğini ve bakımını kolaylaştırarak uzun vadeli proje sağlığını güvence altına alır.
    -   **Tahmini Süre:** ~2-3 Gün

-   **Görev ID: AGENT-REFACTOR-02 - HTTP İstemci Soyutlaması**
    -   **Durum:** ⬜ **Planlandı**
    -   **Öncelik:** DÜŞÜK
    -   **Açıklama:** `STTClient` ve `LLMClient` için ortak bir yapı oluşturarak, tekrar eden `http.NewRequestWithContext`, `httpClient.Do` ve hata yönetimi mantığını merkezileştirmek.
    -   **Tahmini Süre:** ~3-4 Saat

### **FAZ 4: Gerçek Zamanlı ve Akıllı Diyalog (Gelecek Vizyonu)**

-   **Görev ID: AGENT-STREAM-01 - Tam Çift Yönlü (Full-Duplex) Diyalog Modu**
    -   **Durum:** ⬜ **Planlandı**
    -   **Öncelik:** DÜŞÜK
    -   **Stratejik Önem:** Google Live API gibi yeni nesil, tek akışlı (single-stream) yapay zeka modelleriyle entegrasyonu sağlar. Bu, platformun en son teknolojileri kullanabilmesi için kritik bir yetenektir.
    -   **Açıklama:** Mevcut "Durum Makinesi" döngüsüne bir alternatif olarak, `media-service`'ten gelen ses akışını doğrudan bir "Canlı Konuşma" modeline (örn: Google Live API) yönlendiren ve modelden gelen ses akışını da doğrudan `media-service`'e geri gönderen yeni bir `StreamHandler` implemente etmek.
    -   **Bağımlılık:** `STT-ADAPT-01` görevinin tamamlanması ve Google API entegrasyon tecrübesi.    