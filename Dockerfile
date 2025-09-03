### File: `sentiric-agent-service/Dockerfile`

# --- İNŞA AŞAMASI (DEBIAN TABANLI) ---
FROM golang:1.24-bullseye AS builder

# YENİ: Build argümanlarını build aşamasında kullanılabilir yap
ARG GIT_COMMIT="unknown"
ARG BUILD_DATE="unknown"
ARG SERVICE_VERSION="0.0.0"

# Git, CGO ve diğer bağımlılıklar için
RUN apt-get update && apt-get install -y --no-install-recommends git build-essential

WORKDIR /app

# Sadece bağımlılıkları indir ve cache'le
COPY go.mod go.sum ./
RUN go mod download
RUN go mod verify

# Tüm kaynak kodunu kopyala
COPY . .

# GÜNCELLEME: ldflags ile build-time değişkenlerini Go binary'sine göm
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-X main.GitCommit=${GIT_COMMIT} -X main.BuildDate=${BUILD_DATE} -X main.ServiceVersion=${SERVICE_VERSION} -w -s" \
    -o /app/bin/sentiric-agent-service ./cmd/agent-service

# --- ÇALIŞTIRMA AŞAMASI (ALPINE) ---
FROM alpine:latest

# TLS doğrulaması için ca-certificates gerekli
RUN apk add --no-cache ca-certificates

# GÜVENLİK: Root olmayan bir kullanıcı oluştur
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /app

# Dosyaları kopyala ve sahipliği yeni kullanıcıya ver
COPY --from=builder /app/bin/sentiric-agent-service .
RUN chown appuser:appgroup ./sentiric-agent-service

# GÜVENLİK: Kullanıcıyı değiştir
USER appuser

ENTRYPOINT ["./sentiric-agent-service"]