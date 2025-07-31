FROM golang:1.24.5-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
# YENİ EKLENEN SATIR: Go'ya proxy'yi atlayıp doğrudan git'ten çekmesini söylüyoruz.
# Bu, önbellek gecikmelerini ortadan kaldırır.
RUN go env -w GOPROXY=direct
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /agent-service .

FROM scratch
WORKDIR /
COPY --from=builder /agent-service .
ENTRYPOINT ["/agent-service"]