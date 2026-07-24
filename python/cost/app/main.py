"""
Раздел «Себестоимость» — FastAPI-приложение.

Контейнер запускается изолированно (см. swarm/docker-compose.cost.yml) и ходит
в отдельный postgres-cost. Маршруты раздела всегда висят за префиксом
COST_API_PREFIX (по умолчанию /api/cost) — этот же префикс знает nginx-cost.

Точка расширения — app/routes.py.
"""

from __future__ import annotations

import asyncio
import os
from contextlib import asynccontextmanager
from datetime import datetime, timezone

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from app.db import close_pool, get_cache_status, init_pool, load_cost_data_to_cache
from app.routes import router

API_PREFIX = os.environ.get("COST_API_PREFIX", "/api/cost")

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
                print("[cache_worker] initial fill OK", flush=True)
            else:
                print(f"[cache_worker] initial fill FAILED: {result.get('error', 'unknown')[:200]}", flush=True)
        else:
            # Hot start: cache already populated — treat as fresh
            last_full = datetime.now(timezone.utc)
            print(f"[cache_worker] hot start: cache has {status['row_count']} rows", flush=True)
    except Exception as exc:
        print(f"[cache_worker] startup error: {exc}", flush=True)

    while True:
        await asyncio.sleep(3 * 3600)
        try:
            now = datetime.now(timezone.utc)
            if last_full is None or (now - last_full).total_seconds() >= 86400:
                result = await load_cost_data_to_cache()
                if result.get("success"):
                    last_full = now
                    print(f"[cache_worker] full refresh OK ({result.get('row_count')} rows)", flush=True)
                else:
                    # Don't update last_full — next tick will retry full refresh
                    print(f"[cache_worker] full refresh FAILED: {result.get('error', 'unknown')[:200]}", flush=True)
            else:
                result = await load_cost_data_to_cache(partial_months=2)
                if result.get("success"):
                    print(f"[cache_worker] partial refresh OK ({result.get('row_count')} rows)", flush=True)
                else:
                    print(f"[cache_worker] partial refresh FAILED: {result.get('error', 'unknown')[:200]}", flush=True)
                # Don't update last_full on partial — only full refreshes count for 24h cycle
        except Exception as exc:
            print(f"[cache_worker] tick error: {exc}", flush=True)


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

app.include_router(router, prefix=API_PREFIX)


@app.get("/healthz")
async def healthz() -> dict:
    return {"status": "ok", "section": "cost"}
