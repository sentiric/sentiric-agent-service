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
    -   **Problem Tanımı:** `agent-service`, `stt-service`'ten gelen "no_speech_timeout" durumunu doğru yönetemiyor, bunu bir anlama hatası olarak kabul edip hata sayacını artırıyor ve çağrıyı erken sonlandırıyordu.
    -   **Çözüm Stratejisi:** `StateFnListening` fonksiyonu, `streamAndTranscribe`'dan dönen `TranscriptionResult` nesnesini kontrol edecek şekilde güncellendi. Eğer `IsNoSpeechTimeout` alanı `true` ise, hata sayacı artırılmaz, bunun yerine "Sizi duyamıyorum" anonsu çalınır ve durum tekrar `StateListening`'e ayarlanarak kullanıcıya bir şans daha verilir. Bu, diyalog döngüsünün sonsuz bir hata döngüsüne girmesini engeller.
    -   **Kabul Kriterleri:**
        -   [x] Kullanıcı 10-15 saniye konuşmadığında, `agent-service` "Sizi duyamıyorum" anonsunu çalar.
        -   [x] Bu durumda `consecutive_failures` sayacı artırılmaz.
        -   [x] Çağrı, sadece kullanıcı gerçekten konuşup anlaşılamadığında sonlandırılır.

-   **Görev ID: AGENT-FEAT-01 - Dinamik TTS Ses Seçimi**
    -   **Durum:** ✅ **Tamamlandı**
    -   **Öncelik:** YÜKSEK
    -   **Problem Tanımı:** `dialplan`'de belirtilen `voice_selector` değeri (`tr-TR-EmelNeural`) `tts-gateway`'e iletilmiyordu ve varsayılan ses kullanılıyordu.
    -   **Çözüm Stratejisi:** `playText` fonksiyonu, `SynthesizeRequest` oluştururken `st.Event.Dialplan.Action.ActionData.Data` içinden `voice_selector` anahtarını okuyacak ve isteğe ekleyecek şekilde güncellendi.
    -   **Kabul Kriterleri:**
        -   [x] Test çağrısında, `dialplan`'de belirtilen `EmelNeural` sesinin duyulduğu doğrulandı.
        -   [x] `tts-gateway` loglarında, gelen isteğin `voice_selector` alanını içerdiği görüldü.

-   **Görev ID: AGENT-RAG-01 - `knowledge-service` Entegrasyonu**
    -   **Durum:** ⬜ **Planlandı**
    -   **Öncelik:** ORTA
    -   **Tahmini Süre:** ~1 Gün

---