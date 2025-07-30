# Dockerfile (Python Multi-Stage & Alpine)

# --- STAGE 1: Builder ---
# Builder olarak alpine tabanlı python imajını kullanıyoruz.
# 'build-base' paketi, C tabanlı kütüphaneleri derlemek için gerekli araçları içerir.
FROM python:3.10-alpine AS builder

# Gerekli build araçlarını kur
RUN apk add --no-cache git build-base

WORKDIR /app

# Bağımlılıkları kur
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt


# --- STAGE 2: Final (Minimal & Secure) Image ---
# Final imajımız da alpine tabanlı olacak.
FROM python:3.10-alpine

# Güvenlik için 'root' olmayan bir kullanıcı oluştur ve kullan
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
USER appuser
WORKDIR /home/appuser

# Builder'dan SADECE kurulu paketleri kopyala
COPY --from=builder /usr/local/lib/python3.10/site-packages /usr/local/lib/python3.10/site-packages

# SADECE gerekli uygulama dosyalarını kopyala
COPY main.py .
COPY logger_config.py .
# Eğer başka dosyalar/klasörler varsa onları da ekle:
# COPY ./src ./src

# Python'u unbuffered modda çalıştır
CMD ["python", "-u", "main.py"]