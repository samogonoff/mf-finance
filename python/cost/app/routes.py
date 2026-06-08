from __future__ import annotations

import asyncio
import os
from datetime import date, datetime, timezone
from typing import Any

from fastapi import APIRouter, HTTPException, Request

from app import mocks
from app.db import (apply_pending_changes, clear_pending_changes, get_cache_status, get_dwh_conn, get_gpartner_conn, get_margin_targets, get_mssql_conn, get_olap_conn, get_pending_changes, load_cost_data_to_cache, pool, save_margin_targets, upsert_pending_change, upsert_pending_changes_batch)
from app.notify import notify_admins

router = APIRouter()


def _is_mock() -> bool:
    return os.environ.get("COST_MOCK", "").strip() == "1"


# ── Фильтр-конфиг (DWH.dim.groups) ──────────────────────────────────────────

FILTER_CONFIG: dict[str, str | tuple[str, str]] = {
    "brand_manager": "BRAND_FIO",
    "level01": ("gr1", "group1"),
    "level02": ("gr2", "group2"),
    "level03": ("gr3", "group3"),
    "level04": ("gr4", "group4"),
    "level05": ("gr5", "group5"),
}

CASCADE_KEYS = ["brand_manager", "level01", "level02", "level03", "level04", "level05"]
LEVEL_KEYS = ["level01", "level02", "level03", "level04", "level05"]

# ── Фильтр-options (DWH.dim.groups) ─────────────────────────────────────────


@router.get("/filter-options")
def get_filter_options(request: Request) -> dict:
    """Возвращает значения фильтров из DWH.dim.groups с каскадом."""
    selected: dict[str, list[str]] = {}
    for key in CASCADE_KEYS:
        values = request.query_params.getlist(key)
        if values:
            selected[key] = values

    if _is_mock():
        return mocks.get_filter_options(selected)

    conn = get_dwh_conn()
    cursor = conn.cursor()
    try:
        result: dict[str, Any] = {}

        # 1. Бренд-менеджеры — НЕ фильтруются уровнями (top-down cascade)
        cursor.execute("""
            SELECT DISTINCT RTRIM(BRAND_FIO) AS val
            FROM [DWH].[dim].[groups]
            WHERE BRAND_FIO IS NOT NULL AND BRAND_FIO != ''
            ORDER BY val
        """)
        result["brand_manager"] = [row[0].strip() for row in cursor.fetchall() if row[0]]

        # 2. Level 01-05 — только top-down каскад (вышестоящие уровни фильтруют нижестоящие)
        for i in range(1, 6):
            col_gr = f"gr{i}"
            col_group = f"group{i}"

            query = f"""
                SELECT DISTINCT {col_gr}, {col_group}
                FROM [DWH].[dim].[groups]
                WHERE {col_gr} IS NOT NULL
            """
            params = []

            if selected.get("brand_manager"):
                vals = [v.strip() for v in selected["brand_manager"]]
                placeholders = ",".join(["?"] * len(vals))
                query += f" AND BRAND_FIO IN ({placeholders})"
                params.extend(vals)

            # Только уровни выше (меньший индекс) фильтруют текущий уровень
            for j in range(1, i):
                key = f"level0{j}"
                vals = selected.get(key)
                if vals:
                    placeholders = ",".join(["?"] * len(vals))
                    query += f" AND gr{j} IN ({placeholders})"
                    params.extend(v.strip() for v in vals)

            # Уровни ниже (больший индекс) НЕ фильтруют вышестоящие — чистый top-down

            query += f" ORDER BY {col_group}"
            cursor.execute(query, params)
            rows = cursor.fetchall()
            result[f"level0{i}"] = [
                {"id": str(row[0]), "text": row[1].strip() if row[1] else str(row[0])}
                for row in rows
            ]

        # 3. Признак калькуляции (статический список)
        result["calc_sign"] = ["ПКПСС", "КПСС", "ПФКСС", "ФКСС"]

        return result
    finally:
        conn.close()


# ── Load data (сырые данные с пагинацией) ────────────────────────────────────

MULTI_FILTER_COLUMNS: dict[str, str] = {
    "level01": "Level 01",
    "level02": "Level 02",
    "level03": "Level 03",
    "level04": "Level 04",
    "level05": "Level 05",
    "brand_manager": "Бренд-менеджер",
    "country": "Страна пр-ва",
    "family": "Семья",
    "season": "Сезон",
    "calc_sign": "Признак калькуляции",
    "model": "Модель",
    "articul": "Артикул",
}


@router.post("/load-data")
async def load_data(payload: dict) -> dict:
    """Загружает сырые отфильтрованные данные из кеша с пагинацией."""
    if _is_mock():
        return mocks.load_data(payload)

    limit = min(payload.get("limit", 1000), 5000)
    offset = payload.get("offset", 0)

    params: list[Any] = []
    where_parts: list[str] = []

    if payload.get("date_from"):
        where_parts.append(f'"дата расчета" >= ${len(params) + 1}')
        params.append(date.fromisoformat(payload["date_from"]))
    if payload.get("date_to"):
        where_parts.append(f'"дата расчета" <= ${len(params) + 1}')
        params.append(date.fromisoformat(payload["date_to"]))

    for key, col in MULTI_FILTER_COLUMNS.items():
        values = payload.get(key) or []
        if values and "all" not in values:
            placeholders = ",".join(f"${len(params) + i + 1}" for i in range(len(values)))
            where_parts.append(f'TRIM("{col}") IN ({placeholders})')
            params.extend(values)

    where = " AND ".join(where_parts) if where_parts else "TRUE"

    async with pool().acquire() as conn:
        total = await conn.fetchval(f"SELECT COUNT(*) FROM cost_data_cache WHERE {where}", *params) or 0

        paginated_params = params + [limit, offset]
        query = f"SELECT * FROM cost_data_cache WHERE {where} ORDER BY id LIMIT ${len(params) + 1} OFFSET ${len(params) + 2}"
        rows = await conn.fetch(query, *paginated_params)
        data = [dict(row) for row in rows]

    return {"data": data, "count": len(data), "total": total, "offset": offset, "limit": limit}


# ── Aggregated data ──────────────────────────────────────────────────────────

AGG_GROUP_FIELDS = [
    "Бренд-менеджер",
    "Модель",
    "Артикул",
    "Наименование модели",
    "PLAN_ID",
    "Признак калькуляции",
    "дата расчета",
    "Уровень цен",
    "Страна пр-ва",
    "Семья",
    "Сезон",
    "Level 01",
    "Level 02",
    "Level 03",
    "Level 04",
    "Level 05",
]

AGG_AVG_FIELDS = [
    "Розничная цена по уровню, руб.",
    "Отпускная цена по уровню, руб",
    "Розничная цена по уровню, USD.",
    "Отпускная цена по уровню, USD.",
    "Пошив, руб.",
    "Пошив, USD.",
    "Раскрой, руб.",
    "Раскрой, USD.",
    "Декоры, руб.",
    "Декоры, USD.",
    "Вязание, руб.",
    "Вязание, USD.",
]

AGG_SUM_FIELDS = [
    "Основные материалы, руб.",
    "Основные материалы, USD.",
    "Вспомогательные материалы, руб.",
    "Вспомогательные материалы, USD.",
]

# Поля-компоненты для расчёта себестоимости (сумма 6 статей)
SEBEST_COMPONENTS_RUB = [
    "avg_Пошив, руб.",
    "avg_Раскрой, руб.",
    "avg_Декоры, руб.",
    "avg_Вязание, руб.",
    "sum_Основные материалы, руб.",
    "sum_Вспомогательные материалы, руб.",
]
SEBEST_COMPONENTS_USD = [
    "avg_Пошив, USD.",
    "avg_Раскрой, USD.",
    "avg_Декоры, USD.",
    "avg_Вязание, USD.",
    "sum_Основные материалы, USD.",
    "sum_Вспомогательные материалы, USD.",
]


@router.post("/aggregated")
async def get_aggregated(payload: dict) -> dict:
    if _is_mock():
        return mocks.aggregated(payload)

    select_parts: list[str] = [f'"{f}"' for f in AGG_GROUP_FIELDS]
    select_parts += [f'AVG("{f}") AS "avg_{f}"' for f in AGG_AVG_FIELDS]
    select_parts += [f'SUM("{f}") AS "sum_{f}"' for f in AGG_SUM_FIELDS]

    params: list[Any] = []
    where_parts: list[str] = []

    if payload.get("date_from"):
        where_parts.append(f'"дата расчета" >= ${len(params) + 1}')
        params.append(date.fromisoformat(payload["date_from"]))
    if payload.get("date_to"):
        where_parts.append(f'"дата расчета" <= ${len(params) + 1}')
        params.append(date.fromisoformat(payload["date_to"]))

    if payload.get("no_wholesale_only"):
        where_parts.append('("Отпускная цена по уровню, руб" IS NULL OR "Отпускная цена по уровню, руб" = 0)')

    for key, col in MULTI_FILTER_COLUMNS.items():
        values = payload.get(key) or []
        if values and "all" not in values:
            placeholders = ",".join(f"${len(params) + i + 1}" for i in range(len(values)))
            where_parts.append(f'TRIM("{col}") IN ({placeholders})')
            params.extend(values)

    where = " AND ".join(where_parts) if where_parts else "TRUE"
    query = f"SELECT {', '.join(select_parts)} FROM cost_data_cache WHERE {where} GROUP BY {', '.join(f'"{f}"' for f in AGG_GROUP_FIELDS)}"

    async with pool().acquire() as conn:
        rows = await conn.fetch(query, *params)
        data = [dict(row) for row in rows]

    for row in data:
        row["sum_Себестоимость, руб."] = round(
            sum(float(row.get(f, 0) or 0) for f in SEBEST_COMPONENTS_RUB), 2
        )
        row["sum_Себестоимость, USD."] = round(
            sum(float(row.get(f, 0) or 0) for f in SEBEST_COMPONENTS_USD), 2
        )

    # Inject margin targets per level1
    try:
        targets_raw = await get_margin_targets()
        target_map: dict[str, float] = {
            t["level1"]: float(t["target_margin_pct"]) for t in targets_raw
        }
        for row in data:
            l1 = (row.get("Level 01") or "").strip()
            row["target_margin_pct"] = target_map.get(l1)
    except Exception:
        pass  # no targets yet — leave field empty

    return {"data": data, "count": len(data)}


# ── Details по модели ────────────────────────────────────────────────────────


@router.post("/details")
async def get_details(payload: dict) -> dict:
    """Детализация по модели с GROUP BY и опциональными фильтрами."""
    model = (payload.get("model") or "").strip()
    if not model:
        raise HTTPException(400, "model required")

    if _is_mock():
        return mocks.details(model)

    params: list[Any] = []
    where_parts: list[str] = [f'TRIM("Модель") = ${len(params) + 1}']
    params.append(model)

    date_from = (payload.get("date_from") or "").strip()
    date_to = (payload.get("date_to") or "").strip()
    calc_sign = payload.get("calc_sign") or []

    if date_from:
        where_parts.append(f'"дата расчета" >= ${len(params) + 1}')
        params.append(date.fromisoformat(date_from))
    if date_to:
        where_parts.append(f'"дата расчета" <= ${len(params) + 1}')
        params.append(date.fromisoformat(date_to))
    if calc_sign:
        placeholders = ",".join(f"${len(params) + i + 1}" for i in range(len(calc_sign)))
        where_parts.append(f'TRIM("Признак калькуляции") IN ({placeholders})')
        params.extend(calc_sign)

    where = " AND ".join(where_parts)

    query = f"""
        SELECT
            "дата расчета",
            "Признак калькуляции",
            TRIM("Модель") AS "Модель",
            TRIM("Артикул") AS "Артикул",
            TRIM("Наименование модели") AS "Наименование модели",
            TRIM("Номер задания производства") AS "Номер задания производства",
            COALESCE(AVG("Розничная цена по уровню, руб."), 0) AS "Розничная цена, руб.",
            COALESCE(AVG("Отпускная цена по уровню, руб"), 0) AS "Оптовая цена, руб.",
            COALESCE(SUM("Основные материалы, руб."), 0) AS "Осн. материалы, руб.",
            COALESCE(SUM("Вспомогательные материалы, руб."), 0) AS "Вспом. материалы, руб.",
            COALESCE(AVG("Пошив, руб."), 0) AS "Пошив, руб.",
            COALESCE(AVG("Раскрой, руб."), 0) AS "Раскрой, руб.",
            COALESCE(AVG("Декоры, руб."), 0) AS "Декор, руб.",
            COALESCE(AVG("Вязание, руб."), 0) AS "Вязание, руб."
        FROM cost_data_cache
        WHERE {where}
        GROUP BY
            "дата расчета",
            "Признак калькуляции",
            TRIM("Модель"),
            TRIM("Артикул"),
            TRIM("Наименование модели"),
            TRIM("Номер задания производства")
        ORDER BY "дата расчета" DESC, TRIM("Артикул")
    """

    async with pool().acquire() as conn:
        rows = await conn.fetch(query, *params)
        details_data: list[dict[str, Any]] = []
        for row in rows:
            r = dict(row)
            poshiv = r.get("Пошив, руб.") or 0
            raskr = r.get("Раскрой, руб.") or 0
            decor = r.get("Декор, руб.") or 0
            vyaz = r.get("Вязание, руб.") or 0
            osn_mat = r.get("Осн. материалы, руб.") or 0
            vspom_mat = r.get("Вспом. материалы, руб.") or 0
            r["Себестоимость, руб."] = round(float(poshiv) + float(raskr) + float(decor) + float(vyaz) + float(osn_mat) + float(vspom_mat), 2)
            opt = r.get("Оптовая цена, руб.") or 0
            seb = r["Себестоимость, руб."] or 0
            markup = float(opt) - seb
            r["Наценка, руб."] = round(markup, 2)
            r["Наценка, %"] = round(markup / seb * 100, 2) if seb else 0
            r["Маржинальность, %"] = round(markup / float(opt) * 100, 2) if float(opt) else 0
            details_data.append(r)
        return {"data": details_data, "count": len(details_data)}


# ── Price levels ─────────────────────────────────────────────────────────────


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


# ── Save changes ─────────────────────────────────────────────────────────────


@router.post("/save-changes")
async def save_price_changes(payload: dict) -> dict:
    username = "system"

    calc_sign = payload.get("calc_sign") or payload.get("Признак калькуляции")
    if calc_sign == "ФКСС":
        raise HTTPException(400, "Уровень цен запрещен для редактирования для признака калькуляции 'ФКСС'")

    # Build full row data for upsert (pending table stores full snapshot)
    row_data = {
        "Бренд-менеджер": payload.get("brand_manager"),
        "Модель": payload.get("model"),
        "Артикул": payload.get("articul"),
        "Признак калькуляции": calc_sign,
        "дата расчета": payload.get("date"),
        "Уровень цен": payload.get("price_level"),
        "Страна пр-ва": payload.get("country"),
        "Семья": payload.get("family"),
        "Сезон": payload.get("season"),
        "Level 01": payload.get("level01"),
        "Level 02": payload.get("level02"),
        "Level 03": payload.get("level03"),
        "Level 04": payload.get("level04"),
        "Level 05": payload.get("level05"),
        "Наименование модели": payload.get("model_name"),
        "Номер задания производства": payload.get("task_number"),
        "PLAN_ID": payload.get("plan_id"),
        "Розничная цена по уровню, руб.": payload.get("retail_rub"),
        "Отпускная цена по уровню, руб": payload.get("wholesale_rub"),
        "Розничная цена по уровню, USD.": payload.get("retail_usd"),
        "Отпускная цена по уровню, USD.": payload.get("wholesale_usd"),
        "Основные материалы, руб.": payload.get("materials_rub"),
        "Основные материалы, USD.": payload.get("materials_usd"),
        "Вспомогательные материалы, руб.": payload.get("aux_materials_rub"),
        "Вспомогательные материалы, USD.": payload.get("aux_materials_usd"),
        "Пошив, руб.": payload.get("sewing_rub"),
        "Пошив, USD.": payload.get("sewing_usd"),
        "Раскрой, руб.": payload.get("cutting_rub"),
        "Раскрой, USD.": payload.get("cutting_usd"),
        "Декоры, руб.": payload.get("decors_rub"),
        "Декоры, USD.": payload.get("decors_usd"),
        "Вязание, руб.": payload.get("knitting_rub"),
        "Вязание, USD.": payload.get("knitting_usd"),
        "Себестоимость, руб.": payload.get("cost_rub"),
        "Себестоимость, USD.": payload.get("cost_usd"),
    }

    if _is_mock():
        pending_id = await upsert_pending_change(row_data, username)
        return {"success": True, "mock": True, "pending_id": pending_id}

    pending_id = await upsert_pending_change(row_data, username)
    return {"success": True, "pending_id": pending_id}


def _row_data_from_payload(c: dict) -> dict:
    """Build full row snapshot dict from a change payload (for upsert into pending)."""
    calc_sign = c.get("calc_sign") or c.get("Признак калькуляции")
    return {
        "Бренд-менеджер": c.get("brand_manager"),
        "Модель": c.get("model"),
        "Артикул": c.get("articul"),
        "Признак калькуляции": calc_sign,
        "дата расчета": c.get("date"),
        "Уровень цен": c.get("price_level"),
        "Страна пр-ва": c.get("country"),
        "Семья": c.get("family"),
        "Сезон": c.get("season"),
        "Level 01": c.get("level01"),
        "Level 02": c.get("level02"),
        "Level 03": c.get("level03"),
        "Level 04": c.get("level04"),
        "Level 05": c.get("level05"),
        "Наименование модели": c.get("model_name"),
        "Номер задания производства": c.get("task_number"),
        "PLAN_ID": c.get("plan_id"),
        "Розничная цена по уровню, руб.": c.get("retail_rub"),
        "Отпускная цена по уровню, руб": c.get("wholesale_rub"),
        "Розничная цена по уровню, USD.": c.get("retail_usd"),
        "Отпускная цена по уровню, USD.": c.get("wholesale_usd"),
        "Основные материалы, руб.": c.get("materials_rub"),
        "Основные материалы, USD.": c.get("materials_usd"),
        "Вспомогательные материалы, руб.": c.get("aux_materials_rub"),
        "Вспомогательные материалы, USD.": c.get("aux_materials_usd"),
        "Пошив, руб.": c.get("sewing_rub"),
        "Пошив, USD.": c.get("sewing_usd"),
        "Раскрой, руб.": c.get("cutting_rub"),
        "Раскрой, USD.": c.get("cutting_usd"),
        "Декоры, руб.": c.get("decors_rub"),
        "Декоры, USD.": c.get("decors_usd"),
        "Вязание, руб.": c.get("knitting_rub"),
        "Вязание, USD.": c.get("knitting_usd"),
        "Себестоимость, руб.": c.get("cost_rub"),
        "Себестоимость, USD.": c.get("cost_usd"),
    }


@router.post("/save-batch")
async def save_batch_changes(payload: dict) -> dict:
    username = "system"
    changes = payload.get("changes") or []

    filtered = [
        c for c in changes
        if (c.get("calc_sign") or c.get("Признак калькуляции")) != "ФКСС"
    ]
    if not filtered:
        return {"success": False, "error": "Нет изменений для сохранения после фильтрации ФКСС", "count": 0}

    row_data_list = [_row_data_from_payload(c) for c in filtered]

    if _is_mock():
        pending_ids = await upsert_pending_changes_batch(row_data_list, username)
        return {"success": True, "count": len(pending_ids), "mock": True, "pending_ids": pending_ids}

    pending_ids = await upsert_pending_changes_batch(row_data_list, username)
    return {"success": True, "count": len(pending_ids), "pending_ids": pending_ids}


# ── Price approval workflow ────────────────────────────────────────────────────


@router.get("/pending-changes")
async def list_pending_changes() -> dict:
    """Return all pending changes."""
    return {"data": await get_pending_changes()}


@router.post("/pending-changes/apply")
async def apply_changes(payload: dict) -> dict:
    """Apply (approve) a batch of pending changes.

    Body: { "ids": [1, 2, 3] }
    - Записывает выбранные строки в OLAP CostHistory_Changes (с 4 новыми полями)
    - Дублирует в локальный audit
    - Удаляет строки из cost_price_pending
    """
    ids = payload.get("ids") or []
    if not ids:
        raise HTTPException(400, "ids list is required")
    reviewed_by = (payload.get("reviewed_by") or "system").strip()
    count = await apply_pending_changes(ids, reviewed_by)
    return {"success": True, "applied": count}


@router.post("/pending-changes/clear")
async def clear_changes() -> dict:
    """Delete ALL rows from cost_price_pending."""
    count = await clear_pending_changes()
    return {"success": True, "deleted": count}


# ── Margin targets ──────────────────────────────────────────────────────────


@router.get("/margin-targets")
async def margin_targets() -> list[dict]:
    """Return all saved margin targets keyed by level1."""
    if _is_mock():
        return mocks.margin_targets()
    return await get_margin_targets()


@router.post("/margin-targets")
async def update_margin_targets(payload: dict) -> dict:
    """Save margin targets (upsert by level1) with username tracking."""
    targets = payload.get("targets") or []
    username = (payload.get("username") or "system").strip()
    if _is_mock():
        return mocks.save_margin_targets(targets, username)
    await save_margin_targets(targets, username)
    return {"success": True, "count": len(targets)}


# ── Margin targets ──────────────────────────────────────────────────────────


@router.get("/margin-targets")
async def margin_targets() -> list[dict]:
    """Return all saved margin targets keyed by level1."""
    if _is_mock():
        return mocks.margin_targets()
    return await get_margin_targets()


@router.post("/margin-targets")
async def update_margin_targets(payload: dict) -> dict:
    """Save margin targets (upsert by level1) with username tracking."""
    targets = payload.get("targets") or []
    username = (payload.get("username") or "system").strip()
    if _is_mock():
        return mocks.save_margin_targets(targets, username)
    await save_margin_targets(targets, username)
    return {"success": True, "count": len(targets)}


# ── Cache refresh & status ──────────────────────────────────────────────────


@router.post("/refresh-cache")
async def refresh_cache() -> dict:
    """Принудительное обновление кеша из MSSQL v_CostHistory_MatchedOrLatest."""
    if _is_mock():
        return mocks.refresh_cache()

    status = await get_cache_status()
    if status and status["is_refreshing"]:
        REFRESH_TIMEOUT_MINUTES = 10
        refreshed_at = status.get("refreshed_at")
        if refreshed_at:
            age = (datetime.now(timezone.utc) - refreshed_at).total_seconds() / 60
            if age < REFRESH_TIMEOUT_MINUTES:
                return {"status": "already_refreshing", "message": "Cache refresh already in progress"}

    asyncio.ensure_future(load_cost_data_to_cache(partial_months=2))
    return {"status": "started", "message": "Cache refresh (last 2 months) started in background"}


@router.get("/cache-status")
async def cache_status() -> dict:
    """Текущее состояние кеша."""
    if _is_mock():
        return mocks.cache_status()

    status = await get_cache_status()
    if status is None:
        return {"refreshed_at": None, "row_count": 0, "is_refreshing": False, "error_message": "Cache not initialized"}
    return status
