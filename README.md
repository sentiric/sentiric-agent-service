# 🤖 Sentiric Agent Service

[![Status](https://img.shields.io/badge/status-active-success.svg)]()
[![Role](https://img.shields.io/badge/role-Conversation_Specialist-blue.svg)]()
[![Protocol](https://img.shields.io/badge/protocol-gRPC_&_RabbitMQ-green.svg)]()

**Sentiric Agent Service**, platformun **Konuşma Uzmanıdır (Conversation Specialist)**.

Eskiden tüm çağrı akışını yöneten bu servis, **v3.0 Mimarisi** ile birlikte sadece **"İnsan-Makine Etkileşimi"ne** odaklanmıştır. Çağrı yönlendirme ve karar verme yetkileri `Workflow Service`'e devredilmiş; Agent Service ise kendisine atanan bir konuşmayı en doğal, akıcı ve zeki şekilde yürütmekten sorumlu hale gelmiştir.

## 🎯 Temel Sorumluluklar

1.  **Diyalog Yönetimi:** Kullanıcıdan gelen metni (STT'den) alır, bağlamı (Context) koruyarak LLM'e iletir ve gelen cevabı seslendirir (TTS).
2.  **Full-Duplex Etkileşim:** Kullanıcının sözünü kesmesini (Barge-in) algılar ve konuşmayı dinamik olarak durdurup/başlatır.
3.  **Kişilik Yönetimi:** Kendisine verilen "Persona" (Satış Temsilcisi, Teknik Destek vb.) doğrultusunda davranır.
4.  **Emir Kulu:** Kendi başına arama başlatmaz veya sonlandırmaz. `Workflow Service`'ten gelen "Sahneye Çık" emrini bekler.

## 🛠️ Teknoloji Yığını

*   **Dil:** Go (Yüksek performanslı eşzamanlılık için)
*   **İletişim:** 
    *   **Giriş:** RabbitMQ (Olay tabanlı tetiklenme)
    *   **Çıkış:** gRPC (Diğer servislere komut gönderme)
*   **Durum:** Redis (Kısa süreli konuşma hafızası)
*   **AI Entegrasyonu:** `stt-gateway`, `llm-gateway` ve `tts-gateway` istemcisi.

## 🔌 API Etkileşimleri

Bu servis bir sunucu portu açmaz, bir **Worker** gibi çalışır.

*   **Gelen (Tüketici):**
    *   `RabbitMQ`: `conversation.start` gibi iş emirlerini dinler.
*   **Giden (İstemci):**
    *   `telephony-action-service`: Sesi oynatmak ve dinlemek için.
    *   `llm-gateway`: Akıl danışmak için.
    *   `user-service`: Konuştuğu kişiyi tanımak için.

## 🔄 Eski vs Yeni Mimari

| Özellik | Eski Agent (v2.0) | Yeni Agent (v3.0) |
| :--- | :--- | :--- |
| **Rolü** | Orkestratör (Patron) | Uzman (Çalışan) |
| **Karar Yetkisi** | "Aramayı kime aktarayım?" | "Kullanıcıya ne cevap vereyim?" |
| **Çağrı Akışı** | Yönetir | Dahil Olur |
| **Veri Tabanı** | Dialplan Kurallarını Okur | Sadece Konuşma Geçmişini Tutar |

---
## 🏛️ Anayasal Konum

Bu servis, [Sentiric Anayasası'nın](https://github.com/sentiric/sentiric-governance) **Logic Layer**'ında yer alır ancak artık bir Karar Verici değil, **Yetenek Sağlayıcı (Capability Provider)** konumundadır.