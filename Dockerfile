# --- İNŞA AŞAMASI (DEBIAN TABANLI) ---
FROM golang:1.24-bullseye AS builder

# Git, CGO ve diğer bağımlılıklar için
RUN apt-get update && apt-get install -y --no-install-recommends git build-essential

WORKDIR /app

# Sadece bağımlılıkları indir ve cache'le
COPY go.mod go.sum ./
RUN go mod download
RUN go mod verify

# Tüm kaynak kodunu kopyala
COPY . .

# Çıktı binary'sinin adını dinamik olarak almak için ARG kullanıyoruz.
ARG SERVICE_NAME
# NOT: Derlenecek paket yolu servise göre değişebilir. Bu agent-service için doğru.
# Diğerleri için gerekirse ./ olarak değiştirilebilir.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/bin/${SERVICE_NAME} ./cmd/agent-service

# --- ÇALIŞTIRMA AŞAMASI (ALPINE) ---
# Çalışma zamanı için hala küçük ve güvenli alpine'ı kullanabiliriz.
FROM alpine:latest

# TLS doğrulaması için ca-certificates gerekli
RUN apk add --no-cache ca-certificates

ARG SERVICE_NAME
WORKDIR /app

# Sadece derlenmiş binary'yi kopyala
COPY --from=builder /app/bin/${SERVICE_NAME} .

# Güvenlik için root olmayan bir kullanıcıyla çalıştır
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
USER appuser

# Her servis kendi adıyla çağrılmalı
ENTRYPOINT ["./sentiric-agent-service"]