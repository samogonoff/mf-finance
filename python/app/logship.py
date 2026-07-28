"""
Логи python-analytics → stdout + Logstash/ELK.

Тот же контракт, что у Go-API (go/internal/logship), Nitro-роутов Nuxt
(nuxt/server/utils/logstash.ts) и раздела «Себестоимость»
(python/cost/app/logship.py):

  * одна JSON-запись на строку (кодек json_lines), поля
    time/level/msg/service/host + свои — чтобы в общем индексе ELK все рантаймы
    кабинета лежали единообразно. service=finance-analytics;
  * базовый канал — stdout (его собирает docker) — остаётся ВСЕГДА;
  * отправка асинхронная и НИКОГДА не блокирует обработку запроса: строка
    кладётся в очередь, фоновый поток-демон держит TCP-соединение и
    переподключается с backoff. Нет связи или очередь переполнена — строка
    отбрасывается, растёт счётчик dropped (его раз в минуту пишет сам логгер).

Копия, а не импорт из python/cost/: контуры изолированы и собираются в разные
образы (swarm/python-analytics/Dockerfile.dev видит только python/, образ
python-cost — только python/cost/). Зависимостей у модуля нет — чистый stdlib,
как и весь этот sandbox.

ENV:
  LOGSTASH_HOST — пусто → трансляция выключена, остаётся только stdout;
  LOGSTASH_PORT — пусто при заданном HOST → 5044.
"""
from __future__ import annotations

import json
import logging
import os
import queue
import socket
import threading
import time
from datetime import datetime, timezone
from typing import Any

SERVICE = "finance-analytics"
HOSTNAME = socket.gethostname()

_HOST = os.environ.get("LOGSTASH_HOST", "").strip()
_PORT = int(os.environ.get("LOGSTASH_PORT") or "5044")
_MAX_QUEUE = 4096
_DIAL_TIMEOUT = 5.0
_BACKOFF_START = 1.0
_BACKOFF_MAX = 30.0

# Python зовёт уровни иначе, чем slog/Nitro — приводим к общему словарю.
_LEVELS = {"WARNING": "WARN", "CRITICAL": "ERROR"}


class _Shipper:
    """Фоновый поток-демон: очередь строк → TCP-соединение с Logstash."""

    def __init__(self, host: str, port: int) -> None:
        self._host = host
        self._port = port
        self._queue: queue.Queue[str] = queue.Queue(maxsize=_MAX_QUEUE)
        self.dropped = 0
        threading.Thread(target=self._run, name="logship", daemon=True).start()

    def ship(self, line: str) -> None:
        """Кладёт строку (с '\\n' на конце) в очередь. Не блокирует, не бросает."""
        try:
            self._queue.put_nowait(line)
        except queue.Full:
            self.dropped += 1  # очередь полна — роняем, не тормозим запрос

    def _run(self) -> None:
        sock: socket.socket | None = None
        backoff = _BACKOFF_START
        while True:
            line = self._queue.get()
            if sock is None:
                try:
                    sock = socket.create_connection((self._host, self._port), _DIAL_TIMEOUT)
                except OSError:
                    # Logstash недоступен — роняем строку и ждём backoff, чтобы
                    # не долбить мёртвый адрес на каждой записи лога.
                    self.dropped += 1
                    time.sleep(backoff)
                    backoff = min(backoff * 2, _BACKOFF_MAX)
                    continue
                backoff = _BACKOFF_START
            try:
                sock.sendall(line.encode("utf-8"))
            except OSError:
                try:
                    sock.close()
                finally:
                    sock = None
                self.dropped += 1  # строку потеряли, переподключимся на следующей


_shipper: _Shipper | None = None


def ship_log(line: str) -> None:
    if _shipper is not None:
        _shipper.ship(line)


def dropped_count() -> int:
    return _shipper.dropped if _shipper is not None else 0


class JsonFormatter(logging.Formatter):
    """Формат записи — общий для всех рантаймов кабинета (см. шапку модуля)."""

    def format(self, record: logging.LogRecord) -> str:  # noqa: A003
        payload: dict[str, Any] = {
            "time": datetime.fromtimestamp(record.created, timezone.utc)
            .isoformat()
            .replace("+00:00", "Z"),
            "level": _LEVELS.get(record.levelname, record.levelname),
            "msg": record.getMessage(),
            "service": SERVICE,
            "host": HOSTNAME,
            "logger": record.name,
        }
        fields = getattr(record, "fields", None)
        if isinstance(fields, dict):
            payload.update(fields)
        if record.exc_info:
            payload["error"] = self.formatException(record.exc_info)
        return json.dumps(payload, ensure_ascii=False, default=str)


class _StdoutAndLogstashHandler(logging.StreamHandler):
    """stdout — базовый канал; та же строка уходит в Logstash (если включён)."""

    def emit(self, record: logging.LogRecord) -> None:
        super().emit(record)
        try:
            ship_log(self.format(record) + "\n")
        except Exception:  # noqa: BLE001 — лог не имеет права ронять запрос
            pass


def log(level: int, msg: str, **fields: Any) -> None:
    """Разовая структурная запись: log(logging.INFO, "…", rows=123)."""
    logging.getLogger("analytics").log(level, msg, extra={"fields": fields})


def _watch_dropped() -> None:
    """Раз в минуту сообщает, сколько строк не доехало в Logstash.

    Рост счётчика = канал в ELK ослеп (нет связи либо переполняется очередь);
    это повод для алерта. Пишем только при изменении, чтобы не сорить.
    """
    last = 0
    while True:
        time.sleep(60)
        now = dropped_count()
        if now != last:
            log(logging.WARNING, "logship: строки лога не доставлены в Logstash",
                dropped_total=now, since_last=now - last)
            last = now


def setup_logging() -> None:
    """Переводит логирование процесса на JSON и поднимает канал в Logstash."""
    global _shipper
    if _HOST and _shipper is None:
        _shipper = _Shipper(_HOST, _PORT)

    handler = _StdoutAndLogstashHandler()
    handler.setFormatter(JsonFormatter())

    root = logging.getLogger()
    root.handlers = [handler]
    root.setLevel(logging.INFO)

    if _shipper is not None:
        log(logging.INFO, "logship: трансляция логов в Logstash включена",
            addr=f"{_HOST}:{_PORT}")
        threading.Thread(target=_watch_dropped, name="logship-dropped", daemon=True).start()
