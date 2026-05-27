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
# Bootstrap-утилита заливки Premaster1C → ClickHouse. На проде запускается
# через `docker exec <task> /debt-bootstrap` под нужный BOOTSTRAP_COMPANIES.
# См. docs/reports/debt/etl-runbook.md.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /debt-bootstrap ./cmd/debt-bootstrap


# -------- Runtime --------
FROM registry.markformelle.ru/services/docker-registry/finance-alpine-${tag}

RUN apk add --no-cache ca-certificates tzdata wget && update-ca-certificates

WORKDIR /app

COPY --from=builder /server /server
COPY --from=builder /debt-bootstrap /debt-bootstrap
COPY swarm/go-api/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 8080

ENTRYPOINT ["/entrypoint.sh"]
