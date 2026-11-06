"""Реестр листов выгрузки «1:1 как в деке» (задача 663620, фаза 1 — p17).

Декларативная карта: вкладка → прототипы актива в порядке слайдов выгрузки.
Порядок записей = порядок слайдов в pptx; вариант(ы) прототипа (например,
страны для одного и того же листа «по подразделениям») задаёт функция
`variants` — движок клонирует прототип на каждый вариант.

Новая вкладка добавляется одной записью здесь плюс биндером; движок, формат и
работа с XML от вкладки не зависят.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Callable

from app.fot_deck_export.binders import p17


@dataclass(frozen=True)
class SlideSpec:
    """Прототип актива и способ его заполнения.

    proto    — имя прототипа в активе (cSld/@name);
    binder   — (ctx, данные, вариант) → заполняет слайд, может добавить
               продолжения через ctx.continuation();
    variants — данные вкладки → [(вариант, данные_варианта), …]; None — один
               слайд на прототип; пустой список — слайд не нужен (нет данных).
    """

    proto: str
    binder: Callable
    variants: Callable | None = None


@dataclass(frozen=True)
class SheetSpec:
    """Вкладка выгрузки: список прототипов в порядке слайдов."""

    tab: str
    slides: tuple[SlideSpec, ...]


SHEETS: tuple[SheetSpec, ...] = (
    SheetSpec("p17", (
        # Свод по группе, затем по странам — как листы деки 23→24→25→26→27.
        SlideSpec("hc_summary", p17.bind_summary),
        SlideSpec("hc_country_by", p17.bind_country,
                  variants=p17.country_variants("BY")),
        SlideSpec("hc_country_ru", p17.bind_country,
                  variants=p17.country_variants("RU")),
        SlideSpec("hc_country_kz", p17.bind_country,
                  variants=p17.country_variants("KZ")),
        SlideSpec("hc_country_uz", p17.bind_country,
                  variants=p17.country_variants("UZ")),
    )),
)

SHEETS_BY_TAB = {sheet.tab: sheet for sheet in SHEETS}

SUPPORTED_TABS = tuple(sheet.tab for sheet in SHEETS)


def sheet_for(tab: str) -> SheetSpec:
    """SheetSpec вкладки; неизвестная — KeyError (наверху → 400)."""
    return SHEETS_BY_TAB[tab]


__all__ = ["SHEETS", "SHEETS_BY_TAB", "SUPPORTED_TABS", "SheetSpec", "SlideSpec",
           "sheet_for"]
