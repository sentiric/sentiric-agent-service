# 🏢 Sentiric Agent Service

[![Status](https://img.shields.io/badge/status-active-neon_green.svg)]()
[![Language](https://img.shields.io/badge/language-Go_1.24-blue.svg)]()

**Sentiric Agent Service**, platformun "İnsan-Makine Etkileşim Koordinatörü"dür. Yapay Zeka (AI) ve İnsan Ajanlar arasındaki geçişleri (Handover), ajan kuyruklarını ve canlı çağrı yönetimini orkestre eder.

## 🚀 Hızlı Başlangıç

### 1. Çalıştırma
```bash
# Bağımlılıkları çek
go mod tidy

# Servisi başlat
go run cmd/agent-service/main.go
```

### 2. Doğrulama (Health Check)
```bash
curl http://localhost:12030/health
```

## 🏛️ Mimari Anayasa ve Kılavuzlar
* **Geliştirici Kuralları (AI/İnsan):** Kod yazmaya başlamadan önce GİZLİ [.context.md](.context.md) dosyasını okuyun.
* **İş Mantığı ve Handover:** Ajan eşleştirme ve "Unified Bridge" mantığı için [LOGIC.md](LOGIC.md) dosyasını inceleyin.
* **Global Anayasa:** Bu servisin platformdaki konumu, gRPC portları ve bağımlılıkları **[sentiric-spec](https://github.com/sentiric/sentiric-spec)** içindedir.
EOF