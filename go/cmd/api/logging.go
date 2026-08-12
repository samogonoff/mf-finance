package main

import (
	"context"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/company/finance-api/internal/auth"
	"github.com/company/finance-api/internal/logship"
	"github.com/google/uuid"
)

// Логи finance-api: одна JSON-строка на событие в stdout (его собирает docker)
// и та же строка — в Logstash/ELK, если задан LOGSTASH_HOST. Форма записи общая
// для всех рантаймов кабинета (time/level/msg/service/host + свои поля), чтобы в
// одном индексе ELK лежали единообразно: service=finance-api (Go),
// finance-nuxt (Nitro), finance-cost (FastAPI раздела «Себестоимость»).

type ctxKey int

const ctxKeyReqInfo ctxKey = iota

// reqInfo — изменяемый «конверт» запроса: логирующий middleware кладёт указатель
// в контекст до маршрутизации, а auth.AuthObserver дописывает в него user_id уже
// после аутентификации (RequireBearer кладёт *User в клон запроса, наружу он
// не виден — см. комментарий у auth.AuthObserver).
type reqInfo struct {
	id     string
	userID int64
}

// setupLogging переводит логи процесса на slog+JSON и, если задан адрес
// Logstash, дублирует их туда. Возвращает шиппер (может быть nil — трансляция
// выключена) для Close/наблюдения за счётчиком потерь.
//
// Заодно перенаправляет stdlib-log в тот же slog: по коду разбросаны
// log.Printf («plans: dir seed: …», «debt: backend=…»), переписывать их все
// смысла нет — bridge отдаёт их как msg обычной JSON-записи.
func setupLogging(logstashAddr string) *logship.Writer {
	shipper := logship.New(logstashAddr, 4096)

	var dest io.Writer = os.Stdout
	if shipper != nil {
		dest = io.MultiWriter(os.Stdout, shipper)
	}

	host, _ := os.Hostname()
	slog.SetDefault(slog.New(slog.NewJSONHandler(dest, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})).With("service", "finance-api", "host", host))

	// stdlib log → slog. Флаги гасим: время и уровень проставляет slog.
	log.SetFlags(0)
	log.SetOutput(stdlogBridge{})

	// user_id в access-лог: RequireBearer сообщает id сразу после проверки токена.
	auth.AuthObserver = func(ctx context.Context, userID int64) {
		if info, ok := ctx.Value(ctxKeyReqInfo).(*reqInfo); ok {
			info.userID = userID
		}
	}

	if shipper != nil {
		slog.Info("logship: трансляция логов в Logstash включена", "addr", logstashAddr)
		go watchDropped(shipper)
	}
	return shipper
}

// stdlogBridge — приёмник вывода stdlib log: строка становится msg JSON-записи.
// Уровень всегда INFO (у log.Printf его просто нет). log.Fatalf успевает лечь в
// stdout синхронно; долететь до Logstash перед os.Exit он не обязан — базовый
// канал в любом случае цел.
type stdlogBridge struct{}

func (stdlogBridge) Write(p []byte) (int, error) {
	slog.Info(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// watchDropped раз в минуту сообщает, сколько строк не доехало в Logstash.
// Рост счётчика = канал в ELK ослеп (нет связи или буфер переполняется) — это
// повод для алерта, а не косметика. Пишем только при изменении, чтобы не
// сорить в спокойном режиме.
func watchDropped(w *logship.Writer) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	var last int64
	for range t.C {
		if n := w.Dropped(); n != last {
			slog.Warn("logship: строки лога не доставлены в Logstash", "dropped_total", n, "since_last", n-last)
			last = n
		}
	}
}

// statusRecorder — перехватывает код ответа и объём тела для access-лога.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// withLogging — access-лог в structured JSON (slog). Заодно проставляет
// X-Request-Id: свой или пришедший от reverse-proxy.
func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		rid := r.Header.Get("X-Request-Id")
		if rid == "" {
			rid = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", rid)

		info := &reqInfo{id: rid}
		r = r.WithContext(context.WithValue(r.Context(), ctxKeyReqInfo, info))

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		attrs := []any{
			"request_id", rid,
			"route", r.Method + " " + r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"bytes", rec.bytes,
		}
		if info.userID != 0 {
			attrs = append(attrs, "user_id", info.userID)
		}
		if rec.status >= http.StatusInternalServerError {
			slog.Error("http request", attrs...)
			return
		}
		slog.Info("http request", attrs...)
	})
}
