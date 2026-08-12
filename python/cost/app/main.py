"""
Раздел «Себестоимость» — FastAPI-приложение.

Контейнер запускается изолированно (см. swarm/docker-compose.cost.yml) и ходит
в отдельный postgres-cost. Маршруты раздела всегда висят за префиксом
COST_API_PREFIX (по умолчанию /api/cost) — этот же префикс знает nginx-cost.

Точка расширения — app/routes.py.
"""

from __future__ import annotations

import asyncio
import logging
import os
import time
import uuid
from contextlib import asynccontextmanager
from datetime import datetime, timezone

from fastapi import FastAPI, Request
from fastapi.middleware.cors import CORSMiddleware

from app.db import close_pool, get_cache_status, init_pool, load_cost_data_to_cache
from app.logship import log, setup_logging
from app.routes import router

API_PREFIX = os.environ.get("COST_API_PREFIX", "/api/cost")

# Логи: JSON в stdout + (если задан LOGSTASH_HOST) дубль в Logstash/ELK.
# Ставим до создания приложения, чтобы перехватить и логгеры uvicorn.
setup_logging()

_cache_task: asyncio.Task | None = None


async def _cache_worker() -> None:
    """Initial cache fill + periodic refresh every 3 hours.

    Full refresh (all rows) once per 24 hours, partial refresh (last 2 months)
    on the remaining 3-hourly ticks.
    """
    last_full: datetime | None = None
    try:
        status = await get_cache_status()
        if status is None or status["row_count"] == 0:
            result = await load_cost_data_to_cache()
            if result.get("success"):
                last_full = datetime.now(timezone.utc)
                log(logging.INFO, "cache_worker: initial fill OK", phase="initial",
                    row_count=result.get("row_count"))
            else:
                log(logging.ERROR, "cache_worker: initial fill FAILED", phase="initial",
                    error=str(result.get("error", "unknown"))[:200])
        else:
            # Hot start: cache already populated — treat as fresh
            last_full = datetime.now(timezone.utc)
            log(logging.INFO, "cache_worker: hot start", phase="initial",
                row_count=status["row_count"])
    except Exception as exc:
        log(logging.ERROR, "cache_worker: startup error", phase="initial", error=str(exc))

    while True:
        await asyncio.sleep(3 * 3600)
        try:
            now = datetime.now(timezone.utc)
            if last_full is None or (now - last_full).total_seconds() >= 86400:
                result = await load_cost_data_to_cache()
                if result.get("success"):
                    last_full = now
                    log(logging.INFO, "cache_worker: full refresh OK", phase="full",
                        row_count=result.get("row_count"))
                else:
                    # Don't update last_full — next tick will retry full refresh
                    log(logging.ERROR, "cache_worker: full refresh FAILED", phase="full",
                        error=str(result.get("error", "unknown"))[:200])
            else:
                result = await load_cost_data_to_cache(partial_months=2)
                if result.get("success"):
                    log(logging.INFO, "cache_worker: partial refresh OK", phase="partial",
                        row_count=result.get("row_count"))
                else:
                    log(logging.ERROR, "cache_worker: partial refresh FAILED", phase="partial",
                        error=str(result.get("error", "unknown"))[:200])
                # Don't update last_full on partial — only full refreshes count for 24h cycle
        except Exception as exc:
            log(logging.ERROR, "cache_worker: tick error", error=str(exc))


@asynccontextmanager
async def lifespan(_: FastAPI):
    await init_pool()
    try:
        global _cache_task
        _cache_task = asyncio.create_task(_cache_worker())
        yield
    finally:
        if _cache_task is not None:
            _cache_task.cancel()
        await close_pool()


app = FastAPI(title="Finance · Cost", lifespan=lifespan)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_methods=["*"],
    allow_headers=["*"],
)


@app.middleware("http")
async def access_log(request: Request, call_next):
    """Access-лог в общем формате кабинета (одна JSON-строка на запрос).

    X-Request-Id пробрасываем сквозняком: свой или пришедший от nginx/фронта —
    по нему в ELK сшивается цепочка nuxt → go-api → python-cost.
    Пользователь раздела приходит заголовком X-Cost-User (см. app/middleware.py).
    """
    started = time.perf_counter()
    rid = request.headers.get("X-Request-Id") or uuid.uuid4().hex
    fields = {
        "request_id": rid,
        "route": f"{request.method} {request.url.path}",
        "user": request.headers.get("X-Cost-User") or None,
    }
    try:
        response = await call_next(request)
    except Exception as exc:
        log(logging.ERROR, "http request", status=500,
            duration_ms=int((time.perf_counter() - started) * 1000),
            error=str(exc), **fields)
        raise
    duration_ms = int((time.perf_counter() - started) * 1000)
    response.headers["X-Request-Id"] = rid
    level = logging.ERROR if response.status_code >= 500 else logging.INFO
    log(level, "http request", status=response.status_code, duration_ms=duration_ms, **fields)
    return response

app.include_router(router, prefix=API_PREFIX)


@app.get("/healthz")
async def healthz() -> dict:
    return {"status": "ok", "section": "cost"}
