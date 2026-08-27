from __future__ import annotations

import asyncio
import os
import sys
import traceback
import json
from datetime import date, datetime, timezone
from decimal import ROUND_HALF_UP, Decimal, InvalidOperation
from typing import Any

from fastapi import APIRouter, Depends, HTTPException, Request

from app import commercial, mocks
from app.db import (aggregate_plan_decors, aggregate_plan_materials, apply_plan_price_set, delete_plan_price_set, get_plan_price_set, list_plan_price_sets, save_plan_price_set, unapply_plan_price_set, add_mp_constants, apply_pending_changes, call_calc_sign_procedure, clear_pending_changes, clear_pending_changes_by_user, compute_mp_price, fetch_gpartner_internal_rate, fetch_gpartner_planned, fetch_olap_changes, get_cache_status, get_dwh_conn, get_gpartner_conn, get_latest_mp_constants, get_margin_targets, get_mssql_conn, get_olap_conn, get_pending_changes, get_pending_filter_options, list_mp_constants, load_cost_data_to_cache, pool, refresh_in_progress, acquire_or_reclaim_refresh_lock, save_margin_targets, upsert_pending_change, upsert_pending_changes_batch, checkout_calculation, save_version_draft, submit_version, approve_version, reject_version, get_active_version, delete_version, archive_versions_by_key, get_version_info, get_calc_state, reset_price_fields, delete_pending_by_key, delete_dwh_record, save_approval, save_approvals_batch, revoke_approval, revoke_approvals_batch, get_approval_status, get_raw_cache_rows, list_versions, get_version_rows, create_version, get_prev_stage_prices, get_max_calc_cost, get_user_table_prefs, save_user_table_prefs, get_reopened_keys, reopen_dwh_calculation, reopen_dwh_calculations_batch, revoke_dwh_reopen, list_dwh_reopens, get_price_history, backfill_price_history_from_olap)
from app.middleware import require_perm
from app.notify import notify_admins
from app.permissions import COST_PERMISSIONS
from app.roles import (
    assign_role,
    create_role,
    delete_role,
    get_roles,
    get_user_permissions,
    get_user_roles,
    remove_user_role,
    update_role,
)

router = APIRouter()


def _is_mock() -> bool:
    return os.environ.get("COST_MOCK", "").strip() == "1"


def _require_perm(permission: str):
    """Skip permission check in mock mode."""
    if _is_mock():
        return lambda: None
    return require_perm(permission)


def _require_any_perm(*permissions: str):
    """Skip permission check in mock mode. User needs ANY of the given permissions."""
    if _is_mock():
        return lambda: None
    async def _check(request: Request) -> str:
        email = request.headers.get("X-Cost-User", "")
        if not email:
            raise HTTPException(401, "Не передан заголовок X-Cost-User")
        perms = await get_user_permissions(email)
        for perm in permissions:
            if perm in perms:
                return email
        joined = ", ".join(permissions)
        raise HTTPException(403, f"Недостаточно прав: требуется одно из ({joined})")
    return _check


async def _is_cost_admin(user_email: str | None) -> bool:
    if not user_email:
        return False
    if user_email in ("cost-dev@local",):
        return True
    try:
        perms = await get_user_permissions(user_email)
        return "cost:admin" in perms
    except Exception:
        return False


async def _can_approve_or_peo(user_email: str | None) -> bool:
    """True if the user may approve (PEO) or mark PEO status — i.e. is NOT a 'pure' brand-manager."""
    if not user_email:
        return False
    if user_email in ("cost-dev@local",):
        # Test/dev super-user: behave as a non-approver to exercise brand-manager locks.
        return False
    try:
        perms = await get_user_permissions(user_email)
        return "cost:approve" in perms or "cost:peo_mark" in perms or "cost:admin" in perms
    except Exception:
        return False


async def _check_calc_locks(user_email: str, model: str, articul: str, calc_sign, plan_id, date_str, task_number=None) -> None:
    """Raise 403 if the calculation is locked for this user."""
    if await _is_cost_admin(user_email):
        return
    state = await get_calc_state(model, articul, calc_sign, plan_id, date_str, task_number)
    if state["has_dwh_record"]:
        raise HTTPException(403, "Калькуляция заблокирована: цена передана в DWH")
    if state["has_pending_price"]:
        raise HTTPException(403, "Калькуляция заблокирована: цена отправлена на согласование бренд-менеджером")


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


def _get_dwh_filter_options(selected: dict[str, list[str]]) -> dict:
    """Синхронный запрос к DWH для brand_manager и level01-05 (запускается в executor)."""
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

        return result
    finally:
        conn.close()


@router.get("/filter-options")
async def get_filter_options(request: Request) -> dict:
    """Возвращает значения фильтров из DWH.dim.groups с каскадом и plan_id из кеша."""
    selected: dict[str, list[str]] = {}
    for key in CASCADE_KEYS:
        values = request.query_params.getlist(key)
        if values:
            selected[key] = values

    if _is_mock():
        return mocks.get_filter_options(selected)

    # DWH-запросы (brand_manager, level01-05) — в threadpool, т.к. pyodbc синхронный
    loop = asyncio.get_event_loop()
    dwh_result = await loop.run_in_executor(None, _get_dwh_filter_options, selected)

    result: dict[str, Any] = {**dwh_result}

    # 3. Признак калькуляции (статический список)
    result["calc_sign"] = ["ПКПСС", "КПСС", "ПФКСС", "ФКСС"]

    # 4. PLAN_ID — distinct значения из cost_data_cache (postgres, надёжнее чем прямой MSSQL)
    try:
        async with pool().acquire() as conn:
            rows = await conn.fetch(
                'SELECT DISTINCT TRIM("PLAN_ID") AS val FROM cost_data_cache'
                ' WHERE "PLAN_ID" IS NOT NULL AND "PLAN_ID" != \'\' ORDER BY val'
            )
            result["plan_id"] = [row["val"] for row in rows if row["val"]]
    except Exception:
        result["plan_id"] = []

    return result


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
    "plan_id": "PLAN_ID",
}


@router.post("/load-data")
async def load_data(payload: dict, _: str = Depends(_require_perm("cost:view"))) -> dict:
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
    join = (
        'LEFT JOIN cost_calc_approvals ca'
        '  ON TRIM(cd."Модель") = ca.model'
        '  AND TRIM(cd."Артикул") = ca.articul'
        '  AND TRIM(cd."Признак калькуляции") = ca.calc_sign'
        '  AND TRIM(cd."PLAN_ID") = ca.plan_id'
        # Задание в ключе обязательно: без него строка с несогласованным
        # заданием подхватывала статус соседнего согласованного, и один и тот же
        # ряд показывал разный статус здесь и в /aggregated.
        '  AND TRIM(COALESCE(cd."Номер задания производства", \'\'))'
        '      = COALESCE(ca.task_number, \'\')'
    )

    async with pool().acquire() as conn:
        total = await conn.fetchval(
            f"SELECT COUNT(*) FROM cost_data_cache cd {join} WHERE {where}", *params
        ) or 0

        paginated_params = params + [limit, offset]
        query = (
            f'SELECT cd.*, ca.status AS peo_status, ca.approved_by AS peo_approved_by,'
            f'  ca.approved_at AS peo_approved_at'
            f' FROM cost_data_cache cd {join} WHERE {where}'
            f' ORDER BY cd.id LIMIT ${len(params) + 1} OFFSET ${len(params) + 2}'
        )
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
    "Номер задания производства",
    # Цвет постоянен внутри задания (проверено на 85 583 группах), поэтому
    # добавление в группировку не размножает строки агрегата.
    "color",
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
    "Курс на дату расчета",
    "Ставка НДС",
    "Пошив, минуты",
    "Раскрой, минуты",
]

AGG_SUM_FIELDS = [
    "Пошив, руб.",
    "Пошив, USD.",
    "Раскрой, руб.",
    "Раскрой, USD.",
    "Декоры, руб.",
    "Декоры, USD.",
    "Вязание, руб.",
    "Вязание, USD.",
    "Основные материалы, руб.",
    "Основные материалы, USD.",
    "Вспомогательные материалы, руб.",
    "Вспомогательные материалы, USD.",
]

# Поля-компоненты для расчёта себестоимости (сумма 6 статей)
SEBEST_COMPONENTS_RUB = [
    "sum_Пошив, руб.",
    "sum_Раскрой, руб.",
    "sum_Декоры, руб.",
    "sum_Вязание, руб.",
    "sum_Основные материалы, руб.",
    "sum_Вспомогательные материалы, руб.",
]
SEBEST_COMPONENTS_USD = [
    "sum_Пошив, USD.",
    "sum_Раскрой, USD.",
    "sum_Декоры, USD.",
    "sum_Вязание, USD.",
    "sum_Основные материалы, USD.",
    "sum_Вспомогательные материалы, USD.",
]

# Точность промежуточной агрегации себестоимости (миграция 0030). Бакеты в кэше
# теперь хранятся с 6 знаками — реальной точностью источника, — поэтому SUM() по
# ним точен. Складываем на Decimal, а не на float, и округляем ТОЛЬКО итог, до 4
# знаков; до копеек округляет уже UI при отображении. Раньше здесь было
# round(sum(float(...)), 2) — потери копеек набегали на каждой строке агрегата.
_AGG_QUANT = Decimal("0.0001")


def _sum_components(row: dict, fields: list[str]) -> float:
    """Точная (Decimal) сумма компонентов себестоимости, округлённая до 4 знаков."""
    total = Decimal(0)
    for f in fields:
        val = row.get(f)
        if val is None:
            continue
        try:
            total += val if isinstance(val, Decimal) else Decimal(str(val))
        except (InvalidOperation, ValueError, TypeError):
            continue
    return float(total.quantize(_AGG_QUANT, rounding=ROUND_HALF_UP))


@router.post("/aggregated")
async def get_aggregated(payload: dict, _: str = Depends(_require_perm("cost:view"))) -> dict:
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

    # Фильтр «только строки без оптовой цены» здесь НЕ применяется: цены не
    # хранятся в cost_data_cache, а накладываются на выдачу из pending/DWH ниже.
    # Условие в WHERE отсекало по пустой цене источника и пропускало калькуляции,
    # где цена уже установлена и видна в таблице. Фильтр перенесён после
    # наложения цен — см. блок «Фильтр по отсутствию отпускной цены».

    for key, col in MULTI_FILTER_COLUMNS.items():
        values = payload.get(key) or []
        if values and "all" not in values:
            placeholders = ",".join(f"${len(params) + i + 1}" for i in range(len(values)))
            where_parts.append(f'TRIM("{col}") IN ({placeholders})')
            params.extend(values)

    where = " AND ".join(where_parts) if where_parts else "TRUE"
    join = (
        'LEFT JOIN cost_calc_approvals ca'
        '  ON TRIM(cd."Модель") = ca.model'
        '  AND TRIM(cd."Артикул") = ca.articul'
        '  AND TRIM(cd."Признак калькуляции") = ca.calc_sign'
        '  AND TRIM(cd."PLAN_ID") = ca.plan_id'
        # Пустое задание в кэше — пустая СТРОКА, а не NULL (на dev: 2 582 строки
        # против 2 с NULL), поэтому сравнение без COALESCE теряло согласования
        # всех калькуляций без номера задания: '' IS NOT DISTINCT FROM NULL даёт
        # FALSE. С 24.08.2026 в cost_calc_approvals task_number NOT NULL
        # DEFAULT '' (миграция 0038), но COALESCE оставлен с обеих сторон — в
        # кэше NULL всё ещё встречается, он приходит из источника.
        '  AND TRIM(COALESCE(cd."Номер задания производства", \'\'))'
        '      = COALESCE(ca.task_number, \'\')'
    )
    # Порядок ветвей важен: «согласовано» только когда согласованы ВСЕ задания
    # калькуляции, дальше — самый весомый негативный сигнал. Возврат на
    # корректировку (миграция 0040) мягче отклонения, поэтому идёт после него;
    # без этой ветви статус 'returned' проваливался в ELSE и приходил как NULL —
    # оранжевый кружок в таблице не появлялся вовсе.
    select_parts.append(
        "CASE WHEN BOOL_AND(ca.status = 'approved') THEN 'approved'"
        "     WHEN BOOL_OR(ca.status = 'rejected') THEN 'rejected'"
        "     WHEN BOOL_OR(ca.status = 'returned') THEN 'returned'"
        "     ELSE NULL END AS peo_status"
    )
    query = f"SELECT {', '.join(select_parts)} FROM cost_data_cache cd {join} WHERE {where} GROUP BY {', '.join(f'cd."{f}"' for f in AGG_GROUP_FIELDS)}"

    async with pool().acquire() as conn:
        rows = await conn.fetch(query, *params)
        data = [dict(row) for row in rows]

    # ── Group-level approval status (for BM lock) ──────────────────────────
    # A row is _group_approved only when ALL task_numbers within the same
    # 4-tuple (model, articul, calc_sign, plan_id) have peo_status='approved'.
    _gkey = lambda r: (
        (r.get("Модель") or "").strip(),
        (r.get("Артикул") or "").strip(),
        (r.get("Признак калькуляции") or "").strip(),
        (r.get("PLAN_ID") or "").strip(),
    )
    _group_all_approved: dict[tuple, bool] = {}
    # Возврат на корректировку (статус 'returned', миграция 0040) тоже открывает
    # строку бренд-менеджеру: смысл возврата в том, чтобы он поправил цену.
    # Иначе ПЭО вернул бы калькуляцию, а править её было бы некому.
    _group_returned: dict[tuple, bool] = {}
    for row in data:
        k = _gkey(row)
        if k not in _group_all_approved:
            _group_all_approved[k] = True
            _group_returned[k] = False
        if row.get("peo_status") != "approved":
            _group_all_approved[k] = False
        if row.get("peo_status") == "returned":
            _group_returned[k] = True
    for row in data:
        k = _gkey(row)
        row["_group_returned"] = _group_returned.get(k, False)
        row["_group_approved"] = (
            _group_all_approved.get(k, False) or _group_returned.get(k, False)
        )

    for row in data:
        row["sum_Себестоимость, руб."] = _sum_components(row, SEBEST_COMPONENTS_RUB)
        row["sum_Себестоимость, USD."] = _sum_components(row, SEBEST_COMPONENTS_USD)

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

    # ── Fetch price-levels reference once (used by Task 1 below + Gpartner fallback) ──
    price_levels: list[dict] = []
    try:
        loop = asyncio.get_event_loop()
        price_levels = await loop.run_in_executor(None, _get_price_levels_sync)
    except Exception:
        pass

    # ── Price levels → price_rf/kz/uz (Task 1) ───────────────────────────
    try:
        pl_map: dict[str, dict] = {pl["name"]: pl for pl in price_levels}
        for row in data:
            pl_name = str(row.get("Уровень цен", "") or "").strip()
            matched = pl_map.get(pl_name)
            if matched:
                row["price_rf"] = matched.get("price_type4")
                row["price_kz"] = matched.get("price_type5")
                row["price_uz"] = matched.get("price_type6")
    except Exception:
        pass

    # ── Плановые цены (planned_retail/planned_wholesale) — read-only (Task 3) ──
    try:
        # Collect distinct (model, articul, plan_id, calc_sign) that need lookups
        need_chain: list[tuple[str, str, str, str]] = []
        for row in data:
            cs = str(row.get("Признак калькуляции", "") or "").strip()
            if cs in ("КПСС", "ПФКСС", "ФКСС"):
                m = str(row.get("Модель", "") or "").strip()
                a = str(row.get("Артикул", "") or "").strip()
                p = str(row.get("PLAN_ID", "") or "").strip()
                if m and a:
                    need_chain.append((m, a, p, cs))

        if need_chain:
            # Pre-fetch latest retail/wholesale for each source calc_sign
            source_signs: dict[str, str] = {"КПСС": "ПКПСС", "ПФКСС": "КПСС", "ФКСС": "ПФКСС"}
            lookup_cache: dict[tuple[str, str, str], tuple[float | None, float | None]] = {}

            for target_cs, source_cs in source_signs.items():
                pairs = [(m, a, p) for (m, a, p, cs) in need_chain if cs == target_cs]
                if not pairs:
                    continue

                # $1 занят признаком калькуляции, поэтому ключи нумеруются С $2.
                # Раньше и признак, и первая модель претендовали на $1: asyncpg
                # отвечал «server expects N arguments, N+1 were passed», запрос
                # падал ВСЕГДА при непустом наборе пар, а внешний except это
                # проглатывал. В итоге вся цепочка плановых цен
                # (ПКПСС → КПСС → ПФКСС → ФКСС) не работала ни разу, и planned_*
                # заполнялись только фолбэком из Gpartner. Проверено 27.08.2026:
                # с правильной нумерацией по парам, которые есть и в ПКПСС, и в
                # КПСС, цены находятся (например 112866 / 26-47073Б-0 —
                # розница 19.99, опт 11.90).
                #
                # Батчами — предел 32767 параметров, см. KEYS_PER_QUERY.
                async with pool().acquire() as conn:
                    if target_cs == "КПСС":
                        # КПСС → ПКПСС: сопоставление по модели и артикулу, без плана
                        for _batch in _key_batches(pairs):
                            values_list = ", ".join(
                                f"(${i*2+2}::text, ${i*2+3}::text)" for i in range(len(_batch))
                            )
                            flat_params: list[str] = []
                            for m, a, _ in _batch:
                                flat_params.extend([m, a])

                            rows = await conn.fetch(
                                f"""SELECT DISTINCT ON (cd."Модель", cd."Артикул")
                                    cd."Модель", cd."Артикул",
                                    cd."Розничная цена по уровню, руб.",
                                    cd."Отпускная цена по уровню, руб"
                                    FROM cost_data_cache cd
                                    WHERE cd."Признак калькуляции" = $1
                                      AND (cd."Модель", cd."Артикул") IN (VALUES {values_list})
                                    ORDER BY cd."Модель", cd."Артикул", cd."дата расчета" DESC
                                """,
                                source_cs,
                                *flat_params,
                            )
                            for r in rows:
                                key = (str(r["Модель"]).strip(), str(r["Артикул"]).strip(), "")
                                lookup_cache[key] = (
                                    r["Розничная цена по уровню, руб."],
                                    r["Отпускная цена по уровню, руб"],
                                )
                    else:
                        # ПФКСС → КПСС / ФКСС → ПФКСС: сопоставление с планом
                        for _batch in _key_batches(pairs):
                            values_list = ", ".join(
                                f"(${i*3+2}::text, ${i*3+3}::text, ${i*3+4}::text)"
                                for i in range(len(_batch))
                            )
                            flat_params = []
                            for m, a, pl in _batch:
                                flat_params.extend([m, a, pl])

                            rows = await conn.fetch(
                                f"""SELECT DISTINCT ON (cd."Модель", cd."Артикул", cd."PLAN_ID")
                                    cd."Модель", cd."Артикул", cd."PLAN_ID",
                                    cd."Розничная цена по уровню, руб.",
                                    cd."Отпускная цена по уровню, руб"
                                    FROM cost_data_cache cd
                                    WHERE cd."Признак калькуляции" = $1
                                      AND (cd."Модель", cd."Артикул", cd."PLAN_ID") IN (VALUES {values_list})
                                    ORDER BY cd."Модель", cd."Артикул", cd."PLAN_ID", cd."дата расчета" DESC
                                """,
                                source_cs,
                                *flat_params,
                            )
                            for r in rows:
                                key = (
                                    str(r["Модель"]).strip(), str(r["Артикул"]).strip(),
                                    str(r["PLAN_ID"]).strip(),
                                )
                                lookup_cache[key] = (
                                    r["Розничная цена по уровню, руб."],
                                    r["Отпускная цена по уровню, руб"],
                                )
            # Assign planned prices
            for row in data:
                cs = str(row.get("Признак калькуляции", "") or "").strip()
                if cs not in source_signs:
                    continue
                m = str(row.get("Модель", "") or "").strip()
                a = str(row.get("Артикул", "") or "").strip()
                p = str(row.get("PLAN_ID", "") or "").strip()

                if cs == "КПСС":
                    key = (m, a, "")
                else:
                    key = (m, a, p)

                prices = lookup_cache.get(key)
                if prices:
                    row["planned_retail"] = prices[0]
                    row["planned_wholesale"] = prices[1]
    except Exception:
        pass

    # Кэш ответов Gpartner S_MODELI (включая ru_nds) по (model, articul) —
    # переиспользуется ниже для «Цена для МП», чтобы не дублировать round-trip
    # на те же ключи.
    gpartner_cache: dict[tuple[str, str], dict] = {}

    # ── Плановые цены — fallback Gpartner S_MODELI (только КПСС/ПФКСС) ──────
    try:
        need_gpartner: list[tuple[str, str]] = []
        for row in data:
            cs = str(row.get("Признак калькуляции", "") or "").strip()
            if cs not in ("КПСС", "ПФКСС"):
                continue
            if row.get("planned_retail") is not None and row.get("planned_wholesale") is not None:
                continue
            m = str(row.get("Модель", "") or "").strip()
            a = str(row.get("Артикул", "") or "").strip()
            if m and a:
                need_gpartner.append((m, a))

        if need_gpartner:
            loop = asyncio.get_event_loop()
            gpartner_map = await loop.run_in_executor(None, fetch_gpartner_planned, need_gpartner)
            gpartner_cache.update(gpartner_map)
            # PRICE_TYPE1 → [PRICE_TYPE3, ...] (round for float-safe key match)
            pt1_to_pt3: dict[float, list[float]] = {}
            for pl in price_levels:
                pt1_to_pt3.setdefault(round(pl["price_type1"], 2), []).append(pl["price_type3"])

            for row in data:
                cs = str(row.get("Признак калькуляции", "") or "").strip()
                if cs not in ("КПСС", "ПФКСС"):
                    continue
                m = str(row.get("Модель", "") or "").strip()
                a = str(row.get("Артикул", "") or "").strip()
                gp = gpartner_map.get((m, a))
                if not gp:
                    continue

                price_mopt = gp.get("price_mopt")
                nnds = gp.get("nnds")
                plan_price = gp.get("plan_price")

                if row.get("planned_wholesale") is None and price_mopt is not None:
                    row["planned_wholesale"] = price_mopt

                if row.get("planned_retail") is None and price_mopt is not None:
                    candidates = pt1_to_pt3.get(round(price_mopt, 2), [])
                    if len(candidates) == 1:
                        row["planned_retail"] = candidates[0]
                    elif len(candidates) > 1:
                        level1 = str(row.get("Level 01", "") or "").strip()
                        mult = 1.3 if level1 in ("Девочкам", "Мальчикам") else 1.4
                        target = price_mopt * mult * (1 + (nnds or 0) / 100)
                        row["planned_retail"] = min(candidates, key=lambda c: abs(c - target))

                if row.get("planned_cost") is None and plan_price is not None:
                    row["planned_cost"] = plan_price
    except Exception:
        pass

    # ── Цена для МП, рос. руб. — живой предпросмотр (см. discussion с пользователем:
    # окончательное значение считается заново в apply_pending_changes на момент
    # утверждения, здесь — просто предпросмотр той же формулы "как сейчас").
    # internal_rate/mp_constants отдаются на верхнем уровне ответа (return ниже) —
    # фронтенд пересчитывает mp_price_rub сам при ручном выборе цены/наценки.
    internal_rate = None
    mp_constants = None
    # Верхний предел на ДОПОЛНИТЕЛЬный round-trip в Gpartner ради ru_nds — при
    # неотфильтрованной выгрузке (все 84k+ строк) уникальных пар может быть
    # 20-30 тысяч, что при батчах по 500 даёт десятки последовательных
    # запросов к живому серверу через VPN (замерено: ~90 сек на полный датасет).
    # Свыше лимита — не тянем ru_nds для "хвоста", эти строки останутся без
    # "Цена для МП" (не критично для бесфильтрового обзора), лишь бы не вешать
    # загрузку целиком. При нормальном отфильтрованном просмотре (артикул/план)
    # пар мало, лимит не мешает.
    _MP_EXTRA_GPARTNER_PAIRS_LIMIT = 500
    try:
        missing_pairs = list({
            (m, a)
            for row in data
            if (m := str(row.get("Модель", "") or "").strip())
            and (a := str(row.get("Артикул", "") or "").strip())
            and (m, a) not in gpartner_cache
        })
        if missing_pairs and len(missing_pairs) <= _MP_EXTRA_GPARTNER_PAIRS_LIMIT:
            loop = asyncio.get_event_loop()
            extra_map = await loop.run_in_executor(None, fetch_gpartner_planned, missing_pairs)
            gpartner_cache.update(extra_map)

        if gpartner_cache:
            loop = asyncio.get_event_loop()
            internal_rate = await loop.run_in_executor(None, fetch_gpartner_internal_rate)
            mp_constants = await get_latest_mp_constants()

        if internal_rate is not None and mp_constants is not None:
            for row in data:
                m = str(row.get("Модель", "") or "").strip()
                a = str(row.get("Артикул", "") or "").strip()
                gp = gpartner_cache.get((m, a))
                ru_nds = gp.get("ru_nds") if gp else None
                # Отдаём ru_nds на строке — фронтенд пересчитывает формулу сам,
                # когда пользователь вручную выбирает уровень цены/наценку
                # (avg_Отпускная цена по уровню, руб меняется на клиенте ещё до
                # сохранения — см. onMarkupSelect/onRetailPriceSelect в index.vue).
                row["mp_ru_nds"] = ru_nds
                # У план-типов (КПСС/ПФКСС/ФКСС) "Отпускная цена по уровню, руб"
                # в cost_data_cache часто NULL — реальная цена приходит через
                # тот же фоллбэк, что и planned_wholesale (Task 3/Gpartner выше).
                wholesale = row.get("avg_Отпускная цена по уровню, руб")
                if wholesale is None:
                    wholesale = row.get("planned_wholesale")
                row["mp_price_rub"] = compute_mp_price(
                    wholesale,
                    internal_rate,
                    mp_constants["markup_mp"],
                    mp_constants["expense_pct_mp"],
                    ru_nds,
                    mp_constants["spp_discount"],
                )
    except Exception:
        print("[mp-price] failed to compute:", traceback.format_exc())

    # ── Override prices/comment from FinSandBox (OLAP, primary) + pending ──
    try:
        all_keys: list[tuple[str, str, str, str]] = []
        for row in data:
            m = str(row.get("Модель", "") or "").strip()
            a = str(row.get("Артикул", "") or "").strip()
            cs = str(row.get("Признак калькуляции", "") or "").strip()
            pi = str(row.get("PLAN_ID", "") or "").strip()
            if m and a:
                all_keys.append((m, a, cs, pi))

        if all_keys:
            keys = list(set(all_keys))
            override_map: dict[tuple[str, str, str, str], dict[str, Any]] = {}

            # 1. PENDING (local, unapproved) — highest priority
            async with pool().acquire() as conn:
                # Батчами: иначе на широких срезах запрос упирается в предел
                # 32767 параметров и цены не накладываются вовсе (см. KEYS_PER_QUERY).
                pending_rows = []
                for batch in _key_batches(keys):
                    pending_ph = ", ".join(
                        f"(${i*4+1}::text, ${i*4+2}::text, ${i*4+3}::text, ${i*4+4}::text)"
                        for i in range(len(batch))
                    )
                    pending_params: list[str] = []
                    for m, a, cs, pi in batch:
                        pending_params.extend([m, a, cs, pi])

                    pending_rows.extend(await conn.fetch(
                        f"""SELECT DISTINCT ON ("Модель", "Артикул", "PLAN_ID", "Признак калькуляции")
                            "Модель", "Артикул", "PLAN_ID", "Признак калькуляции",
                            "Уровень цен",
                            "Розничная цена по уровню, руб.", "Отпускная цена по уровню, руб",
                            "Цена РФ", "Цена КЗ", "Цена УЗ", "Комментарий"
                            FROM cost_price_pending
                            WHERE ("Модель", "Артикул", "Признак калькуляции", "PLAN_ID") IN (VALUES {pending_ph})
                            ORDER BY "Модель", "Артикул", "PLAN_ID", "Признак калькуляции", created_at DESC
                        """,
                        *pending_params,
                    ))
                for r in pending_rows:
                    key = (
                        str(r["Модель"]).strip(), str(r["Артикул"]).strip(),
                        str(r["Признак калькуляции"]).strip(), str(r["PLAN_ID"]).strip(),
                    )
                    override_map[key] = {
                        "price_level": str(r["Уровень цен"] or "").strip() or None,
                        "retail_rub": r["Розничная цена по уровню, руб."],
                        "wholesale_rub": r["Отпускная цена по уровню, руб"],
                        "price_rf": r["Цена РФ"], "price_kz": r["Цена КЗ"], "price_uz": r["Цена УЗ"],
                        "comment": r["Комментарий"] or "",
                    }

            # 2. FinSandBox.CostHistory_Changes (OLAP, primary approved source)
            olap_ok = False
            if os.environ.get("COST_MOCK", "").strip() != "1":
                try:
                    loop = asyncio.get_event_loop()
                    olap_records = await loop.run_in_executor(None, fetch_olap_changes, keys)
                    for rec in olap_records:
                        key = (
                            str(rec.get("Модель") or "").strip(),
                            str(rec.get("Артикул") or "").strip(),
                            str(rec.get("calc_sign") or "").strip(),
                            str(rec.get("plan_id") or "").strip(),
                        )
                        if key not in override_map:
                            override_map[key] = {
                                "price_level": str(rec.get("price_level") or "").strip() or None,
                                "retail_rub": rec.get("retail_rub"),
                                "wholesale_rub": rec.get("wholesale_rub"),
                                "price_rf": rec.get("price_rf"),
                                "price_kz": rec.get("price_kz"),
                                "price_uz": rec.get("price_uz"),
                                "comment": str(rec.get("comment") or ""),
                            }
                    olap_ok = True
                except Exception:
                    # Молчать здесь нельзя: откат на локальный аудит меняет то,
                    # что видит пользователь (аудит хранит лишь последнюю запись
                    # на пару модель+артикул, без учёта признака и плана), и
                    # выглядит как «цены пропали». Именно так тихо падал запрос
                    # по лимиту параметров ODBC на широких выборках.
                    print(
                        f"[cost] fetch_olap_changes упал на {len(keys)} ключах — "
                        f"откат на локальный аудит:\n{traceback.format_exc()}",
                        flush=True,
                    )

            # 3. LOCAL AUDIT — запасной источник, когда OLAP недоступен.
            #
             # Сюда попадают реальные отказы: 27.08.2026 в логах
            # «fetch_olap_changes упал на 20 ключах — откат на локальный аудит»,
            # причина HYT00 Login timeout expired. То есть ветка рабочая, и от её
            # точности напрямую зависит, увидит ли пользователь свои цены.
            #
            # Раньше здесь было две ошибки. Первая: ключом служила пара
            # (модель, артикул) — одна запись раздавалась ВСЕМ признакам
            # калькуляции и всем планам этой пары, то есть цена КПСС могла
            # подмениться ценой ПФКСС. Вторая: в SELECT не было price_level,
            # хотя код ниже его читает, — уровень цен всегда оставался пустым.
            # Теперь ключ полный, как в основной ветке, и уровень выбирается.
            if not olap_ok:
                async with pool().acquire() as conn:
                    audit_rows = []
                    for batch in _key_batches(keys):
                        audit_ph = ", ".join(
                            f"(${i*4+1}::text, ${i*4+2}::text, ${i*4+3}::text, ${i*4+4}::text)"
                            for i in range(len(batch))
                        )
                        audit_params: list[str] = []
                        for m, a, cs, pi in batch:
                            audit_params.extend([m, a, cs, pi])
                        audit_rows.extend(await conn.fetch(
                            f"""SELECT DISTINCT ON (model, articul, COALESCE(calc_sign, ''), COALESCE(plan_id, ''))
                                model, articul, calc_sign, plan_id, price_level,
                                retail_rub, wholesale_rub,
                                price_rf, price_kz, price_uz, comment
                                FROM cost_price_changes_audit
                                WHERE (model, articul, COALESCE(calc_sign, ''), COALESCE(plan_id, ''))
                                      IN (VALUES {audit_ph})
                                ORDER BY model, articul, COALESCE(calc_sign, ''), COALESCE(plan_id, ''),
                                         changed_at DESC
                            """,
                            *audit_params,
                        ))

                    audit_map: dict[tuple[str, str, str, str], dict] = {}
                    for r in audit_rows:
                        k = (
                            str(r["model"]).strip(), str(r["articul"]).strip(),
                            str(r["calc_sign"] or "").strip(), str(r["plan_id"] or "").strip(),
                        )
                        if k not in audit_map:
                            audit_map[k] = dict(r)

                    for ky in keys:
                        if ky in override_map:
                            continue
                        rec = audit_map.get(ky)
                        if rec:
                            override_map[ky] = {
                                "price_level": str(rec.get("price_level") or "").strip() or None,
                                "retail_rub": rec.get("retail_rub"),
                                "wholesale_rub": rec.get("wholesale_rub"),
                                "price_rf": rec.get("price_rf"),
                                "price_kz": rec.get("price_kz"),
                                "price_uz": rec.get("price_uz"),
                                "comment": str(rec.get("comment") or ""),
                            }
            # Apply overrides to ALL rows
            for row in data:
                m = str(row.get("Модель", "") or "").strip()
                a = str(row.get("Артикул", "") or "").strip()
                cs = str(row.get("Признак калькуляции", "") or "").strip()
                pi = str(row.get("PLAN_ID", "") or "").strip()
                ky = (m, a, cs, pi)
                rec = override_map.get(ky)
                if not rec:
                    continue

                if rec.get("price_level"):
                    row["Уровень цен"] = rec["price_level"]
                if rec.get("retail_rub") is not None:
                    row["avg_Розничная цена по уровню, руб."] = rec["retail_rub"]
                if rec.get("wholesale_rub") is not None:
                    row["avg_Отпускная цена по уровню, руб"] = rec["wholesale_rub"]
                if rec.get("price_rf") is not None:
                    row["price_rf"] = rec["price_rf"]
                if rec.get("price_kz") is not None:
                    row["price_kz"] = rec["price_kz"]
                if rec.get("price_uz") is not None:
                    row["price_uz"] = rec["price_uz"]
                if rec.get("comment"):
                    row["comment"] = rec["comment"]
    except Exception:
        # Падение здесь означает, что НИ ОДНА утверждённая цена не наложится на
        # выдачу — пользователь увидит пустые цены при полностью корректных
        # данных в DWH. Такое обязано быть видно в логах.
        print(f"[cost] наложение утверждённых цен не выполнено:\n{traceback.format_exc()}", flush=True)

    # ── Lock state: _has_pending / _has_audit / _lock_reason ────────────────
    try:
        lock_keys = set()
        for row in data:
            m = str(row.get("Модель", "") or "").strip()
            a = str(row.get("Артикул", "") or "").strip()
            cs = str(row.get("Признак калькуляции", "") or "").strip()
            pi = str(row.get("PLAN_ID", "") or "").strip()
            if m and a:
                lock_keys.add((m, a, cs, pi))

        if lock_keys:
            lock_list = list(lock_keys)

            # 1. Pending changes — full tuple match
            # Батчами — тот же предел 32767 параметров, что и в наложении цен.
            pend_rows = []
            async with pool().acquire() as conn:
                for batch in _key_batches(lock_list):
                    pend_ph = ", ".join(
                        f"(${i*4+1}::text, ${i*4+2}::text, ${i*4+3}::text, ${i*4+4}::text)"
                        for i in range(len(batch))
                    )
                    pend_params: list[str] = []
                    for m, a, cs, pi in batch:
                        pend_params.extend([m, a, cs, pi])
                    pend_rows.extend(await conn.fetch(
                        f"""SELECT DISTINCT "Модель", "Артикул", "Признак калькуляции", "PLAN_ID"
                            FROM cost_price_pending
                            WHERE ("Модель", "Артикул", "Признак калькуляции", "PLAN_ID") IN (VALUES {pend_ph})""",
                        *pend_params,
                    ))
            pending_set: set[tuple[str, str, str, str]] = set()
            for r in pend_rows:
                pending_set.add((
                    str(r["Модель"]).strip(), str(r["Артикул"]).strip(),
                    str(r["Признак калькуляции"]).strip(), str(r["PLAN_ID"]).strip(),
                ))

            # 2. Audit records — запись в DWH идёт по калькуляции целиком, поэтому
            # сверяем полный ключ (model, articul, calc_sign, plan_id). Раньше
            # сверялась только пара model+articul, и запись по КПСС (в DWH попадает
            # только он) помечала «записано в DWH» и блокировала ПФКСС/ФКСС той же
            # модели — там ни цены править, ни статус ПЭО поставить было нельзя.
            #
            # У записей до миграции 0022 признака и плана нет: для них оставляем
            # прежнее поведение, иначе с легаси-строк блокировка исчезла бы вовсе.
            audit_pairs = list(set((m, a) for m, a, _, _ in lock_list))
            audit_rows = []
            async with pool().acquire() as conn:
                for batch in _key_batches(audit_pairs):
                    audit_ph = ", ".join(
                        f"(${i*2+1}::text, ${i*2+2}::text)" for i in range(len(batch))
                    )
                    audit_params: list[str] = []
                    for m, a in batch:
                        audit_params.extend([m, a])
                    audit_rows.extend(await conn.fetch(
                        f"""SELECT DISTINCT model, articul, calc_sign, plan_id
                            FROM cost_price_changes_audit
                            WHERE (model, articul) IN (VALUES {audit_ph})""",
                        *audit_params,
                    ))
            audit_set: set[tuple[str, str, str, str]] = set()
            audit_legacy: set[tuple[str, str]] = set()
            for r in audit_rows:
                m_a = (str(r["model"] or "").strip(), str(r["articul"] or "").strip())
                cs = str(r["calc_sign"] or "").strip()
                pi = str(r["plan_id"] or "").strip()
                if cs and pi:
                    audit_set.add((*m_a, cs, pi))
                else:
                    audit_legacy.add(m_a)

            # 3. Переоткрытые админом калькуляции (миграция 0039). Запись в DWH
            # остаётся на месте, но блокировку с строки снимаем: админ разрешил
            # второй цикл «правка → ПЭО → установка цен». Разрешение истекает
            # само, как только цены установлены заново.
            reopened_keys = await get_reopened_keys(lock_list)

            for row in data:
                m = str(row.get("Модель", "") or "").strip()
                a = str(row.get("Артикул", "") or "").strip()
                cs = str(row.get("Признак калькуляции", "") or "").strip()
                pi = str(row.get("PLAN_ID", "") or "").strip()

                has_pending = (m, a, cs, pi) in pending_set
                has_audit = (m, a, cs, pi) in audit_set or (m, a) in audit_legacy
                is_reopened = (m, a, cs, pi) in reopened_keys

                row["_has_pending"] = has_pending
                # Значок «записано в DWH» оставляем — факт записи никуда не
                # делся, — но отдельным флагом сообщаем фронту, что правка
                # разрешена. Так видно и то, что калькуляция уже в DWH, и то,
                # что она открыта на исправление.
                row["_has_audit"] = has_audit and not is_reopened
                row["_in_dwh"] = has_audit
                row["_reopened"] = is_reopened

                # Возвращённая на корректировку калькуляция НЕ блокируется
                # незакрытой заявкой. Заявка там и остаётся специально: с
                # 25.08.2026 возврат сохраняет введённую бренд-менеджером цену
                # вместо обнуления, и он же должен её править. Без этого условия
                # ПЭО отправляла строку на корректировку, а у бренд-менеджера
                # она оставалась неактивной — жалоба 26.08.2026 по плану 9518.
                is_returned = row.get("peo_status") == "returned"
                if has_pending and not is_returned:
                    row["_lock_reason"] = "pending_changes"
                else:
                    row["_lock_reason"] = None
    except Exception:
        pass  # lock state is advisory — don't break the page

    # ── Фильтр по отсутствию отпускной цены ────────────────────────────────
    # Считаем по ИТОГОВОМУ значению, то есть после наложения цен из
    # cost_price_pending и CostHistory_Changes: в самом кэше отпускной цены нет
    # почти нигде, и фильтр по источнику показывал строки с уже установленной
    # ценой (в таблице у них заполнен «Сред. опт» и стоит значок 📤).
    if payload.get("no_wholesale_only"):
        def _has_no_wholesale(row: dict) -> bool:
            value = row.get("avg_Отпускная цена по уровню, руб")
            if value is None:
                return True
            try:
                return float(value) == 0
            except (TypeError, ValueError):
                return True

        data = [r for r in data if _has_no_wholesale(r)]

    # ── Filter by PEO approval status (sent by frontend peoFilter) ─────────
    peo_filter = (payload.get("peo_filter") or "").strip()
    if peo_filter and peo_filter != "all":
        if peo_filter == "none":
            data = [r for r in data if r.get("peo_status") is None]
        else:
            data = [r for r in data if r.get("peo_status") == peo_filter]

    mp_formula_inputs = None
    if internal_rate is not None and mp_constants is not None:
        mp_formula_inputs = {
            "internal_rate": internal_rate,
            "markup_mp": float(mp_constants["markup_mp"]),
            "expense_pct_mp": float(mp_constants["expense_pct_mp"]),
            "spp_discount": float(mp_constants["spp_discount"]),
        }
    return {"data": data, "count": len(data), "mp_formula_inputs": mp_formula_inputs}


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
            MAX("дата производства") AS "Дата выпуска",
            "Признак калькуляции",
            TRIM("Модель") AS "Модель",
            TRIM("Артикул") AS "Артикул",
            TRIM("Наименование модели") AS "Наименование модели",
            TRIM("Номер задания производства") AS "Номер задания производства",
            COALESCE(AVG("Розничная цена по уровню, руб."), 0) AS "Розничная цена, руб.",
            COALESCE(AVG("Отпускная цена по уровню, руб"), 0) AS "Оптовая цена, руб.",
            COALESCE(SUM("Основные материалы, руб."), 0) AS "Осн. материалы, руб.",
            COALESCE(SUM("Вспомогательные материалы, руб."), 0) AS "Вспом. материалы, руб.",
            COALESCE(SUM("Пошив, руб."), 0) AS "Пошив, руб.",
            COALESCE(SUM("Раскрой, руб."), 0) AS "Раскрой, руб.",
            COALESCE(SUM("Декоры, руб."), 0) AS "Декор, руб.",
            COALESCE(SUM("Вязание, руб."), 0) AS "Вязание, руб."
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


# ── Raw rows (исходные строки по агрегированной строке) ─────────────────────


@router.post("/raw-rows")
async def get_raw_rows(payload: dict) -> dict:
    """Сырые строки из кеша, отфильтрованные по полям группировки агрегированной строки."""
    if _is_mock():
        return mocks.raw_rows(payload)

    params: list[Any] = []
    where_parts: list[str] = []
    parsed_date = None

    for field in AGG_GROUP_FIELDS:
        value = payload.get(field)
        if value is not None and value != "" and value != "—":
            if field == "дата расчета" and isinstance(value, str):
                value = date.fromisoformat(value.split("T")[0])
                parsed_date = value
            where_parts.append(f'"{field}" = ${len(params) + 1}')
            params.append(value)

    where = " AND ".join(where_parts) if where_parts else "TRUE"
    query = f"SELECT * FROM cost_data_cache WHERE {where} ORDER BY id"

    try:
        async with pool().acquire() as conn:
            rows = await conn.fetch(query, *params)
            data = [dict(row) for row in rows]
            version = await get_active_version(
                payload.get("Модель"), payload.get("Артикул"),
                payload.get("Признак калькуляции") or None,
                payload.get("PLAN_ID") or None,
                parsed_date or payload.get("дата расчета"),
                payload.get("Номер задания производства") or None,
            )
            if version:
                ver_rows = await conn.fetch(
                    "SELECT * FROM cost_calc_version_rows WHERE version_id=$1 ORDER BY sort_order",
                    version["version"]["id"],
                )
                if ver_rows:
                    data = [dict(r) for r in ver_rows]
                    return {"data": data, "count": len(data), "version_id": version["version"]["id"], "version_status": version["version"]["status"]}
        return {"data": data, "count": len(data)}
    except Exception as e:
        import traceback
        detail = f"[raw-rows] query={query!r} params={params!r} error={e}"
        print(detail)
        traceback.print_exc()
        from fastapi import HTTPException
        raise HTTPException(status_code=500, detail=detail)


# ── Price levels ─────────────────────────────────────────────────────────────


def _get_price_levels_sync() -> list[dict]:
    """Синхронный запрос справочника уровней цен из Gpartner."""
    conn = get_gpartner_conn()
    cursor = conn.cursor()
    try:
        cursor.execute(
            "SELECT ITEM_ID, NAME, PRICE_TYPE1, PRICE_TYPE3, PRICE_TYPE4, PRICE_TYPE5, PRICE_TYPE6 FROM [dbo].[s_price_level] ORDER BY NAME"
        )
        return [
            {
                "id": int(row[0] or 0),
                "name": (row[1] or "").strip(),
                "price_type1": float(row[2] or 0),
                "price_type3": float(row[3] or 0),
                "price_type4": float(row[4] or 0),
                "price_type5": float(row[5] or 0),
                "price_type6": float(row[6] or 0),
            }
            for row in cursor.fetchall()
        ]
    finally:
        conn.close()


@router.get("/commercial")
async def commercial_dashboard(
    request: Request, _: str = Depends(_require_perm("cost:view"))
) -> dict:
    """Весь дашборд коммерческой эффективности ОДНИМ запросом.

    Плитки, три серии и метаданные приходят вместе. Тот же макет в Superset
    разошёлся на двадцать с лишним независимых запросов — по одному на виджет
    и на каждый фильтр, — и каждый заново сканировал витрину.

    Обе валюты в ответе: переключатель BYN/USD на фронте не ходит на сервер.

    Списочные фильтры принимаются повторяющимся параметром (?season=SS2025&
    season=SS2026), включая год и месяц (?year=2026&month=07). Всё остальное
    игнорируется: имена колонок берутся из белого списка, а не из запроса.

    structure_path — путь проваливания по иерархии для графика структуры
    себестоимости, тоже повторяющимся параметром и в порядке уровней
    (?structure_path=Иванов&structure_path=Одежда).

    date_basis — по какой дате считать год, месяц и динамику: production
    (дата производства, по умолчанию) или calc (дата расчёта).
    """
    if _is_mock():
        return mocks.commercial_dashboard()

    qp = request.query_params
    filters: dict = {key: qp.getlist(key) for key in commercial.FILTER_KEYS if qp.getlist(key)}

    try:
        return await commercial.dashboard(
            filters,
            dimension=(qp.get("dimension") or "model_name").strip(),
            measure=(qp.get("measure") or "volume_pcs").strip(),
            structure_path=qp.getlist("structure_path"),
            date_basis=(qp.get("date_basis") or commercial.DEFAULT_DATE_BASIS).strip(),
        )
    except ValueError as exc:
        # Измерение или мера вне белого списка — это ошибка клиента, а не 500.
        raise HTTPException(400, str(exc))


@router.get("/price-levels")
def get_price_levels() -> list[dict]:
    if _is_mock():
        return mocks.PRICE_LEVELS
    return _get_price_levels_sync()


# ── Save changes ─────────────────────────────────────────────────────────────


async def _check_save_locks(
    user_email: str,
    rows: list[dict],
) -> list[dict]:
    """Check if any of the given rows are locked for this user.

    Returns list of lock info dicts for locked rows.  Empty list = all clear.
    Each lock info: {model, articul, calc_sign, plan_id, reason}
    """
    # Full Admin bypass — cost:admin permission can override all locks.
    # Note: cost-dev@local is intentionally NOT bypassed here anymore; it is a
    # brand-manager test identity and must exercise brand-manager lock logic.
    if user_email:
        try:
            perms = await get_user_permissions(user_email)
            if "cost:admin" in perms:
                return []
        except Exception:
            pass  # fail-closed: on DB error, proceed with normal lock checks

    if not rows:
        return []

    lock_keys: set[tuple[str, str, str, str]] = set()
    # Map 4-tuple → set of task_numbers seen in the data
    group_tasks: dict[tuple[str, str, str, str], set[str]] = {}
    for r in rows:
        m = str(r.get("model", "") or "").strip()
        a = str(r.get("articul", "") or "").strip()
        cs = str(r.get("calc_sign") or r.get("Признак калькуляции", "") or "").strip()
        pi = str(r.get("plan_id", "") or "").strip()
        # Пустое задание — пустая строка, как и в cost_calc_approvals после
        # миграции 0038. Раньше здесь было `or None`, и ключ группы не
        # совпадал с тем, что реально записано в согласованиях.
        tn = str(r.get("task_number") or r.get("Номер задания производства", "") or "").strip()
        if m and a:
            key4 = (m, a, cs, pi)
            lock_keys.add(key4)
            group_tasks.setdefault(key4, set()).add(tn)
    if not lock_keys:
        return []

    lock_list = list(lock_keys)

    audit_set: set[tuple[str, str, str, str]] = set()
    if lock_list:
        # Батчами: save-batch присылает до 1000 строк, но предел параметров общий,
        # и на широкой пачке запрос упал бы так же, как в наложении цен.
        audit_rows: list = []
        async with acquire() as conn:
            for _batch in _key_batches(lock_list):
                audit_ph = ", ".join(
                    f"(${i*4+1}::text, ${i*4+2}::text, ${i*4+3}::text, ${i*4+4}::text)"
                    for i in range(len(_batch))
                )
                audit_params: list[str] = []
                for m, a, cs, pi in _batch:
                    audit_params.extend([m, a, cs, pi])
                audit_rows.extend(await conn.fetch(
                    f"""SELECT DISTINCT model, articul, calc_sign, plan_id
                        FROM cost_price_changes_audit
                        WHERE (model, articul, calc_sign, plan_id) IN (VALUES {audit_ph})""",
                    *audit_params,
                ))
        for r in audit_rows:
            audit_set.add((
                str(r["model"]).strip(), str(r["articul"]).strip(),
                str(r["calc_sign"] or "").strip(), str(r["plan_id"] or "").strip(),
            ))

    # Действующие переоткрытия — по ним снимается запрет «уже передано в DWH».
    reopened_keys: set[tuple[str, str, str, str]] = (
        await get_reopened_keys(lock_list) if lock_list else set()
    )

    approved_groups: set[tuple[str, str, str, str]] = set()
    returned_groups: set[tuple[str, str, str, str]] = set()
    if lock_list:
        # Батчами — тот же предел параметров, см. KEYS_PER_QUERY.
        appr_rows: list = []
        async with acquire() as conn:
            for _batch in _key_batches(lock_list):
                appr_ph = ", ".join(
                    f"(${i*4+1}::text, ${i*4+2}::text, ${i*4+3}::text, ${i*4+4}::text)"
                    for i in range(len(_batch))
                )
                appr_params: list[str] = []
                for m, a, cs, pi in _batch:
                    appr_params.extend([m, a, cs, pi])
                appr_rows.extend(await conn.fetch(
                    f"""SELECT DISTINCT model, articul, calc_sign, plan_id, task_number, status
                        FROM cost_calc_approvals
                        WHERE (model, articul, calc_sign, plan_id) IN (VALUES {appr_ph})""",
                    *appr_params,
                ))
        group_approval: dict[tuple[str, str, str, str], dict[str, str]] = {}
        for r in appr_rows:
            key4 = (
                str(r["model"]).strip(), str(r["articul"]).strip(),
                str(r["calc_sign"] or "").strip(), str(r["plan_id"] or "").strip(),
            )
            tn_val = str(r["task_number"] or "").strip()
            group_approval.setdefault(key4, {})[tn_val] = r["status"]
            if r["status"] == "returned":
                returned_groups.add(key4)
        for key4, expected_tasks in group_tasks.items():
            approvals = group_approval.get(key4, {})
            if all(approvals.get(tn) == "approved" for tn in expected_tasks):
                approved_groups.add(key4)

    locked_rows: list[dict] = []
    for r in rows:
        model = str(r.get("model", "") or "").strip()
        articul = str(r.get("articul", "") or "").strip()
        cs = str(r.get("calc_sign") or r.get("Признак калькуляции", "") or "").strip()
        pi = str(r.get("plan_id", "") or "").strip()
        bm = str(r.get("brand_manager") or r.get("Бренд-менеджер", "") or "").strip()

        if not model or not articul:
            continue

        key4 = (model, articul, cs, pi)

        # Переоткрытая админом калькуляция сохраняется, хотя в DWH уже уехала:
        # именно для этого переоткрытие и существует. Без этой проверки правку
        # можно было бы ввести в таблице, но не сохранить.
        if key4 in audit_set and key4 not in reopened_keys:
            locked_rows.append({
                "model": model, "articul": articul,
                "calc_sign": cs, "plan_id": pi,
                "reason": "Изменения уже переданы в DWH",
            })
            continue

        if not await _can_approve_or_peo(user_email):
            # 'returned' — это и есть просьба поправить цену, поэтому запрет
            # «только после согласования ПЭО» на такие строки не действует.
            if key4 not in approved_groups and key4 not in returned_groups:
                locked_rows.append({
                    "model": model, "articul": articul,
                    "calc_sign": cs, "plan_id": pi,
                    "reason": "Бренд-менеджер может редактировать только после согласования ПЭО",
                })
                continue

    return locked_rows


@router.post("/save-changes")
async def save_price_changes(payload: dict, user_email: str | None = Depends(_require_perm("cost:edit_price"))) -> dict:
    username = (payload.get("author_name") or "").strip() or user_email or "system"

    calc_sign = payload.get("calc_sign") or payload.get("Признак калькуляции")
    if calc_sign == "ФКСС":
        raise HTTPException(400, "Уровень цен запрещен для редактирования для признака калькуляции 'ФКСС'")

    # Lock check (skip in mock mode where user_email is None)
    if user_email:
        locked = await _check_save_locks(user_email, [payload])
        if locked:
            l = locked[0]
            raise HTTPException(403, f"Строка «{l['model']} / {l['articul']}» заблокирована: {l['reason']}")

    # Build full row data for upsert (pending table stores full snapshot)
    raw_date = payload.get("date")
    if isinstance(raw_date, str) and raw_date:
        parsed_date = date.fromisoformat(raw_date.replace("T00:00:00Z", "").replace("T00:00:00", ""))
    else:
        parsed_date = raw_date

    row_data = {
        "Бренд-менеджер": payload.get("brand_manager"),
        "Модель": payload.get("model"),
        "Артикул": payload.get("articul"),
        "Признак калькуляции": calc_sign,
        "дата расчета": parsed_date,
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
        "Цена РФ": payload.get("price_rf"),
        "Цена КЗ": payload.get("price_kz"),
        "Цена УЗ": payload.get("price_uz"),
        "Комментарий": payload.get("comment") or "",
    }

    if _is_mock():
        pending_id = await upsert_pending_change(row_data, username)
        return {"success": True, "mock": True, "pending_id": pending_id}

    pending_id = await upsert_pending_change(row_data, username)
    return {"success": True, "pending_id": pending_id}


def _row_data_from_payload(c: dict) -> dict:
    """Build full row snapshot dict from a change payload (for upsert into pending)."""
    calc_sign = c.get("calc_sign") or c.get("Признак калькуляции")
    raw_date = c.get("date")
    if isinstance(raw_date, str) and raw_date:
        parsed_date = date.fromisoformat(raw_date.replace("T00:00:00Z", "").replace("T00:00:00", ""))
    else:
        parsed_date = raw_date
    return {
        "Бренд-менеджер": c.get("brand_manager"),
        "Модель": c.get("model"),
        "Артикул": c.get("articul"),
        "Признак калькуляции": calc_sign,
        "дата расчета": parsed_date,
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
        "Цена РФ": c.get("price_rf"),
        "Цена КЗ": c.get("price_kz"),
        "Цена УЗ": c.get("price_uz"),
        "Комментарий": c.get("comment") or "",
    }


@router.post("/save-batch")
async def save_batch_changes(payload: dict, user_email: str | None = Depends(_require_perm("cost:edit_price"))) -> dict:
    username = (payload.get("author_name") or "").strip() or user_email or "system"
    changes = payload.get("changes") or []

    filtered = [
        c for c in changes
        if (c.get("calc_sign") or c.get("Признак калькуляции")) != "ФКСС"
    ]
    if not filtered:
        return {"success": False, "error": "Нет изменений для сохранения после фильтрации ФКСС", "count": 0}

    # Lock check (skip in mock mode where user_email is None)
    if user_email:
        locked = await _check_save_locks(user_email, filtered)
        if locked:
            details = "; ".join(f"«{l['model']} / {l['articul']}»: {l['reason']}" for l in locked)
            raise HTTPException(403, f"Некоторые строки заблокированы: {details}")

    row_data_list = [_row_data_from_payload(c) for c in filtered]

    if _is_mock():
        pending_ids = await upsert_pending_changes_batch(row_data_list, username)
        return {"success": True, "count": len(pending_ids), "mock": True, "pending_ids": pending_ids}

    pending_ids = await upsert_pending_changes_batch(row_data_list, username)
    return {"success": True, "count": len(pending_ids), "pending_ids": pending_ids}


# ── Price approval workflow ────────────────────────────────────────────────────


@router.get("/pending-changes")
async def list_pending_changes(request: Request) -> dict:
    """Return pending changes with optional filtering.

    Query params (all optional):
        q — text search (Модель, Артикул, Наименование модели, Бренд-менеджер)
        brand_manager — filter by brand manager (multi)
        level01..level05 — filter by hierarchy (multi)
        calc_sign — filter by calc sign (multi)
        plan_id — filter by plan (multi)
    """
    filters: dict[str, Any] = {}
    for key in ["brand_manager", "level01", "level02", "level03", "level04", "level05", "calc_sign", "plan_id"]:
        vals = request.query_params.getlist(key)
        if vals:
            filters[key] = vals
    q = request.query_params.get("q")
    if q:
        filters["q"] = q.strip()
    return {"data": await get_pending_changes(filters)}


@router.get("/pending-changes/filter-options")
async def pending_filter_options(request: Request) -> dict:
    """Return distinct filter values from cost_price_pending with top-down cascade.

    Query params (all optional, for cascade):
        brand_manager, level01..level05, calc_sign, plan_id
    """
    selected: dict[str, list[str]] = {}
    for key in ["brand_manager", "level01", "level02", "level03", "level04", "level05", "calc_sign", "plan_id"]:
        vals = request.query_params.getlist(key)
        if vals:
            selected[key] = vals
    return await get_pending_filter_options(selected)


def _run_proc_safe(json_str: str) -> None:
    # Wrapper with exception logging — ensure_future swallows thread errors
    try:
        print(f"[cost] _run_proc_safe: {len(json_str)} bytes", flush=True)
        print(f"[cost] _run_proc_safe PAYLOAD:\n{json_str}", flush=True)
        call_calc_sign_procedure(json_str)
    except Exception as exc:
        import traceback
        print(f"[cost] _run_proc_safe FAILED: {exc}", flush=True)
        traceback.print_exc()
        sys.stdout.flush()


@router.post("/pending-changes/apply")
async def apply_changes(payload: dict, _: str = Depends(_require_perm("cost:approve"))) -> dict:
    """Apply (approve) a batch of pending changes.

    Body: { "ids": [1, 2, 3], "reviewed_by": "...", "proc_payload": [...] }
    - Записывает выбранные строки в OLAP CostHistory_Changes (с 4 новыми полями)
    - Дублирует в локальный audit
    - Удаляет строки из cost_price_pending
    - Группирует proc_payload: КПСС по (calc_sign, plan_id), ПФКСС — все в один пакет.
      Каждая группа отправляется процедуре одним JSON-массивом.
    """
    ids = payload.get("ids") or []
    if not ids:
        raise HTTPException(400, "ids list is required")
    reviewed_by = (payload.get("reviewed_by") or "system").strip()
    proc_payload: list[dict] | None = payload.get("proc_payload")

    # Ключ строки согласования (Модель, Артикул, calc_sign, PLAN_ID) → имя уровня цен.
    # Фронт шлёт в proc_payload только name-поля, а ITEM_ID уровня берём на бэке из
    # справочника s_price_level — для поля price_level_id в JSON процедуры.
    level_name_by_row: dict[tuple[str, str, str, str], str] = {}

    # One-step flow: pressing «Установить цены» is itself the PEO approval action.
    # Mark every pending row as approved in cost_calc_approvals BEFORE the DWH write.
    # save_approval is an UPSERT — calling it for an already-approved row is a no-op.
    if not _is_mock():
        async with pool().acquire() as conn:
            pending_rows = await conn.fetch(
                "SELECT \"Модель\", \"Артикул\", \"Признак калькуляции\", \"PLAN_ID\", \"Номер задания производства\", \"Уровень цен\" FROM cost_price_pending WHERE id = ANY($1::bigint[])",
                ids,
            )
        for pr in pending_rows:
            tn = str(pr.get("Номер задания производства", "") or "").strip()
            await save_approval(
                pr["Модель"], pr["Артикул"], pr["Признак калькуляции"], pr["PLAN_ID"],
                "approved", reviewed_by, task_number=tn,
            )
            level_name_by_row[(
                str(pr.get("Модель", "") or "").strip(),
                str(pr.get("Артикул", "") or "").strip(),
                str(pr.get("Признак калькуляции", "") or "").strip(),
                str(pr.get("PLAN_ID", "") or "").strip(),
            )] = str(pr.get("Уровень цен", "") or "").strip()

    count = await apply_pending_changes(ids, reviewed_by)
    result = {"success": True, "applied": count}

    # Себестоимость для прейскуранта — та же максимальная по заданиям, что уходит
    # в DWH (см. get_max_calc_cost). Фронт присылает в proc_payload значение
    # конкретной строки таблицы, то есть одного задания; без подмены прейскурант
    # и DWH расходились бы между собой.
    if proc_payload and not _is_mock():
        try:
            max_cost = await get_max_calc_cost([
                (
                    str(it.get("model", "") or "").strip(),
                    str(it.get("articul", "") or "").strip(),
                    str(it.get("calc_sign", "") or "").strip(),
                    str(it.get("plan_id", "") or "").strip(),
                )
                for it in proc_payload
            ])
            for it in proc_payload:
                key = (
                    str(it.get("model", "") or "").strip(),
                    str(it.get("articul", "") or "").strip(),
                    str(it.get("calc_sign", "") or "").strip(),
                    str(it.get("plan_id", "") or "").strip(),
                )
                mx = max_cost.get(key)
                if not mx:
                    continue
                try:
                    cur = float(it.get("cost_rub") or it.get("Себестоимость, руб.") or 0)
                except (TypeError, ValueError):
                    cur = 0.0
                if mx["rub"] > cur:
                    it["cost_rub"] = mx["rub"]
        except Exception:
            print(
                "[cost] максимум себестоимости для прейскуранта не посчитан:\n"
                f"{traceback.format_exc()}",
                flush=True,
            )

    if proc_payload:
        import json as _json

        # Группировка:
        #   КПСС   — по (calc_sign, plan_id): отдельный прейскурант на план
        #   ПФКСС  — только по calc_sign: все строки в один прейскурант
        groups: dict[tuple[str, str], list[dict]] = {}
        for item in proc_payload:
            cs = (item.get("calc_sign") or "").strip()
            if cs == "КПСС":
                pi = (item.get("plan_id") or "").strip()
                groups.setdefault((cs, pi), []).append(item)
            elif cs == "ПФКСС":
                groups.setdefault((cs, ""), []).append(item)

        # Построить вложенный JSON: один документ на группу с prices1[]
        calc_sign_price_type = {"КПСС": 3, "ПФКСС": 1}

        # Маппинг имени уровня цен → ITEM_ID справочника s_price_level.
        # Вне mock тянем справочник из Gpartner; если он недоступен — все id = 0.
        price_level_id_by_name: dict[str, int] = {}
        if not _is_mock():
            try:
                loop = asyncio.get_event_loop()
                price_levels = await loop.run_in_executor(None, _get_price_levels_sync)
                price_level_id_by_name = {
                    pl["name"]: int(pl["id"]) for pl in price_levels if pl.get("id")
                }
            except Exception:
                price_level_id_by_name = {}

        docs = []
        for (cs, pi), items in groups.items():
            price_type = calc_sign_price_type.get(cs, items[0].get("price_type", 0))
            author_name = (items[0].get("author_name", "system") or "system")[:15]

            prices1 = []
            for it in items:
                # price_level_id есть только у ПФКСС: имя уровня из строки
                # согласования → ITEM_ID справочника; не нашли — 0.
                price_level_id = 0
                if cs == "ПФКСС":
                    row_key = (
                        str(it.get("model", "") or "").strip(),
                        str(it.get("articul", "") or "").strip(),
                        cs,
                        str(it.get("plan_id", "") or "").strip(),
                    )
                    price_level_id = price_level_id_by_name.get(level_name_by_row.get(row_key, ""), 0)
                prices1.append({
                    "model": it.get("model", ""),
                    "articul": it.get("articul", ""),
                    "wholesale_rub": it.get("wholesale_rub", 0),
                    "plan_price": it.get("cost_rub") or it.get("Себестоимость, руб.", 0),
                    "price_level_id": price_level_id,
                })

            docs.append({
                "plan_id": pi,
                "price_type": price_type,
                "calc_sign": cs,
                "author_name": author_name,
                "prices1": prices1,
            })

        json_str = _json.dumps(docs, ensure_ascii=False, default=str)

        # Один вызов процедуры на все документы (в threadpool, т.к. pyodbc)
        asyncio.ensure_future(
            asyncio.get_event_loop().run_in_executor(
                None, _run_proc_safe, json_str
            )
        )

    return result


@router.post("/pending-changes/clear")
async def clear_changes() -> dict:
    """Delete ALL rows from cost_price_pending."""
    count = await clear_pending_changes()
    return {"success": True, "deleted": count}


@router.post("/pending-changes/clear-my")
async def clear_my_changes(user_email: str = Depends(_require_perm("cost:view"))) -> dict:
    count = await clear_pending_changes_by_user(user_email)
    return {"success": True, "deleted": count}


# ── Versioned calculation editing (Stream G) ────────────────────────────────


@router.get("/checkout-calculation")
async def checkout_calculation_endpoint(request: Request, user_email: str = Depends(_require_perm("cost:edit_materials"))) -> dict:
    model = request.query_params.get("model")
    articul = request.query_params.get("articul")
    calc_sign = request.query_params.get("calc_sign") or None
    plan_id = request.query_params.get("plan_id") or None
    date_str = request.query_params.get("date")
    username = request.query_params.get("username", "system")
    if not model or not articul:
        raise HTTPException(400, "model and articul are required")
    if isinstance(date_str, str) and date_str:
        parsed_date = date.fromisoformat(date_str.replace("T00:00:00Z", "").replace("T00:00:00", ""))
    else:
        parsed_date = date_str
    if _is_mock():
        return mocks.checkout_calculation(model, articul, calc_sign, plan_id, parsed_date, username)
    await _check_calc_locks(user_email, model, articul, calc_sign, plan_id, date_str)
    return await checkout_calculation(model, articul, calc_sign, plan_id, parsed_date, username)


@router.get("/raw-data")
async def raw_data_endpoint(request: Request, _: str = Depends(_require_perm("cost:view"))) -> dict:
    model = request.query_params.get("model")
    articul = request.query_params.get("articul")
    calc_sign = request.query_params.get("calc_sign") or None
    plan_id = request.query_params.get("plan_id") or None
    date_str = request.query_params.get("date")
    task_number = request.query_params.get("task_number") or None
    if not model or not articul:
        raise HTTPException(400, "model and articul are required")
    if _is_mock():
        return mocks.get_raw_cache_rows(model, articul, calc_sign, plan_id, date_str)
    return await get_raw_cache_rows(model, articul, calc_sign, plan_id, date_str, task_number)


@router.get("/versions")
async def versions_endpoint(request: Request, _: str = Depends(_require_perm("cost:view"))) -> list[dict]:
    model = request.query_params.get("model")
    articul = request.query_params.get("articul")
    calc_sign = request.query_params.get("calc_sign") or None
    plan_id = request.query_params.get("plan_id") or None
    date_str = request.query_params.get("date")
    task_number = request.query_params.get("task_number") or None
    if not model or not articul:
        raise HTTPException(400, "model and articul are required")
    if _is_mock():
        return mocks.list_versions(model, articul, calc_sign, plan_id, date_str)
    return await list_versions(model, articul, calc_sign, plan_id, date_str, task_number)


@router.get("/version-rows/{version_id}")
async def version_rows_endpoint(version_id: int, _: str = Depends(_require_perm("cost:view"))) -> dict:
    if _is_mock():
        return mocks.get_version_rows(version_id)
    data = await get_version_rows(version_id)
    if data is None:
        raise HTTPException(404, "version not found")
    return data


@router.post("/create-version")
async def create_version_endpoint(payload: dict, user_email: str = Depends(_require_perm("cost:edit_materials"))) -> dict:
    model = payload.get("model")
    articul = payload.get("articul")
    calc_sign = payload.get("calc_sign")
    plan_id = payload.get("plan_id")
    date_str = payload.get("date")
    username = payload.get("username", user_email or "system")
    rows = payload.get("rows", [])
    status = payload.get("status", "draft")
    task_number = payload.get("task_number") or None
    if not model or not articul:
        raise HTTPException(400, "model and articul are required")
    if status not in ("draft", "pending"):
        raise HTTPException(400, "status must be 'draft' or 'pending'")
    if status == "pending":
        await _check_calc_locks(user_email, model, articul, calc_sign, plan_id, date_str, task_number)
    if _is_mock():
        return mocks.create_version(model, articul, calc_sign, plan_id, date_str, username, rows, status)
    return await create_version(model, articul, calc_sign, plan_id, date_str, username, rows, status, task_number)


@router.post("/save-calculation-draft")
async def save_calculation_draft(payload: dict, user_email: str = Depends(_require_perm("cost:edit_materials"))) -> dict:
    version_id = payload.get("version_id")
    rows = payload.get("rows", [])
    if not version_id:
        raise HTTPException(400, "version_id required")
    if _is_mock():
        return mocks.save_version_draft(version_id, rows)
    ver = await get_version_info(version_id)
    if ver:
        if ver["status"] == "original":
            raise HTTPException(409, "Исходная версия неизменяема — редактирование создаёт новую версию")
        await _check_calc_locks(user_email, ver["model"], ver["articul"], ver["calc_sign"], ver["plan_id"], ver["дата расчета"], ver.get("task_number"))
    await save_version_draft(version_id, rows)
    return {"success": True}


@router.post("/submit-calculation-draft")
async def submit_calculation_draft(payload: dict, user_email: str = Depends(_require_perm("cost:edit_materials"))) -> dict:
    version_id = payload.get("version_id")
    if not version_id:
        raise HTTPException(400, "version_id required")
    comment = payload.get("comment")
    if _is_mock():
        return mocks.submit_version(version_id, comment)
    ver = await get_version_info(version_id)
    if ver:
        if ver["status"] == "original":
            raise HTTPException(409, "Исходная версия неизменяема — редактирование создаёт новую версию")
        await _check_calc_locks(user_email, ver["model"], ver["articul"], ver["calc_sign"], ver["plan_id"], ver["дата расчета"], ver.get("task_number"))
    await submit_version(version_id, comment)
    return {"success": True}


@router.post("/approve-calculation-version")
async def approve_calculation_version(payload: dict, _: str = Depends(_require_perm("cost:approve"))) -> dict:
    version_id = payload.get("version_id")
    action = payload.get("action")
    approved_by = payload.get("approved_by", "system")
    comment = payload.get("comment")
    if not version_id or not action:
        raise HTTPException(400, "version_id and action required")
    if _is_mock():
        if action == "approve":
            return mocks.approve_version(version_id, approved_by)
        return mocks.reject_version(version_id, approved_by, comment)
    if action == "approve":
        await approve_version(version_id, approved_by)
    elif action == "reject":
        await reject_version(version_id, approved_by, comment)
    else:
        raise HTTPException(400, "action must be 'approve' or 'reject'")
    return {"success": True}


@router.get("/calculation-draft-status")
async def calculation_draft_status(request: Request, _: str = Depends(_require_perm("cost:view"))) -> dict:
    model = request.query_params.get("model")
    articul = request.query_params.get("articul")
    calc_sign = request.query_params.get("calc_sign") or None
    plan_id = request.query_params.get("plan_id") or None
    date_str = request.query_params.get("date")
    if not model or not articul:
        raise HTTPException(400, "model and articul are required")
    if isinstance(date_str, str) and date_str:
        parsed_date = date.fromisoformat(date_str.replace("T00:00:00Z", "").replace("T00:00:00", ""))
    else:
        parsed_date = date_str
    if _is_mock():
        return mocks.get_active_version(model, articul, calc_sign, plan_id, parsed_date)
    version = await get_active_version(
        model, articul, calc_sign, plan_id, parsed_date,
        request.query_params.get("task_number") or None,
    )
    if version:
        return {"has_draft": True, "version_id": version["version"]["id"], "status": version["version"]["status"], "rows": version.get("rows", [])}
    return {"has_draft": False}


# ── Admin tools (version management) ───────────────────────────────────────


@router.delete("/admin/versions/{version_id}")
async def admin_delete_version(version_id: int, _: str = Depends(_require_perm("cost:admin"))) -> dict:
    if _is_mock():
        return {"success": True, "mock": True}
    await delete_version(version_id)
    return {"success": True}


@router.post("/admin/unlock-row")
async def admin_unlock_row(payload: dict, _: str = Depends(_require_perm("cost:admin"))) -> dict:
    model = (payload.get("model") or "").strip()
    articul = (payload.get("articul") or "").strip()
    calc_sign = payload.get("calc_sign")
    plan_id = payload.get("plan_id")
    date_str = payload.get("date")
    if not model or not articul:
        raise HTTPException(400, "model and articul are required")
    if isinstance(date_str, str) and date_str:
        parsed_date = date.fromisoformat(date_str.replace("T00:00:00Z", "").replace("T00:00:00", ""))
    else:
        parsed_date = date_str
    if _is_mock():
        return {"success": True, "archived": 0, "mock": True}
    count = await archive_versions_by_key(
        model, articul, calc_sign, plan_id, parsed_date, payload.get("task_number") or None
    )
    return {"success": True, "archived": count}


@router.post("/reject-price")
async def reject_price(payload: dict, user_email: str = Depends(_require_perm("cost:approve"))) -> dict:
    model = (payload.get("model") or "").strip()
    articul = (payload.get("articul") or "").strip()
    calc_sign = payload.get("calc_sign")
    plan_id = payload.get("plan_id")
    date_str = payload.get("date")
    if not model or not articul:
        raise HTTPException(400, "model and articul are required")
    if isinstance(date_str, str) and date_str:
        parsed_date = date.fromisoformat(date_str.replace("T00:00:00Z", "").replace("T00:00:00", ""))
    else:
        parsed_date = date_str
    if _is_mock():
        return {"success": True, "mock": True}
    # Задание пробрасываем: статус «возврат на корректировку» ставится ровно
    # тому заданию, которое вернули, а не всем заданиям калькуляции.
    await reset_price_fields(
        model, articul, calc_sign, plan_id, parsed_date,
        returned_by=user_email or "system",
        task_number=(payload.get("task_number") or "").strip() or None,
    )
    return {"success": True, "reset_by": user_email or "system"}


@router.delete("/admin/dwh-record")
async def admin_delete_dwh_record(payload: dict, _: str = Depends(_require_perm("cost:admin"))) -> dict:
    model = (payload.get("model") or "").strip()
    articul = (payload.get("articul") or "").strip()
    calc_sign = payload.get("calc_sign")
    plan_id = payload.get("plan_id")
    if not model or not articul:
        raise HTTPException(400, "model and articul are required")
    if _is_mock():
        return {"success": True, "audit_deleted": 0, "olap_deleted": 0, "mock": True}
    result = await delete_dwh_record(model, articul, calc_sign, plan_id)
    return {"success": True, **result, "warning": "Цены уже переданы в Fox ERP и не могут быть откачены"}


# ── Переоткрытие калькуляции после записи в DWH (миграция 0039) ───────────────
#
# Только админ раздела (`cost:admin`) — решение заказчика 25.08.2026. Записи в
# DWH не удаляются: повторная установка цен добавит новую строку, а в учётной
# системе более новый прейскурант перекрывает старый.


@router.post("/admin/dwh-reopen")
async def admin_reopen_dwh(payload: dict, user_email: str = Depends(_require_perm("cost:admin"))) -> dict:
    model = (payload.get("model") or "").strip()
    articul = (payload.get("articul") or "").strip()
    calc_sign = (payload.get("calc_sign") or "").strip()
    plan_id = (payload.get("plan_id") or "").strip()
    reason = (payload.get("reason") or "").strip()
    if not model or not articul:
        raise HTTPException(400, "model and articul are required")
    # Причина обязательна: операция нежелательная, и без неё журнал бесполезен.
    if len(reason) < 5:
        raise HTTPException(400, "Нужна причина переоткрытия (не короче 5 символов)")
    if _is_mock():
        return {"success": True, "mock": True}
    result = await reopen_dwh_calculation(
        model, articul, calc_sign, plan_id, user_email or "system", reason,
        task_number=(payload.get("task_number") or "").strip() or None,
    )
    return {
        "success": True, **result,
        "warning": "Прейскурант в учётной системе не откатывается — "
                   "повторная установка цен создаст новый, он перекроет прежний",
    }


# Потолок на пачку возврата. Возврат — операция нежелательная и ручная: пачка в
# несколько сотен калькуляций означает, что человек выделил не то, и лучше
# отказать, чем открыть половину плана.
DWH_REOPEN_BATCH_LIMIT = 500


@router.post("/admin/dwh-reopen/batch")
async def admin_reopen_dwh_batch(payload: dict, user_email: str = Depends(_require_perm("cost:admin"))) -> dict:
    """Массовый возврат из DWH: много калькуляций с одной причиной (пункт 15)."""
    items = payload.get("items") or []
    reason = (payload.get("reason") or "").strip()
    if not items:
        raise HTTPException(400, "items list required")
    if len(items) > DWH_REOPEN_BATCH_LIMIT:
        raise HTTPException(400, f"Слишком большая пачка: {len(items)} > {DWH_REOPEN_BATCH_LIMIT}")
    if len(reason) < 5:
        raise HTTPException(400, "Нужна причина возврата (не короче 5 символов)")
    for it in items:
        if not (it.get("model") or "").strip() or not (it.get("articul") or "").strip():
            raise HTTPException(400, "у каждой калькуляции нужны model и articul")
    if _is_mock():
        return {"success": True, "reopened": len(items), "mock": True}
    result = await reopen_dwh_calculations_batch(items, user_email or "system", reason)
    return {
        "success": True, **result,
        "warning": "Прейскуранты в учётной системе не откатываются — повторная "
                   "установка цен создаст новые, они перекроют прежние",
    }


@router.post("/admin/dwh-reopen/revoke")
async def admin_revoke_dwh_reopen(payload: dict, user_email: str = Depends(_require_perm("cost:admin"))) -> dict:
    model = (payload.get("model") or "").strip()
    articul = (payload.get("articul") or "").strip()
    if not model or not articul:
        raise HTTPException(400, "model and articul are required")
    if _is_mock():
        return {"success": True, "revoked": 0, "mock": True}
    revoked = await revoke_dwh_reopen(
        model, articul, (payload.get("calc_sign") or "").strip(),
        (payload.get("plan_id") or "").strip(), user_email or "system",
    )
    return {"success": True, "revoked": revoked}


@router.get("/admin/dwh-reopen")
async def admin_list_dwh_reopens(request: Request, _: str = Depends(_require_perm("cost:admin"))) -> dict:
    if _is_mock():
        return {"data": [], "mock": True}
    limit = min(int(request.query_params.get("limit") or 200), 1000)
    return {"data": await list_dwh_reopens(limit)}


@router.post("/admin/price-history/backfill")
async def admin_backfill_price_history(_: str = Depends(_require_perm("cost:admin"))) -> dict:
    """Разово подтянуть предысторию цен из CostHistory_Changes. Идемпотентно."""
    if _is_mock():
        return {"success": True, "fetched": 0, "inserted": 0, "mock": True}
    return {"success": True, **await backfill_price_history_from_olap()}


@router.get("/price-history")
async def price_history(request: Request, _: str = Depends(_require_perm("cost:view"))) -> dict:
    """История установленных цен по калькуляции — то, что раньше было только в OLAP."""
    if _is_mock():
        return {"data": [], "mock": True}
    q = request.query_params
    limit = min(int(q.get("limit") or 500), 5000)
    return {"data": await get_price_history(
        q.get("model"), q.get("articul"), q.get("calc_sign"), q.get("plan_id"), limit,
    )}


# ── PEO approval (Stream H) ────────────────────────────────────────────────


# Потолок на одну пачку: массовое согласование с главной таблицы шлётся чанками,
# лимит защищает от «согласовать весь кеш одним запросом».
APPROVALS_BATCH_LIMIT = 1000

# Сколько ключей отдавать одному SQL-запросу.
#
# Postgres не принимает больше 32767 параметров на запрос. Ключи калькуляций
# передаются списком VALUES по 4 параметра на ключ, поэтому предел наступает уже
# на 8192 ключах — а полная выдача даёт 27 647 ключей (110 588 параметров).
# Запрос падал, внешний try проглатывал ошибку, и пользователь получал таблицу
# БЕЗ своих цен и комментариев со статусом 200: в логах «наложение утверждённых
# цен не выполнено», на экране — молча пустые цены. По замерам заказчика это
# каждый ~14-й запрос /aggregated (14 из 204 за сутки), с 10.07.2026.
#
# 2000 ключей = 8000 параметров: вчетверо ниже предела, с запасом на случай, если
# в запрос добавят ещё поле ключа. Ключи между батчами не пересекаются, поэтому
# DISTINCT ON внутри батча остаётся корректным.
KEYS_PER_QUERY = 2000


def _key_batches(items: list, per_query: int = KEYS_PER_QUERY):
    """Разбить список ключей на порции, помещающиеся в один SQL-запрос."""
    for i in range(0, len(items), per_query):
        yield items[i:i + per_query]


@router.post("/approve-calculation")
async def approve_calculation(payload: dict, _: str = Depends(_require_any_perm("cost:approve", "cost:peo_mark"))) -> dict:
    approvals = payload.get("approvals", [])
    approved_by = payload.get("approved_by", "system")
    if not approvals:
        raise HTTPException(400, "approvals list required")
    if len(approvals) > APPROVALS_BATCH_LIMIT:
        raise HTTPException(400, f"Слишком большая пачка: {len(approvals)} > {APPROVALS_BATCH_LIMIT}")
    for a in approvals:
        if not a.get("model") or not a.get("articul"):
            raise HTTPException(400, "model and articul required for every approval")
        # 'returned' — возврат на корректировку (миграция 0040). Штатно его
        # ставят /reject-price и /admin/dwh-reopen, но принимаем и здесь: иначе
        # массовая простановка этого статуса упиралась бы в валидацию.
        if a.get("status") not in ("approved", "rejected", "pending", "returned"):
            raise HTTPException(400, f"Недопустимый статус: {a.get('status')!r}")
        a.setdefault("approved_by", approved_by)
    if _is_mock():
        return mocks.save_approvals_batch(approvals)
    result = await save_approvals_batch(approvals)
    return {"success": True, "count": len(result)}


@router.post("/revoke-approval")
async def revoke_approval_endpoint(payload: dict, _: str = Depends(_require_any_perm("cost:approve", "cost:peo_mark"))) -> dict:
    model = payload.get("model")
    articul = payload.get("articul")
    calc_sign = payload.get("calc_sign")
    plan_id = payload.get("plan_id")
    task_number = payload.get("task_number")
    if not model or not articul:
        raise HTTPException(400, "model and articul required")
    if _is_mock():
        return mocks.revoke_approval(model, articul, calc_sign, plan_id, task_number)
    await revoke_approval(model, articul, calc_sign, plan_id, task_number=task_number)
    return {"success": True}


@router.post("/revoke-approvals-batch")
async def revoke_approvals_batch_endpoint(payload: dict, _: str = Depends(_require_any_perm("cost:approve", "cost:peo_mark"))) -> dict:
    """Массовое снятие согласования — тот же ключ, что у /approve-calculation."""
    items = payload.get("approvals", [])
    if not items:
        raise HTTPException(400, "approvals list required")
    if len(items) > APPROVALS_BATCH_LIMIT:
        raise HTTPException(400, f"Слишком большая пачка: {len(items)} > {APPROVALS_BATCH_LIMIT}")
    for it in items:
        if not it.get("model") or not it.get("articul"):
            raise HTTPException(400, "model and articul required for every item")
    if _is_mock():
        return mocks.revoke_approvals_batch(items)
    deleted = await revoke_approvals_batch(items)
    return {"success": True, "count": deleted}


@router.get("/approval-status")
async def approval_status(request: Request) -> dict:
    filters: dict[str, Any] = {}
    for key in ("model", "articul", "calc_sign", "plan_id", "status"):
        vals = request.query_params.getlist(key)
        if len(vals) == 1:
            filters[key] = vals[0]
        elif len(vals) > 1:
            filters[key] = vals
    if _is_mock():
        return mocks.get_approval_status(filters)
    data = await get_approval_status(filters)
    return {"data": data}


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


@router.get("/mp-constants")
async def mp_constants_history(_: str = Depends(_require_perm("cost:view"))) -> list[dict]:
    """История констант для формулы «Цена для МП» (наценка МП/% расходов МП/скидка СПП)."""
    if _is_mock():
        return mocks.list_mp_constants()
    return await list_mp_constants()


@router.post("/mp-constants")
async def add_mp_constants_endpoint(payload: dict, user_email: str = Depends(_require_perm("cost:edit_materials"))) -> dict:
    """Добавляет новую строку в историю констант МП (не апдейт — новая запись)."""
    markup_mp = payload.get("markup_mp")
    expense_pct_mp = payload.get("expense_pct_mp")
    spp_discount = payload.get("spp_discount")
    if markup_mp is None or expense_pct_mp is None or spp_discount is None:
        raise HTTPException(400, "markup_mp, expense_pct_mp, spp_discount required")
    effective_date = payload.get("effective_date")
    parsed_date = date.fromisoformat(effective_date) if effective_date else None
    username = (payload.get("username") or user_email or "system").strip()
    if _is_mock():
        return mocks.add_mp_constants(markup_mp, expense_pct_mp, spp_discount, parsed_date, username)
    new_id = await add_mp_constants(markup_mp, expense_pct_mp, spp_discount, parsed_date, username)
    return {"success": True, "id": new_id}


# ── Role-based permissions ────────────────────────────────────────────────────


@router.get("/permissions")
async def list_permissions() -> dict:
    return {"permissions": COST_PERMISSIONS}


@router.get("/roles")
async def list_roles_endpoint() -> dict:
    if _is_mock():
        return {"roles": mocks.list_roles()}
    return {"roles": await get_roles()}


@router.post("/roles")
async def create_role_endpoint(payload: dict, _: str = Depends(_require_perm("cost:admin"))) -> dict:
    name = payload.get("name", "").strip()
    perms = payload.get("permissions") or []
    if not name:
        raise HTTPException(400, "Role name required")
    return await create_role(name, perms)


@router.put("/roles/{role_id}")
async def update_role_endpoint(role_id: int, payload: dict, _: str = Depends(_require_perm("cost:admin"))) -> dict:
    name = payload.get("name", "").strip() or None
    perms = payload.get("permissions")
    result = await update_role(role_id, name, perms)
    if not result:
        raise HTTPException(404, "Role not found")
    return result


@router.delete("/roles/{role_id}")
async def delete_role_endpoint(role_id: int, _: str = Depends(_require_perm("cost:admin"))) -> dict:
    deleted = await delete_role(role_id)
    if not deleted:
        raise HTTPException(400, "Role not found or is system role")
    return {"success": True}


@router.get("/roles/users")
async def list_user_roles_endpoint(email: str | None = None) -> dict:
    if _is_mock():
        return {"assignments": mocks.list_user_roles()}
    return {"assignments": await get_user_roles(email)}


@router.post("/roles/users")
async def assign_role_endpoint(payload: dict, _: str = Depends(_require_perm("cost:admin"))) -> dict:
    email = payload.get("email", "").strip()
    role_id = payload.get("role_id")
    granted_by = payload.get("granted_by", "")
    if not email or not role_id:
        raise HTTPException(400, "email and role_id required")
    try:
        return await assign_role(email, role_id, granted_by)
    except ValueError as e:
        raise HTTPException(409, str(e))


@router.delete("/roles/users/{assignment_id}")
async def remove_user_role_endpoint(assignment_id: int, _: str = Depends(_require_perm("cost:admin"))) -> dict:
    deleted = await remove_user_role(assignment_id)
    if not deleted:
        raise HTTPException(404, "Assignment not found")
    return {"success": True}


@router.get("/roles/my")
async def my_roles_permissions(request: Request) -> dict:
    email = request.headers.get("X-Cost-User", "")
    if not email:
        raise HTTPException(401, "Не передан заголовок X-Cost-User")
    if _is_mock():
        return mocks.my_roles_permissions(email)
    roles = await get_user_roles(email)
    permissions = await get_user_permissions(email)
    return {"email": email, "roles": roles, "permissions": permissions}


# ── Cache refresh & status ──────────────────────────────────────────────────


# Ссылка на фоновую задачу обновления. asyncio держит задачи слабой ссылкой —
# без этого поля задачу может собрать GC прямо посреди обновления.
_refresh_task: asyncio.Task | None = None


def _start_refresh(partial_months: int | None = 2) -> None:
    """Запустить фоновое обновление. Флаг is_refreshing уже захвачен вызывающим.

    *partial_months* = None — полное обновление (TRUNCATE + вся CostHistory).
    """
    global _refresh_task
    _refresh_task = asyncio.ensure_future(
        load_cost_data_to_cache(partial_months=partial_months, lock_held=True)
    )


@router.post("/refresh-cache")
async def refresh_cache() -> dict:
    """Принудительное обновление кеша из MSSQL [Checks].[dbo].[CostHistory]."""
    if _is_mock():
        return mocks.refresh_cache()

    # Порядок проверок важен. Сначала — надёжный признак: идёт ли обновление
    # прямо в этом процессе. Если идёт, не трогаем его, СКОЛЬКО БЫ оно ни
    # длилось: второй запуск поверх первого ничего не ускоряет, а дублирует
    # строки — каждая транзакция удаляет только то, что видела на своём старте,
    # и вставляет свою полную копию.
    #
    # Раньше первым стоял возраст флага с порогом в 10 минут. Полное обновление
    # идёт ~25 минут, поэтому здоровое обновление объявлялось зависшим на 11-й
    # минуте и поверх него запускалось второе. Отсюда и брались задвоенные и
    # затроенные калькуляции в cost_data_cache.
    if refresh_in_progress():
        return {
            "status": "already_refreshing",
            "message": "Обновление уже идёт — дождитесь завершения",
        }

    # В этом процессе обновления нет: флаг либо свободен, либо осиротел после
    # падения процесса, либо его держит соседний контейнер во время деплоя
    # (order: start-first). Отбираем только заведомо протухший — см.
    # acquire_or_reclaim_refresh_lock и COST_STALE_REFRESH_MINUTES.
    if await acquire_or_reclaim_refresh_lock():
        _start_refresh()
        return {"status": "started", "message": "Обновление кеша запущено"}

    return {
        "status": "already_refreshing",
        "message": "Обновление уже запущено другим пользователем",
    }


@router.post("/refresh-cache-full")
async def refresh_cache_full(_: str = Depends(_require_perm("cost:admin"))) -> dict:
    """Принудительное ПОЛНОЕ обновление кеша: TRUNCATE + вся CostHistory.

    Зачем отдельная ручка. Обычная кнопка обновляет только последние 2 месяца по
    «дата расчета» — этого достаточно для свежих данных, но не для чистки. Если
    в кэше накопились дубли (так было при наложении двух обновлений до фикса от
    17.08.2026) или сменился набор колонок, лечит только полный перезалив.

    Право cost:admin, а не общее: операция дорогая и заметная.
      * идёт ~25 минут на ~1 млн строк;
      * TRUNCATE держит ACCESS EXCLUSIVE на cost_data_cache до конца транзакции,
        то есть ВСЕ запросы раздела к кэшу на это время встают. Запускать в окно.

    Блокировка — та же, что у частичного обновления: второе поверх первого не
    запустится (см. load_cost_data_to_cache и acquire_or_reclaim_refresh_lock),
    иначе как раз и получаются дубли.
    """
    if _is_mock():
        return mocks.refresh_cache()

    if refresh_in_progress():
        return {
            "status": "already_refreshing",
            "message": "Обновление уже идёт — дождитесь завершения",
        }

    if await acquire_or_reclaim_refresh_lock():
        _start_refresh(partial_months=None)
        return {
            "status": "started",
            "message": "Запущено полное обновление кеша (~25 минут, раздел будет "
                       "недоступен для запросов к кэшу)",
        }

    return {
        "status": "already_refreshing",
        "message": "Обновление уже запущено другим пользователем",
    }


@router.get("/cache-status")
async def cache_status() -> dict:
    """Текущее состояние кеша."""
    if _is_mock():
        return mocks.cache_status()

    status = await get_cache_status()
    if status is None:
        return {"refreshed_at": None, "row_count": 0, "is_refreshing": False, "error_message": "Cache not initialized"}
    return status


# ── Наборы цен на материалы для плана (миграция 0033) ─────────────────────────


@router.get("/calc-stage-prices")
async def calc_stage_prices_endpoint(request: Request) -> dict:
    """Цены материалов на предыдущих этапах калькулирования.

    Для редактора расчёта: на КПСС отдаём цены ПКПСС, на ПФКСС — КПСС и ПКПСС.
    Для ПКПСС список пустой — это первый этап, сравнивать не с чем.
    """
    q = request.query_params
    model = (q.get("model") or "").strip()
    articul = (q.get("articul") or "").strip()
    calc_sign = (q.get("calc_sign") or "").strip()
    if not model or not articul:
        raise HTTPException(400, "model and articul required")
    if _is_mock():
        return {"stages": [], "mock": True}
    stages = await get_prev_stage_prices(
        model, articul, calc_sign,
        q.get("plan_id"), q.get("task_number"),
    )
    return {"stages": stages, "count": sum(len(s["rows"]) for s in stages)}


# ── Настройки таблицы на пользователя (миграция 0041) ────────────────────────
#
# Своими настройками распоряжается сам пользователь, поэтому право — cost:view,
# то же, что на просмотр раздела. Email берём из заголовка X-Cost-User, а не из
# тела запроса: иначе один пользователь мог бы перезаписать настройки другого.

# Потолок на документ настроек. 41 колонка с видимостью, порядком и шириной
# укладывается в ~4 КБ; 64 КБ дают запас на будущие настройки и одновременно
# не позволяют использовать таблицу как хранилище чего попало.
TABLE_PREFS_MAX_BYTES = 64 * 1024


@router.get("/table-prefs")
async def get_table_prefs(user_email: str = Depends(_require_perm("cost:view"))) -> dict:
    if _is_mock():
        return {"prefs": {}, "mock": True}
    return {"prefs": await get_user_table_prefs(user_email or "")}


@router.put("/table-prefs")
async def put_table_prefs(payload: dict, user_email: str = Depends(_require_perm("cost:view"))) -> dict:
    prefs = payload.get("prefs")
    if not isinstance(prefs, dict):
        raise HTTPException(400, "prefs должен быть объектом")
    size = len(json.dumps(prefs, ensure_ascii=False).encode())
    if size > TABLE_PREFS_MAX_BYTES:
        raise HTTPException(413, f"Настройки слишком большие: {size} Б > {TABLE_PREFS_MAX_BYTES} Б")
    if _is_mock():
        return {"success": True, "mock": True}
    await save_user_table_prefs(user_email or "", prefs)
    return {"success": True, "bytes": size}

@router.get("/plan-materials")
async def plan_materials_endpoint(request: Request, _: str = Depends(_require_any_perm("cost:edit_materials", "cost:admin"))) -> dict:
    """Материалы плана, агрегированные по ключу из пяти полей источника.

    Отдаёт средние цены и курс, разброс внутри группы и сколько строк уже
    перекрыто активной версией калькуляции (там цена набора не подействует —
    у версии приоритет).
    """
    plan_id = (request.query_params.get("plan_id") or "").strip()
    if not plan_id:
        raise HTTPException(400, "plan_id required")
    if _is_mock():
        return {"data": [], "count": 0, "decors": [], "decors_count": 0, "mock": True}
    data = await aggregate_plan_materials(plan_id)
    # Декоры отдаём отдельным списком: у них другой ключ («Декоры, наименование»)
    # и другая модель цены — сумма вместо Норма × цена (см. миграцию 0035).
    decors = await aggregate_plan_decors(plan_id)
    return {"data": data, "count": len(data), "decors": decors, "decors_count": len(decors)}


@router.get("/plan-price-sets")
async def plan_price_sets_endpoint(request: Request, _: str = Depends(_require_any_perm("cost:edit_materials", "cost:admin"))) -> dict:
    """Список наборов. plan_id опционален — без него отдаём все, чтобы в UI
    работал фильтр по номеру плана."""
    if _is_mock():
        return {"data": [], "count": 0, "mock": True}
    data = await list_plan_price_sets(request.query_params.get("plan_id"))
    return {"data": data, "count": len(data)}


@router.get("/plan-price-sets/{set_id}")
async def plan_price_set_endpoint(set_id: int, _: str = Depends(_require_any_perm("cost:edit_materials", "cost:admin"))) -> dict:
    if _is_mock():
        raise HTTPException(404, "not available in mock mode")
    data = await get_plan_price_set(set_id)
    if data is None:
        raise HTTPException(404, "набор не найден")
    return data


@router.post("/plan-price-sets")
async def save_plan_price_set_endpoint(
    payload: dict, user_email: str = Depends(_require_any_perm("cost:edit_materials", "cost:admin"))
) -> dict:
    plan_id = (payload.get("plan_id") or "").strip()
    if not plan_id:
        raise HTTPException(400, "plan_id required")
    # Курс необязателен, но пустая строка из формы уходила прямо в numeric-колонку
    # и роняла запрос с 500 — «изменения просто не сохранились» без внятной причины.
    rate = payload.get("rate")
    if isinstance(rate, str):
        rate = rate.strip().replace(",", ".")
        if not rate:
            rate = None
        else:
            try:
                rate = float(rate)
            except ValueError:
                raise HTTPException(400, f"Курс должен быть числом, получено {payload.get('rate')!r}")
    if rate is not None and float(rate) <= 0:
        rate = None
    if _is_mock():
        return {"success": True, "set_id": 0, "mock": True}
    try:
        set_id = await save_plan_price_set(
            plan_id,
            payload.get("title") or "",
            rate,
            payload.get("rows") or [],
            payload.get("username") or user_email or "system",
            payload.get("set_id"),
            payload.get("comment"),
        )
    except ValueError as e:
        raise HTTPException(400, str(e))
    return {"success": True, "set_id": set_id}


@router.post("/plan-price-sets/{set_id}/apply")
async def apply_plan_price_set_endpoint(
    set_id: int, payload: dict | None = None,
    user_email: str = Depends(_require_any_perm("cost:edit_materials", "cost:admin")),
) -> dict:
    """Применяет набор к расчёту себестоимости плана.

    Перезаливает строки плана из источника и накладывает заново цены и версии —
    поэтому ответ содержит счётчики затронутого (см. refresh_plan_from_source).
    """
    if _is_mock():
        return {"success": True, "mock": True}
    username = ((payload or {}).get("username") or user_email or "system")
    try:
        result = await apply_plan_price_set(set_id, username)
    except ValueError as e:
        raise HTTPException(400, str(e))
    return {"success": True, **result}


@router.post("/plan-price-sets/{set_id}/unapply")
async def unapply_plan_price_set_endpoint(
    set_id: int, _: str = Depends(_require_any_perm("cost:edit_materials", "cost:admin"))
) -> dict:
    """Исключает набор из расчёта и возвращает строки плана к данным источника."""
    if _is_mock():
        return {"success": True, "mock": True}
    try:
        result = await unapply_plan_price_set(set_id)
    except ValueError as e:
        raise HTTPException(400, str(e))
    return {"success": True, **result}


@router.delete("/plan-price-sets/{set_id}")
async def delete_plan_price_set_endpoint(
    set_id: int, _: str = Depends(_require_any_perm("cost:edit_materials", "cost:admin"))
) -> dict:
    if _is_mock():
        return {"success": True, "mock": True}
    try:
        await delete_plan_price_set(set_id)
    except ValueError as e:
        raise HTTPException(400, str(e))
    return {"success": True}
