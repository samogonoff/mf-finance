"""Выгрузка ФОТ-дашборда «1:1 как в деке заказчика» (задача 663620).

Отличие от `app/fot_pptx.py`: тот собирает слайды кодом (реплика деки
приблизительно), а этот модуль клонирует слайды-прототипы самой деки
(обезличенный актив `app/assets/fot_deck/`) и подставляет живые данные — на
выходе вёрстка, шрифты, заливки и геометрия совпадают с декой 1:1.

Слои:
  collect.py  — данные вкладки из app.fot.dashboard (биндеры в SQL не ходят);
  registry.py — декларативный список вкладка → прототипы → биндеры;
  shapes.py   — работа с XML актива (клонирование слайдов, раны, таблицы);
  binders/    — заполнение конкретных листов (p17 — численность ГК);
  format.py   — типографика чисел деки (пробел-тысячи, запятая, U+2212);
  engine.py   — сборка одной презентации на запрос.
"""

from app.fot_deck_export import collect
from app.fot_deck_export.engine import (
    AssetMismatch,
    DeckEngine,
    EmptySlice,
    export_deck,
    load_manifest,
    load_template,
    reset_cache,
)

__all__ = ["AssetMismatch", "DeckEngine", "EmptySlice", "collect", "export_deck",
           "load_manifest", "load_template", "reset_cache"]
