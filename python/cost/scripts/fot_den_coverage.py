# -*- coding: utf-8 -*-
"""Покрытие знаменателей долей ФОТ правилами страна × направление → канал.

Задача 663620, этап 0 (сверочный, только чтение БД): LEGACY-правила (ветки
grp/kz/uz по стране витрины, fot_den_rules) против фактического зерна
fot_monthly. Отвечает на четыре вопроса перед этапом 1:

  (a) какими ячейками (страна витрины, флаг kz_denominator, канал,
      направление, группа ЦФО) ляжет выручка в новый CTE den_grain и сколько
      в каждой факта 2025/2026 — топ по выручке и хвосты с пустыми
      измерениями;
  (b) доля выручки страны БЕЗ канала (пустой sales_channel в fot_cfo или код
      ЦФО вне справочника) — что потеряет правило «→ канал» этапа 1;
  (c) сумма выручки там, где страна витрины (m.country_code) не совпала со
      страной справочника (c.country_code): зерно ФОТ режет страну по
      справочнику, den_grain — по витрине, расхождение считает знаменатель;
  (d) все ячейки ФОТ (страна справочника × направление × группа ЦФО) с
      сработавшим LEGACY-правилом — карта, на которую этап 1 повесит правила
      страна × направление → канал.

Вывод — читаемый текст в stdout (суммы в тыс. BYN, факт-сценарий). Запуск
из контейнера:
    docker exec -e PYTHONPATH=/var/www/cost swarm-python-cost-1 \
        python -m scripts.fot_den_coverage
"""
from __future__ import annotations

import asyncio

from app import db, fot
from app import fot_den_rules

TOP_CELLS = 25   # (a): сколько строк зерна печатать (остальные — суммой хвоста)


def _t(v) -> float:
    """Сумма витрины (asyncpg отдаёт Decimal) → тыс. BYN (None → 0)."""
    return round(float(v or 0) / 1000.0, 1)


def _rev_facts(col: str = "amount_byn") -> dict[str, str]:
    """SUM FILTER факта «Продажи без НДС» за 2025 и 2026 годы."""
    def one(year: int) -> str:
        return ("sum(m.%s) FILTER (WHERE m.scenario_src = 'факт'"
                " AND date_part('year', m.month_date) = %d"
                " AND %s)" % (col, year, fot._SQL_REV_FAMILY))
    return {"rv_f25": one(fot.YEAR_PREV), "rv_f26": one(fot.YEAR_PLAN)}


async def _cells_sql(pg) -> list[dict]:
    """(a): зерно den_grain — DISTINCT (ctry, kzf, chan, dir, g1) + факт-2025/26.
    Среза intercompany нет: знаменатель дашборда при «все» видит все строки."""
    rv = _rev_facts()
    return await pg.fetch(f"""
        SELECT m.country_code AS ctry, m.kz_denominator AS kzf,
               coalesce(c.sales_channel, '') AS chan,
               coalesce(c.direction, '') AS dir,
               coalesce(c.group_cfo1, '') AS g1,
               {rv['rv_f25']} AS rv_f25, {rv['rv_f26']} AS rv_f26
        FROM fot_monthly m
        LEFT JOIN fot_cfo c ON c.code_cfo = m.code_cfo
        GROUP BY 1, 2, 3, 4, 5
    """)


def _print_cells(cells: list[dict]) -> None:
    rows = sorted(cells, key=lambda r: (r["rv_f25"] or 0) + (r["rv_f26"] or 0), reverse=True)
    print("(a) ячейки знаменателя den_grain (страна витрины × kz_denominator ×"
          " канал × направление × группа ЦФО), факт 2025/2026, тыс BYN;"
          " всего ячеек: %d" % len(rows))
    print("    %-4s %-4s %-24s %-22s %-20s %12s %12s" % (
        "ctry", "kzf", "chan", "dir", "g1", "факт25", "факт26"))
    for r in rows[:TOP_CELLS]:
        print("    %-4s %-4s %-24s %-22s %-20s %12s %12s" % (
            r["ctry"] or "·", "1" if r["kzf"] else "0",
            (r["chan"] or "·")[:24], (r["dir"] or "·")[:22], (r["g1"] or "·")[:20],
            _fmt(_t(r["rv_f25"])), _fmt(_t(r["rv_f26"]))))
    if len(rows) > TOP_CELLS:
        tail = rows[TOP_CELLS:]
        print("    … хвост %d ячеек: факт25 %s / факт26 %s тыс BYN" % (
            len(tail), _fmt(_t(sum(r["rv_f25"] or 0 for r in tail))),
            _fmt(_t(sum(r["rv_f26"] or 0 for r in tail)))))
    tot25 = sum(r["rv_f25"] or 0 for r in rows)
    tot26 = sum(r["rv_f26"] or 0 for r in rows)
    for dim, label in (("chan", "без канала"), ("dir", "без направления"), ("g1", "без группы ЦФО")):
        s25 = sum(r["rv_f25"] or 0 for r in rows if not r[dim])
        s26 = sum(r["rv_f26"] or 0 for r in rows if not r[dim])
        print("    выручка %s: факт25 %s (%.2f%%) / факт26 %s (%.2f%%)" % (
            label, _fmt(_t(s25)), _pct(s25, tot25), _fmt(_t(s26)), _pct(s26, tot26)))
    print("    итог: факт25 %s / факт26 %s тыс BYN" % (_fmt(_t(tot25)), _fmt(_t(tot26))))


def _print_no_channel(cells: list[dict]) -> None:
    """(b): доля выручки страны без канала — по стране витрины (как режет den_grain)."""
    print("\n(b) выручка без канала по странам витрины (den_grain), тыс BYN")
    print("    %-6s %14s %14s %8s" % ("ctry", "без канала", "всего", "доля,%"))
    by: dict = {}
    for r in cells:
        e = by.setdefault(r["ctry"] or "·", [0, 0])
        e[0] += r["rv_f25"] or 0
        e[1] += r["rv_f26"] or 0
    for ctry in sorted(by, key=lambda c: by[c][0] + by[c][1], reverse=True):
        no_ch = sum((r["rv_f25"] or 0) + (r["rv_f26"] or 0)
                    for r in cells if (r["ctry"] or "") == (ctry if ctry != "·" else "")
                    and not r["chan"])
        total = by[ctry][0] + by[ctry][1]
        print("    %-6s %14s %14s %8s" % (ctry, _fmt(_t(no_ch)), _fmt(_t(total)),
                                           "%.2f" % (_pct(no_ch, total),)))


async def _print_country_mismatch(pg) -> None:
    """(c): выручка там, где m.country_code ≠ c.country_code (факт 2025/2026)."""
    rv = _rev_facts()
    rows = await pg.fetch(f"""
        SELECT * FROM (
            SELECT coalesce(m.country_code, '·') AS m_ctry,
                   coalesce(c.country_code, '·') AS c_ctry,
                   count(*) AS n,
                   {rv['rv_f25']} AS rv_f25, {rv['rv_f26']} AS rv_f26
            FROM fot_monthly m
            LEFT JOIN fot_cfo c ON c.code_cfo = m.code_cfo
            WHERE m.country_code IS DISTINCT FROM c.country_code
            GROUP BY 1, 2
        ) t
        WHERE t.rv_f25 IS NOT NULL OR t.rv_f26 IS NOT NULL
        ORDER BY coalesce(t.rv_f25, 0) + coalesce(t.rv_f26, 0) DESC
    """)
    print("\n(c) расхождение стран витрина/справочник (m.country_code ≠"
          " c.country_code), выручка факта, тыс BYN")
    if not rows:
        print("    нет")
        return
    print("    %-6s %-6s %8s %14s %14s" % ("витр.", "справ.", "строк", "факт25", "факт26"))
    for r in rows:
        print("    %-6s %-6s %8d %14s %14s" % (
            r["m_ctry"], r["c_ctry"], r["n"],
            _fmt(_t(r["rv_f25"])), _fmt(_t(r["rv_f26"]))))


async def _print_fot_coverage(pg) -> None:
    """(d): все ячейки ФОТ (страна справочника × направление × группа ЦФО) с
    сработавшим LEGACY-правилом знаменателя."""
    rv = _rev_facts()
    rows = await pg.fetch(f"""
        SELECT coalesce(c.country_code, '') AS ctry,
               coalesce(c.direction, '') AS dir,
               coalesce(c.group_cfo1, '') AS g1,
               sum(m.amount_byn) FILTER (WHERE {fot._SQL_FOT_FAMILY}) AS fot_sum,
               {rv['rv_f25']} AS rv_f25, {rv['rv_f26']} AS rv_f26
        FROM fot_monthly m
        LEFT JOIN fot_cfo c ON c.code_cfo = m.code_cfo
        GROUP BY 1, 2, 3
        HAVING sum(m.amount_byn) FILTER (WHERE {fot._SQL_FOT_FAMILY}) IS NOT NULL
        ORDER BY 4 DESC NULLS LAST
    """)
    print("\n(d) ячейки ФОТ (страна справочника × направление × группа ЦФО)"
          " и LEGACY-правило знаменателя; ФОТ — все сценарии/годы, тыс BYN")
    print("    %-4s %-24s %-22s %-9s %14s %12s %12s" % (
        "ctry", "dir", "g1", "правило", "ФОТ", "факт25 rv", "факт26 rv"))
    by_rule: dict = {}
    for r in rows:
        rule = fot_den_rules.matching_rule(r["ctry"], r["dir"], r["g1"])
        by_rule[rule[1]] = by_rule.get(rule[1], 0) + (r["fot_sum"] or 0)
        print("    %-4s %-24s %-22s %-9s %14s %12s %12s" % (
            r["ctry"] or "·", (r["dir"] or "·")[:24], (r["g1"] or "·")[:22],
            rule[1], _fmt(_t(r["fot_sum"])),
            _fmt(_t(r["rv_f25"])), _fmt(_t(r["rv_f26"]))))
    print("    Σ ФОТ по правилам: " + ", ".join(
        "%s %s" % (name, _fmt(_t(s))) for name, s in sorted(by_rule.items())))


def _fmt(v: float) -> str:
    return "{:,.1f}".format(v).replace(",", " ")


def _pct(part, total) -> float:
    return 100.0 * float(part) / float(total) if total else 0.0


async def main() -> None:
    await db.init_pool()
    try:
        async with db.acquire() as pg:
            cells = await _cells_sql(pg)
            _print_cells(cells)
            _print_no_channel(cells)
            await _print_country_mismatch(pg)
            await _print_fot_coverage(pg)
    finally:
        await db.close_pool()


if __name__ == "__main__":
    asyncio.run(main())
