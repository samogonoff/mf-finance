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
            await load_cost_data_to_cache()
            last_full = datetime.now(timezone.utc)
    except Exception:
        pass

    while True:
        await asyncio.sleep(3 * 3600)
        try:
            now = datetime.now(timezone.utc)
            if last_full is None or (now - last_full).total_seconds() >= 86400:
                await load_cost_data_to_cache()
                last_full = now
            else:
                await load_cost_data_to_cache(partial_months=2)
        except Exception:
            pass


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
