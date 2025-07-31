FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
# Kontratları çek
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /agent-service .

FROM scratch
WORKDIR /
COPY --from=builder /agent-service .
ENTRYPOINT ["/agent-service"]