"""Сбор данных для выгрузки «1:1 как в деке».

Модуль — единственная граница между движком рендера и данными: биндеры НЕ
ходят в SQL и не знают про витрины, они получают готовый payload отсюда.
Источник — те же сборщики, что у дашборда (`app.fot.dashboard`) и у «Разбора
ИИ» (`fot_insights._fot_dashboard`), поэтому числа выгрузки совпадают с тем,
что видит пользователь на экране, включая mock-режим (COST_MOCK/FOT_MOCK).
"""

from __future__ import annotations

import asyncio
import logging
import os

from app import fot_insights
from app.logship import log

#: Вкладки, которые выгрузка умеет собирать «1:1» (дека размечена по листам
#: 1–3, 4–6, 7, 8 и 23–27). Расширение — новая запись в registry.SHEETS.
SUPPORTED_TABS = ("b1", "p14", "p15", "p16", "p17", "b3")

#: Сколько payload'ов вкладок собирать ОДНОВРЕМЕННО. Пул asyncpg раздела —
#: max_size=8 (db.init_pool), и соединение занято всё время SQL вкладки, так
#: что 6 оставляет два соединения живому дашборду и сохранению цен: это
#: ГИГИЕНА ПУЛА, а не найденный предел — живой замер выдержал и 12, и 20
#: (ошибок asyncpg не было), но при 8 задачах весь пул держит ОДНА выгрузка,
#: и запрос пользователя ловит POOL_ACQUIRE_TIMEOUT (15 с).
#: Замер полной выгрузки: 2 → 36.8 с, 4 → 25.9, 6 → 22.8, 8 → 22.1. Дальше
#: время определяется волнами зависимостей, а не шириной семафора.
COLLECT_CONCURRENCY = int(os.environ.get("FOT_DECK_COLLECT_CONCURRENCY", "6"))

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
    params = _prepare(tab, filters)
    data = await fot_insights._fot_dashboard(tab, params)
    log(logging.INFO, "fot_deck: данные вкладки собраны", tab=tab,
        cards=len(data.get("cards") or []),
        depts=len(data.get("cards_by_department") or []),
        slides=len(data.get("slides") or []),
        details=len(data.get("country_details") or {}))
    return data


def _prepare(tab: str, filters: dict) -> dict:
    """Срезы запроса → параметры сборщика вкладки (до образа к SQL).

    Вынесено из `collect`, потому что по ЭТИМ параметрам считается ключ
    дедупликации: два разных набора срезов могут дать один и тот же запрос
    (например, `country` строкой и списком из одного элемента), и повторно
    собирать его незачем.
    """
    params = fot_insights._params_from_context(filters or {})
    _ensure_half(params, filters)
    _ensure_b3_local(params, filters)
    if tab in COUNTRY_SLICED:
        _ensure_single_country(tab, params, filters)
    return params


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


def b3_countries(summary: dict) -> list[str]:
    """Страны присутствия одного подразделения (карточки его свода)."""
    return [str(c.get("cc") or "") for c in summary.get("cards_by_country") or []]


def _filters_key(tab: str, filters: dict) -> tuple:
    """Ключ дедупликации: вкладка + срезы запроса в устойчивом виде.

    Срезы, а не параметры сборщика: `collect` дёргает `_params_from_context`
    на каждом вызове, и второй прогон того же разбора писал бы в лог
    повторные строки. Значения приводятся к строкам (фронт присылает год
    числом, `country` — строкой или списком), пустые отбрасываются, списки
    сортируются: `{"country": ["BY"]}` и `{"country": "BY"}` — один запрос.
    """
    flat = []
    for key, value in (filters or {}).items():
        if value in (None, "", [], ()):
            continue
        items = value if isinstance(value, (list, tuple)) else [value]
        # дубли внутри списка снимаем: ["BY", "BY"] — тот же срез, что ["BY"]
        # (фронт иногда присылает страну дважды при склейке селекторов)
        items = sorted({str(v).strip() for v in items if str(v).strip()})
        if items:
            flat.append((str(key), tuple(items)))
    return (tab, tuple(sorted(flat)))


class Collector:
    """Параллельный сбор payload'ов выгрузки: общий семафор + дедупликация.

    Варианты листов деки — это ДЕСЯТКИ независимых запросов к витринам
    (полная выгрузка: p14 по 4 странам, p15/p16 по 4, блок 3 — свод на каждое
    из 19 подразделений плюс статьи каждой его страны). Последовательно они
    складывались линейно и давали ~200 с на живых витринах — за пределы
    клиентского таймаута. Здесь все они запускаются задачами, а ожидание
    собрано в пачки по волнам зависимостей (см. engine.export_deck), поэтому
    время выгрузки ≈ сумма по волнам, а не по запросам.

    Одинаковые срезы дедуплицируются в рамках ОДНОГО запроса: повторный
    ключ возвращает ту же задачу, а не второй поход в SQL. Биндеры payload не
    меняют (`_only` копирует словарь), поэтому делиться результатом безопасно.
    """

    def __init__(self) -> None:
        self._tasks: dict[tuple, asyncio.Task] = {}
        self._sem = asyncio.Semaphore(COLLECT_CONCURRENCY)
        self.jobs = 0        # сколько задач реально запущено
        self.reused = 0      # сколько раз ключ переиспользован

    def payload(self, tab: str, filters: dict) -> asyncio.Task:
        """Задача «payload вкладки по этим срезам» (или уже готовая по ключу)."""
        key = _filters_key(tab, filters)
        return self._task(key, lambda: collect(tab, filters))

    def payload_country(self, tab: str, filters: dict, cc: str) -> asyncio.Task:
        """Задача «лист страны»: ключ — те же срезы с добавленной страной."""
        key = _filters_key(tab, dict(filters or {}, country=[cc]))
        return self._task(key, lambda: collect_country(tab, filters, cc))

    def _task(self, key: tuple, factory) -> asyncio.Task:
        task = self._tasks.get(key)
        if task is not None:
            self.reused += 1
            log(logging.INFO, "fot_deck: повторный срез переиспользован",
                tab=key[0], filters=dict(key[1]))
            return task
        self.jobs += 1
        task = asyncio.ensure_future(self._run(factory))
        self._tasks[key] = task
        return task

    async def _run(self, factory):
        """Один поход в витрины под общим семафором (пул БД не вычерпываем)."""
        async with self._sem:
            return await factory()


async def settle(tasks) -> None:
    """Дожидается пачки задач; любая упавшая валит ВСЮ выгрузку (fail-closed).

    Ждём до ПЕРВОГО сбоя, а не до конца пачки: остальные задачи отменяются, и
    пользователь получает ошибку сразу, а не через минуту ожидания остальных
    срезов. Сбой выбирается по порядку запуска (порядок деки), поэтому тип
    ошибки детерминирован: EmptySlice → 422, RuntimeError → 503. Частичный
    pptx не отдаётся — исключение поднимается до первого клона слайда.
    """
    tasks = [t for t in dict.fromkeys(tasks) if t is not None]
    if not tasks:
        return
    done, pending = await asyncio.wait(tasks, return_when=asyncio.FIRST_EXCEPTION)
    if pending:
        for task in pending:
            task.cancel()
        # отменённые надо погасить здесь же: иначе их CancelledError всплывёт
        # «Task exception was never retrieved» уже после ответа пользователю
        await asyncio.gather(*pending, return_exceptions=True)
    for task in tasks:
        if task.cancelled():
            continue
        error = task.exception()
        if error is not None:
            raise error


__all__ = ["COLLECT_CONCURRENCY", "COUNTRY_ORDER", "COUNTRY_SLICED",
           "SUPPORTED_TABS", "Collector", "UnsupportedTab", "b3_countries",
           "b3_depts", "b3_direction", "collect", "collect_country", "has_rows",
           "settle"]
