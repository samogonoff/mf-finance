"""
Разовая миграция данных (не схемы): нормализует легаси-коды типа строки
('шт', 'себестоимость лиса осн/всп', 'пошив', 'раскорой' — реальные значения
из источника, см. db._normalize_row_type) в cost_calc_version_rows к
каноническим значениям дропдауна редактора версий, выводит Норма/цена для
операционных строк и пересчитывает денежные бакеты (db._recalc_cost_buckets) —
во всех версиях со статусом draft/pending. Approved/archived/rejected версии
не трогает: это уже применённые к cost_data_cache исторические расчёты.

В отличие от более ранней (уже не актуальной) идеи "расщепления" — реальные
данные уже хранят Пошив и Раскрой отдельными строками, так что количество
строк не меняется, миграция — это UPDATE на месте.

Запуск внутри контейнера python-cost (там доступен COST_DATABASE_URL):
    python -m scripts.migrate_normalize_operation_rows --dry-run   # только показать
    python -m scripts.migrate_normalize_operation_rows             # применить
"""

from __future__ import annotations

import argparse
import asyncio

from app import db
from app.db import CACHE_COLUMNS, _apply_version_rows_to_cache, _normalize_row_type, _recalc_cost_buckets

_NORMALIZED_COLUMNS = [
    "Материал/операция/декор(призн)",
    "Норма",
    "цена материала, руб.",
    "цена материала, USD.",
    "Основные материалы, руб.", "Основные материалы, USD.",
    "Вспомогательные материалы, руб.", "Вспомогательные материалы, USD.",
    "Декоры, руб.", "Декоры, USD.",
    "Пошив, руб.", "Пошив, USD.",
    "Раскрой, руб.", "Раскрой, USD.",
    "Вязание, руб.", "Вязание, USD.",
]
assert set(_NORMALIZED_COLUMNS) <= set(CACHE_COLUMNS)


async def _migrate_version(conn, version_id: int, dry_run: bool) -> int:
    rows = await conn.fetch(
        """SELECT id, "Материал/операция/декор(призн)", "Level 01", "Норма",
                  "цена материала, руб.", "цена материала, USD.",
                  "Пошив, минуты", "Пошив, руб.", "Пошив, USD.",
                  "Раскрой, минуты", "Раскрой, руб.", "Раскрой, USD."
           FROM cost_calc_version_rows WHERE version_id=$1""",
        version_id,
    )
    touched = 0
    for r in rows:
        row = dict(r)
        raw_type = (row.get("Материал/операция/декор(призн)") or "").strip()
        if raw_type not in ("шт", "Декоры лиса", "себестоимость лиса осн",
                             "себестоимость лиса всп", "пошив", "раскорой"):
            continue  # уже канонический или неизвестный тип — не трогаем
        _normalize_row_type(row)
        _recalc_cost_buckets(row)
        touched += 1
        if dry_run:
            continue
        set_clause = ", ".join(f'"{c}" = ${i+2}' for i, c in enumerate(_NORMALIZED_COLUMNS))
        await conn.execute(
            f"""UPDATE cost_calc_version_rows SET {set_clause} WHERE id=$1""",
            row["id"], *[row.get(c) for c in _NORMALIZED_COLUMNS],
        )
    return touched


async def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--dry-run", action="store_true", help="только показать, что будет изменено, без записи")
    args = parser.parse_args()

    await db.init_pool()
    try:
        async with db.pool().acquire() as conn:
            versions = await conn.fetch(
                """SELECT id, model, articul, plan_id, status FROM cost_calc_versions
                   WHERE status IN ('draft', 'pending')
                   ORDER BY id"""
            )
            print(f"Найдено {len(versions)} draft/pending версий")
            total_rows = 0
            touched_versions = 0
            for v in versions:
                async with conn.transaction():
                    n = await _migrate_version(conn, v["id"], args.dry_run)
                    # pending-версии уже скопированы в cost_data_cache при submit_version —
                    # обновляем и эту копию, иначе основная таблица останется со старыми значениями.
                    if n and not args.dry_run and v["status"] == "pending":
                        await _apply_version_rows_to_cache(conn, v["id"])
                if n:
                    touched_versions += 1
                    total_rows += n
                    print(f"  version {v['id']} ({v['model']}/{v['articul']}/{v['plan_id']}): {n} строк нормализовано")
            prefix = "(dry-run) " if args.dry_run else ""
            print(f"{prefix}Затронуто версий: {touched_versions}, строк нормализовано: {total_rows}")
    finally:
        await db.close_pool()


if __name__ == "__main__":
    asyncio.run(main())
