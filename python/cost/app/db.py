"""
Подключения к БД раздела «Себестоимость».

Источники данных:
  • postgres-cost (asyncpg)  — локальная БД раздела: учётки, аудит изменений.
  • srv-sql / Checks (pyodbc) — основная база CostHistory (~13M строк, read-only).
  • srv-sql / Gpartner (pyodbc) — справочник уровней цен.
  • srv-olap / FinSandBox (pyodbc) — приёмник изменений (CostHistory_Changes).
  • proc-db (pyodbc) — БД для вызова SQL-процедуры утверждения цен
    (отдельный сервер, креды через PROC_DB_* env).

pyodbc — синхронный драйвер; FastAPI запускает sync-эндпоинты в threadpool,
поэтому мы не оборачиваем коннекты в run_in_executor вручную.
"""

from __future__ import annotations

import asyncio
import datetime
import os

import asyncpg
import pyodbc

# ── postgres-cost (asyncpg pool) ─────────────────────────────────────────────

_pool: asyncpg.Pool | None = None


async def init_pool() -> None:
    global _pool
    dsn = os.environ["COST_DATABASE_URL"]
    for attempt in range(10):
        try:
            _pool = await asyncpg.create_pool(dsn=dsn, min_size=1, max_size=8)
            return
        except (OSError, asyncpg.PostgresError) as e:
            if attempt < 9:
                import asyncio
                await asyncio.sleep(2 ** attempt * 0.2)  # exponential backoff: 0.2, 0.4, 0.8, ...
            else:
                raise


async def close_pool() -> None:
    global _pool
    if _pool is not None:
        await _pool.close()
        _pool = None


def pool() -> asyncpg.Pool:
    if _pool is None:
        raise RuntimeError("DB pool is not initialised")
    return _pool


# ── MSSQL / OLAP (pyodbc) ────────────────────────────────────────────────────

_DRIVER = os.environ.get("MSSQL_DRIVER", "{ODBC Driver 18 for SQL Server}")


def _mssql_connect(server: str, database: str, user: str, password: str, *, readonly: bool = False) -> pyodbc.Connection:
    conn_str = (
        f"DRIVER={_DRIVER};"
        f"SERVER={server};"
        f"DATABASE={database};"
        f"UID={user};"
        f"PWD={password};"
        "TrustServerCertificate=yes;"
        "Encrypt=optional;"
        "LoginTimeout=30;"
    )
    return pyodbc.connect(conn_str, readonly=readonly, timeout=30)


def get_mssql_conn() -> pyodbc.Connection:
    """Чтение CostHistory (база Checks)."""
    return _mssql_connect(
        server=os.environ["MSSQL_SERVER_IP"],
        database=os.environ.get("MSSQL_DATABASE", "Checks"),
        user=os.environ["MSSQL_USER"],
        password=os.environ["MSSQL_PASSWORD"],
        readonly=True,
    )


def get_gpartner_conn() -> pyodbc.Connection:
    """Чтение справочника уровней цен (база Gpartner на том же сервере)."""
    return _mssql_connect(
        server=os.environ["MSSQL_SERVER_IP"],
        database=os.environ.get("GPARTNER_DATABASE", "Gpartner"),
        user=os.environ["MSSQL_USER"],
        password=os.environ["MSSQL_PASSWORD"],
        readonly=True,
    )


def get_olap_conn() -> pyodbc.Connection:
    """Запись изменений в FinSandBox.CostHistory_Changes (write-enabled)."""
    return _mssql_connect(
        server=os.environ["OLAP_SERVER_IP"],
        database=os.environ.get("OLAP_DATABASE", "FinSandBox"),
        user=os.environ["OLAP_USER"],
        password=os.environ["OLAP_PASSWORD"],
        readonly=False,
    )


def get_proc_db_conn() -> pyodbc.Connection | None:
    """БД для вызова SQL-процедуры утверждения цен.

    Использует PROC_DB_* env.  Если PROC_DB_SERVER_IP не задан —
    падает на MSSQL_SERVER_IP (тот же сервер srv-sql).
    Если PROC_DB_USER не задан — падает на MSSQL_USER (аналогично пароль).
    Если сервер всё равно пуст — возвращает None.
    """
    server = os.environ.get("PROC_DB_SERVER_IP") or os.environ.get("MSSQL_SERVER_IP")
    if not server:
        return None
    user = os.environ.get("PROC_DB_USER") or os.environ.get("MSSQL_USER", "")
    password = os.environ.get("PROC_DB_PASSWORD") or os.environ.get("MSSQL_PASSWORD", "")
    return _mssql_connect(
        server=server,
        database=os.environ.get("PROC_DB_DATABASE", "Gpartner"),
        user=user,
        password=password,
        readonly=False,
    )


def call_calc_sign_procedure(json_str: str) -> None:
    """Вызвать SQL-процедуру [dbo].[createPriceList_inFox] с JSON-пакетом.

    Принимает готовую JSON-строку (вложенный формат с массивами prices1),
    отправляет один EXEC со всем пакетом.

    Формат JSON:
        [
          {
            "plan_id": "9272",
            "price_type": 3,
            "calc_sign": "КПСС",
            "author_name": "...",
            "prices1": [
              {"model": "411220", "articul": "26-5956П-5", "wholesale_rub": 2280.00},
              ...
            ]
          },
          ...
        ]

    Если PROC_DB_* env не заданы — логирует и пропускает.
    """
    proc_name = os.environ.get("PROC_DB_PROCEDURE", "")
    conn = get_proc_db_conn()
    if conn is None:
        print(f"[cost] proc-db not configured — skipping, {len(json_str)} bytes", flush=True)
        return

    if not proc_name:
        print(f"[cost] PROC_DB_PROCEDURE not set — stub, {len(json_str)} bytes", flush=True)
        conn.close()
        return

    cursor = conn.cursor()
    try:
        # Процедура использует @JSON_OUT OUTPUT + PRINT, а не SELECT.
        # Захватываем OUTPUT-параметр через DECLARE + SELECT.
        cursor.execute(
            f"DECLARE @out NVARCHAR(MAX); "
            f"EXEC {proc_name} @JSON_IN = ?, @JSON_OUT = @out OUTPUT; "
            f"SELECT @out AS result",
            json_str,
        )

        result_val: str | None = None
        # Procedure result может быть не в первом result set — перебираем все
        while True:
            try:
                if cursor.description:
                    cols = [c[0] for c in cursor.description]
                    row = cursor.fetchone()
                    # Нас интересует именно result set от SELECT @out AS result
                    if row and row[0] and cols == ["result"]:
                        result_val = str(row[0])
                        break
            except Exception:
                pass
            if not cursor.nextset():
                break

        conn.commit()

        if result_val:
            print(f"[cost] {proc_name} RETURN: {result_val}", flush=True)
        else:
            print(f"[cost] {proc_name} ok (no output), {len(json_str)} bytes", flush=True)
    except Exception as exc:
        print(f"[cost] procedure failed: {exc}", flush=True)
    finally:
        conn.close()
        return

    cursor = conn.cursor()
    try:
        cursor.execute(f"EXEC {proc_name} @JSON_IN = ?", json_str)
        conn.commit()
        print(f"[cost] {proc_name} ok, {len(json_str)} bytes", flush=True)
    except Exception as exc:
        print(f"[cost] procedure failed: {exc}", flush=True)
    finally:
        conn.close()


def fetch_olap_changes(keys: list[tuple[str, str, str, str]]) -> list[dict]:
    """Запрос утверждённых изменений из FinSandBox.CostHistory_Changes (primary source).

    keys: список (model, articul, calc_sign, plan_id).
    Возвращает список записей — последнюю для каждой уникальной комбинации
    (Модель, Артикул, calc_sign, plan_id), отсортированную по approved_at DESC.
    """
    if not keys:
        return []
    keys = list(set(keys))
    olap = get_olap_conn()
    cursor = olap.cursor()
    try:
        conditions = " OR ".join(
            f"(Модель = ? AND Артикул = ? AND calc_sign = ? AND plan_id = ?)" for _ in keys
        )
        params: list[str | None] = []
        for m, a, cs, pi in keys:
            params.extend([m, a, cs or None, pi or None])

        cursor.execute(f"""
            SELECT Модель, Артикул, calc_sign, plan_id,
                   Розничная_цена_руб, Отпускная_цена_руб,
                   price_rf, price_kz, price_uz, comment,
                   approved_at, Уровень_цен
            FROM CostHistory_Changes
            WHERE {conditions}
            ORDER BY approved_at DESC
        """, params)

        cols = [d[0] for d in cursor.description]
        seen: set[tuple[str, str, str, str]] = set()
        result: list[dict] = []
        for row in cursor.fetchall():
            rec = dict(zip(cols, row))
            key = (
                str(rec.get("Модель") or "").strip(),
                str(rec.get("Артикул") or "").strip(),
                str(rec.get("calc_sign") or "").strip() if rec.get("calc_sign") else "",
                str(rec.get("plan_id") or "").strip() if rec.get("plan_id") else "",
            )
            if key not in seen:
                seen.add(key)
                # Normalise column names to match cache convention
                rec["retail_rub"] = rec.pop("Розничная_цена_руб")
                rec["wholesale_rub"] = rec.pop("Отпускная_цена_руб")
                rec["price_level"] = rec.pop("Уровень_цен")
                result.append(rec)
        return result
    finally:
        olap.close()


def get_dwh_conn() -> pyodbc.Connection:
    """Чтение справочника групп (DWH.dim.groups) для каскадных фильтров."""
    return _mssql_connect(
        server=os.environ["OLAP_SERVER_IP"],
        database="DWH",
        user=os.environ["OLAP_USER"],
        password=os.environ["OLAP_PASSWORD"],
        readonly=True,
    )


# ── Cache helpers (postgres-cost) ─────────────────────────────────────────────

# Колонки НЕ текстового типа — их значения передаём как есть
_CACHE_NON_TEXT: set[str] = {
    "дата расчета",
    "дата производства",
    "Курс на дату расчета",
    "Розничная цена по уровню, руб.",
    "Отпускная цена по уровню, руб",
    "Розничная цена по уровню, USD.",
    "Отпускная цена по уровню, USD.",
    "Основные материалы, руб.", "Основные материалы, USD.",
    "Вспомогательные материалы, руб.", "Вспомогательные материалы, USD.",
    "Пошив, минуты",
    "Пошив, руб.", "Пошив, USD.",
    "Раскрой, минуты",
    "Раскрой, руб.", "Раскрой, USD.",
    "Декоры, руб.", "Декоры, USD.",
    "Вязание, руб.", "Вязание, USD.",
    "Ставка НДС",
    "Себестоимость, руб.", "Себестоимость, USD.",
    "Норма",
    "цена материала, руб.", "цена материала, USD.",
}

# Маппинг коротких имён PG → полные имена MSSQL для колонок,
# чьи оригинальные имена превышают лимит PG в 63 байта (NAMEDATALEN).
CACHE_COLUMN_MSSQL_MAP: dict[str, str] = {
    "Материал/операция/декор(призн)": "Материал/техоперация/декор(признак)",
}

CACHE_COLUMNS: list[str] = [
    "Бренд-менеджер",
    "Модель",
    "Артикул",
    "Признак калькуляции",
    "дата расчета",
    "дата производства",
    "Курс на дату расчета",
    "Уровень цен",
    "Страна пр-ва",
    "Семья",
    "Сезон",
    "Level 01", "Level 02", "Level 03", "Level 04", "Level 05",
    "Наименование модели",
    "Номер задания производства",
    "PLAN_ID",
    "Материал/операция/декор(призн)",
    "Наименование",
    "артикул материала",
    "свойство1",
    "свойство2",
    "свойство3",
    "Норма",
    "цена материала, руб.",
    "цена материала, USD.",
    "Ставка НДС",
    "Розничная цена по уровню, руб.",
    "Отпускная цена по уровню, руб",
    "Розничная цена по уровню, USD.",
    "Отпускная цена по уровню, USD.",
    "Основные материалы, руб.", "Основные материалы, USD.",
    "Вспомогательные материалы, руб.", "Вспомогательные материалы, USD.",
    "Пошив, минуты",
    "Пошив, руб.", "Пошив, USD.",
    "Раскрой, минуты",
    "Раскрой, руб.", "Раскрой, USD.",
    "Декоры, руб.", "Декоры, USD.",
    "Вязание, руб.", "Вязание, USD.",
    "Себестоимость, руб.", "Себестоимость, USD.",
]


async def get_cache_status() -> dict | None:
    async with pool().acquire() as conn:
        row = await conn.fetchrow(
            "SELECT refreshed_at, row_count, is_refreshing, error_message, refreshing_since FROM cost_cache_status WHERE id = 1"
        )
        if row is None:
            return None
        return {
            "refreshed_at": row["refreshed_at"],
            "row_count": row["row_count"],
            "is_refreshing": row["is_refreshing"],
            "error_message": row["error_message"],
            "refreshing_since": row["refreshing_since"],
        }


async def set_cache_refreshing(is_refreshing: bool) -> None:
    async with pool().acquire() as conn:
        if is_refreshing:
            await conn.execute(
                "UPDATE cost_cache_status SET is_refreshing = TRUE, error_message = NULL, refreshing_since = NOW() WHERE id = 1"
            )
        else:
            await conn.execute(
                "UPDATE cost_cache_status SET is_refreshing = FALSE, refreshing_since = NULL WHERE id = 1"
            )


async def set_cache_error(error_message: str) -> None:
    async with pool().acquire() as conn:
        await conn.execute(
            "UPDATE cost_cache_status SET error_message = $1, is_refreshing = FALSE, refreshing_since = NULL WHERE id = 1",
            error_message,
        )


async def set_cache_completed(row_count: int) -> None:
    async with pool().acquire() as conn:
        await conn.execute(
            "UPDATE cost_cache_status SET refreshed_at = NOW(), row_count = $1, is_refreshing = FALSE, error_message = NULL, refreshing_since = NULL WHERE id = 1",
            row_count,
        )


async def try_acquire_refresh_lock() -> bool:
    """Atomically set is_refreshing=TRUE if currently FALSE. Returns True if lock acquired."""
    async with pool().acquire() as conn:
        result = await conn.execute(
            "UPDATE cost_cache_status SET is_refreshing = TRUE,"
            "  refreshing_since = NOW(), error_message = NULL"
            " WHERE id = 1 AND is_refreshing = FALSE"
        )
        return result == "UPDATE 1"


async def clear_cache() -> None:
    async with pool().acquire() as conn:
        await conn.execute("TRUNCATE TABLE cost_data_cache")


# ── Margin targets ────────────────────────────────────────────────────────────


async def get_margin_targets() -> list[dict]:
    """Return all margin targets keyed by level1."""
    async with pool().acquire() as conn:
        rows = await conn.fetch(
            "SELECT level1, target_margin_pct, updated_at, updated_by"
            "  FROM cost_margin_targets"
            "  ORDER BY level1"
        )
        return [dict(r) for r in rows]


async def save_margin_targets(targets: list[dict], username: str) -> None:
    """Upsert margin targets in a single transaction."""
    async with pool().acquire() as conn:
        async with conn.transaction():
            for t in targets:
                await conn.execute(
                    """
                    INSERT INTO cost_margin_targets (level1, target_margin_pct, created_by, updated_by)
                    VALUES ($1, $2, $3, $3)
                    ON CONFLICT (level1) DO UPDATE SET
                        target_margin_pct = EXCLUDED.target_margin_pct,
                        updated_by        = EXCLUDED.updated_by,
                        updated_at        = NOW()
                    """,
                    t["level1"],
                    t.get("target_margin_pct") or 0,
                    username,
                )


BATCH_SIZE = 10_000


def _get_cutoff_date(months_ago: int) -> datetime.date:
    """Return the first day of N months ago (inclusive) as datetime.date."""
    today = datetime.date.today()
    month = today.month - months_ago
    year = today.year
    while month < 1:
        month += 12
        year -= 1
    return datetime.date(year, month, 1)


def _convert_mssql_row(row: tuple, col_indices: list[int], cache_columns: list[str]) -> tuple:
    """Convert a single MSSQL row: strip text fields, pass numerics/dates as-is."""
    converted = []
    for i, col in zip(col_indices, cache_columns):
        val = row[i]
        if col not in _CACHE_NON_TEXT:
            if val is None:
                converted.append(None)
                continue
            if not isinstance(val, str):
                val = str(val)
            val = val.strip()
        converted.append(val)
    return tuple(converted)


async def load_cost_data_to_cache(partial_months: int | None = None) -> dict:
    """Fetch from MSSQL [Checks].[dbo].[CostHistory] and bulk insert into cache.

    Batches of BATCH_SIZE rows — never loads the full dataset into Python memory.
    Uses a producer thread (MSSQL fetch) + async consumer (PG copy) with a
    threading.Queue for backpressure.

    If *partial_months* is set (e.g. 2), only refreshes records where
    ``[дата расчета] >= N months ago`` — deletes those rows from the cache
    and re-inserts them.  Full refresh (= TRUNCATE + all rows) when omitted.

    Returns {'success': True, 'row_count': N} or {'success': False, 'error': '...'}.
    """
    import queue as thr_queue
    import traceback

    await set_cache_refreshing(True)

    q: thr_queue.Queue = thr_queue.Queue(maxsize=4)

    cutoff_date: datetime.date | None = _get_cutoff_date(partial_months) if partial_months is not None else None
    cache_columns: list[str] | None = None
    total_rows = 0

    def _producer() -> None:
        nonlocal cache_columns
        conn = get_mssql_conn()
        conn.timeout = 300  # query timeout 5 min (pyodbc: timeout is on Connection, not Cursor)
        cursor = conn.cursor()
        try:
            if cutoff_date is not None:
                cursor.execute("SELECT * FROM [Checks].[dbo].[CostHistory] WHERE [дата расчета] >= ?", cutoff_date)
            else:
                cursor.execute("SELECT * FROM [Checks].[dbo].[CostHistory]")
            mssql_columns = [desc[0] for desc in cursor.description]
            # Для колонок, чьи PG-имена короче MSSQL-оригиналов,
            # ищем по полному MSSQL-имени через CACHE_COLUMN_MSSQL_MAP.
            cols = [
                c for c in CACHE_COLUMNS
                if (CACHE_COLUMN_MSSQL_MAP.get(c, c)) in mssql_columns
            ]
            idx = [
                mssql_columns.index(CACHE_COLUMN_MSSQL_MAP.get(c, c))
                for c in cols
            ]
            cache_columns = cols

            while True:
                rows = cursor.fetchmany(BATCH_SIZE)
                if not rows:
                    break
                batch = [_convert_mssql_row(r, idx, cols) for r in rows]
                q.put(batch)
        finally:
            q.put(None)
            conn.close()

    loop = asyncio.get_running_loop()

    try:
        prod_fut = loop.run_in_executor(None, _producer)

        async with pool().acquire() as conn:
            async with conn.transaction():
                if cutoff_date is not None:
                    await conn.execute('DELETE FROM cost_data_cache WHERE "дата расчета" >= $1', cutoff_date)
                else:
                    await conn.execute("TRUNCATE TABLE cost_data_cache")

                while True:
                    try:
                        batch = await loop.run_in_executor(None, lambda: q.get(timeout=300))
                    except thr_queue.Empty:
                        raise RuntimeError("Cache producer timed out after 5 minutes")
                    if batch is None:
                        break
                    if cache_columns is None:
                        raise RuntimeError("cache_columns not set by producer")
                    await conn.copy_records_to_table(
                        "cost_data_cache",
                        records=batch,
                        columns=cache_columns,
                    )
                    total_rows += len(batch)

        await prod_fut

        await set_cache_completed(total_rows)

        # Синхронизация cost_price_changes_audit с CostHistory_Changes (OLAP)
        await sync_audit_from_olap()

        return {"success": True, "row_count": total_rows}

    except Exception:
        err_msg = traceback.format_exc()
        await set_cache_error(err_msg)
        return {"success": False, "error": err_msg}


# ── OLAP audit sync ────────────────────────────────────────────────────────────


def _fetch_audit_from_olap() -> list[dict]:
    """Синхронный запрос: получить последнюю запись из CostHistory_Changes для каждой (Модель, Артикул).

    Выполняется в thread executor.  Возвращает список плоских dict-ов,
    где ключи уже приведены к именам колонок cost_price_changes_audit.
    """
    olap = get_olap_conn()
    cursor = olap.cursor()
    try:
        cursor.execute("""
            SELECT
                Модель,
                Артикул,
                Уровень_цен           AS price_level,
                Розничная_цена_руб    AS retail_rub,
                Отпускная_цена_руб    AS wholesale_rub,
                changed_at,
                Пользователь          AS username,
                price_rf,
                price_kz,
                price_uz,
                comment
            FROM (
                SELECT *,
                    ROW_NUMBER() OVER (
                        PARTITION BY Модель, Артикул
                        ORDER BY approved_at DESC, changed_at DESC
                    ) AS rn
                FROM CostHistory_Changes
            ) sub
            WHERE rn = 1
        """)
        cols = [d[0] for d in cursor.description]
        return [dict(zip(cols, row)) for row in cursor.fetchall()]
    finally:
        olap.close()


async def sync_audit_from_olap() -> None:
    """Перезаписать cost_price_changes_audit данными из CostHistory_Changes (OLAP).

    TRUNCATE + bulk insert с актуальными записями из OLAP.
    Вызывается после каждого успешного обновления кэша.
    Если OLAP недоступен или включён MOCK-режим — пропускается без ошибки.
    """
    if os.environ.get("COST_MOCK", "").strip() == "1":
        return

    try:
        loop = asyncio.get_event_loop()
        records: list[dict] = await loop.run_in_executor(None, _fetch_audit_from_olap)
    except Exception:
        return  # OLAP недоступен — sync пропускается, не ломаем кэш

    if not records:
        async with pool().acquire() as conn:
            await conn.execute("TRUNCATE TABLE cost_price_changes_audit")
        return

    rows: list[tuple] = [
        (
            r.get("Модель") or r.get("model"),
            r.get("Артикул") or r.get("articul"),
            r.get("price_level"),
            r.get("retail_rub"),
            r.get("wholesale_rub"),
            r.get("username") or "system",
            r.get("changed_at") or datetime.datetime.now(),
            r.get("price_rf"),
            r.get("price_kz"),
            r.get("price_uz"),
            r.get("comment") or "",
        )
        for r in records
    ]

    async with pool().acquire() as conn:
        async with conn.transaction():
            await conn.execute("TRUNCATE TABLE cost_price_changes_audit")
            await conn.copy_records_to_table(
                "cost_price_changes_audit",
                records=rows,
                columns=[
                    "model", "articul", "price_level", "retail_rub", "wholesale_rub",
                    "username", "changed_at", "price_rf", "price_kz", "price_uz", "comment",
                ],
            )


# ── Price approval workflow ────────────────────────────────────────────────────

# Колонки cost_price_pending, соответствующие CACHE_COLUMNS (для построения SQL)
_PENDING_CACHE_COLS: list[str] = [
    "Бренд-менеджер",
    "Модель",
    "Артикул",
    "Признак калькуляции",
    "дата расчета",
    "Курс на дату расчета",
    "Уровень цен",
    "Страна пр-ва",
    "Семья",
    "Сезон",
    "Level 01", "Level 02", "Level 03", "Level 04", "Level 05",
    "Наименование модели",
    "Номер задания производства",
    "PLAN_ID",
    "Розничная цена по уровню, руб.",
    "Отпускная цена по уровню, руб",
    "Розничная цена по уровню, USD.",
    "Отпускная цена по уровню, USD.",
    "Основные материалы, руб.", "Основные материалы, USD.",
    "Вспомогательные материалы, руб.", "Вспомогательные материалы, USD.",
    "Пошив, руб.", "Пошив, USD.",
    "Раскрой, руб.", "Раскрой, USD.",
    "Декоры, руб.", "Декоры, USD.",
    "Вязание, руб.", "Вязание, USD.",
    "Себестоимость, руб.", "Себестоимость, USD.",
    "Цена РФ",
    "Цена КЗ",
    "Цена УЗ",
]

# Псевдонимы колонок для SQL (экранированные кавычки)
_PENDING_COLS_QUOTED = [f'"{c}"' for c in _PENDING_CACHE_COLS]

# placeholders $1, $2, ..., $N для вставки
_PENDING_PLACEHOLDERS = [f"${i+1}" for i in range(len(_PENDING_CACHE_COLS))]

# Колонки для ON CONFLICT DO UPDATE (все, кроме Модель, Артикул, PLAN_ID, Признак калькуляции)
_PENDING_UPDATE_COLS = [
    c for c in _PENDING_CACHE_COLS
    if c not in ("Модель", "Артикул", "PLAN_ID", "Признак калькуляции")
]


async def upsert_pending_change(row_data: dict, username: str) -> int:
    """Вставка или обновление (upsert) строки в cost_price_pending.

    Уникальный ключ: (Модель, Артикул, PLAN_ID, Признак калькуляции).
    При повторном сохранении — обновляются все поля.
    """
    async with pool().acquire() as conn:
        # Извлекаем значения из row_data для всех _PENDING_CACHE_COLS
        values = [row_data.get(c) for c in _PENDING_CACHE_COLS]
        # Добавляем служебные поля: id (serial, not needed), username, comment, timestamp (default now)
        # username и "Комментарий" идут после cache-колонок
        comment = row_data.get("Комментарий") or ""

        set_expr = ", ".join(
            f'"{c}" = EXCLUDED."{c}"' for c in _PENDING_UPDATE_COLS
        )
        if set_expr:
            set_expr += ", "

        row = await conn.fetchrow(
            f"""
            INSERT INTO cost_price_pending
                ({", ".join(_PENDING_COLS_QUOTED)}, username, "Комментарий")
            VALUES ({", ".join(_PENDING_PLACEHOLDERS)}, ${len(_PENDING_CACHE_COLS) + 1}, ${len(_PENDING_CACHE_COLS) + 2})
            ON CONFLICT ("Модель", "Артикул", "PLAN_ID", "Признак калькуляции") DO UPDATE SET
                {set_expr}
                username = EXCLUDED.username,
                "Комментарий" = EXCLUDED."Комментарий",
                reviewed_by = NULL,
                reviewed_at = NULL,
                review_comment = NULL
            RETURNING id
            """,
            *values,
            username,
            comment,
        )
        return row["id"]


async def upsert_pending_changes_batch(changes: list[dict], username: str) -> list[int]:
    """Upsert нескольких строк в cost_price_pending. Возвращает список id."""
    ids: list[int] = []
    async with pool().acquire() as conn:
        for c in changes:
            values = [c.get(col) for col in _PENDING_CACHE_COLS]
            comment = c.get("Комментарий") or ""

            set_expr = ", ".join(
                f'"{col}" = EXCLUDED."{col}"' for col in _PENDING_UPDATE_COLS
            )
            if set_expr:
                set_expr += ", "

            row = await conn.fetchrow(
                f"""
                INSERT INTO cost_price_pending
                    ({", ".join(_PENDING_COLS_QUOTED)}, username, "Комментарий")
                VALUES ({", ".join(_PENDING_PLACEHOLDERS)}, ${len(_PENDING_CACHE_COLS) + 1}, ${len(_PENDING_CACHE_COLS) + 2})
                ON CONFLICT ("Модель", "Артикул", "PLAN_ID", "Признак калькуляции") DO UPDATE SET
                    {set_expr}
                    username = EXCLUDED.username,
                    "Комментарий" = EXCLUDED."Комментарий",
                    reviewed_by = NULL,
                    reviewed_at = NULL,
                    review_comment = NULL
                RETURNING id
                """,
                *values,
                username,
                comment,
            )
            ids.append(row["id"])
    return ids


async def get_pending_changes(filters: dict | None = None) -> list[dict]:
    """Return pending changes with optional filtering, ordered by created_at DESC.

    filters supports:
        q: str — text search across Модель, Артикул, Наименование модели, Бренд-менеджер
        brand_manager: list[str]
        level01..level05: list[str]
        calc_sign: list[str]
        plan_id: list[str]
    """
    conditions: list[str] = []
    params: list[Any] = []
    param_idx = 1

    if filters:
        # Text search (OR across several columns)
        q = filters.get("q")
        if q:
            like_val = f"%{q}%"
            conditions.append(
                f'("Модель"::TEXT ILIKE ${param_idx}'
                f' OR "Артикул"::TEXT ILIKE ${param_idx}'
                f' OR "Наименование модели"::TEXT ILIKE ${param_idx}'
                f' OR "Бренд-менеджер"::TEXT ILIKE ${param_idx})'
            )
            params.append(like_val)
            param_idx += 1

        # Exact-match filters (AND, each key can have multiple values → IN)
        filter_keys = [
            "brand_manager", "level01", "level02", "level03", "level04", "level05",
            "calc_sign", "plan_id",
        ]
        col_map = {
            "brand_manager": '"Бренд-менеджер"',
            "level01": '"Level 01"',
            "level02": '"Level 02"',
            "level03": '"Level 03"',
            "level04": '"Level 04"',
            "level05": '"Level 05"',
            "calc_sign": '"Признак калькуляции"',
            "plan_id": '"PLAN_ID"',
        }
        for key in filter_keys:
            vals = filters.get(key)
            if vals and (isinstance(vals, list) and len(vals) > 0) or (isinstance(vals, str) and vals.strip()):
                if isinstance(vals, str):
                    vals = [vals]
                col = col_map[key]
                placeholders = ", ".join(f"${param_idx + i}" for i in range(len(vals)))
                conditions.append(f"{col} IN ({placeholders})")
                params.extend(vals)
                param_idx += len(vals)

    where_clause = ""
    if conditions:
        where_clause = " WHERE " + " AND ".join(conditions)

    query = f"SELECT * FROM cost_price_pending{where_clause} ORDER BY created_at DESC"

    async with pool().acquire() as conn:
        rows = await conn.fetch(query, *params)
        return [dict(r) for r in rows]


_PENDING_FILTER_COLS = [
    "Бренд-менеджер",
    "Level 01", "Level 02", "Level 03", "Level 04", "Level 05",
    "Признак калькуляции",
    "PLAN_ID",
]

_PENDING_CASCADE_KEYS = [
    "brand_manager", "level01", "level02", "level03", "level04", "level05",
    "calc_sign", "plan_id",
]

_PENDING_CASCADE_COL_MAP: dict[str, str] = {
    "brand_manager": '"Бренд-менеджер"',
    "level01": '"Level 01"',
    "level02": '"Level 02"',
    "level03": '"Level 03"',
    "level04": '"Level 04"',
    "level05": '"Level 05"',
    "calc_sign": '"Признак калькуляции"',
    "plan_id": '"PLAN_ID"',
}


async def get_pending_filter_options(selected: dict[str, list[str]]) -> dict[str, list[str]]:
    """Return distinct values from cost_price_pending with top-down cascade.

    selected: { brand_manager: [...], level01: [...], ... }
    Returns:  { brand_manager: [...], level01: [...], ..., calc_sign: [...], plan_id: [...] }
    """
    result: dict[str, list[str]] = {}

    async with pool().acquire() as conn:
        # 1. brand_manager — NOT filtered by levels (top level)
        rows = await conn.fetch(
            'SELECT DISTINCT "Бренд-менеджер" FROM cost_price_pending'
            ' WHERE "Бренд-менеджер" IS NOT NULL AND "Бренд-менеджер" != \'\''
            ' ORDER BY 1'
        )
        result["brand_manager"] = [r[0] for r in rows if r[0]]

        # 2. Level 01-05 — top-down cascade (higher levels filter lower)
        level_keys = ["level01", "level02", "level03", "level04", "level05"]
        level_db_cols = [
            '"Level 01"', '"Level 02"', '"Level 03"', '"Level 04"', '"Level 05"',
        ]

        for i, (lk, lcol) in enumerate(zip(level_keys, level_db_cols)):
            conditions: list[str] = [f"{lcol} IS NOT NULL AND {lcol} != ''"]
            params: list[str] = []

            # Filter by brand_manager
            bm_vals = selected.get("brand_manager")
            if bm_vals and len(bm_vals) > 0:
                placeholders = ", ".join(f"${p+1}" for p in range(len(bm_vals)))
                conditions.append(f'"Бренд-менеджер" IN ({placeholders})')
                params.extend(bm_vals)

            # Filter by higher levels (smaller index)
            for j in range(i):
                higher_key = level_keys[j]
                higher_vals = selected.get(higher_key)
                if higher_vals and len(higher_vals) > 0:
                    placeholders = ", ".join(f"${len(params)+p+1}" for p in range(len(higher_vals)))
                    conditions.append(f"{level_db_cols[j]} IN ({placeholders})")
                    params.extend(higher_vals)

            where = " AND ".join(conditions)
            query = f"SELECT DISTINCT {lcol} FROM cost_price_pending WHERE {where} ORDER BY 1"
            rows = await conn.fetch(query, *params)
            result[lk] = [r[0] for r in rows if r[0]]

        # 3. calc_sign — filtered by ALL selected cascade values
        conditions = ['"Признак калькуляции" IS NOT NULL AND "Признак калькуляции" != \'\'']
        params = []
        for key in ["brand_manager"] + level_keys:
            vals = selected.get(key)
            if vals and len(vals) > 0:
                col = _PENDING_CASCADE_COL_MAP[key]
                placeholders = ", ".join(f"${len(params)+p+1}" for p in range(len(vals)))
                conditions.append(f"{col} IN ({placeholders})")
                params.extend(vals)
        where = " AND ".join(conditions)
        rows = await conn.fetch(
            f'SELECT DISTINCT "Признак калькуляции" FROM cost_price_pending WHERE {where} ORDER BY 1',
            *params,
        )
        result["calc_sign"] = [r[0] for r in rows if r[0]]

        # 4. plan_id — filtered by ALL cascade values including calc_sign
        conditions = ['"PLAN_ID" IS NOT NULL AND "PLAN_ID" != \'\'']
        params = []
        for key in ["brand_manager"] + level_keys + ["calc_sign"]:
            vals = selected.get(key)
            if vals and len(vals) > 0:
                col = _PENDING_CASCADE_COL_MAP[key]
                placeholders = ", ".join(f"${len(params)+p+1}" for p in range(len(vals)))
                conditions.append(f"{col} IN ({placeholders})")
                params.extend(vals)
        where = " AND ".join(conditions)
        rows = await conn.fetch(
            f'SELECT DISTINCT "PLAN_ID" FROM cost_price_pending WHERE {where} ORDER BY 1',
            *params,
        )
        result["plan_id"] = [r[0] for r in rows if r[0]]

    return result


async def apply_pending_changes(change_ids: list[int], reviewed_by: str) -> int:
    """Apply (approve) pending changes: write to OLAP + local audit, delete from pending.

    Все переданные id утверждаются в одной транзакции.
    Каждая строка пишется в CostHistory_Changes (с 4 новыми полями)
    и дублируется в cost_price_changes_audit.
    После успешной записи строки удаляются из cost_price_pending.

    Returns: количество обработанных строк.
    """
    async with pool().acquire() as conn:
        rows = await conn.fetch(
            "SELECT * FROM cost_price_pending WHERE id = ANY($1::bigint[])",
            change_ids,
        )
        if not rows:
            return 0
        records = [dict(r) for r in rows]

    now = datetime.datetime.now()

    if os.environ.get("COST_MOCK", "").strip() != "1":
        olap = get_olap_conn()
        cursor = olap.cursor()
        try:
            for rec in records:
                comment_val = rec.get("Комментарий") or ""
                cursor.execute(
                    """
                    INSERT INTO CostHistory_Changes
                        (Модель, Артикул, Уровень_цен, Розничная_цена_руб, Отпускная_цена_руб,
                         changed_at, Пользователь, calc_sign, plan_id, approved_at, approved_by,
                         sebestoimost_rub, sebestoimost_usd, price_rf, price_kz, price_uz, comment)
                    VALUES (?, ?, ?, ?, ?, GETDATE(), ?, ?, ?, GETDATE(), ?, ?, ?, ?, ?, ?, ?)
                    """,
                    (
                        rec.get("Модель"),
                        rec.get("Артикул"),
                        rec.get("Уровень цен"),
                        rec.get("Розничная цена по уровню, руб."),
                        rec.get("Отпускная цена по уровню, руб"),
                        rec.get("username") or reviewed_by,   # автор изменения
                        rec.get("Признак калькуляции"),
                        rec.get("PLAN_ID"),
                        reviewed_by,                          # кто утвердил
                        rec.get("Себестоимость, руб."),
                        rec.get("Себестоимость, USD."),
                        rec.get("Цена РФ"),
                        rec.get("Цена КЗ"),
                        rec.get("Цена УЗ"),
                        comment_val,
                    ),
                )
            olap.commit()
        finally:
            olap.close()

    async with pool().acquire() as conn2:
        async with conn2.transaction():
            for rec in records:
                comment_val = rec.get("Комментарий") or ""
                await conn2.execute(
                    """
                    INSERT INTO cost_price_changes_audit
                        (model, articul, price_level, retail_rub, wholesale_rub, username, changed_at,
                         price_rf, price_kz, price_uz, comment)
                    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
                    """,
                    rec.get("Модель"),
                    rec.get("Артикул"),
                    rec.get("Уровень цен"),
                    rec.get("Розничная цена по уровню, руб."),
                    rec.get("Отпускная цена по уровню, руб"),
                    rec.get("username") or reviewed_by,       # автор изменения
                    now,
                    rec.get("Цена РФ"),
                    rec.get("Цена КЗ"),
                    rec.get("Цена УЗ"),
                    comment_val,
                )
            await conn2.execute(
                "DELETE FROM cost_price_pending WHERE id = ANY($1::bigint[])",
                change_ids,
            )

    return len(records)


async def clear_pending_changes() -> int:
    """Delete ALL rows from cost_price_pending. Returns count of deleted rows."""
    async with pool().acquire() as conn:
        result = await conn.execute("DELETE FROM cost_price_pending")
        # result looks like "DELETE 42"
        count = int(result.split()[1]) if result.startswith("DELETE") else 0
        return count


async def checkout_calculation(model, articul, calc_sign, plan_id, raw_date, username) -> dict:
    if isinstance(raw_date, str) and raw_date:
        date = datetime.datetime.fromisoformat(raw_date.replace("Z", "+00:00")).date()
    else:
        date = raw_date
    async with pool().acquire() as conn:
        row = await conn.fetchrow(
            """SELECT id FROM cost_calc_versions
               WHERE model=$1 AND articul=$2 AND calc_sign IS NOT DISTINCT FROM $3
                 AND plan_id IS NOT DISTINCT FROM $4 AND "дата расчета"=$5 AND status='draft'
               ORDER BY created_at DESC LIMIT 1""",
            model, articul, calc_sign, plan_id, date,
        )
        if row is not None:
            version_id = row["id"]
            rows = await conn.fetch(
                """SELECT * FROM cost_calc_version_rows WHERE version_id=$1 ORDER BY sort_order""",
                version_id,
            )
            if rows:
                return {"version_id": version_id, "rows": [dict(r) for r in rows]}
            # Empty draft — remove and recreate from cache
            await conn.execute("DELETE FROM cost_calc_versions WHERE id=$1", version_id)

        # Verify cache has data for this key
        cache_count = await conn.fetchval(
            """SELECT COUNT(*) FROM cost_data_cache
               WHERE "Модель"=$1 AND "Артикул"=$2
                 AND "Признак калькуляции" IS NOT DISTINCT FROM $3
                 AND "PLAN_ID" IS NOT DISTINCT FROM $4 AND "дата расчета"=$5""",
            model, articul, calc_sign, plan_id, date,
        )
        if cache_count == 0:
            raise ValueError(f"No cache rows found for model={model}, articul={articul}, calc_sign={calc_sign}, plan_id={plan_id}, date={date}")

        status = await conn.fetchrow("SELECT refreshed_at FROM cost_cache_status WHERE id=1")
        source_refreshed_at = status["refreshed_at"] if status else None

        # Compute next version number to avoid UNIQUE constraint violation on re-checkout
        next_version = await conn.fetchval(
            """SELECT COALESCE(MAX(version), 0) + 1 FROM cost_calc_versions
               WHERE model=$1 AND articul=$2 AND calc_sign IS NOT DISTINCT FROM $3
                 AND plan_id IS NOT DISTINCT FROM $4 AND "дата расчета"=$5""",
            model, articul, calc_sign, plan_id, date,
        )
        version_id = await conn.fetchval(
            """INSERT INTO cost_calc_versions
               (model, articul, calc_sign, plan_id, "дата расчета", version, source_refreshed_at, created_by)
               VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
               RETURNING id""",
            model, articul, calc_sign, plan_id, date, next_version, source_refreshed_at, username,
        )

        col_list = ", ".join(f'"{c}"' for c in CACHE_COLUMNS)
        await conn.execute(
            f"""INSERT INTO cost_calc_version_rows
                (version_id, {col_list}, sort_order, change_type)
                SELECT $1, {col_list},
                       row_number() OVER (ORDER BY id) - 1,
                       'original'
                FROM cost_data_cache
                WHERE "Модель"=$2 AND "Артикул"=$3
                  AND "Признак калькуляции" IS NOT DISTINCT FROM $4
                  AND "PLAN_ID" IS NOT DISTINCT FROM $5
                  AND "дата расчета"=$6""",
            version_id, model, articul, calc_sign, plan_id, date,
        )

        rows = await conn.fetch(
            """SELECT * FROM cost_calc_version_rows WHERE version_id=$1 ORDER BY sort_order""",
            version_id,
        )
        return {"version_id": version_id, "rows": [dict(r) for r in rows]}


async def save_version_draft(version_id, rows) -> None:
    async with pool().acquire() as conn:
        async with conn.transaction():
            await conn.execute(
                "DELETE FROM cost_calc_version_rows WHERE version_id=$1",
                version_id,
            )
            col_names = CACHE_COLUMNS
            for row in rows:
                # Recalculate Основные материалы = Норма × цена материала
                norm = row.get("Норма")
                price_rub = row.get("цена материала, руб.")
                price_usd = row.get("цена материала, USD.")
                if norm is not None and price_rub is not None:
                    try:
                        row["Основные материалы, руб."] = float(norm or 0) * float(price_rub or 0)
                    except (ValueError, TypeError):
                        pass
                if norm is not None and price_usd is not None:
                    try:
                        row["Основные материалы, USD."] = float(norm or 0) * float(price_usd or 0)
                    except (ValueError, TypeError):
                        pass
                values = [version_id]
                for col in col_names:
                    val = row.get(col)
                    if col in _CACHE_NON_TEXT:
                        # Convert date strings to date objects for asyncpg
                        if col in ("дата расчета", "дата производства"):
                            if isinstance(val, str) and val:
                                values.append(datetime.datetime.fromisoformat(val.replace("Z", "+00:00")).date())
                            else:
                                values.append(val if val is not None else None)
                        else:
                            values.append(val)
                    else:
                        values.append(str(val) if val is not None else None)
                sort_order = row.get("sort_order", 0)
                change_type = row.get("change_type", "original")
                await conn.execute(
                    f"""INSERT INTO cost_calc_version_rows
                        (version_id, {", ".join(f'"{c}"' for c in col_names)}, sort_order, change_type)
                        VALUES ($1, {", ".join(f"${j+2}" for j in range(len(col_names)))}, ${len(col_names)+2}, ${len(col_names)+3})""",
                    *values, sort_order, change_type,
                )


async def submit_version(version_id, comment=None) -> None:
    async with pool().acquire() as conn:
        async with conn.transaction():
            # Get version info to know which calculation this belongs to
            ver = await conn.fetchrow(
                """SELECT model, articul, calc_sign, plan_id
                   FROM cost_calc_versions WHERE id=$1""",
                version_id,
            )
            if ver is None:
                return
            # Reset PEO approval status — a new submission supersedes the old one
            await conn.execute(
                """DELETE FROM cost_calc_approvals
                   WHERE model=$1 AND articul=$2
                     AND calc_sign IS NOT DISTINCT FROM $3
                     AND plan_id IS NOT DISTINCT FROM $4
                     AND status='approved'""",
                ver["model"], ver["articul"], ver["calc_sign"], ver["plan_id"],
            )
            await conn.execute(
                "UPDATE cost_calc_versions SET status='pending', comment=$2 WHERE id=$1",
                version_id, comment,
            )


async def _apply_version_rows_to_cache(conn, version_id) -> None:
    ver = await conn.fetchrow(
        """SELECT model, articul, calc_sign, plan_id, "дата расчета"
           FROM cost_calc_versions WHERE id=$1""",
        version_id,
    )
    if ver is None:
        return
    await conn.execute(
        """DELETE FROM cost_data_cache
           WHERE "Модель"=$1 AND "Артикул"=$2
             AND "Признак калькуляции" IS NOT DISTINCT FROM $3
             AND "PLAN_ID" IS NOT DISTINCT FROM $4
             AND "дата расчета"=$5::timestamptz""",
        ver["model"], ver["articul"], ver["calc_sign"], ver["plan_id"], ver["дата расчета"],
    )
    # Insert version rows into cache (cast date→timestamptz where needed)
    col_list = ", ".join(f'"{c}"' for c in CACHE_COLUMNS)
    select_items = []
    for c in CACHE_COLUMNS:
        if c in ("дата расчета", "дата производства"):
            select_items.append(f'"{c}"::timestamptz')
        else:
            select_items.append(f'"{c}"')
    select_list = ", ".join(select_items)
    await conn.execute(
        f"""INSERT INTO cost_data_cache ({col_list})
            SELECT {select_list}
            FROM cost_calc_version_rows
            WHERE version_id=$1""",
        version_id,
    )


async def approve_version(version_id, approved_by) -> None:
    async with pool().acquire() as conn:
        async with conn.transaction():
            await conn.execute(
                "UPDATE cost_calc_versions SET status='approved', approved_by=$2, approved_at=NOW() WHERE id=$1",
                version_id, approved_by,
            )
            await _apply_version_rows_to_cache(conn, version_id)


async def reject_version(version_id, approved_by, comment=None) -> None:
    async with pool().acquire() as conn:
        await conn.execute(
            "UPDATE cost_calc_versions SET status='rejected', approved_by=$2, approved_at=NOW(), comment=$3 WHERE id=$1",
            version_id, approved_by, comment,
        )


async def get_active_version(model, articul, calc_sign, plan_id, raw_date) -> dict | None:
    if isinstance(raw_date, str) and raw_date:
        date = datetime.datetime.fromisoformat(raw_date.replace("Z", "+00:00")).date()
    else:
        date = raw_date
    async with pool().acquire() as conn:
        ver = await conn.fetchrow(
            """SELECT * FROM cost_calc_versions
               WHERE model=$1 AND articul=$2 AND calc_sign IS NOT DISTINCT FROM $3
                 AND plan_id IS NOT DISTINCT FROM $4 AND "дата расчета"=$5
                 AND status IN ('draft', 'pending')
               ORDER BY created_at DESC LIMIT 1""",
            model, articul, calc_sign, plan_id, date,
        )
        if ver is None:
            return None
        rows = await conn.fetch(
            """SELECT * FROM cost_calc_version_rows WHERE version_id=$1 ORDER BY sort_order""",
            ver["id"],
        )
        return {"version": dict(ver), "rows": [dict(r) for r in rows]}


async def delete_version(version_id) -> None:
    async with pool().acquire() as conn:
        await conn.execute(
            "DELETE FROM cost_calc_versions WHERE id = $1",
            version_id,
        )


# ── PEO approval (cost_calc_approvals) ─────────────────────────────────────


async def save_approval(model, articul, calc_sign, plan_id, status, approved_by, comment=None) -> dict:
    async with pool().acquire() as conn:
        async with conn.transaction():
            row = await conn.fetchrow(
                """
                INSERT INTO cost_calc_approvals
                    (model, articul, calc_sign, plan_id, status, approved_by, approved_at, comment)
                VALUES ($1, $2, $3, $4, $5, $6,
                        CASE WHEN $5 IN ('approved', 'rejected') THEN NOW() ELSE NULL END,
                        $7)
                ON CONFLICT (model, articul, calc_sign, plan_id) DO UPDATE SET
                    status = EXCLUDED.status,
                    approved_by = EXCLUDED.approved_by,
                    approved_at = EXCLUDED.approved_at,
                    comment = EXCLUDED.comment,
                    updated_at = NOW()
                RETURNING *
                """,
                model, articul, calc_sign, plan_id, status, approved_by, comment,
            )
            # When approved, find latest pending version and apply its rows to cache
            if status == 'approved':
                ver = await conn.fetchrow(
                    """SELECT id FROM cost_calc_versions
                       WHERE model=$1 AND articul=$2
                         AND calc_sign IS NOT DISTINCT FROM $3
                         AND plan_id IS NOT DISTINCT FROM $4
                         AND status='pending'
                       ORDER BY created_at DESC LIMIT 1""",
                    model, articul, calc_sign, plan_id,
                )
                if ver is not None:
                    await _apply_version_rows_to_cache(conn, ver["id"])
            return dict(row)


async def save_approvals_batch(approvals: list[dict]) -> list[dict]:
    results = []
    for a in approvals:
        r = await save_approval(
            a["model"], a.get("articul"), a.get("calc_sign"), a.get("plan_id"),
            a["status"], a.get("approved_by"), a.get("comment"),
        )
        results.append(r)
    return results


async def revoke_approval(model, articul, calc_sign, plan_id) -> None:
    async with pool().acquire() as conn:
        await conn.execute(
            "DELETE FROM cost_calc_approvals WHERE model=$1 AND articul=$2 AND calc_sign IS NOT DISTINCT FROM $3 AND plan_id IS NOT DISTINCT FROM $4",
            model, articul, calc_sign, plan_id,
        )


async def get_approval_status(filters: dict | None = None) -> list[dict]:
    conditions = []
    params = []
    param_idx = 1
    if filters:
        for key in ("model", "articul"):
            val = filters.get(key)
            if val:
                conditions.append(f'{key} = ${param_idx}')
                params.append(val)
                param_idx += 1
        for key in ("calc_sign", "plan_id", "status"):
            vals = filters.get(key)
            if vals:
                if isinstance(vals, list):
                    placeholders = ", ".join(f"${param_idx + i}" for i in range(len(vals)))
                    conditions.append(f'{key} IN ({placeholders})')
                    params.extend(vals)
                    param_idx += len(vals)
                elif vals:
                    conditions.append(f'{key} = ${param_idx}')
                    params.append(vals)
                    param_idx += 1
    where = " WHERE " + " AND ".join(conditions) if conditions else ""
    async with pool().acquire() as conn:
        rows = await conn.fetch(f"SELECT * FROM cost_calc_approvals{where} ORDER BY updated_at DESC", *params)
        return [dict(r) for r in rows]
