"""
Статус «Неактуальная модель/артикул» (просьба заказчика 09.09.2026; охват
сужен 26.10.2026 — задача Б24 668461).

Калькулятор или ПЭО отмечают, что калькуляция больше не актуальна. В таблице
такие строки подсвечены серым, в колонке «Этап» стоит ⚫, в фильтре «Статус
согласования» есть отдельное значение.

Сначала (0056) статус хранился на паре модель+артикул и ложился на все
калькуляции изделия. Заказчик уточнил (Журавская М.М., чат «Тестирование
ценообразования на WEB»): у одного плана бывает несколько выгрузок ПФКСС с
разными заданиями, для ценообразования берётся максимальная себестоимость, и
неактуальной надо помечать конкретную калькуляцию с меньшей себестоимостью —
при паре серым заливалась и «ценовая» строка. Поэтому ключ — модель + артикул
+ признак + план + задание (как у статуса ПЭО и отметки «нужна замена
артикула»); имя статуса в интерфейсе сохранено. Старые пары развёрнуты на
калькуляции кэша миграцией 0071.

Статус снимаемый теми же ролями (право `cost:obsolete`, миграция 0056 выдаёт
«Калькулятору», «ПЭО» и «Full Admin»). Хранится последнее состояние с автором и
временем; журнала изменений нет — задача его не требует.
"""
from __future__ import annotations

import logging
from datetime import datetime, timezone
from typing import Any

from app.db import acquire
from app.logship import log

# Ключ статуса — модель, артикул, признак, план, задание. Пустые части хранятся
# как '' (не NULL), чтобы UNIQUE работал как ключ (урок миграции 0023).
ObsoleteKey = tuple[str, str, str, str, str]

# Поля, которые получает каждая строка /aggregated. Без записи — obsolete=False,
# чтобы фронт не отличал «нет записи» от «снято».
EMPTY: dict[str, Any] = {
    "obsolete": False,
    "obsolete_comment": "",
    "obsolete_set_by": None,
    "obsolete_set_at": None,
}


def _s(v: Any) -> str:
    return str(v or "").strip()


def make_key(model: Any, articul: Any, calc_sign: Any, plan_id: Any, task_number: Any) -> ObsoleteKey:
    return (_s(model), _s(articul), _s(calc_sign), _s(plan_id), _s(task_number))


async def flags_for(keys: list[ObsoleteKey]) -> dict[ObsoleteKey, dict[str, Any]]:
    """Статусы по переданным ключам калькуляций — для наложения на строки."""
    uniq = list({k for k in keys if k[0] and k[1]})
    if not uniq:
        return {}
    async with acquire() as conn:
        rows = await conn.fetch(
            """SELECT model, articul, calc_sign, plan_id, task_number,
                      obsolete, comment, set_by, set_at
                 FROM cost_obsolete
                WHERE (model, articul, calc_sign, plan_id, task_number) IN (
                      SELECT * FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[])
                )""",
            [k[0] for k in uniq], [k[1] for k in uniq], [k[2] for k in uniq],
            [k[3] for k in uniq], [k[4] for k in uniq],
        )
    out: dict[ObsoleteKey, dict[str, Any]] = {}
    for r in rows:
        out[(r["model"], r["articul"], r["calc_sign"], r["plan_id"], r["task_number"])] = {
            "obsolete": bool(r["obsolete"]),
            "obsolete_comment": r["comment"] or "",
            "obsolete_set_by": r["set_by"],
            "obsolete_set_at": r["set_at"].isoformat() if r["set_at"] else None,
        }
    return out


def apply_flags(rows: list[dict[str, Any]], flags: dict[ObsoleteKey, dict[str, Any]]) -> None:
    for row in rows:
        f = flags.get(make_key(
            row.get("Модель"), row.get("Артикул"), row.get("Признак калькуляции"),
            row.get("PLAN_ID"), row.get("Номер задания производства"),
        ))
        row.update(f if f else EMPTY)


async def set_flag(key: ObsoleteKey, *, obsolete: bool, comment: str, set_by: str) -> dict[str, Any]:
    """Поставить или снять статус. Возвращает поля строки, как в /aggregated."""
    model, articul, calc_sign, plan_id, task_number = key
    async with acquire() as conn:
        row = await conn.fetchrow(
            """INSERT INTO cost_obsolete
                   (model, articul, calc_sign, plan_id, task_number, obsolete, comment, set_by)
               VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
               ON CONFLICT ON CONSTRAINT uq_obsolete_key DO UPDATE
                  SET obsolete = EXCLUDED.obsolete,
                      comment = EXCLUDED.comment,
                      set_by = EXCLUDED.set_by,
                      set_at = now(),
                      updated_at = now()
               RETURNING set_at""",
            model, articul, calc_sign, plan_id, task_number, obsolete, comment or None, set_by,
        )
    log(
        logging.INFO, "статус неактуальности изделия",
        model=model, articul=articul, calc_sign=calc_sign, plan_id=plan_id,
        task_number=task_number, obsolete=obsolete, comment=comment or None, user=set_by,
    )
    return {
        "model": model,
        "articul": articul,
        "calc_sign": calc_sign,
        "plan_id": plan_id,
        "task_number": task_number,
        "obsolete": obsolete,
        "obsolete_comment": comment or "",
        "obsolete_set_by": set_by,
        "obsolete_set_at": (row["set_at"] if row else datetime.now(timezone.utc)).isoformat(),
    }
