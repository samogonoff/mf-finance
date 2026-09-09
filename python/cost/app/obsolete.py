"""
Статус «Неактуальная модель/артикул» (просьба заказчика 09.09.2026).

Калькулятор или ПЭО отмечают, что изделие больше не актуально. В таблице такие
строки подсвечены серым, в колонке «Этап» стоит ⚫, в фильтре «Статус
согласования» есть отдельное значение. Это состояние изделия, а не калькуляции,
поэтому ключ — модель + артикул (как у карточки 713): неактуальный артикул
неактуален во всех своих калькуляциях, планах и заданиях.

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

ObsoleteKey = tuple[str, str]

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


def make_key(model: Any, articul: Any) -> ObsoleteKey:
    return (_s(model), _s(articul))


async def flags_for(keys: list[ObsoleteKey]) -> dict[ObsoleteKey, dict[str, Any]]:
    """Статусы по переданным парам модель+артикул — для наложения на строки."""
    uniq = list({k for k in keys if k[0] and k[1]})
    if not uniq:
        return {}
    async with acquire() as conn:
        rows = await conn.fetch(
            """SELECT model, articul, obsolete, comment, set_by, set_at
                 FROM cost_obsolete
                WHERE (model, articul) IN (SELECT * FROM unnest($1::text[], $2::text[]))""",
            [k[0] for k in uniq], [k[1] for k in uniq],
        )
    out: dict[ObsoleteKey, dict[str, Any]] = {}
    for r in rows:
        out[(r["model"], r["articul"])] = {
            "obsolete": bool(r["obsolete"]),
            "obsolete_comment": r["comment"] or "",
            "obsolete_set_by": r["set_by"],
            "obsolete_set_at": r["set_at"].isoformat() if r["set_at"] else None,
        }
    return out


def apply_flags(rows: list[dict[str, Any]], flags: dict[ObsoleteKey, dict[str, Any]]) -> None:
    for row in rows:
        f = flags.get(make_key(row.get("Модель"), row.get("Артикул")))
        row.update(f if f else EMPTY)


async def set_flag(key: ObsoleteKey, *, obsolete: bool, comment: str, set_by: str) -> dict[str, Any]:
    """Поставить или снять статус. Возвращает поля строки, как в /aggregated."""
    model, articul = key
    async with acquire() as conn:
        row = await conn.fetchrow(
            """INSERT INTO cost_obsolete (model, articul, obsolete, comment, set_by)
               VALUES ($1, $2, $3, $4, $5)
               ON CONFLICT ON CONSTRAINT uq_obsolete_key DO UPDATE
                  SET obsolete = EXCLUDED.obsolete,
                      comment = EXCLUDED.comment,
                      set_by = EXCLUDED.set_by,
                      set_at = now(),
                      updated_at = now()
               RETURNING set_at""",
            model, articul, obsolete, comment or None, set_by,
        )
    log(
        logging.INFO, "статус неактуальности изделия",
        model=model, articul=articul, obsolete=obsolete, comment=comment or None, user=set_by,
    )
    return {
        "model": model,
        "articul": articul,
        "obsolete": obsolete,
        "obsolete_comment": comment or "",
        "obsolete_set_by": set_by,
        "obsolete_set_at": (row["set_at"] if row else datetime.now(timezone.utc)).isoformat(),
    }
