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
# ETL-утилита заливки FinDebt-вьюх (Payments.report.FinDebt1/3) → ClickHouse
# finance.fact_findebt/_docs. Запуск: `docker exec <task> /findebt-etl`
# (MODE=bootstrap|incremental). См. docs/reports/debt/findebt-verification.md.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /findebt-etl ./cmd/findebt-etl


# -------- Runtime --------
FROM registry.markformelle.ru/services/docker-registry/finance-alpine-${tag}

RUN apk add --no-cache ca-certificates tzdata wget && update-ca-certificates

WORKDIR /app

COPY --from=builder /server /server
COPY --from=builder /findebt-etl /findebt-etl
COPY swarm/go-api/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 8080

ENTRYPOINT ["/entrypoint.sh"]
