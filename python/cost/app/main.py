"""
Раздел «Себестоимость» — FastAPI-приложение.

Контейнер запускается изолированно (см. swarm/docker-compose.cost.yml) и ходит
в отдельный postgres-cost. Маршруты раздела всегда висят за префиксом
COST_API_PREFIX (по умолчанию /api/cost) — этот же префикс знает nginx-cost.

Точка расширения — app/routes.py.
"""

from __future__ import annotations

import os
from contextlib import asynccontextmanager

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from app.db import close_pool, init_pool
from app.routes import router

API_PREFIX = os.environ.get("COST_API_PREFIX", "/api/cost")


@asynccontextmanager
async def lifespan(_: FastAPI):
    await init_pool()
    try:
        yield
    finally:
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
