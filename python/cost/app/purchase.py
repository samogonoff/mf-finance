"""
Закупная готовая продукция (пожелания № 6 и № 7): импорт КПСС из портала БМ.

Ассортимент, который закупается у сторонних производителей, в CostHistory не
попадает вовсе. Его плановая себестоимость (КПСС) считается на портале БМ —
база `mfportal` на OLAP-сервере, таблицы `bm_chl_*`:

  · `bm_chl_checklists` — планы: номер, тип производства («заказ готовой» —
    закупная), поставщик, сезон, бренд-менеджер;
  · `bm_chl_checklist_items` — строки плана: модель, наименование, путь
    группы номенклатуры, количество, дата выпуска и `calculation_json` с ценой
    размещения в USD, розницей с НДС и снимком ставок (курс, пошлина %,
    транспорт %, сертификация $, тесты $). Артикула в строках нет;
  · `bm_chl_jobs` — задания из Fox: № плана, № задания, модель, АРТИКУЛ,
    количество. Артикул и задание для строки плана берём отсюда по (план,
    модель): одна строка плана → столько калькуляций, сколько заданий;
  · `bm_chl_checklist_extra` — страна производства плана (когда заполнена).

Импорт создаёт калькуляции в `cost_manual_calc` (структура cost_data_cache,
читаются через cost_data_all вместе с кэшем; отличие — source_calc_sign =
'ПОРТАЛ'), по одной строке на калькуляцию: в главную таблицу уходит только
ИТОГ себестоимости в «Основные материалы» (решение заказчика), а раскладка —
цена, курс, логистика, таможня, сертификация — в `cost_purchase_cost`, чтобы
потом сравнивать с фактом (ПФКСС по приходу) по каждой статье.

ФОРМУЛА КПСС (допущение, формулу самого портала не видели — подтвердить):
  price_usd     = placement_price_usd
  logistics_usd = price_usd × transport_pct / 100
  customs_usd   = price_usd × duty_pct / 100
  cert_usd      = certification_usd + testing_usd
  total_usd     = сумма четырёх; в бел. руб. — × exchange_rate_rub из снимка.
Опт из портала не приходит: берётся по справочнику уровней цен от розницы
(PRICE_TYPE3 → PRICE_TYPE1), как таблица делает в обратную сторону.
"""
from __future__ import annotations

import asyncio
import json
import logging
import os
import re
import uuid
from datetime import date, datetime, timezone
from decimal import Decimal
from typing import Any

from app.db import _mssql_connect, acquire, get_dwh_conn, get_gpartner_conn
from app.logship import log

PORTAL_SOURCE_SIGN = "ПОРТАЛ"          # source_calc_sign у импортированных строк
PORTAL_PRODUCTION_TYPE = "заказ готовой"
PORTAL_ROW_SIGN = "закупка готовой"    # «Материал/операция/декор(призн)» строки
IMPORT_CALC_SIGN = "КПСС"

PurchaseKey = tuple[str, str, str, str, str]   # model, articul, calc_sign, plan_id, task


def _s(v: Any) -> str:
    return str(v or "").strip()


def _f(v: Any) -> float | None:
    if v is None or v == "":
        return None
    try:
        return float(v)
    except (TypeError, ValueError):
        return None


def make_key(model: Any, articul: Any, calc_sign: Any, plan_id: Any, task: Any) -> PurchaseKey:
    return (_s(model), _s(articul), _s(calc_sign), _s(plan_id), _s(task))


# ── Портал (OLAP / mfportal) ─────────────────────────────────────────────────

def portal_conn():
    return _mssql_connect(
        server=os.environ["OLAP_SERVER_IP"],
        database=os.environ.get("MFPORTAL_DATABASE", "mfportal").strip() or "mfportal",
        user=os.environ["OLAP_USER"],
        password=os.environ["OLAP_PASSWORD"],
        readonly=True,
    )


def portal_enabled() -> bool:
    return bool(os.environ.get("OLAP_SERVER_IP") and os.environ.get("OLAP_USER"))


def _read_portal_sync() -> dict[str, Any]:
    """Строки закупных планов, задания к ним и справочник уровней цен."""
    conn = portal_conn()
    cur = conn.cursor()
    try:
        cur.execute(
            """SELECT i.id, i.checklist_id, RTRIM(i.model), RTRIM(i.article), RTRIM(i.item_name),
                      i.model_group, i.total_qty, i.release_date, i.updated_at, i.calculation_json,
                      COALESCE(NULLIF(RTRIM(i.brand_manager), ''), RTRIM(c.brand_manager)),
                      RTRIM(c.plan_number), c.plan_name, RTRIM(c.supplier), RTRIM(c.season),
                      RTRIM(e.country), RTRIM(i.status)
                 FROM dbo.bm_chl_checklist_items i
                 JOIN dbo.bm_chl_checklists c ON c.id = i.checklist_id
                 LEFT JOIN dbo.bm_chl_checklist_extra e ON e.checklist_id = c.id
                WHERE c.production_type = ?
                  AND c.plan_number IS NOT NULL AND c.plan_number <> ''
                  AND i.model IS NOT NULL AND i.model <> ''""",
            PORTAL_PRODUCTION_TYPE,
        )
        items = []
        for r in cur.fetchall():
            items.append({
                "item_id": int(r[0]), "checklist_id": int(r[1]), "model": _s(r[2]), "article": _s(r[3]),
                "name": _s(r[4]), "group_path": _s(r[5]), "total_qty": _f(r[6]), "release_date": _s(r[7]),
                "updated_at": r[8], "json": r[9], "brand_manager": _s(r[10]), "plan_number": _s(r[11]),
                "plan_name": _s(r[12]), "supplier": _s(r[13]), "season": _s(r[14]), "country": _s(r[15]),
                "status": _s(r[16]),
            })

        cur.execute(
            """SELECT RTRIM(plan_number), RTRIM(model), RTRIM(article), RTRIM(job_number), quantity, RTRIM(proper_name1)
                 FROM dbo.bm_chl_jobs
                WHERE production_type = ? AND plan_number IS NOT NULL AND model IS NOT NULL""",
            PORTAL_PRODUCTION_TYPE,
        )
        jobs: dict[tuple[str, str], list[dict[str, Any]]] = {}
        for pn, model, article, job_no, qty, color in cur.fetchall():
            jobs.setdefault((_s(pn), _s(model)), []).append({
                "article": _s(article), "job_number": _s(job_no), "qty": _f(qty), "color": _s(color),
            })
    finally:
        conn.close()

    # Справочник уровней цен — для опта от розницы.
    gconn = get_gpartner_conn()
    try:
        gcur = gconn.cursor()
        gcur.execute("SELECT RTRIM(NAME), PRICE_TYPE1, PRICE_TYPE3 FROM [dbo].[s_price_level]")
        levels = [(_s(n), _f(p1), _f(p3)) for n, p1, p3 in gcur.fetchall()]
    finally:
        gconn.close()
    return {"items": items, "jobs": jobs, "levels": levels}


# ── Расчёт и разбор ──────────────────────────────────────────────────────────

COUNTRY_WORDS = ("Китай", "Бангладеш", "Узбекистан", "Турция", "Индия", "Вьетнам", "Пакистан",
                 "Индонезия", "Камбоджа", "Мьянма", "Киргизия", "Россия", "Беларусь")


def guess_country(explicit: str, plan_name: str) -> str | None:
    """Страна производства: из карточки плана, иначе из его названия
    («…Готовая Китай WL…», «…Бангладеш…»). Не угадали — None, не выдумываем."""
    if explicit:
        return explicit
    low = plan_name.lower()
    for w in COUNTRY_WORDS:
        if w.lower() in low:
            return w
    return None


def parse_portal_json(raw: Any) -> dict[str, Any] | None:
    """placement_price_usd + reference_snapshot → раскладка себестоимости.
    None — расчёта в строке нет (пустой JSON, нет цены или курса)."""
    if not raw:
        return None
    try:
        d = json.loads(raw) if isinstance(raw, str) else raw
    except ValueError:
        return None
    if not isinstance(d, dict):
        return None
    price = _f(d.get("placement_price_usd"))
    snap = d.get("reference_snapshot") if isinstance(d.get("reference_snapshot"), dict) else {}
    rate = _f(snap.get("exchange_rate_rub"))
    if price is None or price <= 0 or not rate or rate <= 0:
        return None
    duty = _f(snap.get("duty_pct")) or 0.0
    transport = _f(snap.get("transport_pct")) or 0.0
    cert = (_f(snap.get("certification_usd")) or 0.0) + (_f(snap.get("testing_usd")) or 0.0)
    logistics = price * transport / 100.0
    customs = price * duty / 100.0
    total = price + logistics + customs + cert
    return {
        "price_usd": price, "logistics_usd": logistics, "customs_usd": customs, "cert_usd": cert,
        "total_usd": total, "usd_to_byn": rate,
        "price_byn": price * rate, "logistics_byn": logistics * rate, "customs_byn": customs * rate,
        "cert_byn": cert * rate, "total_byn": total * rate,
        "retail_byn": _f(d.get("retail_vat_rub")),
        "snapshot": d,
    }


def wholesale_from_retail(retail: float | None, levels: list[tuple[str, float | None, float | None]]) -> tuple[float | None, str | None]:
    """Опт и имя уровня по рознице: уровень, у которого PRICE_TYPE3 = розница.
    Несколько уровней с одной розницей — берём первый по имени (стабильно)."""
    if retail is None:
        return None, None
    cands = sorted(
        [(name, p1) for name, p1, p3 in levels if p3 is not None and round(p3, 2) == round(retail, 2) and p1],
        key=lambda x: x[0],
    )
    return (cands[0][1], cands[0][0]) if cands else (None, None)


def _parse_date(s: str) -> date | None:
    s = _s(s)[:10]
    for fmt in ("%Y-%m-%d", "%d.%m.%Y"):
        try:
            return datetime.strptime(s, fmt).date()
        except ValueError:
            continue
    return None


def build_row(item: dict[str, Any], job: dict[str, Any] | None, cost: dict[str, Any],
              levels: list[tuple[str, float | None, float | None]]) -> dict[str, Any]:
    """Строка cost_manual_calc для одной калькуляции (модель+артикул+план+задание)."""
    segs = [s.strip() for s in item["group_path"].replace("/", "\\").split("\\") if s.strip()]
    rate = cost["usd_to_byn"]
    retail = cost["retail_byn"]
    wholesale, level_name = wholesale_from_retail(retail, levels)
    qty = (job or {}).get("qty") or item.get("total_qty")
    updated = item.get("updated_at")
    calc_date = updated.date() if isinstance(updated, datetime) else date.today()
    row: dict[str, Any] = {
        "Бренд-менеджер": item["brand_manager"] or None,
        "Модель": item["model"],
        "Артикул": (job or {}).get("article") or item["article"],
        "Признак калькуляции": IMPORT_CALC_SIGN,
        "дата расчета": calc_date,
        "дата производства": _parse_date(item["release_date"]),
        "Курс на дату расчета": rate,
        "Уровень цен": level_name,
        "Страна пр-ва": guess_country(item["country"], item["plan_name"]),
        "Сезон": item["season"] or None,
        "Level 01": segs[0] if len(segs) > 0 else None,
        "Level 02": segs[1] if len(segs) > 1 else None,
        "Level 03": segs[2] if len(segs) > 2 else None,
        "Level 04": segs[3] if len(segs) > 3 else None,
        "Level 05": segs[4] if len(segs) > 4 else None,
        "Наименование модели": item["name"] or None,
        "Номер задания производства": (job or {}).get("job_number") or None,
        "PLAN_ID": item["plan_number"],
        "выпуск шт": qty,
        "Материал/операция/декор(призн)": PORTAL_ROW_SIGN,
        "color": (job or {}).get("color") or None,
        "Наименование": f"Готовая продукция: {item['supplier']}" if item["supplier"] else "Готовая продукция",
        "Норма": 1,
        "цена материала, руб.": cost["total_byn"],
        "цена материала, USD.": cost["total_usd"],
        "Розничная цена по уровню, руб.": retail,
        "Отпускная цена по уровню, руб": wholesale,
        "Розничная цена по уровню, USD.": (retail / rate) if retail else None,
        "Отпускная цена по уровню, USD.": (wholesale / rate) if wholesale else None,
        # Решение заказчика: в главной таблице закупная видна одним итогом в
        # «Основных материалах»; раскладка — в cost_purchase_cost.
        "Основные материалы, руб.": cost["total_byn"],
        "Основные материалы, USD.": cost["total_usd"],
        "Вспомогательные материалы, руб.": 0, "Вспомогательные материалы, USD.": 0,
        "Пошив, руб.": 0, "Пошив, USD.": 0, "Раскрой, руб.": 0, "Раскрой, USD.": 0,
        "Декоры, руб.": 0, "Декоры, USD.": 0, "Вязание, руб.": 0, "Вязание, USD.": 0,
        "Себестоимость, руб.": cost["total_byn"],
        "Себестоимость, USD.": cost["total_usd"],
    }
    return row


# ── Импорт ───────────────────────────────────────────────────────────────────

async def _manual_columns(conn) -> list[str]:
    rows = await conn.fetch(
        """SELECT column_name, data_type FROM information_schema.columns
            WHERE table_schema = 'public' AND table_name = 'cost_manual_calc'
            ORDER BY ordinal_position"""
    )
    return [(r["column_name"], r["data_type"]) for r in rows]


def _cast(value: Any, data_type: str) -> Any:
    """Значения под тип колонки cost_data_cache: там числа хранятся как numeric,
    даты — date/timestamp, а всё текстовое — text."""
    if value is None:
        return None
    if data_type in ("numeric", "double precision", "real", "integer", "bigint"):
        if isinstance(value, (int, float, Decimal)):
            return Decimal(str(round(float(value), 6))) if data_type == "numeric" else value
        return None
    if data_type == "date":
        return value if isinstance(value, date) else None
    if data_type.startswith("timestamp"):
        if isinstance(value, datetime):
            return value
        if isinstance(value, date):
            return datetime(value.year, value.month, value.day)
        return None
    return str(value)


async def run_import(user: str, *, dry_run: bool = False) -> dict[str, Any]:
    """Прочитать портал и создать/обновить калькуляции КПСС закупной продукции.

    Идемпотентно: ключ — модель, артикул, КПСС, план, задание. Существующая
    импортированная строка (source_calc_sign='ПОРТАЛ') обновляется, чужие
    (копии, кэш) не трогаются. Пропуски считаются по причинам: нет расчёта в
    JSON (все планы AW2026), нет артикула (нет заданий в Fox и артикула в
    строке). dry_run — только посчитать, ничего не писать.
    """
    started = datetime.now(timezone.utc)
    run_id: int | None = None
    if not dry_run:
        async with acquire() as conn:
            run_id = await conn.fetchval(
                "INSERT INTO cost_purchase_import_run (started_by) VALUES ($1) RETURNING id", user,
            )
    try:
        loop = asyncio.get_running_loop()
        portal = await loop.run_in_executor(None, _read_portal_sync)
    except Exception as exc:  # noqa: BLE001 — источник за VPN
        if run_id:
            async with acquire() as conn:
                await conn.execute(
                    "UPDATE cost_purchase_import_run SET status='error', error=$2, finished_at=now() WHERE id=$1",
                    run_id, str(exc)[:1000],
                )
        raise RuntimeError(f"портал недоступен: {exc}") from exc

    items, jobs, levels = portal["items"], portal["jobs"], portal["levels"]
    plans = {i["plan_number"] for i in items}
    skipped: dict[str, int] = {}
    created = updated = 0
    batch = uuid.uuid4()
    prepared: list[tuple[dict[str, Any], dict[str, Any], dict[str, Any], dict[str, Any] | None]] = []

    for item in items:
        cost = parse_portal_json(item["json"])
        if cost is None:
            skipped["нет расчёта в портале"] = skipped.get("нет расчёта в портале", 0) + 1
            continue
        item_jobs = jobs.get((item["plan_number"], item["model"])) or []
        if not item_jobs:
            if item["article"]:
                item_jobs = [{"article": item["article"], "job_number": "", "qty": item["total_qty"], "color": ""}]
            else:
                skipped["нет артикула (нет заданий в Fox)"] = skipped.get("нет артикула (нет заданий в Fox)", 0) + 1
                continue
        for job in item_jobs:
            if not job["article"]:
                skipped["задание без артикула"] = skipped.get("задание без артикула", 0) + 1
                continue
            prepared.append((item, job, cost, build_row(item, job, cost, levels)))

    if dry_run:
        return {
            "dry_run": True, "plans": len(plans), "items": len(items), "to_write": len(prepared),
            "skipped": sum(skipped.values()), "skipped_reasons": skipped,
        }

    async with acquire() as conn:
        cols = await _manual_columns(conn)
        col_types = dict(cols)
        data_cols = [c for c, _ in cols if c not in ("id", "batch_id", "source_calc_sign", "source_task",
                                                        "created_by", "created_at", "reason")]
        async with conn.transaction():
            for item, job, cost, row in prepared:
                key = make_key(row["Модель"], row["Артикул"], IMPORT_CALC_SIGN, row["PLAN_ID"],
                               row["Номер задания производства"])
                values = [_cast(row.get(c), col_types[c]) for c in data_cols]
                existing = await conn.fetchval(
                    """SELECT id FROM cost_manual_calc
                        WHERE source_calc_sign = $1
                          AND trim("Модель") = $2 AND trim("Артикул") = $3
                          AND trim(COALESCE("Признак калькуляции", '')) = $4
                          AND trim(COALESCE("PLAN_ID", '')) = $5
                          AND trim(COALESCE("Номер задания производства", '')) = $6
                        ORDER BY id LIMIT 1""",
                    PORTAL_SOURCE_SIGN, *key,
                )
                if existing:
                    sets = ", ".join(f'"{c}" = ${i + 2}' for i, c in enumerate(data_cols))
                    await conn.execute(f"UPDATE cost_manual_calc SET {sets} WHERE id = $1", existing, *values)
                    manual_id = existing
                    updated += 1
                else:
                    col_list = ", ".join(f'"{c}"' for c in data_cols)
                    ph = ", ".join(f"${i + 1}" for i in range(len(data_cols)))
                    n = len(data_cols)
                    manual_id = await conn.fetchval(
                        f"""INSERT INTO cost_manual_calc ({col_list}, batch_id, source_calc_sign, source_task, created_by, reason)
                            VALUES ({ph}, ${n + 1}, ${n + 2}, NULL, ${n + 3}, ${n + 4}) RETURNING id""",
                        *values, batch, PORTAL_SOURCE_SIGN, user,
                        f"импорт из портала БМ (mfportal), строка плана {item['item_id']}",
                    )
                    created += 1

                await conn.execute(
                    """INSERT INTO cost_purchase_cost
                           (model, articul, calc_sign, plan_id, task_number, manual_calc_id, source, portal_item_id,
                            currency, unit_price_cur, cur_to_byn, usd_to_byn, rate_date, qty,
                            price_usd, price_byn, logistics_usd, logistics_byn, customs_usd, customs_byn,
                            cert_usd, cert_byn, total_usd, total_byn, retail_byn, wholesale_byn, snapshot, created_by)
                       VALUES ($1, $2, $3, $4, $5, $6, 'portal', $7,
                               'USD', $8, $9, $9, NULL, $10,
                               $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23::jsonb, $24)
                       ON CONFLICT ON CONSTRAINT uq_purchase_cost_key DO UPDATE
                          SET manual_calc_id = EXCLUDED.manual_calc_id, source = 'portal',
                              portal_item_id = EXCLUDED.portal_item_id, unit_price_cur = EXCLUDED.unit_price_cur,
                              cur_to_byn = EXCLUDED.cur_to_byn, usd_to_byn = EXCLUDED.usd_to_byn,
                              qty = EXCLUDED.qty,
                              price_usd = EXCLUDED.price_usd, price_byn = EXCLUDED.price_byn,
                              logistics_usd = EXCLUDED.logistics_usd, logistics_byn = EXCLUDED.logistics_byn,
                              customs_usd = EXCLUDED.customs_usd, customs_byn = EXCLUDED.customs_byn,
                              cert_usd = EXCLUDED.cert_usd, cert_byn = EXCLUDED.cert_byn,
                              total_usd = EXCLUDED.total_usd, total_byn = EXCLUDED.total_byn,
                              retail_byn = EXCLUDED.retail_byn, wholesale_byn = EXCLUDED.wholesale_byn,
                              snapshot = EXCLUDED.snapshot, updated_at = now()""",
                    *key, manual_id, item["item_id"],
                    cost["price_usd"], cost["usd_to_byn"], row["выпуск шт"],
                    cost["price_usd"], cost["price_byn"], cost["logistics_usd"], cost["logistics_byn"],
                    cost["customs_usd"], cost["customs_byn"], cost["cert_usd"], cost["cert_byn"],
                    cost["total_usd"], cost["total_byn"], cost["retail_byn"], row["Отпускная цена по уровню, руб"],
                    json.dumps({"portal": cost["snapshot"], "plan_name": item["plan_name"], "supplier": item["supplier"],
                                "group_path": item["group_path"], "job": job}, ensure_ascii=False, default=str),
                    user,
                )
            await conn.execute(
                """UPDATE cost_purchase_import_run
                      SET status = 'done', finished_at = now(), plans = $2, items = $3,
                          created_rows = $4, updated_rows = $5, skipped = $6, skipped_reasons = $7::jsonb
                    WHERE id = $1""",
                run_id, len(plans), len(items), created, updated, sum(skipped.values()),
                json.dumps(skipped, ensure_ascii=False),
            )

    result = {
        "run_id": run_id, "plans": len(plans), "items": len(items), "created": created, "updated": updated,
        "skipped": sum(skipped.values()), "skipped_reasons": skipped,
        "seconds": round((datetime.now(timezone.utc) - started).total_seconds(), 1),
    }
    log(logging.INFO, "импорт закупной готовой продукции из портала", user=user, **result)
    return result


async def last_runs(limit: int = 10) -> list[dict[str, Any]]:
    async with acquire() as conn:
        rows = await conn.fetch(
            "SELECT * FROM cost_purchase_import_run ORDER BY id DESC LIMIT $1", limit,
        )
    out = []
    for r in rows:
        d = dict(r)
        for k in ("started_at", "finished_at"):
            d[k] = d[k].isoformat() if d.get(k) else None
        if isinstance(d.get("skipped_reasons"), str):
            d["skipped_reasons"] = json.loads(d["skipped_reasons"])
        out.append(d)
    return out


# ── Наложение на строки /aggregated и детализация ────────────────────────────

async def flags_for(keys: list[PurchaseKey]) -> dict[PurchaseKey, dict[str, Any]]:
    uniq = list({k for k in keys if k[0] and k[1]})
    if not uniq:
        return {}
    async with acquire() as conn:
        rows = await conn.fetch(
            """SELECT model, articul, calc_sign, plan_id, task_number, source,
                      total_byn, total_usd, price_byn, logistics_byn, customs_byn, cert_byn, usd_to_byn
                 FROM cost_purchase_cost
                WHERE (model, articul, calc_sign, plan_id, task_number) IN (
                      SELECT * FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[]))""",
            [k[0] for k in uniq], [k[1] for k in uniq], [k[2] for k in uniq],
            [k[3] for k in uniq], [k[4] for k in uniq],
        )
    out: dict[PurchaseKey, dict[str, Any]] = {}
    for r in rows:
        k = (r["model"], r["articul"], r["calc_sign"], r["plan_id"], r["task_number"])
        out[k] = {
            "purchase_source": r["source"],
            "purchase_total_byn": _f(r["total_byn"]), "purchase_price_byn": _f(r["price_byn"]),
            "purchase_logistics_byn": _f(r["logistics_byn"]), "purchase_customs_byn": _f(r["customs_byn"]),
            "purchase_cert_byn": _f(r["cert_byn"]), "purchase_rate": _f(r["usd_to_byn"]),
        }
    return out


def apply_flags(rows: list[dict[str, Any]], flags: dict[PurchaseKey, dict[str, Any]]) -> None:
    for row in rows:
        k = make_key(row.get("Модель"), row.get("Артикул"), row.get("Признак калькуляции"),
                     row.get("PLAN_ID"), row.get("Номер задания производства"))
        f = flags.get(k)
        row["purchase_source"] = f["purchase_source"] if f else None
        if f:
            row.update(f)


async def detail(key: PurchaseKey) -> dict[str, Any] | None:
    """Раскладка себестоимости калькуляции и, для ПФКСС, плановая раскладка из
    КПСС той же пары модель+артикул+план — для отклонений по статьям."""
    async with acquire() as conn:
        r = await conn.fetchrow(
            "SELECT * FROM cost_purchase_cost WHERE model=$1 AND articul=$2 AND calc_sign=$3 AND plan_id=$4 AND task_number=$5",
            *key,
        )
        if not r:
            return None
        d = _row(r)
        plan = None
        if d["calc_sign"] != IMPORT_CALC_SIGN:
            p = await conn.fetchrow(
                """SELECT * FROM cost_purchase_cost
                    WHERE model=$1 AND articul=$2 AND calc_sign=$3 AND plan_id=$4
                    ORDER BY (task_number = $5) DESC, updated_at DESC LIMIT 1""",
                key[0], key[1], IMPORT_CALC_SIGN, key[3], key[4],
            )
            plan = _row(p) if p else None
    return {"current": d, "plan": plan}


def _row(r: Any) -> dict[str, Any]:
    d = dict(r)
    for k, v in list(d.items()):
        if isinstance(v, Decimal):
            d[k] = float(v)
        elif isinstance(v, datetime):
            d[k] = v.isoformat()
        elif isinstance(v, date):
            d[k] = v.isoformat()
    for jk in ("snapshot", "duty_rub", "totals"):
        if isinstance(d.get(jk), str):
            try:
                d[jk] = json.loads(d[jk])
            except ValueError:
                pass
    return d


# ── Коды ТН ВЭД из справочника S_MODELI ──────────────────────────────────────

def _hs_codes_sync(pairs: list[Pair]) -> dict[Pair, str]:
    """Код ТН ВЭД (S_MODELI.KTNVED) по парам модель+артикул, последняя версия
    пары по ITEM_ID. Заполнен у 95 % справочника (125 164 из 132 444 на
    03.09.2026); у кого пусто — вернётся пустая строка, код введут руками."""
    if not pairs:
        return {}
    conn = get_gpartner_conn()
    cur = conn.cursor()
    try:
        out: dict[Pair, str] = {}
        for i in range(0, len(pairs), 400):
            batch = pairs[i:i + 400]
            conds = " OR ".join("(RTRIM(MODEL) = ? AND RTRIM(ART) = ?)" for _ in batch)
            params: list[str] = []
            for m, a in batch:
                params.extend([m, a])
            cur.execute(
                f"""SELECT MODEL, ART, KTNVED FROM (
                        SELECT RTRIM(MODEL) AS MODEL, RTRIM(ART) AS ART, RTRIM(KTNVED) AS KTNVED,
                               ROW_NUMBER() OVER (PARTITION BY RTRIM(MODEL), RTRIM(ART) ORDER BY ITEM_ID DESC) AS rn
                          FROM [dbo].[S_MODELI] WHERE {conds}
                    ) t WHERE rn = 1""",
                params,
            )
            for m, a, code in cur.fetchall():
                out[(_s(m), _s(a))] = _s(code)
        return out
    finally:
        conn.close()


async def hs_codes_for_pairs(pairs: list[Pair]) -> dict[str, str]:
    """{'модель|артикул': код} — для подстановки в строки инвойса."""
    uniq = list({(_s(m), _s(a)) for m, a in pairs if _s(m) and _s(a)})
    if not uniq:
        return {}
    loop = asyncio.get_running_loop()
    codes = await loop.run_in_executor(None, _hs_codes_sync, uniq)
    return {f"{m}|{a}": code for (m, a), code in codes.items() if code}


# ── Курсы НБ РБ к бел. рублю (DWH.dim.valuta / valuta1) ─────────────────────
#
# Решение заказчика 04.09.2026: все затраты и цены поставщика приводим к
# БЕЛ. РУБЛЮ по курсам НБ РБ на дату прихода, а итоговую себестоимость
# пересчитываем обратно в доллары по курсу USD той же даты.
#
# Источник — `[DWH].[dim].[valuta1]`, колонка **KURS_BANK** (курс НБ РБ);
# `KURS` — внутренний курс компании (USD 3,10 против 2,9756 у НБ РБ на
# 24.08.2026), его не берём. Справочник валют — `[DWH].[dim].[valuta]`,
# ключ `ITEM_ID` = `valuta1.VALUTA_ID`, кратность `KRATN` у всех наших валют 1.
#
# Курс проверен по эталонному Excel заказчика: USD 2,9756 и CNY 0,444 на
# 24.08.2026 совпали с шапкой инвойса.

# Код валюты → ITEM_ID справочника. Названия в NAIM неудобны («RUR.»,
# «руб., коп.»), поэтому коды свои, короткие и однозначные.
CURRENCIES: dict[str, dict[str, Any]] = {
    "BYN": {"id": None, "name": "бел. рубль"},        # база, курс всегда 1
    "USD": {"id": 3, "name": "доллар США"},
    "EUR": {"id": 6, "name": "евро"},
    "RUB": {"id": 2, "name": "рос. рубль"},
    "KZT": {"id": 7, "name": "тенге"},
    "UZS": {"id": 8, "name": "узб. сум"},
    "CNY": {"id": 9, "name": "юань"},
}
CURRENCY_BY_ID = {v["id"]: k for k, v in CURRENCIES.items() if v["id"]}


def _rates_sync(on: date) -> dict[str, Any]:
    """Курсы НБ РБ на дату; если на дату записи нет — ближайшая предыдущая.

    Дату передаём ОБЪЕКТОМ date, а не строкой: сервер с русской локалью
    понимает '2026-09-04' как 9 апреля, и запрос молча вернёт не то (поймано
    04.09.2026 при разведке).
    """
    ids = [v["id"] for v in CURRENCIES.values() if v["id"]]
    ph = ", ".join("?" for _ in ids)
    conn = get_dwh_conn()
    cur = conn.cursor()
    try:
        cur.execute(
            f"""SELECT v1.VALUTA_ID, v1.KURS_BANK, v1.DATA
                  FROM [DWH].[dim].[valuta1] v1
                  JOIN (SELECT VALUTA_ID, MAX(DATA) AS DATA
                          FROM [DWH].[dim].[valuta1]
                         WHERE DATA <= ? AND VALUTA_ID IN ({ph}) AND KURS_BANK > 0
                         GROUP BY VALUTA_ID) last
                    ON last.VALUTA_ID = v1.VALUTA_ID AND last.DATA = v1.DATA""",
            on, *ids,
        )
        rates: dict[str, float] = {"BYN": 1.0}
        as_of: dict[str, str] = {}
        for vid, kurs, data in cur.fetchall():
            code = CURRENCY_BY_ID.get(int(vid))
            rate = _f(kurs)
            if code and rate:
                rates[code] = rate
                as_of[code] = data.date().isoformat() if isinstance(data, datetime) else str(data)
        return {"date": on.isoformat(), "source": "nbrb", "rates": rates, "as_of": as_of}
    finally:
        conn.close()


async def rates_for_date(on: date | str) -> dict[str, Any]:
    d = on if isinstance(on, date) else (_parse_date(str(on)) or date.today())
    loop = asyncio.get_running_loop()
    return await loop.run_in_executor(None, _rates_sync, d)


# ═════════════════════════════════════════════════════════════════════════════
# ПФКСС по приходу: инвойс, распределение накладных, создание калькуляций
# ═════════════════════════════════════════════════════════════════════════════
#
# Эталон — Excel заказчика Invoice_MF8693.xlsx (лист PL), но валютная логика
# переделана по решению заказчика 04.09.2026: считаем не через доллар, а через
# БЕЛ. РУБЛЬ, и валюта задаётся у каждой статьи отдельно.
#
#   цена строки, бел. руб.  = цена в валюте прихода × курс валюты
#   статья накладных, руб.  = сумма × курс своей валюты (сумм на статью может
#                             быть несколько — в эталоне транспорт идёт в двух
#                             валютах)
#   на единицу              = цена_руб × (статья_руб / Σ стоимости строк группы)
#   группа                  = код ТН ВЭД для пошлины, весь инвойс для остального
#   итог, бел. руб.         = цена + логистика + таможня + сертификация
#   итог, $                 = итог_руб / курс USD той же даты
#
# Пять статей сворачиваются в три хранимые: Логистика = транспорт + СВХ,
# Таможня = пошлина + таможенный сбор, Сертификация.

INVOICE_SOURCE_SIGN = "ПРИХОД"        # source_calc_sign у калькуляций ПФКСС из инвойса
INVOICE_CALC_SIGN = "ПФКСС"
INVOICE_ROW_SIGN = "закупка готовой (приход)"

OVERHEAD_KINDS = ("transport", "svh", "customs_fee", "cert", "duty")
OVERHEAD_LABELS = {
    "transport": "Транспорт", "svh": "СВХ", "customs_fee": "Таможенный сбор",
    "cert": "Сертификация", "duty": "Таможенная пошлина",
}
# Во что сворачивается статья при записи раскладки.
OVERHEAD_TO_ARTICLE = {
    "transport": "logistics", "svh": "logistics",
    "duty": "customs", "customs_fee": "customs",
    "cert": "cert",
}

HEADER_FIELDS = ("number", "invoice_date", "arrival_date", "contract", "supplier", "comment",
                 "currency", "cur_rate", "usd_rate", "rate_date", "rates_source", "overheads")


def _norm_hs(v: Any) -> str:
    """Код ТН ВЭД сравниваем без пробелов: «6202 30 000 0» и «6202300000» — одно."""
    return re.sub(r"\s+", "", _s(v))


def _norm_currency(v: Any) -> str:
    c = _s(v).upper()
    return c if c in CURRENCIES else ""


def distribute(header: dict[str, Any], lines: list[dict[str, Any]]) -> dict[str, Any]:
    """Разложить накладные по строкам. Чистая функция: вход — шапка (валюта
    прихода, её курс, курс доллара, статьи накладных со своими валютами и
    курсами) и строки, выход — строки с раскладкой на единицу в бел. рублях и
    долларах, итоги и замечания.

    Ошибки, останавливающие расчёт (ValueError): нет курса валюты прихода, у
    статьи нет курса, строка без количества или цены, пошлина задана по коду,
    которого нет в строках.
    """
    currency = _norm_currency(header.get("currency")) or "USD"
    cur_rate = _f(header.get("cur_rate"))
    usd_rate = _f(header.get("usd_rate"))
    if currency == "BYN":
        cur_rate = 1.0
    if not cur_rate or cur_rate <= 0:
        raise ValueError(f"не задан курс валюты прихода ({currency} → бел. руб.)")

    # ── статьи накладных ────────────────────────────────────────────────────
    raw = header.get("overheads") or []
    if isinstance(raw, str):
        raw = json.loads(raw or "[]")
    overheads: list[dict[str, Any]] = []
    for i, o in enumerate(raw, 1):
        kind = _s(o.get("kind"))
        if kind not in OVERHEAD_KINDS:
            raise ValueError(f"статья {i}: неизвестный вид «{kind}»")
        amount = _f(o.get("amount")) or 0.0
        if amount < 0:
            raise ValueError(f"{OVERHEAD_LABELS[kind]}: отрицательная сумма")
        ocur = _norm_currency(o.get("currency")) or "BYN"
        rate = 1.0 if ocur == "BYN" else _f(o.get("rate"))
        if amount and (not rate or rate <= 0):
            raise ValueError(f"{OVERHEAD_LABELS[kind]}: не задан курс {ocur} → бел. руб.")
        overheads.append({
            "kind": kind, "hs_code": _s(o.get("hs_code")), "_hs": _norm_hs(o.get("hs_code")),
            "amount": amount, "currency": ocur, "rate": rate or 1.0,
            "amount_byn": round(amount * (rate or 1.0), 4),
            "comment": _s(o.get("comment")) or None,
        })

    # ── строки ─────────────────────────────────────────────────────────────
    if not lines:
        raise ValueError("нет строк прихода")
    prepared: list[dict[str, Any]] = []
    for i, ln in enumerate(lines, 1):
        qty = _f(ln.get("qty"))
        price = _f(ln.get("unit_price_cur"))
        if not qty or qty <= 0:
            raise ValueError(f"строка {i}: не задано количество")
        if price is None or price < 0:
            raise ValueError(f"строка {i}: не задана цена")
        model, articul = _s(ln.get("model")), _s(ln.get("articul"))
        if not model or not articul:
            raise ValueError(f"строка {i}: нужны модель и артикул")
        price_byn = price * cur_rate
        prepared.append({**ln, "line_no": i, "model": model, "articul": articul,
                         "plan_id": _s(ln.get("plan_id")), "task_number": _s(ln.get("task_number")),
                         "hs_code": _s(ln.get("hs_code")), "_hs": _norm_hs(ln.get("hs_code")),
                         "qty": qty, "unit_price_cur": price,
                         "price_byn": price_byn, "sum_byn": price_byn * qty})

    total_sum_byn = sum(p["sum_byn"] for p in prepared)
    if total_sum_byn <= 0:
        raise ValueError("стоимость строк нулевая — распределять нечего")

    # Доли общих статей — от всей стоимости инвойса; пошлина — внутри своего кода.
    by_kind: dict[str, float] = {k: 0.0 for k in OVERHEAD_KINDS}
    duty_byn: dict[str, float] = {}
    for o in overheads:
        by_kind[o["kind"]] += o["amount_byn"]
        if o["kind"] == "duty":
            duty_byn[o["_hs"]] = duty_byn.get(o["_hs"], 0.0) + o["amount_byn"]

    group_sum: dict[str, float] = {}
    for p in prepared:
        group_sum[p["_hs"]] = group_sum.get(p["_hs"], 0.0) + p["sum_byn"]
    warnings: list[str] = []
    for hs, amount in duty_byn.items():
        if amount and hs not in group_sum:
            raise ValueError(f"пошлина задана по коду «{hs or 'без кода'}», а строк с таким кодом нет")
    for hs in group_sum:
        if not duty_byn.get(hs):
            warnings.append(f"по коду «{hs or 'без кода'}» пошлина не задана — считается нулевой")
    share_duty = {hs: (duty_byn.get(hs, 0.0) / group_sum[hs]) for hs in group_sum}
    share_logistics = (by_kind["transport"] + by_kind["svh"]) / total_sum_byn
    share_fee = by_kind["customs_fee"] / total_sum_byn
    share_cert = by_kind["cert"] / total_sum_byn

    if not usd_rate or usd_rate <= 0:
        warnings.append("не задан курс доллара — итог в долларах не посчитан")

    out_lines: list[dict[str, Any]] = []
    alloc = {"logistics": 0.0, "customs_fee": 0.0, "duty": 0.0, "cert": 0.0}
    for p in prepared:
        pb = p["price_byn"]
        logistics_b = pb * share_logistics
        duty_b = pb * share_duty[p["_hs"]]
        fee_b = pb * share_fee
        cert_b = pb * share_cert
        customs_b = duty_b + fee_b
        total_b = pb + logistics_b + customs_b + cert_b
        alloc["logistics"] += logistics_b * p["qty"]
        alloc["customs_fee"] += fee_b * p["qty"]
        alloc["duty"] += duty_b * p["qty"]
        alloc["cert"] += cert_b * p["qty"]
        out = {k: v for k, v in p.items() if not k.startswith("_")}
        out.update({
            "price_byn": round(pb, 6), "sum_byn": round(p["sum_byn"], 2),
            "logistics_byn": round(logistics_b, 6), "customs_byn": round(customs_b, 6),
            "duty_byn": round(duty_b, 6), "customs_fee_byn": round(fee_b, 6),
            "cert_byn": round(cert_b, 6), "total_byn": round(total_b, 6),
            "total_usd": round(total_b / usd_rate, 6) if usd_rate else None,
            "price_usd": round(pb / usd_rate, 6) if usd_rate else None,
            "duty_share_pct": round(share_duty[p["_hs"]] * 100, 4),
        })
        out_lines.append(out)

    totals = {
        "currency": currency, "cur_rate": cur_rate, "usd_rate": usd_rate,
        "lines": len(out_lines), "qty": sum(p["qty"] for p in prepared),
        "sum_byn": round(total_sum_byn, 2),
        "sum_usd": round(total_sum_byn / usd_rate, 2) if usd_rate else None,
        "overhead_byn": {k: round(v, 2) for k, v in by_kind.items()},
        "overhead_total_byn": round(sum(by_kind.values()), 2),
        "allocated_byn": {k: round(v, 2) for k, v in alloc.items()},
        "duty_share_pct": {hs or "без кода": round(v * 100, 4) for hs, v in share_duty.items()},
        "total_byn": round(sum(o["total_byn"] * o["qty"] for o in out_lines), 2),
        "total_usd": (round(sum(o["total_byn"] * o["qty"] for o in out_lines) / usd_rate, 2)
                      if usd_rate else None),
        "warnings": warnings,
    }
    return {"lines": out_lines, "overheads": [{k: v for k, v in o.items() if not k.startswith("_")}
                                              for o in overheads], "totals": totals}


def _validate_header(payload: dict[str, Any]) -> dict[str, Any]:
    h: dict[str, Any] = {}
    h["number"] = _s(payload.get("number"))
    if not h["number"]:
        raise ValueError("не указан номер инвойса")
    for f in ("contract", "supplier", "comment"):
        h[f] = _s(payload.get(f)) or None
    h["currency"] = _norm_currency(payload.get("currency")) or "USD"
    for f in ("cur_rate", "usd_rate"):
        v = _f(payload.get(f))
        if v is not None and v <= 0:
            raise ValueError(f"{f}: курс должен быть больше нуля")
        h[f] = v
    if h["currency"] == "BYN":
        h["cur_rate"] = 1.0
    for f in ("invoice_date", "arrival_date", "rate_date"):
        h[f] = _parse_date(_s(payload.get(f))) if payload.get(f) else None
    h["rate_date"] = h["rate_date"] or h["arrival_date"]
    src = _s(payload.get("rates_source")).lower()
    h["rates_source"] = src if src in ("nbrb", "manual") else "nbrb"
    h["overheads"] = payload.get("overheads") or []
    return h


async def save_invoice(payload: dict[str, Any], user: str) -> dict[str, Any]:
    """Создать или обновить черновик инвойса вместе со статьями и строками;
    вернуть его с распределением. Применённый инвойс менять нельзя."""
    header = _validate_header(payload)
    lines_in = payload.get("lines") or []
    if not isinstance(lines_in, list):
        raise ValueError("lines должен быть списком")
    calc = distribute(header, lines_in)   # валидирует всё и считает раскладку
    inv_id = payload.get("id")

    async with acquire() as conn:
        async with conn.transaction():
            if inv_id:
                st = await conn.fetchval("SELECT status FROM cost_purchase_invoice WHERE id = $1 FOR UPDATE", int(inv_id))
                if st is None:
                    raise ValueError("инвойс не найден")
                if st == "applied":
                    raise ValueError("инвойс уже применён — правки невозможны, заведите новый приход")
                await conn.execute(
                    """UPDATE cost_purchase_invoice
                          SET number=$2, invoice_date=$3, arrival_date=$4, contract=$5, supplier=$6, comment=$7,
                              currency=$8, cur_rate=$9, usd_rate=$10, rate_date=$11, rates_source=$12,
                              totals=$13::jsonb, updated_by=$14, updated_at=now()
                        WHERE id=$1""",
                    int(inv_id), header["number"], header["invoice_date"], header["arrival_date"],
                    header["contract"], header["supplier"], header["comment"], header["currency"],
                    header["cur_rate"], header["usd_rate"], header["rate_date"], header["rates_source"],
                    json.dumps(calc["totals"], ensure_ascii=False), user,
                )
                await conn.execute("DELETE FROM cost_purchase_invoice_line WHERE invoice_id = $1", int(inv_id))
                await conn.execute("DELETE FROM cost_purchase_invoice_overhead WHERE invoice_id = $1", int(inv_id))
            else:
                inv_id = await conn.fetchval(
                    """INSERT INTO cost_purchase_invoice
                           (number, invoice_date, arrival_date, contract, supplier, comment, currency,
                            cur_rate, usd_rate, rate_date, rates_source, totals, created_by, updated_by)
                       VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,$13,$13)
                       RETURNING id""",
                    header["number"], header["invoice_date"], header["arrival_date"], header["contract"],
                    header["supplier"], header["comment"], header["currency"], header["cur_rate"],
                    header["usd_rate"], header["rate_date"], header["rates_source"],
                    json.dumps(calc["totals"], ensure_ascii=False), user,
                )
            for o in calc["overheads"]:
                await conn.execute(
                    """INSERT INTO cost_purchase_invoice_overhead
                           (invoice_id, kind, hs_code, amount, currency, rate, amount_byn, comment)
                       VALUES ($1,$2,$3,$4,$5,$6,$7,$8)""",
                    int(inv_id), o["kind"], o["hs_code"], o["amount"], o["currency"], o["rate"],
                    o["amount_byn"], o["comment"],
                )
            for ln in calc["lines"]:
                await conn.execute(
                    """INSERT INTO cost_purchase_invoice_line
                           (invoice_id, line_no, model, articul, plan_id, task_number, name, color, hs_code, qty,
                            unit_price_cur, price_byn, sum_byn, logistics_byn, customs_byn, cert_byn, total_byn, total_usd)
                       VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)""",
                    int(inv_id), ln["line_no"], ln["model"], ln["articul"], ln["plan_id"], ln["task_number"],
                    _s(ln.get("name")) or None, _s(ln.get("color")) or None, ln["hs_code"], ln["qty"],
                    ln["unit_price_cur"], ln["price_byn"], ln["sum_byn"], ln["logistics_byn"],
                    ln["customs_byn"], ln["cert_byn"], ln["total_byn"], ln["total_usd"],
                )
    return await get_invoice(int(inv_id))


async def get_invoice(inv_id: int) -> dict[str, Any] | None:
    async with acquire() as conn:
        h = await conn.fetchrow("SELECT * FROM cost_purchase_invoice WHERE id = $1", inv_id)
        if not h:
            return None
        lines = await conn.fetch(
            "SELECT * FROM cost_purchase_invoice_line WHERE invoice_id = $1 ORDER BY line_no", inv_id,
        )
        overheads = await conn.fetch(
            "SELECT * FROM cost_purchase_invoice_overhead WHERE invoice_id = $1 ORDER BY kind, hs_code, id", inv_id,
        )
    inv = _row(h)
    inv["lines"] = [_row(l) for l in lines]
    inv["overheads"] = [_row(o) for o in overheads]
    return inv


async def list_invoices(limit: int = 50) -> list[dict[str, Any]]:
    async with acquire() as conn:
        rows = await conn.fetch(
            """SELECT i.*, (SELECT COUNT(*) FROM cost_purchase_invoice_line l WHERE l.invoice_id = i.id) AS line_count
                 FROM cost_purchase_invoice i ORDER BY i.updated_at DESC LIMIT $1""", limit,
        )
    return [_row(r) for r in rows]


async def delete_invoice(inv_id: int) -> None:
    async with acquire() as conn:
        st = await conn.fetchval("SELECT status FROM cost_purchase_invoice WHERE id = $1", inv_id)
        if st is None:
            raise ValueError("инвойс не найден")
        if st == "applied":
            raise ValueError("применённый инвойс удалить нельзя")
        await conn.execute("DELETE FROM cost_purchase_invoice WHERE id = $1", inv_id)


async def apply_invoice(inv_id: int, user: str) -> dict[str, Any]:
    """Создать по строкам инвойса калькуляции ПФКСС и записи раскладки.

    Строка ПФКСС — копия строки КПСС этой калькуляции (модель, артикул, план,
    задание) с новым признаком, датой расчёта = дата прихода и себестоимостью
    из распределения; в «Основные материалы» — итог в бел. рублях, остальные
    статьи нули. Если КПСС не найдена (строку добавили руками), строка
    собирается из полей инвойса. Повторное применение невозможно.
    """
    inv = await get_invoice(inv_id)
    if not inv:
        raise ValueError("инвойс не найден")
    if inv["status"] == "applied":
        raise ValueError("инвойс уже применён")
    header = {k: inv.get(k) for k in HEADER_FIELDS}
    header["overheads"] = inv.get("overheads") or []
    calc = distribute(header, inv["lines"])   # пересчитываем — источник истины сервер
    cur_rate = float(inv["cur_rate"])
    usd_rate = _f(inv.get("usd_rate"))
    rate_date = _parse_date(inv.get("rate_date") or "") or _parse_date(inv.get("arrival_date") or "")
    calc_date = (_parse_date(inv.get("arrival_date") or "") or _parse_date(inv.get("invoice_date") or "")
                 or date.today())
    batch = uuid.uuid4()
    created = updated = 0

    async with acquire() as conn:
        cols = await _manual_columns(conn)
        col_types = dict(cols)
        data_cols = [c for c, _ in cols if c not in ("id", "batch_id", "source_calc_sign", "source_task",
                                                        "created_by", "created_at", "reason")]
        async with conn.transaction():
            for ln in calc["lines"]:
                key = make_key(ln["model"], ln["articul"], INVOICE_CALC_SIGN, ln["plan_id"], ln["task_number"])
                # Основа — строка КПСС той же калькуляции (из портала), если есть.
                base = await conn.fetchrow(
                    """SELECT * FROM cost_data_all
                        WHERE trim("Модель") = $1 AND trim("Артикул") = $2
                          AND trim(COALESCE("Признак калькуляции", '')) = $3
                          AND trim(COALESCE("PLAN_ID", '')) = $4
                          AND trim(COALESCE("Номер задания производства", '')) = $5
                        ORDER BY is_manual DESC, "дата расчета" DESC NULLS LAST LIMIT 1""",
                    ln["model"], ln["articul"], IMPORT_CALC_SIGN, ln["plan_id"], ln["task_number"],
                )
                row: dict[str, Any] = {c: base[c] for c in data_cols if base is not None and c in base.keys()} if base else {}
                total_byn, total_usd = ln["total_byn"], ln["total_usd"]
                row.update({
                    "Модель": ln["model"], "Артикул": ln["articul"],
                    "Признак калькуляции": INVOICE_CALC_SIGN,
                    "дата расчета": calc_date,
                    "Курс на дату расчета": usd_rate,
                    "Номер задания производства": ln["task_number"] or None,
                    "PLAN_ID": ln["plan_id"] or None,
                    "выпуск шт": ln["qty"],
                    "Материал/операция/декор(призн)": INVOICE_ROW_SIGN,
                    "Наименование": f"Приход по инвойсу {inv['number']}" + (f" ({inv['supplier']})" if inv.get("supplier") else ""),
                    "Норма": 1,
                    "цена материала, руб.": total_byn, "цена материала, USD.": total_usd,
                    "Основные материалы, руб.": total_byn, "Основные материалы, USD.": total_usd,
                    "Вспомогательные материалы, руб.": 0, "Вспомогательные материалы, USD.": 0,
                    "Пошив, руб.": 0, "Пошив, USD.": 0, "Раскрой, руб.": 0, "Раскрой, USD.": 0,
                    "Декоры, руб.": 0, "Декоры, USD.": 0, "Вязание, руб.": 0, "Вязание, USD.": 0,
                    "Себестоимость, руб.": total_byn, "Себестоимость, USD.": total_usd,
                })
                if ln.get("name"):
                    row["Наименование модели"] = ln["name"]
                if ln.get("color"):
                    row["color"] = ln["color"]
                values = [_cast(row.get(c), col_types[c]) for c in data_cols]

                existing = await conn.fetchval(
                    """SELECT id FROM cost_manual_calc
                        WHERE source_calc_sign = $1
                          AND trim("Модель") = $2 AND trim("Артикул") = $3
                          AND trim(COALESCE("Признак калькуляции", '')) = $4
                          AND trim(COALESCE("PLAN_ID", '')) = $5
                          AND trim(COALESCE("Номер задания производства", '')) = $6
                        ORDER BY id LIMIT 1""",
                    INVOICE_SOURCE_SIGN, *key,
                )
                if existing:
                    sets = ", ".join(f'"{c}" = ${i + 2}' for i, c in enumerate(data_cols))
                    await conn.execute(f"UPDATE cost_manual_calc SET {sets} WHERE id = $1", existing, *values)
                    manual_id = existing
                    updated += 1
                else:
                    col_list = ", ".join(f'"{c}"' for c in data_cols)
                    ph = ", ".join(f"${i + 1}" for i in range(len(data_cols)))
                    n = len(data_cols)
                    manual_id = await conn.fetchval(
                        f"""INSERT INTO cost_manual_calc ({col_list}, batch_id, source_calc_sign, source_task, created_by, reason)
                            VALUES ({ph}, ${n + 1}, ${n + 2}, NULL, ${n + 3}, ${n + 4}) RETURNING id""",
                        *values, batch, INVOICE_SOURCE_SIGN, user,
                        f"ПФКСС по приходу: инвойс {inv['number']} (id {inv_id}), строка {ln['line_no']}",
                    )
                    created += 1

                usd = (lambda v: (round(v / usd_rate, 6) if usd_rate and v is not None else None))
                pc_id = await conn.fetchval(
                    """INSERT INTO cost_purchase_cost
                           (model, articul, calc_sign, plan_id, task_number, manual_calc_id, source, invoice_line_id,
                            currency, unit_price_cur, cur_to_byn, usd_to_byn, rate_date, qty,
                            price_usd, price_byn, logistics_usd, logistics_byn, customs_usd, customs_byn,
                            cert_usd, cert_byn, total_usd, total_byn, retail_byn, wholesale_byn, snapshot, created_by)
                       VALUES ($1,$2,$3,$4,$5,$6,'invoice',NULL,
                               $7,$8,$9,$10,$11,$12,
                               $13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25::jsonb,$26)
                       ON CONFLICT ON CONSTRAINT uq_purchase_cost_key DO UPDATE
                          SET manual_calc_id=EXCLUDED.manual_calc_id, source='invoice',
                              currency=EXCLUDED.currency, unit_price_cur=EXCLUDED.unit_price_cur,
                              cur_to_byn=EXCLUDED.cur_to_byn, usd_to_byn=EXCLUDED.usd_to_byn,
                              rate_date=EXCLUDED.rate_date, qty=EXCLUDED.qty,
                              price_usd=EXCLUDED.price_usd, price_byn=EXCLUDED.price_byn,
                              logistics_usd=EXCLUDED.logistics_usd, logistics_byn=EXCLUDED.logistics_byn,
                              customs_usd=EXCLUDED.customs_usd, customs_byn=EXCLUDED.customs_byn,
                              cert_usd=EXCLUDED.cert_usd, cert_byn=EXCLUDED.cert_byn,
                              total_usd=EXCLUDED.total_usd, total_byn=EXCLUDED.total_byn,
                              retail_byn=EXCLUDED.retail_byn, wholesale_byn=EXCLUDED.wholesale_byn,
                              snapshot=EXCLUDED.snapshot, updated_at=now()
                       RETURNING id""",
                    *key, manual_id,
                    inv["currency"], ln["unit_price_cur"], cur_rate, usd_rate, rate_date, ln["qty"],
                    ln.get("price_usd"), ln["price_byn"],
                    usd(ln["logistics_byn"]), ln["logistics_byn"],
                    usd(ln["customs_byn"]), ln["customs_byn"],
                    usd(ln["cert_byn"]), ln["cert_byn"],
                    total_usd, total_byn,
                    _f(row.get("Розничная цена по уровню, руб.")), _f(row.get("Отпускная цена по уровню, руб")),
                    json.dumps({"invoice_id": inv_id, "invoice": inv["number"], "line": ln,
                                "overheads": calc["overheads"], "totals": calc["totals"],
                                "header": {k: (str(v) if isinstance(v, (date, datetime)) else v)
                                           for k, v in header.items() if k != "overheads"}},
                               ensure_ascii=False, default=str),
                    user,
                )
                await conn.execute(
                    """UPDATE cost_purchase_invoice_line
                          SET manual_calc_id=$3, purchase_cost_id=$4, price_byn=$5, sum_byn=$6, logistics_byn=$7,
                              customs_byn=$8, cert_byn=$9, total_byn=$10, total_usd=$11
                        WHERE invoice_id=$1 AND line_no=$2""",
                    inv_id, ln["line_no"], manual_id, pc_id, ln["price_byn"], ln["sum_byn"],
                    ln["logistics_byn"], ln["customs_byn"], ln["cert_byn"], total_byn, total_usd,
                )
            await conn.execute(
                """UPDATE cost_purchase_invoice
                      SET status='applied', applied_by=$2, applied_at=now(), totals=$3::jsonb, updated_at=now()
                    WHERE id=$1""",
                inv_id, user, json.dumps(calc["totals"], ensure_ascii=False),
            )
    log(logging.INFO, "инвойс применён: созданы калькуляции ПФКСС", invoice_id=inv_id, number=inv["number"],
        user=user, created=created, updated=updated)
    result = await get_invoice(inv_id)
    result["applied"] = {"created": created, "updated": updated}
    return result
