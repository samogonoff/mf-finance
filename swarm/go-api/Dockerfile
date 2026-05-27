ARG tag
FROM registry.markformelle.ru/services/docker-registry/finance-go-${tag} AS builder

# Базовый образ зафиксирован на Go 1.23, а go.mod требует 1.25.7.
# GOTOOLCHAIN=auto заставляет go скачать нужный toolchain при сборке.
ENV GOTOOLCHAIN=auto

WORKDIR /app

COPY go/go.mod go/go.sum ./
RUN go mod download

COPY go/. .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /server ./cmd/api


# -------- Runtime --------
FROM registry.markformelle.ru/services/docker-registry/finance-alpine-${tag}

RUN apk add --no-cache ca-certificates tzdata wget && update-ca-certificates

WORKDIR /app

COPY --from=builder /server /server
COPY swarm/go-api/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 8080

ENTRYPOINT ["/entrypoint.sh"]
