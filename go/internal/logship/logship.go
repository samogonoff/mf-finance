// Package logship — асинхронная трансляция структурных логов (slog JSON) в
// Logstash по TCP, кодек json_lines. Writer встаёт ВТОРЫМ в io.MultiWriter
// рядом с os.Stdout: stdout остаётся базовым каналом (его собирает docker),
// ELK — дополнительный. Порт того же решения, что в кабинете NCI.
//
// Инвариант, ради которого всё и написано: Write НИКОГДА не блокирует и НИКОГДА
// не возвращает ошибку. slog зовётся из каждого HTTP-хендлера (access-лог) —
// заблокируйся отправка в недоступный Logstash, и встал бы горячий путь, а через
// MultiWriter умерла бы и запись в stdout. Поэтому Write лишь кладёт копию строки
// в буфер и мгновенно возвращается; фоновая горутина держит TCP-соединение,
// переподключается с backoff и сливает буфер. Переполнен буфер или нет связи —
// строка отбрасывается, а счётчик Dropped растёт (main периодически пишет его
// в stdout: рост означает, что канал в ELK ослеп).
package logship

import (
	"net"
	"sync/atomic"
	"time"
)

const (
	dialTimeout  = 5 * time.Second
	writeTimeout = 5 * time.Second
	backoffStart = 1 * time.Second
	backoffMax   = 30 * time.Second
)

// Writer — асинхронный шиппер строк лога в Logstash. Реализует io.Writer.
type Writer struct {
	addr    string
	queue   chan []byte
	dropped atomic.Int64
	done    chan struct{}
}

// New поднимает шиппер на addr вида "host:port" и запускает фоновую горутину.
// Пустой addr → nil: трансляция выключена, остаётся только stdout (та же
// конвенция «пусто = интеграция отключена», что у SITE_API_NOTIFY_* и
// B24_USERGET_WEBHOOK). Методы на *Writer nil-safe, но io.MultiWriter принимать
// nil нельзя — проверку делает вызывающий код (cmd/api/logging.go).
func New(addr string, queueSize int) *Writer {
	if addr == "" {
		return nil
	}
	w := &Writer{
		addr:  addr,
		queue: make(chan []byte, queueSize),
		done:  make(chan struct{}),
	}
	go w.loop()
	return w
}

// Write кладёт КОПИЮ строки в буфер и возвращается немедленно. Копия
// обязательна: slog переиспользует буфер записи после возврата Write.
// Всегда возвращает (len(p), nil) — см. инвариант в шапке пакета.
func (w *Writer) Write(p []byte) (int, error) {
	if w == nil {
		return len(p), nil
	}
	b := make([]byte, len(p))
	copy(b, p)
	select {
	case w.queue <- b:
	default:
		w.dropped.Add(1) // буфер полон — роняем, не блокируем горячий путь
	}
	return len(p), nil
}

// Dropped — сколько строк потеряно (буфер полон, dial/write не удались).
func (w *Writer) Dropped() int64 {
	if w == nil {
		return 0
	}
	return w.dropped.Load()
}

// Close останавливает фоновую горутину. Строки, оставшиеся в буфере, не
// досылаются — на завершении процесса это приемлемо (базовый stdout не теряем).
func (w *Writer) Close() {
	if w == nil {
		return
	}
	close(w.done)
}

func (w *Writer) loop() {
	var conn net.Conn
	backoff := backoffStart
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()

	for {
		select {
		case <-w.done:
			return
		case line := <-w.queue:
			if conn == nil {
				c, err := net.DialTimeout("tcp", w.addr, dialTimeout)
				if err != nil {
					// Logstash недоступен — роняем строку и ждём backoff,
					// чтобы не долбить мёртвый адрес на каждой строке.
					w.dropped.Add(1)
					if !w.sleep(backoff) {
						return
					}
					if backoff < backoffMax {
						backoff *= 2
					}
					continue
				}
				conn = c
				backoff = backoffStart
			}
			// Строка slog уже завершается '\n' — ровно то, что ждёт кодек
			// json_lines. Отдельный разделитель добавлять не нужно.
			_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if _, err := conn.Write(line); err != nil {
				_ = conn.Close()
				conn = nil
				w.dropped.Add(1) // строку потеряли, переподключимся на следующей
			}
		}
	}
}

// sleep ждёт d, но прерывается на Close. Возвращает false, если пора выходить.
func (w *Writer) sleep(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-w.done:
		return false
	case <-t.C:
		return true
	}
}
