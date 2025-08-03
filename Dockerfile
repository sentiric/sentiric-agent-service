# Dockerfile for Go services (user-service, dialplan-service, agent-service)

FROM golang:1.24.5-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN go env -w GOPROXY=direct
RUN go mod download

COPY . .

# Servis adını dinamik olarak almak için ARG kullanıyoruz
ARG SERVICE_NAME
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/${SERVICE_NAME} .

# --- ÇALIŞTIRMA AŞAMASI ---
FROM alpine:latest

# Healthcheck için netcat ve TLS doğrulaması için ca-certificates kuruyoruz
RUN apk add --no-cache netcat-openbsd ca-certificates

# Servis adını builder'dan alıyoruz
ARG SERVICE_NAME
WORKDIR /app
COPY --from=builder /app/${SERVICE_NAME} .

COPY --from=builder /app/${SERVICE_NAME} /app/main

ENTRYPOINT ["/app/main"]