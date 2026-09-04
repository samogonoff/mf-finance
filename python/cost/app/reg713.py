"""
Согласование цен по постановлению № 713 (пожелание № 8).

В Беларуси цены на часть ассортимента регулируются (постановление Совмина
№ 713): выпуск новинки и повышение цены требуют согласования с исполкомом.
Здесь — карточка такого согласования на изделие (ключ — модель + артикул, без
признака калькуляции и плана: исполком согласует цену изделия, а не конкретную
калькуляцию — решение заказчика 03.09.2026) и две подсобные операции для неё:

  · поиск артикула-аналога в справочнике Gpartner `S_MODELI` (там 132 тыс.
    записей, 111 тыс. пар модель+артикул; на пару бывает несколько версий,
    берём последнюю по ITEM_ID);
  · цены аналога «по тем же правилам, что в главной таблице»: последняя
    калькуляция аналога в CostHistory (кэш `cost_data_all`) даёт розницу и опт
    по уровню; поверх — утверждённая цена из DWH (`CostHistory_Changes`, а при
    недоступности OLAP — локальный аудит), как накладывается в `/aggregated`;
    если калькуляций нет — плановая из S_MODELI: PRICE_MOPT как опт, розница по
    справочнику уровней (PRICE_TYPE1 → PRICE_TYPE3), как для КПСС/ПФКСС.

Цены аналога хранятся снимком с указанием источника: исполком видел именно
эти цифры. Каждое сохранение карточки пишется в `cost_reg713_log` (было/стало).
"""
from __future__ import annotations

import asyncio
import json
import logging
from datetime import datetime, timezone
from typing import Any

from app.db import acquire, fetch_olap_changes, get_gpartner_conn
from app.logship import log

Reg713Key = tuple[str, str]

KINDS = ("price_increase", "novelty")
DECISIONS = ("approved", "rejected")

# Поля карточки, которые можно менять через API (порядок — как в UI).
EDITABLE = (
    "required", "kind", "analog_model", "analog_articul", "analog_name",
    "analog_retail", "analog_wholesale", "analog_price_source", "analog_price_at",
    "decision", "decision_at", "decision_doc", "comment",
)


def _s(v: Any) -> str:
    return str(v or "").strip()


def make_key(model: Any, articul: Any) -> Reg713Key:
    return (_s(model), _s(articul))


def _num(v: Any) -> float | None:
    if v is None or v == "":
        return None
    try:
        f = float(v)
    except (TypeError, ValueError):
        return None
    return f if f > 0 else None


def _iso(v: Any) -> str | None:
    if isinstance(v, datetime):
        return v.isoformat()
    return v


def _row_to_dict(r: Any) -> dict[str, Any]:
    d = dict(r)
    for k in ("analog_retail", "analog_wholesale"):
        d[k] = float(d[k]) if d.get(k) is not None else None
    for k in ("analog_price_at", "decision_at", "required_at", "created_at", "updated_at"):
        d[k] = _iso(d.get(k))
    return d


# ── Карточки ─────────────────────────────────────────────────────────────────

async def cards_for(keys: list[Reg713Key]) -> dict[Reg713Key, dict[str, Any]]:
    """Карточки по ключам — для наложения на строки /aggregated (поля reg713_*)."""
    uniq = list({k for k in keys if k[0] and k[1]})
    if not uniq:
        return {}
    async with acquire() as conn:
        rows = await conn.fetch(
            """SELECT model, articul, required, kind,
                      analog_model, analog_articul, analog_name,
                      analog_retail, analog_wholesale, analog_price_source,
                      decision, decision_at, decision_doc, comment, updated_by, updated_at
                 FROM cost_reg713
                WHERE (model, articul) IN (SELECT * FROM unnest($1::text[], $2::text[]))""",
            [k[0] for k in uniq], [k[1] for k in uniq],
        )
    out: dict[Reg713Key, dict[str, Any]] = {}
    for r in rows:
        d = _row_to_dict(r)
        k = (d["model"], d["articul"])
        out[k] = {
            "reg713_required": bool(d["required"]),
            "reg713_kind": d["kind"],
            "reg713_analog_model": d["analog_model"],
            "reg713_analog_articul": d["analog_articul"],
            "reg713_analog_name": d["analog_name"],
            "reg713_analog_retail": d["analog_retail"],
            "reg713_analog_wholesale": d["analog_wholesale"],
            "reg713_analog_price_source": d["analog_price_source"],
            "reg713_decision": d["decision"],
            "reg713_decision_at": d["decision_at"],
            "reg713_decision_doc": d["decision_doc"],
            "reg713_comment": d["comment"],
            "reg713_updated_by": d["updated_by"],
            "reg713_updated_at": d["updated_at"],
        }
    return out


REG713_EMPTY = {
    "reg713_required": False, "reg713_kind": None,
    "reg713_analog_model": None, "reg713_analog_articul": None, "reg713_analog_name": None,
    "reg713_analog_retail": None, "reg713_analog_wholesale": None, "reg713_analog_price_source": None,
    "reg713_decision": None, "reg713_decision_at": None, "reg713_decision_doc": None,
    "reg713_comment": None, "reg713_updated_by": None, "reg713_updated_at": None,
}


def apply_cards(rows: list[dict[str, Any]], cards: dict[Reg713Key, dict[str, Any]]) -> None:
    for row in rows:
        k = make_key(row.get("Модель"), row.get("Артикул"))
        row.update(cards.get(k) or REG713_EMPTY)


async def get_card(key: Reg713Key) -> dict[str, Any] | None:
    async with acquire() as conn:
        r = await conn.fetchrow("SELECT * FROM cost_reg713 WHERE model=$1 AND articul=$2", *key)
    return _row_to_dict(r) if r else None


def _validate(payload: dict[str, Any]) -> dict[str, Any]:
    """Поля карточки из тела запроса → значения для БД. Ошибка — ValueError."""
    out: dict[str, Any] = {}
    if "required" in payload:
        if not isinstance(payload["required"], bool):
            raise ValueError("required должен быть true или false")
        out["required"] = payload["required"]
    if "kind" in payload:
        kind = payload["kind"] or None
        if kind is not None and kind not in KINDS:
            raise ValueError("kind: price_increase или novelty")
        out["kind"] = kind
    for f in ("analog_model", "analog_articul", "analog_name", "analog_price_source", "decision_doc", "comment"):
        if f in payload:
            out[f] = _s(payload[f])[:1000] or None
    for f in ("analog_retail", "analog_wholesale"):
        if f in payload:
            v = payload[f]
            if v in (None, ""):
                out[f] = None
            else:
                try:
                    out[f] = round(float(v), 2)
                except (TypeError, ValueError):
                    raise ValueError(f"{f}: не число")
    if "decision" in payload:
        dec = payload["decision"] or None
        if dec is not None and dec not in DECISIONS:
            raise ValueError("decision: approved, rejected или пусто")
        out["decision"] = dec
    # Аналог задан — цены помечаем временем снимка; аналог снят — чистим цены.
    if out.get("analog_articul") is None and "analog_articul" in out:
        out.update({"analog_model": None, "analog_name": None, "analog_retail": None,
                    "analog_wholesale": None, "analog_price_source": None, "analog_price_at": None})
    elif any(f in out for f in ("analog_retail", "analog_wholesale", "analog_articul")):
        out["analog_price_at"] = datetime.now(timezone.utc)
    return out


async def save_card(key: Reg713Key, payload: dict[str, Any], user: str) -> dict[str, Any]:
    """Создать или обновить карточку; изменение — в журнал. Возвращает карточку."""
    fields = _validate(payload)
    if not fields:
        raise ValueError("нет полей для сохранения")

    async with acquire() as conn:
        async with conn.transaction():
            before = await conn.fetchrow(
                "SELECT * FROM cost_reg713 WHERE model=$1 AND articul=$2 FOR UPDATE", *key,
            )
            before_d = _row_to_dict(before) if before else None

            # decision_at — момент, когда решение появилось или сменилось.
            if "decision" in fields:
                prev = before_d["decision"] if before_d else None
                if fields["decision"] != prev:
                    fields["decision_at"] = datetime.now(timezone.utc) if fields["decision"] else None
            # required_at — дата отметки «требуется согласование» (для отчёта):
            # ставится, когда галочка включается; снятие не чистит.
            if fields.get("required") and not (before_d and before_d.get("required")):
                fields["required_at"] = datetime.now(timezone.utc)

            if before:
                sets = ", ".join(f"{f} = ${i + 3}" for i, f in enumerate(fields))
                row = await conn.fetchrow(
                    f"""UPDATE cost_reg713
                           SET {sets}, updated_by = ${len(fields) + 3}, updated_at = now()
                         WHERE model=$1 AND articul=$2
                     RETURNING *""",
                    *key, *fields.values(), user,
                )
            else:
                cols = ["model", "articul", *fields.keys(), "updated_by"]
                ph = ", ".join(f"${i + 1}" for i in range(len(cols)))
                row = await conn.fetchrow(
                    f"INSERT INTO cost_reg713 ({', '.join(cols)}) VALUES ({ph}) RETURNING *",
                    *key, *fields.values(), user,
                )
            after_d = _row_to_dict(row)
            await conn.execute(
                "INSERT INTO cost_reg713_log (reg713_id, changed_by, before, after) VALUES ($1, $2, $3::jsonb, $4::jsonb)",
                after_d["id"], user,
                json.dumps(before_d, ensure_ascii=False, default=str) if before_d else None,
                json.dumps(after_d, ensure_ascii=False, default=str),
            )

    log(logging.INFO, "карточка 713 сохранена", model=key[0], articul=key[1],
        user=user, changed=list(fields.keys()), decision=after_d.get("decision"))
    return after_d


async def history(key: Reg713Key) -> list[dict[str, Any]]:
    async with acquire() as conn:
        rows = await conn.fetch(
            """SELECT l.changed_by, l.changed_at, l.before, l.after
                 FROM cost_reg713_log l JOIN cost_reg713 c ON c.id = l.reg713_id
                WHERE c.model=$1 AND c.articul=$2
                ORDER BY l.changed_at DESC LIMIT 50""",
            *key,
        )
    out = []
    for r in rows:
        out.append({
            "changed_by": r["changed_by"], "changed_at": _iso(r["changed_at"]),
            "before": json.loads(r["before"]) if isinstance(r["before"], str) else r["before"],
            "after": json.loads(r["after"]) if isinstance(r["after"], str) else r["after"],
        })
    return out


# ── Поиск аналога в S_MODELI ─────────────────────────────────────────────────

def _search_sync(q: str, limit: int) -> list[dict[str, Any]]:
    """Поиск по артикулу (с начала и по вхождению), модели и наименованию.

    На пару модель+артикул в S_MODELI бывает несколько версий — берём последнюю
    по ITEM_ID. Архивные (PR_ARH=1) не скрываем: аналогом может быть и снятое с
    производства изделие, но помечаем их и ставим в конец.
    """
    like_any = f"%{q}%"
    like_start = f"{q}%"
    conn = get_gpartner_conn()
    cur = conn.cursor()
    try:
        cur.execute(
            f"""SELECT TOP {int(limit)} MODEL, ART, NAIM, PR_ARH, PRICE_MOPT, PRICE_ROZN, ITEM_ID
                  FROM (
                    SELECT RTRIM(MODEL) AS MODEL, RTRIM(ART) AS ART, RTRIM(NAIM) AS NAIM,
                           PR_ARH, PRICE_MOPT, PRICE_ROZN, ITEM_ID,
                           ROW_NUMBER() OVER (PARTITION BY RTRIM(MODEL), RTRIM(ART) ORDER BY ITEM_ID DESC) AS rn
                      FROM [dbo].[S_MODELI]
                     WHERE ISFOLDER = 0 AND MODEL IS NOT NULL AND ART IS NOT NULL
                       AND (ART LIKE ? OR MODEL LIKE ? OR NAIM LIKE ?)
                  ) t
                 WHERE rn = 1
                 ORDER BY CASE WHEN ART LIKE ? THEN 0 ELSE 1 END, PR_ARH, ART, MODEL""",
            like_any, like_start, like_any, like_start,
        )
        out = []
        for m, a, n, arh, mopt, rozn, item_id in cur.fetchall():
            out.append({
                "model": _s(m), "articul": _s(a), "name": _s(n),
                "archived": bool(arh and int(arh) == 1),
                "price_mopt": _num(mopt), "price_rozn": _num(rozn),
                "item_id": int(item_id) if item_id is not None else None,
            })
        return out
    finally:
        conn.close()


async def search_articuls(q: str, limit: int = 30) -> list[dict[str, Any]]:
    q = _s(q)
    if len(q) < 2:
        return []
    loop = asyncio.get_running_loop()
    return await loop.run_in_executor(None, _search_sync, q, max(1, min(limit, 100)))


# ── Цены «как в главной таблице» — для аналога и для самих изделий отчёта ─────

Pair = tuple[str, str]


def _gpartner_prices_batch_sync(pairs: list[Pair]) -> dict[Pair, dict[str, Any]]:
    """Плановые цены и наименования из S_MODELI + справочник уровней (резерв).

    Розница: PRICE_ROZN, иначе уровень цен изделия (PRICE_LEVEL_ID → PRICE_TYPE3),
    иначе — то же правило, что для КПСС/ПФКСС в /aggregated: уровень, у которого
    PRICE_TYPE1 = опт; при нескольких кандидатах — ближайший к опт × 1,4 × (1 + НДС).
    Группу «Девочкам/Мальчикам» (×1,3) не различаем — у S_MODELI нет Level 01.
    """
    if not pairs:
        return {}
    conn = get_gpartner_conn()
    cur = conn.cursor()
    try:
        cur.execute("SELECT ITEM_ID, PRICE_TYPE1, PRICE_TYPE3 FROM [dbo].[s_price_level]")
        levels = [(int(i or 0), _num(p1), _num(p3)) for i, p1, p3 in cur.fetchall()]
        by_id = {i: p3 for i, _, p3 in levels}

        out: dict[Pair, dict[str, Any]] = {}
        for i in range(0, len(pairs), 400):
            batch = pairs[i:i + 400]
            conds = " OR ".join("(RTRIM(MODEL) = ? AND RTRIM(ART) = ?)" for _ in batch)
            params: list[str] = []
            for m, a in batch:
                params.extend([m, a])
            cur.execute(
                f"""SELECT MODEL, ART, NAIM, PRICE_MOPT, PRICE_ROZN, NDS, PRICE_LEVEL_ID FROM (
                        SELECT RTRIM(MODEL) AS MODEL, RTRIM(ART) AS ART, RTRIM(NAIM) AS NAIM,
                               PRICE_MOPT, PRICE_ROZN, NDS, PRICE_LEVEL_ID,
                               ROW_NUMBER() OVER (PARTITION BY RTRIM(MODEL), RTRIM(ART) ORDER BY ITEM_ID DESC) AS rn
                          FROM [dbo].[S_MODELI] WHERE {conds}
                    ) t WHERE rn = 1""",
                params,
            )
            for m, a, name, mopt, rozn, nds, level_id in cur.fetchall():
                wholesale = _num(mopt)
                retail = _num(rozn)
                if retail is None and level_id:
                    retail = by_id.get(int(level_id))
                if retail is None and wholesale is not None:
                    cands = [p3 for _, p1, p3 in levels
                             if p1 is not None and p3 is not None and round(p1, 2) == round(wholesale, 2)]
                    if len(cands) == 1:
                        retail = cands[0]
                    elif cands:
                        target = wholesale * 1.4 * (1 + float(nds or 0) / 100)
                        retail = min(cands, key=lambda c: abs(c - target))
                out[(_s(m), _s(a))] = {"name": _s(name), "retail": retail, "wholesale": wholesale}
        return out
    finally:
        conn.close()


async def prices_for_pairs(pairs: list[Pair]) -> dict[Pair, dict[str, Any]]:
    """Розница и опт по правилам главной таблицы для набора пар модель+артикул.

    1. Последняя калькуляция пары в CostHistory (`cost_data_all`): розница и
       опт «по уровню», наименование, признак и план (ключ для шага 2).
    2. Утверждённая цена из DWH по ключу этой калькуляции (OLAP
       `CostHistory_Changes`, при недоступности — локальный аудит
       `cost_price_changes_audit`), как накладывается в `/aggregated`. Главнее.
    3. Пары без цен — плановые из S_MODELI (см. _gpartner_prices_batch_sync).
    На пару: {name, retail, wholesale, source, calc_sign, plan_id, calc_date}.
    """
    uniq = list({(_s(m), _s(a)) for m, a in pairs if _s(m) and _s(a)})
    result: dict[Pair, dict[str, Any]] = {
        p: {"name": None, "retail": None, "wholesale": None, "source": None,
            "calc_sign": None, "plan_id": None, "calc_date": None}
        for p in uniq
    }
    if not uniq:
        return result

    async with acquire() as conn:
        rows = await conn.fetch(
            """SELECT DISTINCT ON (m, a) m, a, cs, pi, d, name, retail, wholesale FROM (
                   SELECT TRIM("Модель") AS m, TRIM("Артикул") AS a,
                          TRIM("Признак калькуляции") AS cs, TRIM("PLAN_ID") AS pi,
                          MAX("дата расчета") AS d,
                          MAX(TRIM("Наименование модели")) AS name,
                          AVG("Розничная цена по уровню, руб.") AS retail,
                          AVG("Отпускная цена по уровню, руб") AS wholesale
                     FROM cost_data_all
                    WHERE (TRIM("Модель"), TRIM("Артикул")) IN (SELECT * FROM unnest($1::text[], $2::text[]))
                    GROUP BY 1, 2, 3, 4
               ) t ORDER BY m, a, d DESC NULLS LAST""",
            [p[0] for p in uniq], [p[1] for p in uniq],
        )
    keys4: list[tuple[str, str, str, str]] = []
    for r in rows:
        p = (r["m"], r["a"])
        info = result[p]
        info.update({
            "name": r["name"], "calc_sign": r["cs"], "plan_id": r["pi"], "calc_date": _iso(r["d"]),
            "retail": _num(r["retail"]), "wholesale": _num(r["wholesale"]),
        })
        if info["retail"] is not None or info["wholesale"] is not None:
            info["source"] = "calc"
        keys4.append((p[0], p[1], r["cs"] or "", r["pi"] or ""))

    # Утверждённые цены поверх — тот же порядок источников, что в /aggregated.
    if keys4:
        overrides: dict[tuple[str, str, str, str], dict[str, Any]] = {}
        try:
            loop = asyncio.get_running_loop()
            recs = await loop.run_in_executor(None, fetch_olap_changes, keys4)
            for rec in recs:
                k = (_s(rec.get("Модель")), _s(rec.get("Артикул")), _s(rec.get("calc_sign")), _s(rec.get("plan_id")))
                overrides.setdefault(k, {"retail": _num(rec.get("retail_rub")), "wholesale": _num(rec.get("wholesale_rub"))})
        except Exception as exc:  # noqa: BLE001 — OLAP за VPN
            log(logging.WARNING, "reg713: OLAP недоступен, утверждённые цены из локального аудита", error=str(exc))
            async with acquire() as conn:
                arows = await conn.fetch(
                    """SELECT DISTINCT ON (model, articul, COALESCE(calc_sign,''), COALESCE(plan_id,''))
                              model, articul, COALESCE(calc_sign,'') AS cs, COALESCE(plan_id,'') AS pi,
                              retail_rub, wholesale_rub
                         FROM cost_price_changes_audit
                        WHERE (model, articul, COALESCE(calc_sign,''), COALESCE(plan_id,'')) IN (
                              SELECT * FROM unnest($1::text[], $2::text[], $3::text[], $4::text[]))
                        ORDER BY model, articul, COALESCE(calc_sign,''), COALESCE(plan_id,''), changed_at DESC""",
                    [k[0] for k in keys4], [k[1] for k in keys4], [k[2] for k in keys4], [k[3] for k in keys4],
                )
                for a in arows:
                    overrides[(a["model"], a["articul"], a["cs"], a["pi"])] = {
                        "retail": _num(a["retail_rub"]), "wholesale": _num(a["wholesale_rub"])}
        for k, o in overrides.items():
            info = result.get((k[0], k[1]))
            if not info or (o["retail"] is None and o["wholesale"] is None):
                continue
            if o["retail"] is not None:
                info["retail"] = o["retail"]
            if o["wholesale"] is not None:
                info["wholesale"] = o["wholesale"]
            info["source"] = "dwh"

    # Резерв — плановые из S_MODELI для пар без цен (и наименование, если пусто).
    missing = [p for p, i in result.items() if i["retail"] is None and i["wholesale"] is None]
    if missing:
        try:
            loop = asyncio.get_running_loop()
            gp = await loop.run_in_executor(None, _gpartner_prices_batch_sync, missing)
        except Exception as exc:  # noqa: BLE001
            log(logging.WARNING, "reg713: Gpartner недоступен для плановых цен", error=str(exc))
            gp = {}
        for p, g in gp.items():
            info = result[p]
            info["name"] = info["name"] or g["name"]
            info["retail"], info["wholesale"] = g["retail"], g["wholesale"]
            if g["retail"] is not None or g["wholesale"] is not None:
                info["source"] = "gpartner"
    return result


async def analog_prices(model: str, articul: str) -> dict[str, Any]:
    """Цены одной пары (аналога) — обёртка над prices_for_pairs."""
    model, articul = _s(model), _s(articul)
    base = {"model": model, "articul": articul, "name": None, "retail": None, "wholesale": None,
            "source": None, "calc_sign": None, "plan_id": None, "calc_date": None}
    if not model or not articul:
        return base
    info = (await prices_for_pairs([(model, articul)])).get((model, articul)) or {}
    base.update(info)
    return base


# ── Отчёт: все изделия, отмеченные на согласование ───────────────────────────

async def list_cards(*, include_unmarked: bool = False) -> list[dict[str, Any]]:
    """Карточки для отчёта «Согласование 713»: изделие с наименованием и
    ценами (по правилам главной таблицы), аналог с его снимком цен, вид,
    решение, даты. По умолчанию — только отмеченные «требуется согласование»."""
    async with acquire() as conn:
        rows = await conn.fetch(
            "SELECT * FROM cost_reg713" + ("" if include_unmarked else " WHERE required")
            + " ORDER BY required_at DESC NULLS LAST, updated_at DESC"
        )
    cards = [_row_to_dict(r) for r in rows]
    prices = await prices_for_pairs([(c["model"], c["articul"]) for c in cards])
    for c in cards:
        info = prices.get((c["model"], c["articul"])) or {}
        c["name"] = info.get("name")
        c["retail"] = info.get("retail")
        c["wholesale"] = info.get("wholesale")
        c["price_source"] = info.get("source")
        c["calc_sign"] = info.get("calc_sign")
        c["plan_id"] = info.get("plan_id")
    return cards
