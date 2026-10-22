# -*- coding: utf-8 -*-
"""Правила странового знаменателя доли ФОТ (задача 663620, этап 0).

Раньше ветки знаменателя были зашиты в SQL: CTE den из fot._halfyear_sql
собирал 15 колонок dn_<ветка>_<rv-период> по константе fot._DEN_BRANCHES —
порт скрытой меры PBI «_Выручка для доли ФОТ». Теперь выручка лежит мелким
зерном (CTE den_grain: ctry × kzf × chan × dir × g1 + rv-периоды), а состав
каждой ветки описывают ЗДЕСЬ чистые данные — предикаты над ячейкой зерна.

Ячейка — словарь {ctry, kzf, chan, dir, g1} + rv-периоды (нормализация —
fot._den_cells): ctry — m.country_code витрины, kzf — m.kz_denominator
(продажи KZ по кодам 1006/1012), chan/dir/g1 — sales_channel/direction/
group_cfo1 справочника fot_cfo.

Этап 0 — рефакторинг без изменения поведения: LEGACY_DEN_RULES повторяют
старое правило 1:1 (страна одна решает ветку, направление/группа ЦФО не
участвуют). Этап 1 заменит их на правила страна × направление → канал
(заполнятся и methodology_text/validate по живым значениям витрины).
"""
from __future__ import annotations

# Цели веток — ровно условия прежнего _DEN_BRANCHES: grp — вся выручка группы
# (BY/RU/микс стран, включая строки вне казахстанского флага), kz — Казахстан
# по флагу kz_denominator, uz — вся выручка UZ.
TARGET_GRP = ({},)                                   # все ячейки
TARGET_KZ = ({"ctry": "KZ", "kzf": True},)
TARGET_UZ = ({"ctry": "UZ"},)

TARGETS = {"grp": TARGET_GRP, "kz": TARGET_KZ, "uz": TARGET_UZ}

# Ключ правила: (страна, направление | группа ЦФО); None — любое значение
# (wildcard). Цель — имя из TARGETS. Этап 0: направление/группа не участвуют,
# точная страна приоритетнее wildcard (порядок словаря не важен: точные ключи
# не пересекаются с (None, None)).
LEGACY_DEN_RULES = {
    ("KZ", None): "kz",
    ("UZ", None): "uz",
    (None, None): "grp",   # BY, RU, смеси стран и пустая страна
}

DEN_RULES = LEGACY_DEN_RULES


def matching_rule(ctry: str | None, direction: str | None = None,
                  group_cfo1: str | None = None) -> tuple:
    """(ключ правила, имя цели) для ячейки — то же сравнение, что в resolve:
    правило подходит, если его страна совпала (или wildcard), а измерение —
    направление либо группа ЦФО ячейки (или wildcard)."""
    for key, name in DEN_RULES.items():
        r_ctry, r_dim = key
        if r_ctry is not None and r_ctry != (ctry or ""):
            continue
        if r_dim is not None and r_dim not in (direction, group_cfo1):
            continue
        return key, name
    return (None, None), "grp"


def resolve(ctry: str | None, direction: str | None = None,
            group_cfo1: str | None = None) -> tuple:
    """Цель знаменателя для (страна, направление, группа ЦФО) — кортеж
    предикатов над ячейкой den_grain (см. TARGETS)."""
    return TARGETS[matching_rule(ctry, direction, group_cfo1)[1]]


def validate(known_values: dict) -> list[str]:
    """Проблемы правил против известных значений измерений витрины (ключи
    countries / directions / groups — фактические DISTINCT из БД); пустой
    список = ок. Ловит опечатки в правилах (значения вне витрины), цели вне
    TARGETS и страны без покрывающего правила. На LEGACY-правилах всегда
    пусто: wildcard (None, None) покрывает всё."""
    problems: list[str] = []
    countries = set(known_values.get("countries") or [])
    known_dims = set(known_values.get("directions") or []) | set(known_values.get("groups") or [])
    has_wildcard = any(c is None for (c, _dim) in DEN_RULES)
    for (ctry, dim), name in DEN_RULES.items():
        if name not in TARGETS:
            problems.append("правило %r: цель %r вне TARGETS" % ((ctry, dim), name))
        if ctry is not None and countries and ctry not in countries:
            problems.append("правило %r: страны нет в витрине" % ((ctry, dim),))
        if dim is not None and known_dims and dim not in known_dims:
            problems.append("правило %r: направления/группы нет в витрине" % ((ctry, dim),))
    if not has_wildcard:
        for ctry in sorted(countries):
            if not any(c == ctry for (c, _dim) in DEN_RULES):
                problems.append("страна %r без правила (нет ни точного, ни wildcard)" % ctry)
    return problems


def methodology_text() -> str:
    """Текст правила для methodology ответов; на этапе 0 пусто (легаси уже
    описано в METHODOLOGY_P10/P11), заполнится на этапе 1."""
    return ""
