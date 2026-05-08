"""
Эндпоинты раздела «Себестоимость».

Бизнес-логика портирована из var/original/costhistory/views.py.
Идентичность пользователя берём из заголовка X-Username — этот заголовок
ставит фронт раздела (см. python/cost/nuxt-layer/pages/cost/index.vue),
читая текущего пользователя из useAuth() основного контура.
В cost-only режиме фронт подставляет 'cost-dev'.
"""

from __future__ import annotations

import os
from typing import Any

from fastapi import APIRouter, Header, HTTPException, Request

from app import mocks
from app.db import get_gpartner_conn, get_mssql_conn, get_olap_conn, pool

router = APIRouter()


def _is_mock() -> bool:
    """COST_MOCK=1 — отдаём заглушки вместо MSSQL/OLAP. Удобно без VPN."""
    return os.environ.get("COST_MOCK", "").strip() == "1"

# ── Каталог фильтров ─────────────────────────────────────────────────────────

# Ключ → имя колонки в [CostHistory]
FILTER_COLUMNS: dict[str, str] = {
    "brand_manager": "Бренд-менеджер",
    "level01": "Level 01",
    "level02": "Level 02",
    "level03": "Level 03",
    "level04": "Level 04",
    "level05": "Level 05",
    "country": "Страна пр-ва",
    "family": "Семья",
    "season": "Сезон",
    "calc_sign": "Признак калькуляции",
    "model": "Модель",
    "articul": "Артикул",
}

# ── Filter options (каскадная фильтрация) ────────────────────────────────────


@router.get("/filter-options")
def get_filter_options(request: Request) -> dict[str, list[str]]:
    """
    Возвращает уникальные значения для каждого фильтра.
    Если переданы значения других фильтров — оставляет только связанные.
    Двусторонний каскад: каждый фильтр зависит от ВСЕХ остальных, кроме самого себя.
    """
    if _is_mock():
        return mocks.FILTER_OPTIONS

    selected: dict[str, list[str]] = {}
    for key in FILTER_COLUMNS:
        values = request.query_params.getlist(key)
        if values:
            selected[key] = values

    conn = get_mssql_conn()
    cursor = conn.cursor()
    try:
        result: dict[str, list[str]] = {}

        for current_key, current_col in FILTER_COLUMNS.items():
            query = (
                f"SELECT DISTINCT RTRIM([{current_col}]) AS val "
                f"FROM [CostHistory] WHERE [{current_col}] IS NOT NULL"
            )
            params: list[Any] = []

            for key, col in FILTER_COLUMNS.items():
                if key == current_key:
                    continue  # сам себе не фильтр
                values = selected.get(key)
                if not values or "all" in values:
                    continue
                placeholders = ",".join(["?"] * len(values))
                query += f" AND RTRIM([{col}]) IN ({placeholders})"
                params.extend(v.strip() for v in values)

            cursor.execute(query, params)
            result[current_key] = sorted([row[0] for row in cursor.fetchall() if row[0]])

        return result
    finally:
        conn.close()


# ── Aggregated data (GROUP BY на стороне SQL Server) ─────────────────────────

GROUP_FIELDS = [
    "Бренд-менеджер",
    "Модель",
    "Артикул",
    "Признак калькуляции",
    "дата расчета",
    "Уровень цен",
]
AVG_FIELDS = [
    "Розничная цена по уровню, руб.",
    "Отпускная цена по уровню, руб",
    "Розничная цена по уровню, USD.",
    "Отпускная цена по уровню, USD.",
    "Пошив, руб.",
    "Пошив, USD.",
    "Раскрой, руб.",
    "Раскрой, USD.",
]
SUM_FIELDS = [
    "Основные материалы, руб.",
    "Основные материалы, USD.",
    "Вспомогательные материалы, руб.",
    "Вспомогательные материалы, USD.",
    "Декоры, руб.",
    "Декоры, USD.",
    "Себестоимость, руб.",
    "Себестоимость, USD.",
]


@router.post("/aggregated")
def get_aggregated(payload: dict) -> dict:
    if _is_mock():
        return mocks.aggregated()

    select_parts: list[str] = [f"[{f}]" for f in GROUP_FIELDS]
    select_parts += [f"AVG([{f}]) AS [avg_{f}]" for f in AVG_FIELDS]
    select_parts += [f"SUM([{f}]) AS [sum_{f}]" for f in SUM_FIELDS]

    query = f"SELECT {', '.join(select_parts)} FROM [CostHistory] WHERE 1=1"
    params: list[Any] = []

    if payload.get("date_from"):
        query += " AND [дата расчета] >= ?"
        params.append(payload["date_from"])
    if payload.get("date_to"):
        query += " AND [дата расчета] <= ?"
        params.append(payload["date_to"])

    for key, col in FILTER_COLUMNS.items():
        values = payload.get(key) or []
        if values and "all" not in values:
            placeholders = ",".join(["?"] * len(values))
            query += f" AND [{col}] IN ({placeholders})"
            params.extend(values)

    query += " GROUP BY " + ", ".join(f"[{f}]" for f in GROUP_FIELDS)

    conn = get_mssql_conn()
    cursor = conn.cursor()
    try:
        cursor.execute(query, params)
        columns = [d[0] for d in cursor.description]
        rows = cursor.fetchall()
        data = [dict(zip(columns, row)) for row in rows]
        return {"data": data, "count": len(data)}
    finally:
        conn.close()


# ── Details по модели/артикулу ───────────────────────────────────────────────


@router.post("/details")
def get_details(payload: dict) -> dict:
    model = (payload.get("model") or "").strip()
    articul = (payload.get("articul") or "").strip()
    if not model or not articul:
        raise HTTPException(400, "model and articul required")

    if _is_mock():
        return mocks.details(model, articul)

    query = """
        SELECT [Наименование модели], [Артикул], [цена материала, руб.], [Пошив, руб.],
               [Раскрой, руб.], [Вязание, руб.], [Себестоимость, руб.], [Себестоимость, USD.]
        FROM [CostHistory]
        WHERE RTRIM([Модель]) = ? AND RTRIM([Артикул]) = ?
    """
    conn = get_mssql_conn()
    cursor = conn.cursor()
    try:
        cursor.execute(query, (model, articul))
        columns = [d[0] for d in cursor.description]
        rows = cursor.fetchall()
        data = [dict(zip(columns, row)) for row in rows]
        return {"data": data, "count": len(data)}
    finally:
        conn.close()


# ── Уровни цен (Gpartner) ────────────────────────────────────────────────────


@router.get("/price-levels")
def get_price_levels() -> list[dict]:
    if _is_mock():
        return mocks.PRICE_LEVELS

    conn = get_gpartner_conn()
    cursor = conn.cursor()
    try:
        cursor.execute(
            "SELECT NAME, PRICE_TYPE1, PRICE_TYPE3 FROM [dbo].[s_price_level] ORDER BY NAME"
        )
        return [
            {
                "name": (row[0] or "").strip(),
                "price_type1": float(row[1] or 0),
                "price_type3": float(row[2] or 0),
            }
            for row in cursor.fetchall()
        ]
    finally:
        conn.close()


# ── Сохранение изменений уровней цен ─────────────────────────────────────────


def _resolve_username(x_username: str | None) -> str:
    return (x_username or "").strip() or "system"


@router.post("/save-changes")
async def save_price_changes(payload: dict, x_username: str | None = Header(default=None)) -> dict:
    """Сохраняет одно изменение в OLAP + дублирует в локальный аудит."""
    username = _resolve_username(x_username)

    if _is_mock():
        # В mock-режиме не пишем в OLAP, только в локальный аудит.
        async with pool().acquire() as conn:
            await conn.execute(
                """
                INSERT INTO cost_price_changes_audit
                    (model, articul, price_level, retail_rub, wholesale_rub, username)
                VALUES ($1, $2, $3, $4, $5, $6)
                """,
                payload.get("model"),
                payload.get("articul"),
                payload.get("price_level"),
                payload.get("retail_rub"),
                payload.get("wholesale_rub"),
                username,
            )
        return {"success": True, "mock": True}

    olap = get_olap_conn()
    cursor = olap.cursor()
    try:
        cursor.execute(
            """
            INSERT INTO CostHistory_Changes
                (Модель, Артикул, Уровень_цен, Розничная_цена_руб, Отпускная_цена_руб, changed_at, Пользователь)
            VALUES (?, ?, ?, ?, ?, GETDATE(), ?)
            """,
            (
                payload.get("model"),
                payload.get("articul"),
                payload.get("price_level"),
                payload.get("retail_rub"),
                payload.get("wholesale_rub"),
                username,
            ),
        )
        olap.commit()
    finally:
        olap.close()

    async with pool().acquire() as conn:
        await conn.execute(
            """
            INSERT INTO cost_price_changes_audit
                (model, articul, price_level, retail_rub, wholesale_rub, username)
            VALUES ($1, $2, $3, $4, $5, $6)
            """,
            payload.get("model"),
            payload.get("articul"),
            payload.get("price_level"),
            payload.get("retail_rub"),
            payload.get("wholesale_rub"),
            username,
        )

    return {"success": True}


@router.post("/save-batch")
async def save_batch_changes(payload: dict, x_username: str | None = Header(default=None)) -> dict:
    """Массовое сохранение изменений уровней цен."""
    username = _resolve_username(x_username)
    changes = payload.get("changes") or []
    if not changes:
        return {"success": False, "error": "No changes to save", "count": 0}

    if _is_mock():
        async with pool().acquire() as conn:
            await conn.executemany(
                """
                INSERT INTO cost_price_changes_audit
                    (model, articul, price_level, retail_rub, wholesale_rub, username)
                VALUES ($1, $2, $3, $4, $5, $6)
                """,
                [
                    (
                        c.get("model"),
                        c.get("articul"),
                        c.get("price_level"),
                        c.get("retail_rub"),
                        c.get("wholesale_rub"),
                        username,
                    )
                    for c in changes
                ],
            )
        return {"success": True, "count": len(changes), "mock": True}

    olap = get_olap_conn()
    cursor = olap.cursor()
    try:
        for c in changes:
            cursor.execute(
                """
                INSERT INTO [FinSandBox].[dbo].[CostHistory_Changes]
                    (Модель, Артикул, Уровень_цен, Розничная_цена_руб, Отпускная_цена_руб, changed_at, Пользователь)
                VALUES (?, ?, ?, ?, ?, GETDATE(), ?)
                """,
                (
                    c.get("model"),
                    c.get("articul"),
                    c.get("price_level"),
                    c.get("retail_rub"),
                    c.get("wholesale_rub"),
                    username,
                ),
            )
        olap.commit()
    finally:
        olap.close()

    async with pool().acquire() as conn:
        await conn.executemany(
            """
            INSERT INTO cost_price_changes_audit
                (model, articul, price_level, retail_rub, wholesale_rub, username)
            VALUES ($1, $2, $3, $4, $5, $6)
            """,
            [
                (
                    c.get("model"),
                    c.get("articul"),
                    c.get("price_level"),
                    c.get("retail_rub"),
                    c.get("wholesale_rub"),
                    username,
                )
                for c in changes
            ],
        )

    return {"success": True, "count": len(changes)}
