package logship

import (
	"bufio"
	"net"
	"testing"
	"time"
)

// Пустой адрес → трансляция выключена, но Writer остаётся безопасным: все
// методы nil-safe (main кладёт его в io.MultiWriter только когда он не nil).
func TestNewDisabled(t *testing.T) {
	w := New("", 16)
	if w != nil {
		t.Fatalf("New(\"\") = %v, ожидался nil", w)
	}
	if n, err := w.Write([]byte("x\n")); n != 2 || err != nil {
		t.Fatalf("Write на nil-Writer = (%d, %v), ожидалось (2, nil)", n, err)
	}
	if w.Dropped() != 0 {
		t.Fatalf("Dropped на nil-Writer = %d, ожидался 0", w.Dropped())
	}
	w.Close() // не должно паниковать
}

// Строки долетают до Logstash как есть, по одной на строку (кодек json_lines).
func TestWriteDeliversLines(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	got := make(chan string, 2)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		sc := bufio.NewScanner(conn)
		for sc.Scan() {
			got <- sc.Text()
		}
	}()

	w := New(ln.Addr().String(), 16)
	defer w.Close()

	w.Write([]byte(`{"msg":"first"}` + "\n"))
	w.Write([]byte(`{"msg":"second"}` + "\n"))

	for _, want := range []string{`{"msg":"first"}`, `{"msg":"second"}`} {
		select {
		case line := <-got:
			if line != want {
				t.Fatalf("получено %q, ожидалось %q", line, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("строка %q не доехала за 3 с", want)
		}
	}
}

// Ключевой инвариант: недоступный Logstash не блокирует Write и не отдаёт
// ошибку — строки просто теряются, а счётчик Dropped растёт.
func TestWriteNeverBlocksWhenUnreachable(t *testing.T) {
	// Порт, который гарантированно никто не слушает.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	w := New(addr, 2) // маленький буфер: переполнение наступит сразу
	defer w.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			if n, err := w.Write([]byte("line\n")); n != 5 || err != nil {
				t.Errorf("Write = (%d, %v), ожидалось (5, nil)", n, err)
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Write заблокировался на недоступном Logstash")
	}

	if w.Dropped() == 0 {
		t.Fatal("Dropped = 0, хотя Logstash недоступен и буфер переполнялся")
	}
}
