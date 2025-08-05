# --- İNŞA AŞAMASI ---
FROM golang:1.24.5-alpine AS builder

# Git, CGO ve diğer bağımlılıklar için
RUN apk add --no-cache git build-base

WORKDIR /app

# Sadece bağımlılıkları indir ve cache'le
COPY go.mod go.sum ./
RUN go mod download
RUN go mod verify

# Tüm kaynak kodunu kopyala
COPY . .

# Çıktı binary'sinin adını dinamik olarak almak için ARG kullanıyoruz.
ARG SERVICE_NAME=sentiric-agent-service

# DÜZELTME: Derlenecek paket yolu sabit (`./cmd/agent-service`),
# çıktı dosyasının adı ise dinamiktir (`-o /app/bin/${SERVICE_NAME}`).
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/bin/${SERVICE_NAME} ./cmd/agent-service

# --- ÇALIŞTIRMA AŞAMASI ---
FROM alpine:latest

# Healthcheck için netcat ve TLS doğrulaması için ca-certificates kuruyoruz
RUN apk add --no-cache netcat-openbsd ca-certificates

# Servis adını builder'dan alıyoruz
ARG SERVICE_NAME=sentiric-agent-service
WORKDIR /app

# Sadece derlenmiş binary'yi kopyala
COPY --from=builder /app/bin/${SERVICE_NAME} .

# Derlenmiş binary'yi çalıştır
ENTRYPOINT ["./sentiric-agent-service"]