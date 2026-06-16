"""
Подключения к БД раздела «Себестоимость».

Источники данных:
  • postgres-cost (asyncpg)  — локальная БД раздела: учётки, аудит изменений.
  • srv-sql / Checks (pyodbc) — основная база CostHistory (~13M строк, read-only).
  • srv-sql / Gpartner (pyodbc) — справочник уровней цен.
  • srv-olap / FinSandBox (pyodbc) — приёмник изменений (CostHistory_Changes).

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
    "Пошив, руб.", "Пошив, USD.",
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
    "Пошив, руб.", "Пошив, USD.",
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
    """Fetch from MSSQL v_CostHistory_MatchedOrLatest and bulk insert into cache.

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
                cursor.execute("SELECT * FROM [v_CostHistory_MatchedOrLatest] WHERE [дата расчета] >= ?", cutoff_date)
            else:
                cursor.execute("SELECT * FROM [v_CostHistory_MatchedOrLatest]")
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
        return {"success": True, "row_count": total_rows}

    except Exception:
        err_msg = traceback.format_exc()
        await set_cache_error(err_msg)
        return {"success": False, "error": err_msg}


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
        # Добавляем служебные поля: id (serial, not needed), username, timestamp (default now)
        # username идёт после cache-колонок

        set_expr = ", ".join(
            f'"{c}" = EXCLUDED."{c}"' for c in _PENDING_UPDATE_COLS
        )
        if set_expr:
            set_expr += ", "

        row = await conn.fetchrow(
            f"""
            INSERT INTO cost_price_pending
                ({", ".join(_PENDING_COLS_QUOTED)}, username)
            VALUES ({", ".join(_PENDING_PLACEHOLDERS)}, ${len(_PENDING_CACHE_COLS) + 1})
            ON CONFLICT ("Модель", "Артикул", "PLAN_ID", "Признак калькуляции") DO UPDATE SET
                {set_expr}
                username = EXCLUDED.username,
                reviewed_by = NULL,
                reviewed_at = NULL,
                review_comment = NULL
            RETURNING id
            """,
            *values,
            username,
        )
        return row["id"]


async def upsert_pending_changes_batch(changes: list[dict], username: str) -> list[int]:
    """Upsert нескольких строк в cost_price_pending. Возвращает список id."""
    ids: list[int] = []
    async with pool().acquire() as conn:
        for c in changes:
            values = [c.get(col) for col in _PENDING_CACHE_COLS]

            set_expr = ", ".join(
                f'"{col}" = EXCLUDED."{col}"' for col in _PENDING_UPDATE_COLS
            )
            if set_expr:
                set_expr += ", "

            row = await conn.fetchrow(
                f"""
                INSERT INTO cost_price_pending
                    ({", ".join(_PENDING_COLS_QUOTED)}, username)
                VALUES ({", ".join(_PENDING_PLACEHOLDERS)}, ${len(_PENDING_CACHE_COLS) + 1})
                ON CONFLICT ("Модель", "Артикул", "PLAN_ID", "Признак калькуляции") DO UPDATE SET
                    {set_expr}
                    username = EXCLUDED.username,
                    reviewed_by = NULL,
                    reviewed_at = NULL,
                    review_comment = NULL
                RETURNING id
                """,
                *values,
                username,
            )
            ids.append(row["id"])
    return ids


async def get_pending_changes() -> list[dict]:
    """Return all pending changes ordered by created_at DESC."""
    async with pool().acquire() as conn:
        rows = await conn.fetch(
            "SELECT * FROM cost_price_pending ORDER BY created_at DESC"
        )
        return [dict(r) for r in rows]


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
                cursor.execute(
                    """
                    INSERT INTO CostHistory_Changes
                        (Модель, Артикул, Уровень_цен, Розничная_цена_руб, Отпускная_цена_руб,
                         changed_at, Пользователь, calc_sign, plan_id, approved_at, approved_by)
                    VALUES (?, ?, ?, ?, ?, GETDATE(), ?, ?, ?, GETDATE(), ?)
                    """,
                    (
                        rec.get("Модель"),
                        rec.get("Артикул"),
                        rec.get("Уровень цен"),
                        rec.get("Розничная цена по уровню, руб."),
                        rec.get("Отпускная цена по уровню, руб"),
                        reviewed_by,
                        rec.get("Признак калькуляции"),
                        rec.get("PLAN_ID"),
                        reviewed_by,
                    ),
                )
            olap.commit()
        finally:
            olap.close()

    async with pool().acquire() as conn2:
        async with conn2.transaction():
            for rec in records:
                await conn2.execute(
                    """
                    INSERT INTO cost_price_changes_audit
                        (model, articul, price_level, retail_rub, wholesale_rub, username, changed_at)
                    VALUES ($1, $2, $3, $4, $5, $6, $7)
                    """,
                    rec.get("Модель"),
                    rec.get("Артикул"),
                    rec.get("Уровень цен"),
                    rec.get("Розничная цена по уровню, руб."),
                    rec.get("Отпускная цена по уровню, руб"),
                    reviewed_by,
                    now,
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
