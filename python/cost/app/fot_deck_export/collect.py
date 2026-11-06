"""Сбор данных для выгрузки «1:1 как в деке» (вкладка p17).

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

#: Вкладки, которые фаза 1 умеет выгружать «1:1» (дека пока размечена только
#: по листам численности ГК). Расширение — новая запись в registry.SHEETS.
SUPPORTED_TABS = ("p17",)


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
    # «half» не входит в _SCALAR_PARAM_KEYS (это срез панели, а не контекста
    # ИИ-разбора), но для деки он обязателен: от него зависят заголовки
    # слайдов и месяц факта. Без него сборщик уходит в дефолт «2 ПГ» и
    # выгрузка за 1 ПГ подписана как 2 ПГ. Пустое значение — дефолт сборщика.
    if params.get("half") in (None, ""):
        half = (filters or {}).get("half")
        if half not in (None, ""):
            params["half"] = half
    data = await fot_insights._fot_dashboard(tab, params)
    log(logging.INFO, "fot_deck: данные вкладки собраны", tab=tab,
        cards=len(data.get("cards") or []),
        details=len(data.get("country_details") or {}))
    return data


__all__ = ["SUPPORTED_TABS", "UnsupportedTab", "collect"]
