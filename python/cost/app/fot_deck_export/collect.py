"""Сбор данных для выгрузки «1:1 как в деке».

Модуль — единственная граница между движком рендера и данными: биндеры НЕ
ходят в SQL и не знают про витрины, они получают готовый payload отсюда.
Источник — те же сборщики, что у дашборда (`app.fot.dashboard`) и у «Разбора
ИИ» (`fot_insights._fot_dashboard`), поэтому числа выгрузки совпадают с тем,
что видит пользователь на экране, включая mock-режим (COST_MOCK/FOT_MOCK).
"""

from __future__ import annotations

import logging

from app import fot_insights
from app.logship import log

#: Вкладки, которые выгрузка умеет собирать «1:1» (дека размечена по листам
#: 1–3, 4–6, 7, 8 и 23–27). Расширение — новая запись в registry.SHEETS.
SUPPORTED_TABS = ("b1", "p14", "p15", "p16", "p17", "b3")

#: Вкладки, у которых срез «страна» сужает ВСЮ выгрузку: на листах 4–8 дека
#: показывает одну страну на лист, поэтому вариант строится по срезу, а не по
#: перечню стран из ответа.
COUNTRY_SLICED = frozenset({"p14", "p15", "p16"})

#: Страны присутствия в порядке деки — из них строятся варианты листов 7/8,
#: когда пользователь не сузил срез одной страной.
COUNTRY_ORDER = ("BY", "RU", "KZ", "UZ")


class UnsupportedTab(ValueError):
    """Вкладка не поддержана выгрузкой-декой (роут отдаёт 400)."""

    def __init__(self, tab: str, supported: tuple[str, ...] = SUPPORTED_TABS) -> None:
        super().__init__(
            "fot_deck: вкладка %r не поддержана выгрузкой «1:1»; доступны: %s"
            % (tab, ", ".join(supported)))
        self.tab = tab
        self.supported = supported


async def collect(tab: str, filters: dict) -> dict:
    """Payload вкладки для биндеров (форма — как у GET /fot/dashboard).

    Срезы приходят тем же словарём, что и в теле запроса выгрузки; разбор в
    параметры сборщика делает fot_insights._params_from_context (он же
    применяет mock/live-ветвление).
    """
    if tab not in SUPPORTED_TABS:
        raise UnsupportedTab(tab)
    params = fot_insights._params_from_context(filters or {})
    _ensure_half(params, filters)
    _ensure_b3_local(params, filters)
    if tab in COUNTRY_SLICED:
        _ensure_single_country(tab, params, filters)
    data = await fot_insights._fot_dashboard(tab, params)
    log(logging.INFO, "fot_deck: данные вкладки собраны", tab=tab,
        cards=len(data.get("cards") or []),
        depts=len(data.get("cards_by_department") or []),
        slides=len(data.get("slides") or []),
        details=len(data.get("country_details") or {}))
    return data


def _ensure_half(params: dict, filters: dict) -> None:
    """Переносит срез «half» в параметры сборщика.

    «half» не входит в _SCALAR_PARAM_KEYS (это срез панели, а не контекста
    ИИ-разбора), но для деки он обязателен: от него зависят заголовки слайдов
    и месяц факта. Без него сборщик уходит в дефолт «2 ПГ» и выгрузка за 1 ПГ
    подписана как 2 ПГ. Пустое значение — дефолт сборщика.
    """
    if params.get("half") in (None, ""):
        half = (filters or {}).get("half")
        if half not in (None, ""):
            params["half"] = half


def _ensure_b3_local(params: dict, filters: dict) -> None:
    """Переносит локальные селекторы вкладки b3 (`b3_dept`/`b3_country`).

    У блока 3 свои селекторы: подразделение (`b3_dept`) заменяет глобальный
    срез «Направление», страна (`b3_country`) открывает вид статей. В
    `_SCALAR_PARAM_KEYS` ИИ-разбора их нет — это параметры ВКЛАДКИ, а не
    контекста, — поэтому выгрузка передаёт их явно: без этого биндер получал
    дефолт сборщика (первое из dept_options) и выгрузка одного подразделения
    молча превращалась в выгрузку другого.
    """
    for key in ("b3_dept", "b3_country"):
        if params.get(key) in (None, ""):
            value = (filters or {}).get(key)
            if value not in (None, ""):
                params[key] = str(value).strip()


def _ensure_single_country(tab: str, params: dict, filters: dict) -> None:
    """Сужает срез листов 7/8 до ОДНОЙ страны — иначе вариантов не построить.

    Дека показывает «Отделы» по стране, а сборщик при нескольких странах
    отдаёт свод группы без `meta.slide.cc`, и лист остался бы без шапки.
    Берём первую страну среза в порядке запроса.

    Ключ среза — `country` (так его зовёт /fot/dashboard и кладёт фронт);
    `countries` — внутреннее имя параметра сборщика. Принимаем оба, иначе
    фильтр страны молча игнорировался и «срез BY» возвращал группу.
    """
    countries = params.get("countries") or []
    if len(countries) <= 1:
        return
    params["countries"] = countries[:1]
    log(logging.INFO, "fot_deck: срез стран сужен до одной для листа отделов",
        tab=tab, was=",".join(countries), now=params["countries"][0])


async def collect_country(tab: str, filters: dict, cc: str) -> dict:
    """Payload листа по ОДНОЙ стране: срез страны добавляется к фильтрам.

    Декa печатает листы 4–8 постранично по странам присутствия, а сборщик без
    среза отдаёт свод группы (`meta.slide.cc` пуст). Поэтому вариант страны
    собирается отдельным запросом: `country=[cc]` — тот же ключ среза, что у
    /fot/dashboard. Страны без единой строки дают пустой payload, и движок
    пропускает прототип (см. `has_rows`).
    """
    scoped = dict(filters or {})
    scoped["country"] = [cc]
    data = await collect(tab, scoped)
    log(logging.INFO, "fot_deck: данные листа по стране", tab=tab, cc=cc,
        depts=len(data.get("cards_by_department") or []),
        directions=len(data.get("directions") or []))
    return data


def has_rows(tab: str, payload: dict) -> bool:
    """Есть ли в payload'е страны данные для её листа (иначе лист пропускается).

    Признак СВОЙ у каждой вкладки: у листов 7/8 это отделы страны, у листов
    4–6 — направления свода. Пустой лист дека не печатает, и выгрузка тоже не
    должна: у страны без строк иначе появилась бы страница с «н/д».
    """
    if tab == "p14":
        return bool(payload.get("directions") or [])
    return bool(payload.get("cards_by_department") or [])


def b3_direction(filters: dict) -> str:
    """Направление, которым сужен срез «Направление» панели (или "").

    У блока 3 глобальный срез «Направление» на САМ дашборд не действует — его
    заменяет селектор вкладки (METHODOLOGY_B3). Но для ВЫГРУЗКИ он остаётся
    правилом отбора: пользователь, сузив панель до одного направления, ждёт
    один свод, а не девятнадцать. Это правило выгрузки, а не серверного среза,
    поэтому оно живёт здесь, а не в сборщике вкладки.
    """
    value = (filters or {}).get("direction")
    if isinstance(value, str):
        return value.strip()
    if isinstance(value, (list, tuple)):
        items = [str(v).strip() for v in value if str(v).strip()]
        return items[0] if len(items) == 1 else ""
    return ""


def b3_depts(payload: dict, filters: dict) -> list[str]:
    """Подразделения, под которые строятся варианты выгрузки блока 3.

    Пустой фильтр направления — ВСЕ подразделения сборщика (`dept_options`:
    направления, присутствующие в ≥2 странах; живьём их 19). Один вариант
    выгрузки = свод этого подразделения плюс лист статей на каждую его страну,
    поэтому у полной выгрузки блок 3 даёт десятки слайдов — это осознанно:
    пользователь просит «все листы», и декa тоже печатает по слайду на срез.
    """
    options = [str(o.get("dept") or "") for o in
               (payload.get("meta") or {}).get("dept_options") or []]
    options = [o for o in options if o]
    chosen = b3_direction(filters)
    if chosen:
        if chosen not in options:
            # направление вне «≥2 стран»: у него нет ни карточек, ни статей —
            # лист не строится, и это не ошибка пользователя
            return []
        return [chosen]
    return options


__all__ = ["COUNTRY_ORDER", "COUNTRY_SLICED", "SUPPORTED_TABS", "UnsupportedTab",
           "b3_depts", "b3_direction", "collect", "collect_country", "has_rows"]
