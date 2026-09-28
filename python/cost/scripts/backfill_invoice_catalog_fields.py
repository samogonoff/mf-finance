"""
Разовое дозаполнение: бренд-менеджер, Level 01-05 и страна производства у
калькуляций ПФКСС по приходу (cost_manual_calc, source_calc_sign = 'ПРИХОД'),
созданных без КПСС-основы до правки 28.09.2026. Пример — план 8910, модель
121956: строки пришли в главную таблицу без бренд-менеджера, и БМ не находил их
под фильтром по своей фамилии. Применённый инвойс переприменить нельзя, поэтому
поля дописываются на месте.

Значения — из справочника Gpartner, тем же кодом, что у новых строк прихода
(purchase.catalog_for_pairs). Пишется только в пустые поля (NULL, '' или «-»);
заполненное не трогается. Те же поля дописываются в строки версий этих
калькуляций (cost_calc_version_rows): активная версия заменяет собой строки
калькуляции при каждом обновлении кэша (db._reapply_active_versions_to_cache),
и без этого поля снова стали бы пустыми после ближайшего refresh.

Запуск внутри контейнера python-cost (там доступны COST_DATABASE_URL и Gpartner):
    python -m scripts.backfill_invoice_catalog_fields --dry-run   # только показать
    python -m scripts.backfill_invoice_catalog_fields             # применить

--dry-run выполняет те же UPDATE и откатывает транзакцию, поэтому счётчики в
нём точные.
"""

from __future__ import annotations

import argparse
import asyncio

from app import db
from app.purchase import CATALOG_FIELDS, INVOICE_SOURCE_SIGN, catalog_for_pairs


class _DryRun(Exception):
    """Выход из транзакции с откатом в режиме --dry-run."""


def _blank_sql(column: str, alias: str = "") -> str:
    return f"COALESCE(trim({alias}\"{column}\"), '') IN ('', '-')"


def _set_sql(columns: list[str], first_param: int, alias: str = "") -> str:
    """SET только для пустых значений: заполненное остаётся как есть."""
    return ", ".join(
        f'"{c}" = CASE WHEN {_blank_sql(c, alias)} THEN ${first_param + i} ELSE {alias}"{c}" END'
        for i, c in enumerate(columns)
    )


def _count(status: str) -> int:
    """Число строк из статуса asyncpg вида 'UPDATE 3'."""
    return int(status.rsplit(" ", 1)[-1] or 0)


async def _fill_pair(conn, model: str, articul: str, fields: dict[str, str]) -> tuple[int, int]:
    """Дописать поля одной паре: строки калькуляций прихода и строки их версий."""
    columns = [c for c in CATALOG_FIELDS if c in fields]
    values = [fields[c] for c in columns]
    n_calc = _count(await conn.execute(
        f"""UPDATE cost_manual_calc SET {_set_sql(columns, 4)}
             WHERE source_calc_sign = $1
               AND trim("Модель") = $2 AND trim(COALESCE("Артикул", '')) = $3
               AND ({" OR ".join(_blank_sql(c) for c in columns)})""",
        INVOICE_SOURCE_SIGN, model, articul, *values,
    ))
    n_ver = _count(await conn.execute(
        f"""UPDATE cost_calc_version_rows r SET {_set_sql(columns, 4, "r.")}
              FROM cost_calc_versions v
             WHERE r.version_id = v.id
               AND trim(v.model) = $2 AND trim(v.articul) = $3
               AND EXISTS (
                   SELECT 1 FROM cost_manual_calc m
                    WHERE m.source_calc_sign = $1
                      AND trim(m."Модель") = trim(v.model)
                      AND trim(COALESCE(m."Артикул", '')) = trim(v.articul)
                      AND m."Признак калькуляции" IS NOT DISTINCT FROM v.calc_sign
                      AND m."PLAN_ID" IS NOT DISTINCT FROM v.plan_id)
               AND ({" OR ".join(_blank_sql(c, "r.") for c in columns)})""",
        INVOICE_SOURCE_SIGN, model, articul, *values,
    ))
    return n_calc, n_ver


async def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--dry-run", action="store_true", help="только показать, что будет изменено, без записи")
    args = parser.parse_args()

    await db.init_pool()
    try:
        async with db.pool().acquire() as conn:
            pairs = await conn.fetch(
                f"""SELECT trim("Модель") AS model, trim(COALESCE("Артикул", '')) AS articul, count(*) AS n
                      FROM cost_manual_calc
                     WHERE source_calc_sign = $1
                       AND ({" OR ".join(_blank_sql(c) for c in CATALOG_FIELDS)})
                     GROUP BY 1, 2 ORDER BY 1, 2""",
                INVOICE_SOURCE_SIGN,
            )
            # Кандидаты — с запасом: у пути из четырёх уровней Level 05 пуст
            # законно, и такие пары остаются здесь при каждом запуске.
            print(f"Пар модель+артикул прихода с пустыми полями: {len(pairs)}")
            if not pairs:
                return
            catalog = await catalog_for_pairs([(p["model"], p["articul"]) for p in pairs])

            calc_rows = version_rows = filled_pairs = unchanged = 0
            not_found: list[str] = []
            try:
                async with conn.transaction():
                    for p in pairs:
                        fields = {c: v for c, v in (catalog.get((p["model"], p["articul"])) or {}).items() if v}
                        if not fields:
                            not_found.append(f"{p['model']} / {p['articul']}")
                            continue
                        n_calc, n_ver = await _fill_pair(conn, p["model"], p["articul"], fields)
                        if not n_calc and not n_ver:
                            unchanged += 1
                            continue
                        filled_pairs += 1
                        calc_rows += n_calc
                        version_rows += n_ver
                        path = " \\ ".join(fields[c] for c in CATALOG_FIELDS if c.startswith("Level") and c in fields)
                        print(f"  {p['model']} / {p['articul']}: БМ «{fields.get('Бренд-менеджер', '—')}», "
                              f"{path or 'без уровней'}, страна «{fields.get('Страна пр-ва', '—')}» — "
                              f"строк калькуляций {n_calc}, строк версий {n_ver}")
                    if args.dry_run:
                        raise _DryRun
            except _DryRun:
                pass

            for pair in not_found:
                print(f"  {pair}: нет в справочнике Gpartner — пропуск")
            prefix = "(dry-run, откатено) " if args.dry_run else ""
            print(f"{prefix}Дописано пар: {filled_pairs}, строк калькуляций: {calc_rows}, "
                  f"строк версий: {version_rows}; дописывать нечего: {unchanged}, "
                  f"нет в справочнике: {len(not_found)}")
    finally:
        await db.close_pool()


if __name__ == "__main__":
    asyncio.run(main())
