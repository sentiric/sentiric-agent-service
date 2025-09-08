# 🧠 Sentiric Agent Service - Görev Listesi (v5.8 - Bütünlük ve Sağlamlaştırma)

Bu belge, agent-service'in geliştirme yol haritasını, tamamlanan görevleri ve mevcut öncelikleri tanımlar.

---

### **FAZ 1: Temel Olay Orkestrasyonu (Tamamlandı)**

**Amaç:** Gelen çağrı olaylarını işleyerek temel diyalog adımlarını ve medya eylemlerini yöneten çekirdek altyapıyı kurmak.

-   [x] **Görev ID: AGENT-CORE-01 - Olay Tüketimi ve Servis İstemcileri:** RabbitMQ'dan `call.started` gibi olayları dinler ve diğer servislere (media, user, tts, llm) gRPC/HTTP ile bağlanır.
-   [x] **Görev ID: AGENT-CORE-02 - Misafir Kullanıcı Akışı:** `PROCESS_GUEST_CALL` eylemi geldiğinde `user-service`'i çağırarak yeni bir misafir kullanıcı oluşturur.
-   [x] **Görev ID: AGENT-CORE-03 - Temel Durum Makinesi:** Redis üzerinde çağrı durumunu (`CallState`) yönetir ve `WELCOMING`, `LISTENING` gibi temel durumlar arasında geçiş yapar.
-   [x] **Görev ID: AGENT-BUG-07 - STT Sessizlik/Timeout Yönetimi:** `stt-service`'ten gelen "no_speech_timeout" durumunu doğru yöneterek "Sizi duyamıyorum" anonsu çalar ve hata sayacını artırmaz.
-   [x] **Görev ID: AGENT-FEAT-01 - Dinamik TTS Ses Seçimi:** `dialplan`'de belirtilen `voice_selector` değerini `tts-gateway`'e ileterek doğru sesin kullanılmasını sağlar.

---

### **FAZ 2: Dayanıklılık ve Veri Bütünlüğü (Mevcut Odak)**

**Amaç:** Canlı testlerde tespit edilen kritik hataları gidererek platformun temel diyalog akışını kararlı hale getirmek ve raporlama için veri bütünlüğünü sağlamak.

-   **Görev ID: AGENT-BUG-04 - `user.identified.for_call` Olayını Yayınlama (KRİTİK)**
    -   **Durum:** ⬜ **Yapılacak (Öncelik 1)**
    -   **Problem Tanımı:** Veritabanı kayıtları, `calls` tablosundaki `user_id` ve `tenant_id` alanlarının `NULL` olduğunu gösteriyor. Bu, `agent-service`'in, bir kullanıcıyı tanımladıktan sonra bu bilgiyi `user.identified.for_call` olayı ile platforma (özellikle `cdr-service`'e) duyurma görevini yerine getirmediğini kanıtlamaktadır. Bu, temel raporlama için kritik bir hatadır.
    -   **Kabul Kriterleri:**
        -   [ ] `handleProcessGuestCall` ve `handleStartAIConversation` fonksiyonlarının içinde, `event.Dialplan.MatchedUser` ve `MatchedContact` bilgileri mevcut olduğunda, bu bilgilerle dolu bir `user.identified.for_call` olayı oluşturulmalı ve RabbitMQ üzerinden yayınlanmalıdır.
        -   [ ] Bir çağrı başladığında, `agent-service` loglarında "user.identified.for_call olayı yayınlanıyor..." mesajı görülmelidir.
        -   [ ] Bu değişiklik sonrası `cdr-service`'in `calls` tablosunu doğru bir şekilde güncellediği doğrulanmalıdır.

-   **Görev ID: AGENT-BUG-08 - STT Halüsinasyonlarına Karşı Savunma (KRİTİK)**
    -   **Durum:** ⬜ **Yapılacak (Öncelik 2)**
    -   **Problem Tanımı:** Canlı testler, `stt-service`'in anlamsız gürültüleri "Bu dizinin betimlemesi..." gibi alakasız metinler olarak yorumladığını göstermiştir. `agent-service` bu hatalı metni doğru kabul ederek anlamsız AI yanıtları üretmekte ve diyalog akışını bozmaktadır.
    -   **Çözüm Stratejisi:** `StateFnListening` fonksiyonunda, STT'den dönen metin üzerinde basit bir "anlamlılık kontrolü" (sanity check) yapılmalıdır.
    -   **Kabul Kriterleri:**
        -   [ ] STT'den dönen metin, çok kısa (örn: 3 karakterden az) veya bilinen anlamsız kalıplar içeriyorsa, bu bir anlama hatası olarak kabul edilmelidir.
        -   [ ] Bu durumda metin LLM'e gönderilmemeli; bunun yerine `ANNOUNCE_SYSTEM_CANT_UNDERSTAND` anonsu çalınmalı ve `consecutive_failures` sayacı artırılmalıdır.
        -   [ ] Bu, LLM'in hatalı verilerle beslenmesini engelleyecektir.

---

### **FAZ 2.5: İyileştirme ve Sağlamlaştırma (Yeni Görevler)**

**Amaç:** Kod kalitesini, yapılandırılabilirliği ve gözlemlenebilirliği artırarak servisin uzun vadeli bakımını kolaylaştırmak.

-   **Görev ID: AGENT-IMPRV-01 - Yapılandırmanın İyileştirilmesi (Hardcoded Değerler)**
    -   **Durum:** ⬜ **Yapılacak**
    -   **Problem Tanımı:** Kod içinde `ConsecutiveFailures` (ardışık hata) limiti (`2`) ve ses klonlama için izin verilen `allowedSpeakerDomains` gibi kritik değerler sabit olarak yazılmıştır. Bu, esnekliği azaltır.
    -   **Kabul Kriterleri:**
        -   [ ] `AGENT_MAX_CONSECUTIVE_FAILURES` adında yeni bir ortam değişkeni oluşturulmalı ve `StateFnListening` fonksiyonunda bu değer kullanılmalıdır.
        -   [ ] `AGENT_ALLOWED_SPEAKER_DOMAINS` adında, virgülle ayrılmış domain listesi içeren (örn: "domain1.com,domain2.com") bir ortam değişkeni oluşturulmalı ve `isAllowedSpeakerURL` fonksiyonu bu listeyi kullanacak şekilde güncellenmelidir.
        -   [ ] `.env.docker` dosyasına bu yeni değişkenler için varsayılan değerler eklenmelidir.

-   **Görev ID: AGENT-REFACTOR-01 - Gözlemlenebilirliğin Artırılması (Context-Aware Logging)**
    -   **Durum:** ⬜ **Yapılacak**
    -   **Problem Tanımı:** `dialog` paketi içindeki loglamalarda `call_id` ve `trace_id` gibi bağlamsal bilgiler her seferinde manuel olarak logger'a eklenmekte, bu da kod tekrarına ve potansiyel unutkanlıklara yol açmaktadır.
    -   **Kabul Kriterleri:**
        -   [ ] `event_handler.go` içinde, bir olay işlenmeye başlandığında, `zerolog.Logger` nesnesi `call_id` ve `trace_id` ile zenginleştirilmelidir.
        -   [ ] Bu zenginleştirilmiş logger, `context.Context` aracılığıyla `RunDialogLoop` ve diğer alt fonksiyonlara aktarılmalıdır.
        -   [ ] Alt fonksiyonlar, logger'ı doğrudan context'ten almalı, böylece her log mesajı otomatik olarak doğru bağlama sahip olur.

-   **Görev ID: AGENT-BUG-09 - Graceful Shutdown Kapsamının Genişletilmesi**
    -   **Durum:** ⬜ **Yapılacak**
    -   **Problem Tanımı:** Mevcut `Graceful Shutdown` mekanizması, `call.started` olayı ile başlatılan ve uzun sürebilen diyalog Go rutinlerinin tamamlanmasını beklemeden servisi sonlandırabilir.
    -   **Kabul Kriterleri:**
        -   [ ] Aktif diyalog Go rutinlerini takip etmek için merkezi bir `sync.WaitGroup` veya benzeri bir mekanizma oluşturulmalıdır.
        -   [ ] Her yeni diyalog (`go dialog.RunDialogLoop...`) başladığında bu `WaitGroup`'e eklenmeli ve bittiğinde çıkarılmalıdır.
        -   [ ] `main.go`'daki kapatma bloğu, RabbitMQ tüketicisine ek olarak bu diyalog `WaitGroup`'inin de tamamlanmasını beklemelidir. Bu, devam eden çağrıların aniden kesilmesini önleyecektir.

---

### **FAZ 3: Akıllı RAG ve Gelişmiş Görevler (Gelecek Vizyonu)**

-   [ ] **Görev ID: AGENT-RAG-01 - `knowledge-service` Entegrasyonu:**
    -   **Durum:** ⬜ **Planlandı**
    -   **Açıklama:** Kullanıcıdan gelen bilgi taleplerini, önce `knowledge-service`'e sorarak RAG bağlamı oluşturmak ve bu bağlamla zenginleştirilmiş prompt'u LLM'e göndermek.