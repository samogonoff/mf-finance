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
              {"model": "411220", "articul": "26-5956П-5", "wholesale_rub": 2280.00, "plan_price": 4.12},
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


def fetch_gpartner_planned(pairs: list[tuple[str, str]]) -> dict[tuple[str, str], dict]:
    """Плановая оптовая цена/НДС/себестоимость из Gpartner S_MODELI (fallback source).

    pairs: список (model, articul). Возвращает
    {(model, articul): {price_mopt, nnds, plan_price, ru_nds}}.
    Резервный источник planned_retail/planned_wholesale/planned_cost для КПСС/ПФКСС,
    когда основная цепочка (cost_data_cache) не даёт данных — см. routes.get_aggregated.

    ru_nds — ставка НДС РФ (уже готовое число, напр. 20.00 — не ссылка на
    справочник: в этой базе S_NDS не существует) для формулы «Цена для МП».
    Читается тем же запросом к S_MODELI, чтобы не плодить второй round-trip
    в Gpartner на тех же ключах.
    """
    if not pairs:
        return {}
    pairs = list(set(pairs))
    conn = get_gpartner_conn()
    cursor = conn.cursor()
    try:
        result: dict[tuple[str, str], dict] = {}
        # ODBC/MSSQL ограничивает число параметров запроса (~2100) — при большом
        # наборе пар (напр. "Цена для МП" по всему незафильтрованному датасету,
        # тысячи уникальных (model, articul)) один запрос на все пары падает с
        # pyodbc.Error 07002 "COUNT field incorrect". Бьём на батчи по 500 пар
        # (=1000 параметров), с запасом от лимита.
        batch_size = 500
        for i in range(0, len(pairs), batch_size):
            batch = pairs[i:i + batch_size]
            conditions = " OR ".join("(MODEL = ? AND ART = ?)" for _ in batch)
            params: list[str] = []
            for m, a in batch:
                params.extend([m, a])

            cursor.execute(
                f"SELECT MODEL, ART, PRICE_MOPT, NDS, PLAN_PRICE, RU_NDS FROM [dbo].[S_MODELI] WHERE {conditions}",
                params,
            )
            for row in cursor.fetchall():
                key = (str(row[0] or "").strip(), str(row[1] or "").strip())
                result[key] = {
                    "price_mopt": float(row[2]) if row[2] is not None else None,
                    "nnds": float(row[3]) if row[3] is not None else None,
                    "plan_price": float(row[4]) if row[4] is not None else None,
                    "ru_nds": float(row[5]) if row[5] is not None else None,
                }
        return result
    finally:
        conn.close()


def fetch_gpartner_internal_rate() -> float | None:
    """Внутренний курс рубля (Gpartner.valuta1, VALUTA_ID=2) — самая свежая
    запись на сегодня. Используется в формуле «Цена для МП, рос. руб.»;
    по требованию — всегда актуальный курс, не фиксируется заранее.
    """
    conn = get_gpartner_conn()
    cursor = conn.cursor()
    try:
        cursor.execute(
            "SELECT TOP 1 [KURS] FROM [dbo].[valuta1] WHERE [VALUTA_ID] = 2 ORDER BY [DATA] DESC"
        )
        row = cursor.fetchone()
        return float(row[0]) if row and row[0] is not None else None
    finally:
        conn.close()


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
    "cost_factor_rub", "cost_factor_usd",
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
    # Цвет модели. В источнике постоянен внутри задания (проверено: 85 583
    # группы, у всех одно значение), поэтому входит и в AGG_GROUP_FIELDS.
    "color",
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
    "Декоры, наименование",
    "Вязание, руб.", "Вязание, USD.",
    "Себестоимость, руб.", "Себестоимость, USD.",
    # Коэффициент источника (миграция 0031). В MSSQL такой колонки нет — при
    # загрузке она отфильтруется сама (см. cols в _producer) и заполнится
    # отдельным UPDATE через _compute_cost_factors. В списке нужна, чтобы
    # коэффициент переносился между cost_data_cache и cost_calc_version_rows.
    "cost_factor_rub", "cost_factor_usd",
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


# ── МП constants (наценка МП / % расходов МП / скидка СПП) ──────────────────
# История значений (append-only), в отличие от cost_margin_targets — здесь
# нужно именно "когда что действовало", а не только текущее значение.


async def list_mp_constants() -> list[dict]:
    """Вся история констант МП, самые новые сверху."""
    async with pool().acquire() as conn:
        rows = await conn.fetch(
            """SELECT id, effective_date::text, markup_mp, expense_pct_mp, spp_discount,
                      created_by, created_at::text
               FROM cost_mp_constants
               ORDER BY effective_date DESC, created_at DESC"""
        )
        return [dict(r) for r in rows]


async def get_latest_mp_constants() -> dict | None:
    """Самая свежая по (effective_date, created_at) запись — то, что "сейчас
    действует" для формулы «Цена для МП». None, если константы ещё ни разу
    не задавались."""
    async with pool().acquire() as conn:
        row = await conn.fetchrow(
            """SELECT markup_mp, expense_pct_mp, spp_discount, effective_date::text
               FROM cost_mp_constants
               ORDER BY effective_date DESC, created_at DESC
               LIMIT 1"""
        )
        return dict(row) if row else None


async def add_mp_constants(markup_mp, expense_pct_mp, spp_discount, effective_date, username: str) -> int:
    """Добавляет новую строку истории (не апдейт существующей)."""
    async with pool().acquire() as conn:
        new_id = await conn.fetchval(
            """INSERT INTO cost_mp_constants
                   (effective_date, markup_mp, expense_pct_mp, spp_discount, created_by)
               VALUES (COALESCE($1, CURRENT_DATE), $2, $3, $4, $5)
               RETURNING id""",
            effective_date, markup_mp, expense_pct_mp, spp_discount, username,
        )
        return new_id


def compute_mp_price(avg_wholesale_rub, internal_rate, markup_mp, expense_pct_mp, nds_rate, spp_discount) -> float | None:
    """Цена для МП, рос. руб. Формула как задал пользователь буквально:
        Сред.опт(руб) / 0.036 * 1.5 * 1.05 * 1.22 / 0.75

    markup_mp/expense_pct_mp/spp_discount — уже готовые множители из
    cost_mp_constants (1.5, а не 50%; вводятся в этом виде через модалку
    констант). nds_rate приходит из Gpartner S_MODELI.RU_NDS как "20.00"
    (проценты) — здесь приводится к множителю (1 + 20/100 = 1.2).

    Возвращает None, если какого-то входного значения не хватает — нельзя
    молча посчитать по неполным данным.
    """
    if None in (avg_wholesale_rub, internal_rate, markup_mp, expense_pct_mp, nds_rate, spp_discount):
        return None
    if internal_rate == 0 or spp_discount == 0:
        return None
    nds_multiplier = 1 + float(nds_rate) / 100
    return (
        float(avg_wholesale_rub) / float(internal_rate)
        * float(markup_mp) * float(expense_pct_mp) * nds_multiplier
        / float(spp_discount)
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


_BUCKET_SUM_SQL = (
    'COALESCE("Основные материалы, {c}",0) + COALESCE("Вспомогательные материалы, {c}",0)'
    ' + COALESCE("Пошив, {c}",0) + COALESCE("Раскрой, {c}",0)'
    ' + COALESCE("Декоры, {c}",0) + COALESCE("Вязание, {c}",0)'
)


async def _compute_cost_factors(conn, cutoff_date=None) -> None:
    """Заполняет cost_factor_rub/usd = бакет / (Норма × цена материала).

    Вызывать ТОЛЬКО сразу после реимпорта из источника и ДО наложения версий —
    иначе коэффициент будет посчитан от уже отредактированных значений.

    Коэффициент поглощает две особенности источника: цена материала там округлена
    до 4 знаков (money), а бакет посчитан из неокруглённой; плюс у части строк
    бакет отличается от произведения в разы по доменным причинам. Благодаря
    коэффициенту `Норма × цена × factor` воспроизводит источник точно, а правка
    цены меняет стоимость пропорционально. NULL = «неизвестен, считать 1»:
    так остаются операционные строки, где цены в источнике нет вообще.
    См. миграцию 0031 и _recalc_cost_buckets.
    """
    where = ""
    params: list = []
    if cutoff_date is not None:
        where = ' WHERE "дата расчета" >= $1'
        params.append(cutoff_date)
    await conn.execute(
        f"""
        UPDATE cost_data_cache SET
            cost_factor_rub = CASE
                WHEN COALESCE("Норма",0) <> 0 AND COALESCE("цена материала, руб.",0) <> 0
                THEN ({_BUCKET_SUM_SQL.format(c="руб.")}) / ("Норма" * "цена материала, руб.")
            END,
            cost_factor_usd = CASE
                WHEN COALESCE("Норма",0) <> 0 AND COALESCE("цена материала, USD.",0) <> 0
                THEN ({_BUCKET_SUM_SQL.format(c="USD.")}) / ("Норма" * "цена материала, USD.")
            END
        {where}
        """,
        *params,
    )


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

                # Коэффициент связи «Норма × цена» с реальной стоимостью статьи —
                # считаем ЗДЕСЬ, пока в кэше лежат нетронутые данные источника и
                # версии ещё не наложены (миграция 0031, подробности там).
                await _compute_cost_factors(conn, cutoff_date)

                # Обновление кэша (TRUNCATE/partial-delete + реимпорт из MSSQL)
                # стирает эффект применённых наборов цен и версий — накладываем их
                # заново в той же транзакции. ПОРЯДОК ВАЖЕН и задаёт приоритет:
                # сначала цены плана, затем версии калькуляций поверх них, потому
                # что версия должна побеждать цену плана (миграция 0033).
                await _reapply_applied_plan_price_sets(conn)
                await _reapply_active_versions_to_cache(conn)

        await prod_fut

        await set_cache_completed(total_rows)

        # Синхронизация cost_price_changes_audit с CostHistory_Changes (OLAP)
        await sync_audit_from_olap()

        return {"success": True, "row_count": total_rows}

    except asyncio.CancelledError:
        await set_cache_error("Cache refresh cancelled")
        raise

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
        records = [dict(r) for r in rows]

    # Цена для МП — живой предпросмотр той же формулы, что и в основной
    # таблице (см. get_aggregated в routes.py); итоговое значение при
    # утверждении пересчитывается заново в apply_pending_changes.
    try:
        pairs = list({
            (str(rec.get("Модель") or "").strip(), str(rec.get("Артикул") or "").strip())
            for rec in records
        })
        gpartner_map = fetch_gpartner_planned(pairs) if pairs else {}
        internal_rate = fetch_gpartner_internal_rate()
        mp_constants = await get_latest_mp_constants()
        if internal_rate is not None and mp_constants is not None:
            for rec in records:
                key = (str(rec.get("Модель") or "").strip(), str(rec.get("Артикул") or "").strip())
                ru_nds = gpartner_map.get(key, {}).get("ru_nds")
                rec["mp_price_rub"] = compute_mp_price(
                    rec.get("Отпускная цена по уровню, руб"),
                    internal_rate,
                    mp_constants["markup_mp"],
                    mp_constants["expense_pct_mp"],
                    ru_nds,
                    mp_constants["spp_discount"],
                )
    except Exception:
        pass

    return records


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

    # Цена для МП, рос. руб. — считается заново прямо сейчас, на момент
    # утверждения (курс Gpartner.valuta1 и константы cost_mp_constants —
    # "как сейчас", не то, что было при создании pending-записи; см.
    # обсуждение с пользователем). "Отпускная цена по уровню, руб" — уже
    # зафиксированное решение бренд-менеджера, берётся из pending как есть.
    mp_prices: dict[int, float | None] = {}
    try:
        pairs = list({
            (str(rec.get("Модель") or "").strip(), str(rec.get("Артикул") or "").strip())
            for rec in records
        })
        gpartner_map = fetch_gpartner_planned(pairs) if pairs else {}
        internal_rate = fetch_gpartner_internal_rate()
        mp_constants = await get_latest_mp_constants()
        if internal_rate is not None and mp_constants is not None:
            for rec in records:
                key = (str(rec.get("Модель") or "").strip(), str(rec.get("Артикул") or "").strip())
                ru_nds = gpartner_map.get(key, {}).get("ru_nds")
                mp_prices[rec["id"]] = compute_mp_price(
                    rec.get("Отпускная цена по уровню, руб"),
                    internal_rate,
                    mp_constants["markup_mp"],
                    mp_constants["expense_pct_mp"],
                    ru_nds,
                    mp_constants["spp_discount"],
                )
    except Exception:
        pass  # МП-цену не посчитали — пишем NULL, остальное утверждение не блокируем

    # COST_SKIP_DWH_WRITE=1 — не писать в FinSandBox.CostHistory_Changes.
    # Нужен для тестов вызова SQL-процедуры на тестовом контуре: сама процедура
    # и локальный аудит отрабатывают как обычно, а боевой приёмник изменений
    # остаётся нетронутым. В проде флаг должен быть пустым.
    _skip_dwh = os.environ.get("COST_SKIP_DWH_WRITE", "").strip() == "1"
    if _skip_dwh:
        print(f"[cost] COST_SKIP_DWH_WRITE=1 — пропускаю запись {len(records)} строк в CostHistory_Changes", flush=True)

    if os.environ.get("COST_MOCK", "").strip() != "1" and not _skip_dwh:
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
                         sebestoimost_rub, sebestoimost_usd, price_rf, price_kz, price_uz, comment,
                         mp_price_rub)
                    VALUES (?, ?, ?, ?, ?, GETDATE(), ?, ?, ?, GETDATE(), ?, ?, ?, ?, ?, ?, ?, ?)
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
                        mp_prices.get(rec["id"]),
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
                         price_rf, price_kz, price_uz, comment, calc_sign, plan_id, mp_price_rub)
                    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
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
                    rec.get("Признак калькуляции"),
                    rec.get("PLAN_ID"),
                    mp_prices.get(rec["id"]),
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


async def clear_pending_changes_by_user(username: str) -> int:
    """Delete rows from cost_price_pending created by a specific user."""
    async with pool().acquire() as conn:
        result = await conn.execute(
            "DELETE FROM cost_price_pending WHERE username = $1", username
        )
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

        # Compute next version number to avoid UNIQUE constraint violation on re-checkout.
        # БЕЗ фильтра по дате: с миграции 0027 дата не входит в ключ версии, и
        # нумерация в разрезе даты давала бы номер, уже занятый другой датой того
        # же ключа. Этот эндпоинт фронтендом не используется (легаси Stream G) и
        # задание не проставляет — созданная им версия попадает в легаси-область
        # с пустым task_number (см. миграцию 0032).
        next_version = await conn.fetchval(
            """SELECT COALESCE(MAX(version), 0) + 1 FROM cost_calc_versions
               WHERE model=$1 AND articul=$2 AND calc_sign IS NOT DISTINCT FROM $3
                 AND plan_id IS NOT DISTINCT FROM $4""",
            model, articul, calc_sign, plan_id,
        )
        version_id = await conn.fetchval(
            """INSERT INTO cost_calc_versions
               (model, articul, calc_sign, plan_id, "дата расчета", version, source_refreshed_at, created_by)
               VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
               RETURNING id""",
            model, articul, calc_sign, plan_id, date, next_version, source_refreshed_at, username,
        )

        col_list = ", ".join(f'"{c}"' for c in CACHE_COLUMNS)
        cache_rows = await conn.fetch(
            f"""SELECT {col_list} FROM cost_data_cache
                WHERE "Модель"=$1 AND "Артикул"=$2
                  AND "Признак калькуляции" IS NOT DISTINCT FROM $3
                  AND "PLAN_ID" IS NOT DISTINCT FROM $4
                  AND "дата расчета"=$5
                ORDER BY id""",
            model, articul, calc_sign, plan_id, date,
        )
        # Нормализуем легаси-коды типа ('шт', 'себестоимость лиса осн/всп',
        # 'пошив', 'раскорой') к каноническим значениям дропдауна и выводим
        # Норма/цена для операционных строк — см. _normalize_row_type.
        for sort_order, r in enumerate(cache_rows):
            vrow = dict(r)
            _normalize_row_type(vrow)
            _recalc_cost_buckets(vrow)
            insert_values = [vrow.get(c) for c in CACHE_COLUMNS]
            await conn.execute(
                f"""INSERT INTO cost_calc_version_rows
                    (version_id, {col_list}, sort_order, change_type)
                    VALUES ($1, {", ".join(f"${j+2}" for j in range(len(CACHE_COLUMNS)))},
                            ${len(CACHE_COLUMNS)+2}, ${len(CACHE_COLUMNS)+3})""",
                version_id, *insert_values, sort_order, 'original',
            )

        rows = await conn.fetch(
            """SELECT * FROM cost_calc_version_rows WHERE version_id=$1 ORDER BY sort_order""",
            version_id,
        )
        return {"version_id": version_id, "rows": [dict(r) for r in rows]}


# Материал/операция/декор(призн) → денежный бакет, в который попадает Норма × цена.
# Незнакомый тип в этот словарь не входит — такие строки _recalc_cost_buckets
# не трогает вообще. Включает и канонические значения дропдауна редактора, и
# реальные легаси-коды из источника (MSSQL CostHistory) — они НЕ совпадают
# с каноническими (проверено на живых данных, см. _normalize_row_type).
_BUCKET_BY_MAT_TYPE: dict[str, str] = {
    # Канонические — то, что пишет дропдаун в редакторе версий.
    "Материал основной": "Основные материалы",
    "Материал вспомогательный": "Вспомогательные материалы",
    "Декор": "Декоры",
    "Пошив": "Пошив",
    "Раскрой": "Раскрой",
    "Вязание": "Вязание",
    # Легаси-коды из источника — встречаются в cost_data_cache как есть.
    "шт": "Декоры",
    "Декоры лиса": "Декоры",
    "себестоимость лиса осн": "Основные материалы",
    "себестоимость лиса всп": "Вспомогательные материалы",
    "пошив": "Пошив",
    # "раскорой" (опечатка в источнике) сюда не входит — её бакет зависит от
    # Level 01 (Раскрой/Вязание), см. _normalize_row_type.
}
_MANAGED_COST_BUCKETS = (
    "Основные материалы", "Вспомогательные материалы", "Декоры",
    "Пошив", "Раскрой", "Вязание",
)


def _recalc_cost_buckets(row: dict) -> None:
    """Routes Норма × цена материала into exactly ONE managed cost bucket,
    based on this row's Материал/операция/декор(призн) type.

    All OTHER managed buckets on this row are forced to 0 (clears stale money
    left over from a previous type/edit) so SUM() aggregation across rows never
    double-counts a single row's cost. Rows whose type isn't in
    _BUCKET_BY_MAT_TYPE are left completely untouched — no zeroing, no write —
    since we don't know which bucket (if any) they should own.
    """
    mat_type = (row.get("Материал/операция/декор(призн)") or "").strip()
    target = _BUCKET_BY_MAT_TYPE.get(mat_type)
    if target is None:
        return

    norm = row.get("Норма")
    price_rub = row.get("цена материала, руб.")
    price_usd = row.get("цена материала, USD.")

    def _product(price, factor_key) -> float | None:
        """Норма × цена × коэффициент источника (см. _compute_cost_factors).

        Коэффициент обязателен: в источнике бакет НЕ равен Норма × цена — цена
        там округлена до 4 знаков, а у четверти строк бакет меньше произведения
        в разы (25-й процентиль отношения — 0.1741, 5-й — 0.0002 при медиане
        0.9988). Без коэффициента сохранение версии завышало себестоимость на
        этих строках. NULL/отсутствие коэффициента
        трактуем как 1 — так ведут себя операционные строки, где _derive_norm_price
        уже подобрал цену так, что произведение точно равно стоимости.

        Цена ровно 0 обнуляет стоимость статьи — осознанный компромисс. В базе
        есть 6 строк (на ~1М) с ценой 0.0000 и ненулевой стоимостью, суммарно
        0.02 руб — их стоимость при первом сохранении версии потеряется.
        Альтернатива (сохранять прежнюю стоимость) лишила бы пользователя
        возможности обнулить строку, введя 0, что важнее двух копеек. Цена None
        (в отличие от 0) стоимость НЕ трогает — таких строк 9, там цены просто нет.
        """
        if norm is None or price is None:
            return None
        try:
            value = float(norm) * float(price)
        except (ValueError, TypeError):
            return None
        factor = row.get(factor_key)
        if factor is not None:
            try:
                value *= float(factor)
            except (ValueError, TypeError):
                pass
        return value

    sum_rub = _product(price_rub, "cost_factor_rub")
    sum_usd = _product(price_usd, "cost_factor_usd")

    for bucket in _MANAGED_COST_BUCKETS:
        if bucket != target:
            row[f"{bucket}, руб."] = 0
            row[f"{bucket}, USD."] = 0

    if sum_rub is not None:
        row[f"{target}, руб."] = sum_rub
    if sum_usd is not None:
        row[f"{target}, USD."] = sum_usd


# Level 01, для которых легаси-код "раскорой" в источнике на самом деле означает
# "Вязание" (эти группы вяжутся, а не кроятся — своего кода под вязание в
# источнике нет, используется тот же "раскорой").
_KNITTING_LEVEL01 = {"Носки&Колготки", "Orodoro"}


def _f(v) -> float:
    try:
        return float(v)
    except (TypeError, ValueError):
        return 0.0


def _derive_norm_price(row: dict, minutes_raw, rub_raw, usd_raw) -> None:
    """Пишет в row['Норма']/['цена материала, ...'] такие значения, что
    Норма×цена воспроизводит исходную (уже готовую) сумму в рублях/USD.

    В источнике для техопераций нет поля цены — только минуты и готовая
    стоимость (см. обсуждение с пользователем). Если минуты нулевые, но сумма
    есть (ручная правка без учёта минут) — Норма=1, цена=сумма, тождественно,
    без потери денег.
    """
    minutes = _f(minutes_raw)
    rub = _f(rub_raw)
    usd = _f(usd_raw)
    if minutes:
        row["Норма"] = minutes
        row["цена материала, руб."] = rub / minutes
        row["цена материала, USD."] = usd / minutes
    elif rub or usd:
        row["Норма"] = 1.0
        row["цена материала, руб."] = rub
        row["цена материала, USD."] = usd
    else:
        row["Норма"] = 0.0
        row["цена материала, руб."] = 0.0
        row["цена материала, USD."] = 0.0


# Легаси-коды типа из источника → канонический тип дропдауна (кроме "раскорой" —
# у неё бакет/канон зависит от Level 01, см. _normalize_row_type).
_LEGACY_TYPE_ALIASES: dict[str, str] = {
    "шт": "Декор",
    "Декоры лиса": "Декор",
    "себестоимость лиса осн": "Материал основной",
    "себестоимость лиса всп": "Материал вспомогательный",
    "пошив": "Пошив",
}


def _normalize_row_type(row: dict) -> None:
    """Приводит легаси-строковый код типа строки из источника (cost_data_cache)
    к каноническому значению дропдауна редактора версий, и для операционных
    строк (Пошив/Раскрой/Вязание) выводит Норма/цена материала из минут +
    готовой стоимости, чтобы строку можно было редактировать как материальную
    (Норма×цена), не имея реального поля цены в источнике.

    Строки с уже каноническим или неизвестным типом не трогает.
    """
    raw_type = (row.get("Материал/операция/декор(призн)") or "").strip()

    if raw_type == "раскорой":
        level01 = (row.get("Level 01") or "").strip()
        is_knitting = level01 in _KNITTING_LEVEL01
        row["Материал/операция/декор(призн)"] = "Вязание" if is_knitting else "Раскрой"
        # Только 10% нормы идёт в раскрой — кроме групп вязания, где то же поле
        # источника значит вязание и берётся как есть (без коэффициента).
        minutes = row.get("Раскрой, минуты")
        scale = 1.0 if is_knitting else 0.1
        minutes_scaled = _f(minutes) * scale if minutes is not None else minutes
        _derive_norm_price(row, minutes_scaled, row.get("Раскрой, руб."), row.get("Раскрой, USD."))
        return

    canonical = _LEGACY_TYPE_ALIASES.get(raw_type)
    if canonical is None:
        return
    row["Материал/операция/декор(призн)"] = canonical
    if canonical == "Пошив":
        _derive_norm_price(row, row.get("Пошив, минуты"), row.get("Пошив, руб."), row.get("Пошив, USD."))


async def save_version_draft(version_id, rows) -> None:
    async with pool().acquire() as conn:
        async with conn.transaction():
            status = await conn.fetchval(
                "SELECT status FROM cost_calc_versions WHERE id=$1", version_id
            )
            if status == "original":
                raise ValueError("Исходная версия неизменяема — редактирование создаёт новую версию")
            await conn.execute(
                "DELETE FROM cost_calc_version_rows WHERE version_id=$1",
                version_id,
            )
            col_names = CACHE_COLUMNS
            for row in rows:
                _recalc_cost_buckets(row)
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
            ver = await conn.fetchrow(
                """SELECT model, articul, calc_sign, plan_id
                   FROM cost_calc_versions WHERE id=$1""",
                version_id,
            )
            if ver is None:
                return
            # Archive previous pending versions of this key (supersession)
            await conn.execute(
                """UPDATE cost_calc_versions SET status='archived'
                   WHERE model=$1 AND articul=$2
                     AND calc_sign IS NOT DISTINCT FROM $3
                     AND plan_id IS NOT DISTINCT FROM $4
                     AND status='pending' AND id <> $5""",
                ver["model"], ver["articul"], ver["calc_sign"], ver["plan_id"], version_id,
            )
            await conn.execute(
                "UPDATE cost_calc_versions SET status='pending', comment=$2 WHERE id=$1",
                version_id, comment,
            )
            await _apply_version_rows_to_cache(conn, version_id)
            # Reset PEO approval — new submission supersedes the old one
            await conn.execute(
                """DELETE FROM cost_calc_approvals
                   WHERE model=$1 AND articul=$2
                     AND calc_sign IS NOT DISTINCT FROM $3
                     AND plan_id IS NOT DISTINCT FROM $4
                     AND status='approved'""",
                ver["model"], ver["articul"], ver["calc_sign"], ver["plan_id"],
            )


async def _apply_version_rows_to_cache(conn, version_id) -> None:
    """Применяет строки версии к cost_data_cache — заменяет собой строки
    АКТУАЛЬНОЙ (max) "дата расчета" для этого ключа на момент применения, а не
    даты, под которой версия была создана. Так согласованная версия продолжает
    отражаться в основной таблице даже если источник успел пересчитать задание
    (и проставить новую "дата расчета") между созданием версии и её применением.
    Исторические даты в кэше не трогает.
    """
    ver = await conn.fetchrow(
        """SELECT model, articul, calc_sign, plan_id, task_number
           FROM cost_calc_versions WHERE id=$1""",
        version_id,
    )
    if ver is None:
        return
    current_date = await _current_cache_date(
        conn, ver["model"], ver["articul"], ver["calc_sign"], ver["plan_id"], ver["task_number"]
    )
    if current_date is None:
        return  # для ключа сейчас нет данных в кэше — применять некуда

    # Область замены ограничиваем заданием (миграция 0032). Пустой task_number —
    # версия старого формата: она охватывала все задания ключа, поэтому и
    # заменяет их все, как раньше. Такая версия получит конкретное задание при
    # следующем сохранении.
    task = ver["task_number"] or ""
    task_filter = "" if task == "" else ' AND trim("Номер задания производства") = $6'
    params = [ver["model"], ver["articul"], ver["calc_sign"], ver["plan_id"], current_date]
    if task != "":
        params.append(task)
    await conn.execute(
        f"""DELETE FROM cost_data_cache
           WHERE "Модель"=$1 AND "Артикул"=$2
             AND "Признак калькуляции" IS NOT DISTINCT FROM $3
             AND "PLAN_ID" IS NOT DISTINCT FROM $4
             AND "дата расчета"=$5::timestamptz{task_filter}""",
        *params,
    )
    # Insert version rows into cache — "дата расчета" принудительно = current_date
    # (строки версии могут нести устаревшую дату), "дата производства" — как есть.
    col_list = ", ".join(f'"{c}"' for c in CACHE_COLUMNS)
    select_items = []
    for c in CACHE_COLUMNS:
        if c == "дата расчета":
            select_items.append("$2::timestamptz")
        elif c == "дата производства":
            select_items.append(f'"{c}"::timestamptz')
        else:
            select_items.append(f'"{c}"')
    select_list = ", ".join(select_items)
    await conn.execute(
        f"""INSERT INTO cost_data_cache ({col_list})
            SELECT {select_list}
            FROM cost_calc_version_rows
            WHERE version_id=$1""",
        version_id, current_date,
    )


async def _reapply_active_versions_to_cache(conn) -> int:
    """После обновления кэша (TRUNCATE/partial-delete + реимпорт из MSSQL)
    переприменяет самую свежую pending/approved версию по каждому ключу
    калькуляции поверх только что загруженных данных — иначе обновление кэша
    молча стирает эффект уже согласованных/отправленных на согласование
    изменений цен по ВСЕЙ базе.

    "Свежая" — по created_at, среди pending/approved (draft не учитывается —
    черновики никогда не были применены к cost_data_cache). Это то же правило
    "последнее применение побеждает", что уже действует при обычном
    submit/approve — здесь мы просто восстанавливаем его после реимпорта.

    Группировка — по (model, articul, calc_sign, plan_id, task_number) БЕЗ
    "дата расчета" (см. миграции 0027 и 0032): версия принадлежит заданию, а не
    конкретной исторической дате пересчёта; _apply_version_rows_to_cache сама
    наложит её на актуальную (max) дату для ключа.
    """
    version_ids = await conn.fetch(
        """SELECT DISTINCT ON (model, articul, calc_sign, plan_id, task_number)
                  id
           FROM cost_calc_versions
           WHERE status IN ('pending', 'approved')
           ORDER BY model, articul, calc_sign, plan_id, task_number, created_at DESC"""
    )
    for r in version_ids:
        await _apply_version_rows_to_cache(conn, r["id"])
    return len(version_ids)


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


async def get_active_version(model, articul, calc_sign, plan_id, raw_date=None, task_number=None) -> dict | None:
    """*raw_date* принимается только для обратной совместимости API и не
    используется — версия принадлежит заданию целиком, не дате (миграция 0027).

    Задание сужает выборку (миграция 0032); при равенстве прочего предпочитаем
    версию с конкретным заданием, а не легаси с пустым."""
    task_sql, task_params = _task_match_sql(task_number, 5)
    async with pool().acquire() as conn:
        ver = await conn.fetchrow(
            f"""SELECT * FROM cost_calc_versions
               WHERE model=$1 AND articul=$2 AND calc_sign IS NOT DISTINCT FROM $3
                 AND plan_id IS NOT DISTINCT FROM $4
                 AND status IN ('draft', 'pending'){task_sql}
               ORDER BY (task_number <> '') DESC, created_at DESC LIMIT 1""",
            model, articul, calc_sign, plan_id, *task_params,
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


async def archive_versions_by_key(model, articul, calc_sign, plan_id, raw_date=None, task_number=None) -> int:
    """*raw_date* принимается только для обратной совместимости API и не
    используется — версия принадлежит заданию целиком, не дате (миграция 0027).

    Легаси-версии с пустым заданием тоже архивируются: они сейчас применены и к
    этому заданию, поэтому оставить их — значит оставить калькуляцию в
    противоречивом состоянии после «снять версии»."""
    task_sql, task_params = _task_match_sql(task_number, 5)
    async with pool().acquire() as conn:
        result = await conn.execute(
            f"""UPDATE cost_calc_versions
               SET status = 'archived'
               WHERE model = $1 AND articul = $2
                 AND calc_sign IS NOT DISTINCT FROM $3
                 AND plan_id IS NOT DISTINCT FROM $4
                 AND status IN ('draft', 'pending'){task_sql}""",
            model, articul, calc_sign, plan_id, *task_params,
        )
        return int(result.split()[1]) if result.startswith("UPDATE") else 0


async def get_version_info(version_id) -> dict | None:
    async with pool().acquire() as conn:
        row = await conn.fetchrow(
            """SELECT model, articul, calc_sign, plan_id, task_number, "дата расчета"::text, status
               FROM cost_calc_versions WHERE id = $1""",
            version_id,
        )
        return dict(row) if row else None


async def get_raw_cache_rows(model, articul, calc_sign, plan_id, raw_date=None, task_number=None) -> dict:
    """Return "Исходные данные" for the editor: the frozen 'original' version's
    rows if this calc has already been saved/edited at least once (see
    _ensure_original_version); otherwise the live cost_data_cache rows for the
    CURRENT (max) "дата расчета" — always the latest recalculation from the
    source, regardless of which historical date's row in the aggregated table
    the user opened the editor from (см. миграцию 0027 — версии и "исходные
    данные" принадлежат заданию, а не конкретной дате). *raw_date* принимается
    только для обратной совместимости API и не используется.

    version_id is always None in the response regardless of which source was
    used — the frontend relies on that to route saves through /create-version
    (fork a new version) rather than treating this as an in-place-editable
    version_id.
    """
    task = (task_number or "").strip()
    task_sql, task_params = _task_match_sql(task, 5)
    async with pool().acquire() as conn:
        original = await conn.fetchrow(
            f"""SELECT id FROM cost_calc_versions
               WHERE model=$1 AND articul=$2
                 AND calc_sign IS NOT DISTINCT FROM $3
                 AND plan_id IS NOT DISTINCT FROM $4
                 AND status = 'original'{task_sql}
               ORDER BY task_number DESC
               LIMIT 1""",
            model, articul, calc_sign, plan_id, *task_params,
        )
        if original is not None:
            rows = await conn.fetch(
                """SELECT * FROM cost_calc_version_rows WHERE version_id=$1 ORDER BY sort_order""",
                original["id"],
            )
            return {"version_id": None, "rows": [dict(r) for r in rows]}

        date = await _current_cache_date(conn, model, articul, calc_sign, plan_id, task)
        if date is None:
            return {"version_id": None, "rows": []}
        cache_task_sql = "" if task == "" else ' AND trim("Номер задания производства") = $6'
        cache_params = [model, articul, calc_sign, plan_id, date]
        if task != "":
            cache_params.append(task)
        rows = await conn.fetch(
            f"""SELECT {", ".join(f'"{c}"' for c in CACHE_COLUMNS)}
               FROM cost_data_cache
               WHERE "Модель"=$1 AND "Артикул"=$2
                 AND "Признак калькуляции" IS NOT DISTINCT FROM $3
                 AND "PLAN_ID" IS NOT DISTINCT FROM $4
                 AND "дата расчета"=$5{cache_task_sql}
               ORDER BY id""",
            *cache_params,
        )
        return {"version_id": None, "rows": [dict(r) for r in rows]}


async def list_versions(model, articul, calc_sign, plan_id, raw_date=None, task_number=None) -> list[dict]:
    """Версии принадлежат заданию (model, articul, calc_sign, plan_id,
    task_number) целиком — *raw_date* принимается только для обратной
    совместимости API и не используется (см. миграции 0027 и 0032)."""
    task_sql, task_params = _task_match_sql(task_number, 5)
    async with pool().acquire() as conn:
        rows = await conn.fetch(
            f"""SELECT id, version, status, comment, created_at::text, created_by,
                      approved_by, approved_at::text, task_number
               FROM cost_calc_versions
               WHERE model=$1 AND articul=$2
                 AND calc_sign IS NOT DISTINCT FROM $3
                 AND plan_id IS NOT DISTINCT FROM $4
                 AND status != 'original'{task_sql}
               ORDER BY version DESC""",
            model, articul, calc_sign, plan_id, *task_params,
        )
        return [dict(r) for r in rows]


async def get_version_rows(version_id) -> dict | None:
    async with pool().acquire() as conn:
        ver = await conn.fetchrow(
            """SELECT id, version, status FROM cost_calc_versions WHERE id=$1""",
            version_id,
        )
        if ver is None:
            return None
        rows = await conn.fetch(
            """SELECT * FROM cost_calc_version_rows WHERE version_id=$1 ORDER BY sort_order""",
            version_id,
        )
        return {
            "version_id": version_id,
            "version": ver["version"],
            "status": ver["status"],
            "rows": [dict(r) for r in rows],
        }


def _task_match_sql(task_number, param_idx: int, column: str = "task_number") -> tuple[str, list]:
    """Фильтр «версия относится к этому заданию» (миграция 0032).

    Совпадение точное ИЛИ по пустому заданию: версии старого формата (созданные
    до 0032) охватывали все задания ключа, поэтому продолжают находиться и
    применяться, пока их не перезапишут — при следующем сохранении такая версия
    получает конкретное задание. Пустое задание на входе (ПКПСС, где заданий в
    источнике нет вовсе) фильтр не накладывает.

    Возвращает (SQL-фрагмент, доп. параметры).
    """
    task = (task_number or "").strip()
    if task == "":
        return "", []
    return f" AND ({column} = ${param_idx} OR {column} = '')", [task]


async def _current_cache_date(conn, model, articul, calc_sign, plan_id, task_number=None):
    """Самая свежая "дата расчета" для этого ключа в cost_data_cache — источник
    (MSSQL CostHistory) регулярно пересчитывает задание заново, оставляя старые
    даты как историю; "актуальная" калькуляция — всегда самая свежая из них.
    None, если для ключа в кэше сейчас вообще нет строк.

    task_number сужает до конкретного задания (миграция 0032). Пустой или None —
    считаем по всем заданиям ключа: так ведут себя ПКПСС (где задания в источнике
    нет вовсе) и версии старого формата.
    """
    task = (task_number or "").strip()
    task_filter = "" if task == "" else ' AND trim("Номер задания производства") = $5'
    params = [model, articul, calc_sign, plan_id]
    if task != "":
        params.append(task)
    return await conn.fetchval(
        f"""SELECT max("дата расчета") FROM cost_data_cache
           WHERE "Модель"=$1 AND "Артикул"=$2
             AND "Признак калькуляции" IS NOT DISTINCT FROM $3
             AND "PLAN_ID" IS NOT DISTINCT FROM $4{task_filter}""",
        *params,
    )


async def _ensure_original_version(conn, model, articul, calc_sign, plan_id, username, task_number=None) -> None:
    """Lazily snapshots cost_data_cache for this calc key into an immutable
    version(version=0, status='original') the first time a real save happens
    for this key. No-op if one already exists (ON CONFLICT DO NOTHING on the
    partial unique index — safe under concurrent first-saves).

    Deliberately cheap: only calculations that actually get edited pay for a
    snapshot, unlike mirroring the whole ~1M-row cache. Trade-off: for calcs
    already edited/submitted BEFORE this existed, the snapshot reflects
    whatever is currently in cost_data_cache (possibly already modified), not
    the true historical MSSQL original — accepted, see discussion with user.

    Key is (model, articul, calc_sign, plan_id, task_number) WITHOUT
    "дата расчета" — see migrations 0027 and 0032: the version belongs to the
    production task, not to a specific recalculation date, since the source
    recalculates (and stamps a new date) repeatedly over the task's lifetime.
    """
    task = (task_number or "").strip()
    date = await _current_cache_date(conn, model, articul, calc_sign, plan_id, task)
    if date is None:
        return  # для ключа сейчас нет данных в кэше — снимать нечего

    new_id = await conn.fetchval(
        """INSERT INTO cost_calc_versions
               (model, articul, calc_sign, plan_id, task_number, "дата расчета", version, status, created_by)
           VALUES ($1, $2, $3, $4, $5, $6, 0, 'original', $7)
           ON CONFLICT (model, articul, calc_sign, plan_id, task_number) WHERE status = 'original'
           DO NOTHING
           RETURNING id""",
        model, articul, calc_sign, plan_id, task, date, username,
    )
    if new_id is None:
        return  # уже существует

    col_list = ", ".join(f'"{c}"' for c in CACHE_COLUMNS)
    task_filter = "" if task == "" else ' AND trim("Номер задания производства") = $6'
    cache_params = [model, articul, calc_sign, plan_id, date]
    if task != "":
        cache_params.append(task)
    cache_rows = await conn.fetch(
        f"""SELECT {col_list} FROM cost_data_cache
            WHERE "Модель"=$1 AND "Артикул"=$2
              AND "Признак калькуляции" IS NOT DISTINCT FROM $3
              AND "PLAN_ID" IS NOT DISTINCT FROM $4
              AND "дата расчета"=$5{task_filter}
            ORDER BY id""",
        *cache_params,
    )
    for sort_order, r in enumerate(cache_rows):
        vrow = dict(r)
        _normalize_row_type(vrow)
        _recalc_cost_buckets(vrow)
        insert_values = [vrow.get(c) for c in CACHE_COLUMNS]
        await conn.execute(
            f"""INSERT INTO cost_calc_version_rows
                (version_id, {col_list}, sort_order, change_type)
                VALUES ($1, {", ".join(f"${j+2}" for j in range(len(CACHE_COLUMNS)))},
                        ${len(CACHE_COLUMNS)+2}, ${len(CACHE_COLUMNS)+3})""",
            new_id, *insert_values, sort_order, 'original',
        )


async def create_version(model, articul, calc_sign, plan_id, raw_date, username, rows, status="draft", task_number=None) -> dict:
    """Create a new version. If status='pending': applies rows to cache,
    archives previous pending versions for this key, resets PEO approval.

    Версия принадлежит заданию (model, articul, calc_sign, plan_id, task_number),
    не дате — см. миграции 0027 и 0032. "дата расчета" на самой версии —
    информационная (на основе какой даты она создана); резолвится из текущего
    cost_data_cache, а не из того, что прислал фронтенд (там может быть уже
    устаревшая дата, если источник успел пересчитать задание, пока была открыта
    форма).
    """
    task = (task_number or "").strip()
    async with pool().acquire() as conn:
        async with conn.transaction():
            await _ensure_original_version(conn, model, articul, calc_sign, plan_id, username, task)
            date = await _current_cache_date(conn, model, articul, calc_sign, plan_id, task)
            if date is None:
                # для ключа сейчас нет строк в кэше — используем то, что прислал фронтенд
                if isinstance(raw_date, str) and raw_date:
                    date = datetime.datetime.fromisoformat(raw_date.replace("Z", "+00:00")).date()
                else:
                    date = raw_date
            next_version = await conn.fetchval(
                """SELECT COALESCE(MAX(version), 0) + 1 FROM cost_calc_versions
                   WHERE model=$1 AND articul=$2 AND calc_sign IS NOT DISTINCT FROM $3
                     AND plan_id IS NOT DISTINCT FROM $4 AND task_number = $5""",
                model, articul, calc_sign, plan_id, task,
            )
            status_refreshed = await conn.fetchrow("SELECT refreshed_at FROM cost_cache_status WHERE id=1")
            source_refreshed_at = status_refreshed["refreshed_at"] if status_refreshed else None
            version_id = await conn.fetchval(
                """INSERT INTO cost_calc_versions
                   (model, articul, calc_sign, plan_id, task_number, "дата расчета", version,
                    source_refreshed_at, created_by, status)
                   VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
                   RETURNING id""",
                model, articul, calc_sign, plan_id, task, date, next_version,
                source_refreshed_at, username, status,
            )
            col_names = CACHE_COLUMNS
            for row in rows:
                _recalc_cost_buckets(row)
                values = [version_id]
                for col in col_names:
                    val = row.get(col)
                    if col in _CACHE_NON_TEXT:
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
                        VALUES ($1, {", ".join(f"${j+2}" for j in range(len(col_names)))},
                                ${len(col_names)+2}, ${len(col_names)+3})""",
                    *values, sort_order, change_type,
                )
            if status == "pending":
                # Archive previous pending versions of this key (supersession).
                # Вытесняем только версии ЭТОГО задания — у соседних заданий той
                # же модели своя независимая история (миграция 0032). Пустой task
                # (ПКПСС/легаси) вытесняет весь ключ, как раньше.
                task_ver_filter = "" if task == "" else " AND task_number = $6"
                archive_params = [model, articul, calc_sign, plan_id, version_id]
                if task != "":
                    archive_params.append(task)
                await conn.execute(
                    f"""UPDATE cost_calc_versions SET status='archived'
                       WHERE model=$1 AND articul=$2
                         AND calc_sign IS NOT DISTINCT FROM $3
                         AND plan_id IS NOT DISTINCT FROM $4
                         AND status='pending' AND id <> $5{task_ver_filter}""",
                    *archive_params,
                )
                await _apply_version_rows_to_cache(conn, version_id)
                # Reset PEO approval — new submission supersedes the old one.
                # У cost_calc_approvals задание в ключе было и раньше, поэтому
                # сбрасываем согласование только по этому заданию.
                task_appr_filter = "" if task == "" else " AND COALESCE(trim(task_number),'') = $5"
                appr_params = [model, articul, calc_sign, plan_id]
                if task != "":
                    appr_params.append(task)
                await conn.execute(
                    f"""DELETE FROM cost_calc_approvals
                       WHERE model=$1 AND articul=$2
                         AND calc_sign IS NOT DISTINCT FROM $3
                         AND plan_id IS NOT DISTINCT FROM $4
                         AND status='approved'{task_appr_filter}""",
                    *appr_params,
                )
            return {"version_id": version_id, "version": next_version}


_PRICE_FIELDS = [
    '"Розничная цена по уровню, руб."',
    '"Отпускная цена по уровню, руб"',
    '"Розничная цена по уровню, USD."',
    '"Отпускная цена по уровню, USD."',
    '"Уровень цен"',
]


async def reset_price_fields(model, articul, calc_sign, plan_id, raw_date) -> None:
    if isinstance(raw_date, str) and raw_date:
        d = datetime.datetime.fromisoformat(raw_date.replace("Z", "+00:00")).date()
    else:
        d = raw_date
    set_clause = ", ".join(f"{c} = NULL" for c in _PRICE_FIELDS)
    async with pool().acquire() as conn:
        async with conn.transaction():
            await conn.execute(
                f"""UPDATE cost_data_cache SET {set_clause}
                    WHERE "Модель" = $1 AND "Артикул" = $2
                      AND "Признак калькуляции" IS NOT DISTINCT FROM $3
                      AND "PLAN_ID" IS NOT DISTINCT FROM $4
                      AND "дата расчета" = $5""",
                model, articul, calc_sign, plan_id, d,
            )
            ver = await conn.fetchrow(
                """SELECT id FROM cost_calc_versions
                   WHERE model = $1 AND articul = $2
                     AND calc_sign IS NOT DISTINCT FROM $3
                     AND plan_id IS NOT DISTINCT FROM $4
                     AND "дата расчета" = $5
                     AND status = 'draft'
                   ORDER BY created_at DESC LIMIT 1""",
                model, articul, calc_sign, plan_id, d,
            )
            if ver:
                await conn.execute(
                    f"""UPDATE cost_calc_version_rows SET {set_clause}
                        WHERE version_id = $1""",
                    ver["id"],
                )
            await conn.execute(
                """DELETE FROM cost_price_pending
                   WHERE "Модель" = $1 AND "Артикул" = $2
                     AND "Признак калькуляции" IS NOT DISTINCT FROM $3
                     AND "PLAN_ID" IS NOT DISTINCT FROM $4""",
                model, articul, calc_sign, plan_id,
            )


async def delete_pending_by_key(model, articul, calc_sign, plan_id) -> int:
    async with pool().acquire() as conn:
        result = await conn.execute(
            """DELETE FROM cost_price_pending
               WHERE "Модель" = $1 AND "Артикул" = $2
                 AND "Признак калькуляции" IS NOT DISTINCT FROM $3
                 AND "PLAN_ID" IS NOT DISTINCT FROM $4""",
            model, articul, calc_sign, plan_id,
        )
        return int(result.split()[1]) if result.startswith("DELETE") else 0


async def get_calc_state(model, articul, calc_sign, plan_id, raw_date=None, task_number=None) -> dict:
    """*raw_date* принимается только для обратной совместимости API и не
    используется — версия принадлежит заданию целиком, не дате (миграция 0027).

    Состояние версии резолвится по заданию (миграция 0032), при прочих равных
    предпочитая версию с конкретным заданием легаси-версии с пустым. Признаки
    has_pending_price/has_dwh_record остаются на уровне ключа без задания —
    cost_price_pending и cost_price_changes_audit задания в ключе не имеют."""
    task_sql, task_params = _task_match_sql(task_number, 5)
    async with pool().acquire() as conn:
        ver = await conn.fetchrow(
            f"""SELECT id, status FROM cost_calc_versions
               WHERE model = $1 AND articul = $2
                 AND calc_sign IS NOT DISTINCT FROM $3
                 AND plan_id IS NOT DISTINCT FROM $4
                 AND status IN ('draft', 'pending'){task_sql}
               ORDER BY (task_number <> '') DESC, created_at DESC LIMIT 1""",
            model, articul, calc_sign, plan_id, *task_params,
        )
        has_pending = await conn.fetchval(
            """SELECT EXISTS (
                  SELECT 1 FROM cost_price_pending
                  WHERE "Модель" = $1 AND "Артикул" = $2
                    AND "Признак калькуляции" IS NOT DISTINCT FROM $3
                    AND "PLAN_ID" IS NOT DISTINCT FROM $4
               )""",
            model, articul, calc_sign, plan_id,
        )
        has_dwh = await conn.fetchval(
            """SELECT EXISTS (
                  SELECT 1 FROM cost_price_changes_audit
                  WHERE model = $1 AND articul = $2
                    AND calc_sign IS NOT DISTINCT FROM $3
                    AND plan_id IS NOT DISTINCT FROM $4
               )""",
            model, articul, calc_sign, plan_id,
        )
        # Согласование ПЭО пер-заданное и было таким до 0032 (task_number уже в
        # уникальном ключе cost_calc_approvals) — сужаем, иначе состояние одного
        # задания подсвечивалось бы согласованием соседнего.
        peo_task = (task_number or "").strip()
        peo_sql = "" if peo_task == "" else " AND COALESCE(trim(task_number),'') = $5"
        peo_params = [model, articul, calc_sign, plan_id]
        if peo_task != "":
            peo_params.append(peo_task)
        peo = await conn.fetchrow(
            f"""SELECT status FROM cost_calc_approvals
               WHERE model = $1 AND articul = $2
                 AND calc_sign IS NOT DISTINCT FROM $3
                 AND plan_id IS NOT DISTINCT FROM $4{peo_sql}
               LIMIT 1""",
            *peo_params,
        )
    return {
        "version_status": ver["status"] if ver else None,
        "version_id": ver["id"] if ver else None,
        "has_pending_price": bool(has_pending),
        "has_dwh_record": bool(has_dwh),
        "peo_status": peo["status"] if peo else None,
    }


# ── PEO approval (cost_calc_approvals) ─────────────────────────────────────


async def save_approval(model, articul, calc_sign, plan_id, status, approved_by, comment=None, task_number=None) -> dict:
    async with pool().acquire() as conn:
        async with conn.transaction():
            row = await conn.fetchrow(
                """
                INSERT INTO cost_calc_approvals
                    (model, articul, calc_sign, plan_id, task_number, status, approved_by, approved_at, comment)
                VALUES ($1, $2, $3, $4, $5, $6, $7,
                        CASE WHEN $6 IN ('approved', 'rejected') THEN NOW() ELSE NULL END,
                        $8)
                ON CONFLICT (model, articul, calc_sign, plan_id, task_number) DO UPDATE SET
                    status = EXCLUDED.status,
                    approved_by = EXCLUDED.approved_by,
                    approved_at = EXCLUDED.approved_at,
                    comment = EXCLUDED.comment,
                    updated_at = NOW()
                RETURNING *
                """,
                model, articul, calc_sign, plan_id, task_number, status, approved_by, comment,
            )
            return dict(row)


async def save_approvals_batch(approvals: list[dict]) -> list[dict]:
    results = []
    for a in approvals:
        r = await save_approval(
            a["model"], a.get("articul"), a.get("calc_sign"), a.get("plan_id"),
            a["status"], a.get("approved_by"), a.get("comment"), a.get("task_number"),
        )
        results.append(r)
    return results


async def revoke_approval(model, articul, calc_sign, plan_id, task_number=None) -> None:
    async with pool().acquire() as conn:
        await conn.execute(
            "DELETE FROM cost_calc_approvals WHERE model=$1 AND articul=$2 AND calc_sign IS NOT DISTINCT FROM $3 AND plan_id IS NOT DISTINCT FROM $4 AND task_number IS NOT DISTINCT FROM $5",
            model, articul, calc_sign, plan_id, task_number,
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


async def delete_dwh_record(model, articul, calc_sign, plan_id) -> dict:
    """Delete DWH record from local audit and OLAP. Returns counts."""
    # 1. Delete from local audit (postgres-cost)
    async with pool().acquire() as conn:
        audit_result = await conn.execute(
            """DELETE FROM cost_price_changes_audit
               WHERE model = $1 AND articul = $2
                 AND calc_sign IS NOT DISTINCT FROM $3
                 AND plan_id IS NOT DISTINCT FROM $4""",
            model, articul, calc_sign, plan_id,
        )
        audit_deleted = int(audit_result.split()[1]) if audit_result.startswith("DELETE") else 0

    # 2. Delete from OLAP (MSSQL)
    olap_deleted = 0
    proc_db = os.environ.get("PROC_DB_PROCEDURE")
    if proc_db:
        olap = get_olap_conn()
        cursor = olap.cursor()
        try:
            cursor.execute(
                f"""DELETE FROM CostHistory_Changes
                    WHERE Модель = ? AND Артикул = ?
                      AND calc_sign IS NOT DISTINCT FROM ?
                      AND plan_id IS NOT DISTINCT FROM ?""",
                model, articul, calc_sign, plan_id,
            )
            olap.commit()
            olap_deleted = cursor.rowcount
        finally:
            olap.close()

    return {"audit_deleted": audit_deleted, "olap_deleted": olap_deleted}


# ── Наборы цен на материалы для плана (миграция 0033) ─────────────────────────
#
# Массовая правка цен материалов по PLAN_ID целиком, без привязки к модели,
# артикулу и заданию. Только КПСС — решение пользователя.
#
# Приоритет наложения на cost_data_cache (см. load_cost_data_to_cache):
#   источник → набор цен плана → версии калькуляций (побеждают).

# Типы строк источника, попадающие в наборы: только основные и вспомогательные
# материалы. Декоры и техоперации намеренно вне области — задача про материалы.
_PLAN_OSN_TYPES = ("себестоимость лиса осн", "Материал основной")
_PLAN_VSP_TYPES = ("себестоимость лиса всп", "Материал вспомогательный")
_PLAN_MATERIAL_TYPES: list[str] = list(_PLAN_OSN_TYPES) + list(_PLAN_VSP_TYPES)

# Ключ материала — пять полей источника (см. миграцию 0033).
_PLAN_MAT_KEY = ["Наименование", "артикул материала", "свойство1", "свойство2", "свойство3"]


def _plan_key_select(alias: str = "c") -> str:
    """trim(COALESCE(...)) по каждому полю ключа, с алиасами k0..k4."""
    return ", ".join(
        f"trim(COALESCE({alias}.\"{c}\", '')) AS k{i}"
        for i, c in enumerate(_PLAN_MAT_KEY)
    )


def _plan_key_join(left: str, right: str) -> str:
    """Условие соединения строки кэша со строкой набора по ключу материала."""
    return " AND ".join(
        f"trim(COALESCE({left}.\"{c}\", '')) = {right}.\"{c}\"" for c in _PLAN_MAT_KEY
    )


async def aggregate_plan_materials(plan_id: str) -> list[dict]:
    """Материалы плана, сгруппированные по ключу из пяти полей.

    Цена — средняя по группе (решение пользователя). Дополнительно отдаём
    distinct_prices/min/max, чтобы в UI было видно, где внутри группы был разброс
    и что применение набора его затрёт (таких групп ~1.3%, см. миграцию 0033).

    overridden_rows — сколько строк группы уже перекрыто активной версией
    калькуляции. Версия имеет приоритет над ценой плана, поэтому на этих строках
    цена из набора не подействует. Перекрытие бывает частичным: в плане в среднем
    5.8 пар модель+артикул, и версия есть не у всех.
    """
    plan = (plan_id or "").strip()
    if not plan:
        return []
    async with pool().acquire() as conn:
        rows = await conn.fetch(
            f"""
            WITH mat AS (
                SELECT {_plan_key_select('c')},
                       c."цена материала, руб." AS pr,
                       c."цена материала, USD." AS pu,
                       c."Курс на дату расчета" AS rate,
                       trim(c."Модель") AS m, trim(c."Артикул") AS a,
                       trim(COALESCE(c."Признак калькуляции", '')) AS cs,
                       trim(COALESCE(c."Номер задания производства", '')) AS t
                FROM cost_data_cache c
                WHERE trim(COALESCE(c."PLAN_ID", '')) = $1
                  AND trim(COALESCE(c."Признак калькуляции", '')) = 'КПСС'
                  AND c."Материал/операция/декор(призн)" = ANY($2::text[])
            ),
            active AS (
                SELECT DISTINCT model, articul, COALESCE(calc_sign, '') AS cs, task_number AS t
                FROM cost_calc_versions
                WHERE status IN ('pending', 'approved')
                  AND COALESCE(plan_id, '') = $1
            )
            SELECT mat.k0 AS "Наименование", mat.k1 AS "артикул материала",
                   mat.k2 AS "свойство1", mat.k3 AS "свойство2", mat.k4 AS "свойство3",
                   count(*) AS rows_count,
                   round(avg(mat.pr), 4) AS avg_price_rub,
                   round(avg(mat.pu), 4) AS avg_price_usd,
                   round(avg(mat.rate), 4) AS avg_rate,
                   count(DISTINCT mat.pr) AS distinct_prices,
                   round(min(mat.pr), 4) AS min_price_rub,
                   round(max(mat.pr), 4) AS max_price_rub,
                   count(*) FILTER (WHERE active.model IS NOT NULL) AS overridden_rows
            FROM mat
            LEFT JOIN active
                   ON active.model = mat.m AND active.articul = mat.a
                  AND active.cs = mat.cs
                  -- Пустое task_number у версии = легаси-версия старого формата,
                  -- она охватывает все задания ключа (см. миграцию 0032).
                  AND (active.t = mat.t OR active.t = '')
            GROUP BY mat.k0, mat.k1, mat.k2, mat.k3, mat.k4
            ORDER BY mat.k0, mat.k1, mat.k2, mat.k3, mat.k4
            """,
            plan, _PLAN_MATERIAL_TYPES,
        )
        return [dict(r) for r in rows]


async def list_plan_price_sets(plan_id: str | None = None) -> list[dict]:
    """Наборы цен. Без plan_id — все (для фильтра по номеру плана в UI)."""
    plan = (plan_id or "").strip()
    where = "WHERE s.plan_id = $1" if plan else ""
    params = [plan] if plan else []
    async with pool().acquire() as conn:
        rows = await conn.fetch(
            f"""SELECT s.id, s.plan_id, s.title, s.status, s.rate, s.comment,
                       s.created_by, s.created_at::text, s.updated_at::text,
                       s.applied_by, s.applied_at::text,
                       (SELECT count(*) FROM cost_plan_price_set_rows r
                         WHERE r.set_id = s.id) AS rows_count
                FROM cost_plan_price_sets s
                {where}
                ORDER BY s.plan_id, s.created_at DESC""",
            *params,
        )
        return [dict(r) for r in rows]


async def get_plan_price_set(set_id: int) -> dict | None:
    async with pool().acquire() as conn:
        s = await conn.fetchrow(
            """SELECT id, plan_id, title, status, rate, comment, created_by,
                      created_at::text, updated_at::text, applied_by, applied_at::text
               FROM cost_plan_price_sets WHERE id = $1""",
            set_id,
        )
        if s is None:
            return None
        rows = await conn.fetch(
            """SELECT id, "Наименование", "артикул материала",
                      "свойство1", "свойство2", "свойство3",
                      price_rub, price_usd, source_price_rub, source_price_usd, rows_count
               FROM cost_plan_price_set_rows WHERE set_id = $1
               ORDER BY "Наименование", "артикул материала" """,
            set_id,
        )
        return {"set": dict(s), "rows": [dict(r) for r in rows]}


async def save_plan_price_set(
    plan_id: str, title: str, rate, rows: list[dict], username: str,
    set_id: int | None = None, comment: str | None = None,
) -> int:
    """Создаёт новый набор либо перезаписывает строки существующего.

    Применённый набор править нельзя: его цены уже в расчёте, и молчаливая правка
    рассинхронизировала бы кэш с набором. Нужно сначала снять применение.
    """
    plan = (plan_id or "").strip()
    if not plan:
        raise ValueError("plan_id обязателен")
    async with pool().acquire() as conn:
        async with conn.transaction():
            if set_id is None:
                set_id = await conn.fetchval(
                    """INSERT INTO cost_plan_price_sets
                           (plan_id, title, rate, comment, created_by)
                       VALUES ($1, $2, $3, $4, $5) RETURNING id""",
                    plan, (title or "").strip(), rate, comment, username,
                )
            else:
                status = await conn.fetchval(
                    "SELECT status FROM cost_plan_price_sets WHERE id = $1", set_id
                )
                if status is None:
                    raise ValueError("набор не найден")
                if status == "applied":
                    raise ValueError("Применённый набор нельзя менять — сначала снимите применение")
                await conn.execute(
                    """UPDATE cost_plan_price_sets
                       SET title = $2, rate = $3, comment = $4, updated_at = NOW()
                       WHERE id = $1""",
                    set_id, (title or "").strip(), rate, comment,
                )
                await conn.execute(
                    "DELETE FROM cost_plan_price_set_rows WHERE set_id = $1", set_id
                )
            for r in rows:
                await conn.execute(
                    """INSERT INTO cost_plan_price_set_rows
                           (set_id, "Наименование", "артикул материала",
                            "свойство1", "свойство2", "свойство3",
                            price_rub, price_usd, source_price_rub, source_price_usd, rows_count)
                       VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)""",
                    set_id,
                    str(r.get("Наименование") or "").strip(),
                    str(r.get("артикул материала") or "").strip(),
                    str(r.get("свойство1") or "").strip(),
                    str(r.get("свойство2") or "").strip(),
                    str(r.get("свойство3") or "").strip(),
                    r.get("price_rub"), r.get("price_usd"),
                    r.get("source_price_rub"), r.get("source_price_usd"),
                    int(r.get("rows_count") or 0),
                )
    return set_id


async def delete_plan_price_set(set_id: int) -> None:
    """Удаляет набор. Применённый — только после снятия, иначе его цены остались
    бы в кэше без какого-либо следа происхождения."""
    async with pool().acquire() as conn:
        status = await conn.fetchval(
            "SELECT status FROM cost_plan_price_sets WHERE id = $1", set_id
        )
        if status == "applied":
            raise ValueError("Применённый набор нельзя удалить — сначала снимите применение")
        await conn.execute("DELETE FROM cost_plan_price_sets WHERE id = $1", set_id)


async def _apply_plan_price_set_to_cache(conn, set_id: int) -> int:
    """Накладывает цены набора на cost_data_cache и пересчитывает стоимость статей.

    Стоимость считается как Норма × цена × коэффициент — тем же правилом, что и
    _recalc_cost_buckets (миграция 0031). Именно поэтому правка цены сохраняет
    скрытый коэффициент источника и не раздувает себестоимость.

    Вызывать ДО наложения версий: версия имеет приоритет и должна затирать цену
    плана, а не наоборот.
    """
    osn = ", ".join(f"'{t}'" for t in _PLAN_OSN_TYPES)
    vsp = ", ".join(f"'{t}'" for t in _PLAN_VSP_TYPES)
    join = _plan_key_join("c", "r")
    result = await conn.execute(
        f"""
        UPDATE cost_data_cache c SET
            "цена материала, руб." = COALESCE(r.price_rub, c."цена материала, руб."),
            "цена материала, USD." = COALESCE(r.price_usd, c."цена материала, USD."),
            "Основные материалы, руб." = CASE
                WHEN c."Материал/операция/декор(призн)" IN ({osn}) AND r.price_rub IS NOT NULL
                THEN c."Норма" * r.price_rub * COALESCE(c.cost_factor_rub, 1)
                ELSE c."Основные материалы, руб." END,
            "Основные материалы, USD." = CASE
                WHEN c."Материал/операция/декор(призн)" IN ({osn}) AND r.price_usd IS NOT NULL
                THEN c."Норма" * r.price_usd * COALESCE(c.cost_factor_usd, 1)
                ELSE c."Основные материалы, USD." END,
            "Вспомогательные материалы, руб." = CASE
                WHEN c."Материал/операция/декор(призн)" IN ({vsp}) AND r.price_rub IS NOT NULL
                THEN c."Норма" * r.price_rub * COALESCE(c.cost_factor_rub, 1)
                ELSE c."Вспомогательные материалы, руб." END,
            "Вспомогательные материалы, USD." = CASE
                WHEN c."Материал/операция/декор(призн)" IN ({vsp}) AND r.price_usd IS NOT NULL
                THEN c."Норма" * r.price_usd * COALESCE(c.cost_factor_usd, 1)
                ELSE c."Вспомогательные материалы, USD." END
        FROM cost_plan_price_set_rows r, cost_plan_price_sets s
        WHERE r.set_id = s.id AND s.id = $1
          AND trim(COALESCE(c."PLAN_ID", '')) = s.plan_id
          AND trim(COALESCE(c."Признак калькуляции", '')) = 'КПСС'
          AND c."Материал/операция/декор(призн)" = ANY($2::text[])
          AND c."Норма" IS NOT NULL
          AND {join}
        """,
        set_id, _PLAN_MATERIAL_TYPES,
    )
    return int(result.split()[1]) if result.startswith("UPDATE") else 0


async def _reapply_applied_plan_price_sets(conn) -> int:
    """Переналожение применённых наборов после реимпорта кэша.

    Реимпорт из источника стирает эффект наборов так же, как стирал эффект
    версий, — поэтому вызывается из load_cost_data_to_cache сразу после расчёта
    коэффициентов и ПЕРЕД _reapply_active_versions_to_cache, чтобы версии легли
    сверху и сохранили свой приоритет.
    """
    ids = await conn.fetch("SELECT id FROM cost_plan_price_sets WHERE status = 'applied'")
    for r in ids:
        await _apply_plan_price_set_to_cache(conn, r["id"])
    return len(ids)


async def _fetch_plan_source_records(plan: str) -> tuple[list[str], list[tuple]]:
    """Строки одного плана из MSSQL. Отдельно от записи в Postgres, чтобы сетевой
    ввод-вывод не держал транзакцию открытой и чтобы перестройку кэша можно было
    выполнить одним атомарным блоком (см. _rebuild_plan_cache)."""

    def _fetch() -> tuple[list[str], list[tuple]]:
        conn_ms = get_mssql_conn()
        conn_ms.timeout = 300
        cur = conn_ms.cursor()
        try:
            cur.execute("SELECT * FROM [Checks].[dbo].[CostHistory] WHERE [PLAN_ID] = ?", plan)
            mssql_columns = [d[0] for d in cur.description]
            cols = [c for c in CACHE_COLUMNS
                    if CACHE_COLUMN_MSSQL_MAP.get(c, c) in mssql_columns]
            idx = [mssql_columns.index(CACHE_COLUMN_MSSQL_MAP.get(c, c)) for c in cols]
            out = [_convert_mssql_row(r, idx, cols) for r in cur.fetchall()]
            return cols, out
        finally:
            conn_ms.close()

    loop = asyncio.get_running_loop()
    return await loop.run_in_executor(None, _fetch)


async def _rebuild_plan_cache(conn, plan: str, cols: list[str], records: list[tuple]) -> dict:
    """Пересобирает строки плана в кэше: источник → коэффициенты → набор цен →
    версии. Порядок задаёт приоритет (версия побеждает цену плана).

    Вызывать ВНУТРИ транзакции: вместе с изменением статуса набора это даёт
    атомарность — иначе набор мог остаться помеченным applied при неудачной
    перестройке кэша, и UI показывал бы применение, которого в цифрах нет.
    """
    await conn.execute(
        'DELETE FROM cost_data_cache WHERE trim(COALESCE("PLAN_ID", \'\')) = $1', plan
    )
    if records:
        await conn.copy_records_to_table("cost_data_cache", records=records, columns=cols)
    await conn.execute(
        f"""
        UPDATE cost_data_cache SET
            cost_factor_rub = CASE
                WHEN COALESCE("Норма",0) <> 0 AND COALESCE("цена материала, руб.",0) <> 0
                THEN ({_BUCKET_SUM_SQL.format(c="руб.")}) / ("Норма" * "цена материала, руб.")
            END,
            cost_factor_usd = CASE
                WHEN COALESCE("Норма",0) <> 0 AND COALESCE("цена материала, USD.",0) <> 0
                THEN ({_BUCKET_SUM_SQL.format(c="USD.")}) / ("Норма" * "цена материала, USD.")
            END
        WHERE trim(COALESCE("PLAN_ID", '')) = $1
        """,
        plan,
    )
    applied = await conn.fetchval(
        "SELECT id FROM cost_plan_price_sets WHERE plan_id = $1 AND status = 'applied'",
        plan,
    )
    touched = 0
    if applied is not None:
        touched = await _apply_plan_price_set_to_cache(conn, applied)
    version_ids = await conn.fetch(
        """SELECT DISTINCT ON (model, articul, calc_sign, plan_id, task_number) id
           FROM cost_calc_versions
           WHERE status IN ('pending', 'approved') AND COALESCE(plan_id, '') = $1
           ORDER BY model, articul, calc_sign, plan_id, task_number, created_at DESC""",
        plan,
    )
    for r in version_ids:
        await _apply_version_rows_to_cache(conn, r["id"])
    return {
        "plan_id": plan,
        "source_rows": len(records),
        "price_rows_applied": touched,
        "versions_reapplied": len(version_ids),
    }


async def refresh_plan_from_source(plan_id: str) -> dict:
    """Перезаливает строки одного плана из источника и заново накладывает набор
    цен и версии.

    Нужна для точной семантики применения/снятия набора: наложение цен пишет в
    кэш, и «снять» нельзя, просто вернув среднюю цену, — внутри группы могли быть
    разные цены (те 1.3% групп с разбросом). Поэтому берём исходные строки плана
    из MSSQL заново, а затем накладываем то, что должно быть применено сейчас.

    Область ограничена планом, так что операция дешёвая: под КПСС это ~100 строк
    на план, а не миллион, как при полном рефреше.
    """
    plan = (plan_id or "").strip()
    if not plan:
        raise ValueError("plan_id обязателен")
    cols, records = await _fetch_plan_source_records(plan)
    async with pool().acquire() as conn:
        async with conn.transaction():
            return await _rebuild_plan_cache(conn, plan, cols, records)


async def apply_plan_price_set(set_id: int, username: str) -> dict:
    """Делает набор применённым к расчёту. Прежний применённый набор этого плана
    уходит в archived — по плану может быть применён только один (частичный
    уникальный индекс, см. миграцию 0033).

    Смена статуса и перестройка кэша — в ОДНОЙ транзакции, поэтому набор не может
    остаться помеченным applied при неудачной перестройке. Чтение из MSSQL идёт
    до транзакции, чтобы не держать её открытой на время сетевого запроса.
    """
    async with pool().acquire() as conn:
        row = await conn.fetchrow(
            "SELECT plan_id, status FROM cost_plan_price_sets WHERE id = $1", set_id
        )
    if row is None:
        raise ValueError("набор не найден")
    plan = (row["plan_id"] or "").strip()
    cols, records = await _fetch_plan_source_records(plan)
    async with pool().acquire() as conn:
        async with conn.transaction():
            await conn.execute(
                """UPDATE cost_plan_price_sets SET status = 'archived'
                   WHERE plan_id = $1 AND status = 'applied' AND id <> $2""",
                plan, set_id,
            )
            await conn.execute(
                """UPDATE cost_plan_price_sets
                   SET status = 'applied', applied_by = $2, applied_at = NOW(), updated_at = NOW()
                   WHERE id = $1""",
                set_id, username,
            )
            return await _rebuild_plan_cache(conn, plan, cols, records)


async def unapply_plan_price_set(set_id: int) -> dict:
    """Исключает набор из расчёта: статус возвращается в draft, а строки плана
    перезаливаются из источника (вернуть среднюю цену недостаточно — внутри
    группы цены могли различаться). Тоже одной транзакцией, см. apply."""
    async with pool().acquire() as conn:
        row = await conn.fetchrow(
            "SELECT plan_id, status FROM cost_plan_price_sets WHERE id = $1", set_id
        )
    if row is None:
        raise ValueError("набор не найден")
    if row["status"] != "applied":
        raise ValueError("набор не применён")
    plan = (row["plan_id"] or "").strip()
    cols, records = await _fetch_plan_source_records(plan)
    async with pool().acquire() as conn:
        async with conn.transaction():
            await conn.execute(
                """UPDATE cost_plan_price_sets
                   SET status = 'draft', applied_by = NULL, applied_at = NULL, updated_at = NOW()
                   WHERE id = $1""",
                set_id,
            )
            return await _rebuild_plan_cache(conn, plan, cols, records)
