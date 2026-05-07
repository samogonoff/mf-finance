"""
Finance Analytics Sandbox.

Песочница для аналитика. Контейнер коннектится к БД под ролью analytics_ro:
- SELECT на боевые таблицы (users, transactions, …)
- ALL на схему analytics

Контейнер торчит во внутреннюю сеть; наружу — только через Go-API по
белому списку endpoint'ов.

Здесь — заглушка, чтобы поднялся docker-compose. Полноценный код
(REST/gRPC, ноутбуки, очередь джоб) — задача после первого релиза.
"""

from __future__ import annotations

import os
import socket
from http.server import BaseHTTPRequestHandler, HTTPServer

PORT = int(os.environ.get("PORT", "8090"))
DB_URL = os.environ.get("ANALYTICS_DATABASE_URL", "")


class Handler(BaseHTTPRequestHandler):
    def do_GET(self) -> None:
        if self.path == "/healthz":
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(b'{"status":"ok","sandbox":"python"}')
            return
        self.send_response(404)
        self.end_headers()

    def log_message(self, fmt: str, *args) -> None:
        # Тише, без access-логов в stdout — за этим следит nginx.
        return


def main() -> None:
    print(f"[analytics] starting on :{PORT}, host={socket.gethostname()}", flush=True)
    print(f"[analytics] db_url_present={bool(DB_URL)}", flush=True)
    HTTPServer(("0.0.0.0", PORT), Handler).serve_forever()


if __name__ == "__main__":
    main()
