"""
Finance Analytics Sandbox.

Песочница для аналитика. Контейнер коннектится к БД под ролью analytics_ro:
- SELECT на боевые таблицы (users, transactions, …)
- ALL на схему analytics

Контейнер торчит во внутреннюю сеть; наружу — только через Go-API по
белому списку endpoint'ов.

Здесь — заглушка, чтобы поднялся docker-compose. Полноценный код
(REST/gRPC, ноутбуки, очередь джоб) — задача после первого релиза.

Логи — JSON в stdout + опциональная трансляция в Logstash/ELK
(service=finance-analytics, см. app/logship.py).
"""

from __future__ import annotations

import logging
import os
import socket
import time
import uuid
from http.server import BaseHTTPRequestHandler, HTTPServer

from app.logship import log, setup_logging

PORT = int(os.environ.get("PORT", "8090"))
DB_URL = os.environ.get("ANALYTICS_DATABASE_URL", "")


class Handler(BaseHTTPRequestHandler):
    def do_GET(self) -> None:
        started = time.perf_counter()
        # X-Request-Id пробрасываем сквозняком: по нему в ELK сшивается цепочка
        # nuxt → go-api → analytics (в сеть песочницы ходит только Go-прокси).
        rid = self.headers.get("X-Request-Id") or uuid.uuid4().hex

        if self.path == "/healthz":
            body = b'{"status":"ok","sandbox":"python"}'
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("X-Request-Id", rid)
            self.end_headers()
            self.wfile.write(body)
            self._access_log(rid, 200, started, len(body))
            return

        self.send_response(404)
        self.send_header("X-Request-Id", rid)
        self.end_headers()
        self._access_log(rid, 404, started, 0)

    def _access_log(self, rid: str, status: int, started: float, size: int) -> None:
        log(
            logging.ERROR if status >= 500 else logging.INFO,
            "http request",
            request_id=rid,
            route=f"{self.command} {self.path}",
            status=status,
            duration_ms=int((time.perf_counter() - started) * 1000),
            bytes=size,
        )

    def log_message(self, fmt: str, *args) -> None:
        # Текстовый access-лог BaseHTTPServer подавляем: свой пишем структурно
        # в _access_log, иначе одна и та же строка уезжала бы в ELK дважды.
        return

    def log_error(self, fmt: str, *args) -> None:
        # А вот ошибки протокола (битый запрос, обрыв) в ELK нужны.
        log(logging.WARNING, "http error", error=fmt % args if args else fmt)


def main() -> None:
    setup_logging()
    log(logging.INFO, "analytics: старт", port=PORT, host=socket.gethostname(),
        db_url_present=bool(DB_URL))
    HTTPServer(("0.0.0.0", PORT), Handler).serve_forever()


if __name__ == "__main__":
    main()
