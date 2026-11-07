"""Реестр листов выгрузки «1:1 как в деке» (задача 663620).

Декларативная карта: вкладка → прототипы актива в порядке слайдов выгрузки.
Порядок записей = порядок слайдов в pptx, а порядок ВКЛАДОК — порядок деки:
b1 (листы 1–3) → p15 (7) → p16 (8) → p17 (23–27). Будущие блоки вставляются
в этот же кортеж на своё место в деке (p14 — между b1 и p15, b3 — после p17),
и полная выгрузка «все листы» соберётся в порядке деки без правок движка.

Вариант(ы) прототипа (например, страны одного и того же листа «по отделам»)
задаёт функция `variants` — движок клонирует прототип на каждый вариант.

Новая вкладка добавляется одной записью здесь плюс биндером; движок, формат и
работа с XML от вкладки не зависят.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Callable

from app.fot_deck_export.binders import b1, dept, p17


@dataclass(frozen=True)
class SlideSpec:
    """Прототип актива и способ его заполнения.

    proto    — имя прототипа в активе (cSld/@name);
    binder   — (ctx, данные, вариант) → заполняет слайд, может добавить
               продолжения через ctx.slide(n);
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
    # Блок 1 деки (листы 1–3): один и тот же блок тремя листами — год, 1 ПГ,
    # 2 ПГ. Это «варианты переключателя» отчёта, и дека печатает все три.
    SheetSpec("b1", (
        SlideSpec("b1_year", b1.bind_year),
        SlideSpec("b1_h1", b1.bind_h1),
        SlideSpec("b1_h2", b1.bind_h2),
    )),
    # Листы 7/8 деки: срез отделов ОДНОЙ страны на лист (у деки — BY). Срез
    # приходит уже отфильтрованным, вариант даёт только код страны для шапки.
    SheetSpec("p15", (
        SlideSpec("dept_summary", dept.bind_summary,
                  variants=dept.country_variants),
    )),
    SheetSpec("p16", (
        SlideSpec("dept_cards", dept.bind_cards,
                  variants=dept.country_variants),
    )),
    # Листы численности 23–27: свод по группе, затем по странам.
    SheetSpec("p17", (
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

#: Полный порядок листов «все листы»: порядок деки, он же порядок SHEETS.
ALL_TABS = SUPPORTED_TABS


def sheet_for(tab: str) -> SheetSpec:
    """SheetSpec вкладки; неизвестная — KeyError (наверху → 400)."""
    return SHEETS_BY_TAB[tab]


__all__ = ["ALL_TABS", "SHEETS", "SHEETS_BY_TAB", "SUPPORTED_TABS", "SheetSpec",
           "SlideSpec", "sheet_for"]
