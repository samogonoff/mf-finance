"""
Разовый пересчёт данных (не схемы): заполняет cost_factor_rub/usd в
cost_calc_version_rows и исправляет денежные бакеты уже сохранённых версий.

Зачем. До миграции 0031 db._recalc_cost_buckets перезаписывал стоимость статьи
произведением «Норма × цена материала». В источнике (Checks.CostHistory) стоимость
этому произведению НЕ равна: цена там money(19,4), т.е. сама округлена, а у части
строк стоимость меньше произведения в разы по доменным причинам (25-й процентиль
отношения стоимость/(норма×цена) — 0.1741, 5-й — 0.0002 при медиане 0.9988).
Из-за этого сохранение версии завышало себестоимость на таких строках.

С 0031 стоимость считается как Норма × цена × коэффициент, где коэффициент
берётся из источника при загрузке кэша. Но у УЖЕ сохранённых строк версий
коэффициента нет, и восстановить его из cost_data_cache нельзя: по ключам с
активными версиями кэш содержит строки самой версии, а не исходные данные.
Поэтому читаем источник напрямую.

Что делает:
  • для версий в статусах draft/pending/approved (archived/rejected не трогаем —
    это исторические записи) сопоставляет строки версии со строками источника
    по (задание, Наименование, артикул материала, свойство1..3);
  • считает коэффициент = стоимость_источника / (Норма_источника × цена_источника);
  • пишет коэффициент и пересчитывает бакет как Норма × цена × коэффициент,
    сохраняя ручные правки нормы и цены;
  • строки, добавленные пользователем вручную (двойника в источнике нет),
    оставляет как есть — коэффициент для них вывести не из чего;
  • операционные строки (пошив/раскрой/вязание) не трогает: там цены в источнике
    нет, а _derive_norm_price подобрал её так, что произведение уже точно равно
    стоимости, т.е. коэффициент 1.

Идемпотентен: повторный запуск даёт тот же результат.

После применения нужен полный рефреш кэша, чтобы исправленные версии
переналожились на данные источника:
    python -c "import asyncio;from app.db import *;asyncio.run(load_cost_data_to_cache())"

Запуск внутри контейнера python-cost (там есть и COST_DATABASE_URL, и MSSQL):
    python -m scripts.recalc_version_buckets --dry-run   # только показать
    python -m scripts.recalc_version_buckets             # применить
"""

from __future__ import annotations

import argparse
import asyncio
from collections import defaultdict
from decimal import Decimal, DivisionByZero, InvalidOperation

from app import db
from app.db import get_mssql_conn

# Статусы, которые влияют на текущие цифры либо станут ими при согласовании.
_TARGET_STATUSES = ("draft", "pending", "approved")

# Материальные типы источника. Операционные ('пошив', 'раскорой') намеренно не
# включены — см. docstring.
_SOURCE_MATERIAL_TYPES = (
    "себестоимость лиса осн",
    "себестоимость лиса всп",
    "шт",
    "Декоры лиса",
    "декор",
)

# Пары «денежная статья» → колонки, куда пишем пересчитанное значение.
_BUCKET_COLUMNS = (
    "Основные материалы", "Вспомогательные материалы", "Декоры",
    "Пошив", "Раскрой", "Вязание",
)


def _ident(task, name, art, s1, s2, s3) -> tuple:
    """Ключ сопоставления строки версии со строкой источника."""
    def n(v):
        return (str(v).strip() if v is not None else "")
    return (n(task), n(name), n(art), n(s1), n(s2), n(s3))


def _dec(v):
    if v is None:
        return None
    try:
        return Decimal(str(v))
    except (InvalidOperation, ValueError):
        return None


def _fetch_source_for_keys(keys: list[tuple[str, str, str, str]]) -> dict:
    """{(model, articul, calc_sign, plan_id): {ident: {...}}} из MSSQL.

    Берём строки САМОЙ СВЕЖЕЙ даты расчёта по каждому ключу — той же, что считает
    актуальной db._current_cache_date. Запрос батчим: лимит параметров ODBC ~2100,
    поэтому по 300 ключей (4 параметра на ключ) за раз.
    """
    result: dict = {}
    if not keys:
        return result
    conn = get_mssql_conn()
    conn.timeout = 300
    cur = conn.cursor()
    try:
        batch_size = 300
        for i in range(0, len(keys), batch_size):
            batch = keys[i:i + batch_size]
            cond = " OR ".join(
                "([Модель]=? AND [Артикул]=? AND [Признак калькуляции]=? AND [PLAN_ID]=?)"
                for _ in batch
            )
            params: list = []
            for m, a, cs, p in batch:
                params.extend([m, a, cs or "", p or ""])
            type_list = ", ".join("?" for _ in _SOURCE_MATERIAL_TYPES)
            params.extend(_SOURCE_MATERIAL_TYPES)
            cur.execute(
                f"""
                WITH src AS (
                    SELECT [Модель], [Артикул], [Признак калькуляции], [PLAN_ID],
                           [Номер задания производства], [Наименование],
                           [артикул материала], [свойство1], [свойство2], [свойство3],
                           [Норма], [цена материала, руб.], [цена материала, USD],
                           [Основные материалы, руб.], [Основные материалы, USD.],
                           [Вспомогательные материалы, руб.], [Вспомогательные материалы, USD.],
                           [Декоры, руб.], [Декоры, USD.],
                           [дата расчета],
                           MAX([дата расчета]) OVER (
                               PARTITION BY [Модель], [Артикул], [Признак калькуляции], [PLAN_ID]
                           ) AS max_date
                    FROM [Checks].[dbo].[CostHistory]
                    WHERE ({cond})
                      AND [Материал/техоперация/декор(признак)] IN ({type_list})
                )
                SELECT * FROM src WHERE [дата расчета] = max_date
                """,
                params,
            )
            cols = [d[0] for d in cur.description]
            for row in cur.fetchall():
                r = dict(zip(cols, row))
                key = (
                    str(r["Модель"]).strip(), str(r["Артикул"]).strip(),
                    str(r["Признак калькуляции"] or "").strip(), str(r["PLAN_ID"] or "").strip(),
                )
                ident = _ident(
                    r["Номер задания производства"], r["Наименование"],
                    r["артикул материала"], r["свойство1"], r["свойство2"], r["свойство3"],
                )
                bucket_rub = sum(
                    (_dec(r.get(f"{b}, руб.")) or Decimal(0))
                    for b in ("Основные материалы", "Вспомогательные материалы", "Декоры")
                )
                bucket_usd = sum(
                    (_dec(r.get(f"{b}, USD.")) or Decimal(0))
                    for b in ("Основные материалы", "Вспомогательные материалы", "Декоры")
                )
                # Один ident может встретиться несколько раз (тот же материал
                # использован в калькуляции дважды). Собираем всех кандидатов и
                # разрешаем ниже: неоднозначность идентификатора не мешает, если
                # коэффициент у кандидатов совпадает.
                result.setdefault(key, {}).setdefault(ident, []).append({
                    "norm": _dec(r["Норма"]),
                    "price_rub": _dec(r["цена материала, руб."]),
                    "price_usd": _dec(r["цена материала, USD"]),
                    "bucket_rub": bucket_rub,
                    "bucket_usd": bucket_usd,
                })
    finally:
        conn.close()
    return {k: {i: _resolve_candidates(c) for i, c in idents.items()} for k, idents in result.items()}


def _agree(values: list) -> bool:
    """Все ли коэффициенты кандидатов практически совпадают.

    Сравниваем с округлением до 10 знаков: коэффициент — результат деления, и
    несущественная разница в последних разрядах не должна считаться конфликтом.
    """
    present = [v for v in values if v is not None]
    if not present:
        return True
    if len(present) != len(values):
        return False  # часть кандидатов даёт коэффициент, часть нет — конфликт
    rounded = {v.quantize(Decimal("1.0000000000")) for v in present}
    return len(rounded) == 1


def _resolve_candidates(candidates: list) -> dict:
    """Сводит кандидатов источника к одному набору коэффициентов.

    Если у всех кандидатов коэффициент один и тот же — берём его, сколько бы
    строк ни совпало по идентификатору. Помечаем как неоднозначное только когда
    коэффициенты действительно расходятся: тогда неизвестно, к какой строке
    источника относится строка версии, и молча выбирать одну из них нельзя.
    """
    f_rub = [_factor(c["bucket_rub"], c["norm"], c["price_rub"]) for c in candidates]
    f_usd = [_factor(c["bucket_usd"], c["norm"], c["price_usd"]) for c in candidates]
    ok = _agree(f_rub) and _agree(f_usd)
    first = candidates[0]
    return {
        **first,
        "factor_rub": next((v for v in f_rub if v is not None), None),
        "factor_usd": next((v for v in f_usd if v is not None), None),
        "_ambiguous": not ok,
        "_candidates": len(candidates),
    }


def _factor(bucket, norm, price):
    if bucket is None or norm in (None, 0) or price in (None, 0):
        return None
    try:
        return bucket / (norm * price)
    except (DivisionByZero, InvalidOperation):
        return None


async def main(dry_run: bool) -> None:
    await db.init_pool()
    try:
        async with db.pool().acquire() as conn:
            versions = await conn.fetch(
                """SELECT id, model, articul, calc_sign, plan_id, task_number, status
                   FROM cost_calc_versions
                   WHERE status = ANY($1::text[])
                   ORDER BY id""",
                list(_TARGET_STATUSES),
            )
            print(f"версий к обработке: {len(versions)}")
            if not versions:
                return

            keys = sorted({
                (v["model"].strip(), v["articul"].strip(),
                 (v["calc_sign"] or "").strip(), (v["plan_id"] or "").strip())
                for v in versions
            })
            print(f"уникальных ключей калькуляций: {len(keys)}")
            loop = asyncio.get_running_loop()
            source = await loop.run_in_executor(None, _fetch_source_for_keys, keys)
            print(f"ключей найдено в источнике: {len(source)}")

            stats = defaultdict(int)
            money_before = Decimal(0)
            money_after = Decimal(0)
            per_version_delta: dict[int, Decimal] = defaultdict(Decimal)

            for v in versions:
                key = (v["model"].strip(), v["articul"].strip(),
                       (v["calc_sign"] or "").strip(), (v["plan_id"] or "").strip())
                src_map = source.get(key, {})
                rows = await conn.fetch(
                    """SELECT id, "Номер задания производства" AS task, "Наименование" AS name,
                              "артикул материала" AS art, "свойство1" AS s1,
                              "свойство2" AS s2, "свойство3" AS s3,
                              "Материал/операция/декор(призн)" AS mat_type,
                              "Норма" AS norm,
                              "цена материала, руб." AS price_rub,
                              "цена материала, USD." AS price_usd,
                              cost_factor_rub, cost_factor_usd,
                              "Основные материалы, руб." AS osn_rub,
                              "Вспомогательные материалы, руб." AS vsp_rub,
                              "Декоры, руб." AS dec_rub,
                              "Пошив, руб." AS posh_rub,
                              "Раскрой, руб." AS rask_rub,
                              "Вязание, руб." AS vyaz_rub
                       FROM cost_calc_version_rows WHERE version_id = $1
                       ORDER BY sort_order, id""",
                    v["id"],
                )
                for r in rows:
                    stats["строк всего"] += 1
                    target = db._BUCKET_BY_MAT_TYPE.get((r["mat_type"] or "").strip())
                    if target is None:
                        stats["строк без денежной статьи (пропуск)"] += 1
                        continue
                    if target in ("Пошив", "Раскрой", "Вязание"):
                        stats["операционных строк (пропуск)"] += 1
                        continue

                    ident = _ident(r["task"], r["name"], r["art"], r["s1"], r["s2"], r["s3"])
                    src = src_map.get(ident)
                    if src is None:
                        stats["двойника в источнике нет (пропуск)"] += 1
                        continue
                    if src["_ambiguous"]:
                        stats["неоднозначное сопоставление (пропуск)"] += 1
                        continue

                    f_rub = src["factor_rub"]
                    f_usd = src["factor_usd"]
                    if f_rub is None and f_usd is None:
                        stats["коэффициент не выводится (пропуск)"] += 1
                        continue

                    norm = _dec(r["norm"])
                    p_rub = _dec(r["price_rub"])
                    p_usd = _dec(r["price_usd"])
                    new_rub = (norm * p_rub * f_rub) if (norm is not None and p_rub is not None and f_rub is not None) else None
                    new_usd = (norm * p_usd * f_usd) if (norm is not None and p_usd is not None and f_usd is not None) else None

                    old_rub = _dec(r["osn_rub"]) or Decimal(0)
                    old_rub += _dec(r["vsp_rub"]) or Decimal(0)
                    old_rub += _dec(r["dec_rub"]) or Decimal(0)
                    if new_rub is not None:
                        money_before += old_rub
                        money_after += new_rub
                        per_version_delta[v["id"]] += new_rub - old_rub

                    stats["исправлено строк"] += 1
                    if dry_run:
                        continue

                    # Обнуляем прочие управляемые статьи ровно как _recalc_cost_buckets,
                    # чтобы SUM() по строкам не двоил стоимость. Плейсхолдеры и
                    # параметры собираем вместе — иначе номера разъезжаются, когда
                    # часть значений не пишется.
                    sets = [f'"{b}, руб." = 0, "{b}, USD." = 0' for b in _BUCKET_COLUMNS if b != target]
                    params: list = [r["id"]]

                    def _add(expr: str, value) -> None:
                        params.append(value)
                        sets.append(f"{expr} = ${len(params)}")

                    _add("cost_factor_rub", f_rub)
                    _add("cost_factor_usd", f_usd)
                    if new_rub is not None:
                        _add(f'"{target}, руб."', new_rub)
                    if new_usd is not None:
                        _add(f'"{target}, USD."', new_usd)
                    await conn.execute(
                        f"UPDATE cost_calc_version_rows SET {', '.join(sets)} WHERE id = $1",
                        *params,
                    )

            print("\n--- итоги ---")
            for k in sorted(stats):
                print(f"  {k}: {stats[k]}")
            print(f"\n  стоимость материалов до : {money_before}")
            print(f"  стоимость материалов после: {money_after}")
            print(f"  изменение               : {money_after - money_before:+}")

            moved = {k: d for k, d in per_version_delta.items() if abs(d) > Decimal("0.01")}
            if moved:
                print(f"\n  версий с изменением больше копейки: {len(moved)}")
                for vid, d in sorted(moved.items(), key=lambda kv: abs(kv[1]), reverse=True)[:15]:
                    v = next(x for x in versions if x["id"] == vid)
                    print(f"    версия {vid} ({v['model']}/{v['articul']}/{v['calc_sign']}/"
                          f"план {v['plan_id']}/задание {v['task_number'] or '—'}, {v['status']}): {d:+}")

            if dry_run:
                print("\nDRY-RUN: изменения НЕ применены")
            else:
                print("\nПрименено. Нужен полный рефреш кэша, чтобы версии переналожились.")
    finally:
        await db.close_pool()


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("--dry-run", action="store_true", help="только показать, ничего не менять")
    args = ap.parse_args()
    asyncio.run(main(args.dry_run))
