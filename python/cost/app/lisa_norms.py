"""Нормы расхода материалов из Лисы — справочно в редакторе расчёта.

Задача Б24 661869 (Стельмачёнок Е.А., 02.10.2026): кружево и вышивка, которые
не проходят по карте кроя, не попадают в расчёт ПФКСС 3 шаг, а по калькуляции
не видно, что их не хватает. Ручное добавление материала в редакторе есть (у
всех признаков, кроме ФКСС), но догадаться, ЧТО добавлять, не по чему. Решение
05.10.2026: показать рядом с расчётом нормы изделия из Лисы. Запрос взят из
книги экономистов «Нормы и факт по артикулу» (запросы «Норма» и «Себестоимость
плановая (Марины)»), сопоставление со строками расчёта делает фронт.

ИСТОЧНИК
========
* Есть номер задания (КПСС, ПФКСС, ФКСС) — материалы задания `mat_plan` по
  `NOMER_PR`: конкретный материал с тремя свойствами (цвет, плотность) и
  артикулом материала — в том же виде, что строки калькуляции, — и норма
  задания. Норма карточки (`s_norma` по `NORMA_ID`) и её примечание («Чашка»,
  «Подкладка стана») идут рядом: норма задания от неё отличается не всегда, но
  бывает (М26.7.1372: полотно кулирное 0,001811 против 0,0018).
* Задания нет (у ПКПСС его в источнике нет никогда) или по заданию в Лисе
  материалов нет — карточка норм модели: `s_norma` по `MODEL_ID` последней
  версии пары модель+артикул в `s_modeli`. Свойства у карточки общие, без
  цвета (кружево «—, 22 см» вместо «ZD8323188NS-23, 23 см, Черный»).

Пустой номер задания в запрос не идёт: у `mat_plan` 28 планов со строками без
номера (05.10.2026), а непустой номер в Лисе принадлежит одному плану — поэтому
план в условие не входит.

`NOMER_PR` — char(12), `MODEL`/`ART` у `s_modeli` — char(25). Параметр pyodbc
уходит nvarchar, и без CAST сервер сравнивает по преобразованной колонке мимо
индекса: 913 мс против 21 мс на задание (замер 05.10.2026). Номер длиннее
колонки обрезался бы CAST'ом до чужого задания — такой сразу считаем
отсутствующим.

Плановую цену задания (`mat_plan.PL_PRICE`, она есть в книге экономистов) не
отдаём: у большинства строк 0, а у заполненных валюта не читается (пакет
0,069 при 0,0799 руб. и 0,0266 USD в расчёте того же задания, 05.10.2026).
"""

from __future__ import annotations

import asyncio
from typing import Any

from app.db import get_gpartner_conn

_TASK_LEN = 12
_MODEL_LEN = 25

# Материал задания. MATERIAL_ART уникален по (MATERIAL_ID, PROPER_ID1,
# PROPER_ID2) — проверено 05.10.2026, строки соединение не размножает.
_TASK_SQL = """
SELECT mp.MATERIAL_TIP, RTRIM(sm.NAIM), RTRIM(ma.MATERIAL_ART),
       RTRIM(p1.NAIM), RTRIM(p2.NAIM), RTRIM(p3.NAIM), RTRIM(e.KEDIZM),
       mp.NORMA, n.NORMA, RTRIM(n.PRIM), n.ON_RAZMER
  FROM dbo.mat_plan mp
  JOIN dbo.s_material sm ON sm.ITEM_ID = mp.MATERIAL_ID
  LEFT JOIN dbo.s_edizm e ON e.ITEM_ID = sm.EDIZM_ID
  LEFT JOIN dbo.s_proper p1 ON p1.ITEM_ID = mp.MATERIAL_PR1
  LEFT JOIN dbo.s_proper p2 ON p2.ITEM_ID = mp.MATERIAL_PR2
  LEFT JOIN dbo.s_proper p3 ON p3.ITEM_ID = mp.MATERIAL_PR3
  LEFT JOIN dbo.s_norma n ON n.ITEM_ID = mp.NORMA_ID
  LEFT JOIN dbo.MATERIAL_ART ma ON ma.MATERIAL_ID = mp.MATERIAL_ID
       AND ma.PROPER_ID1 = mp.MATERIAL_PR1 AND ma.PROPER_ID2 = mp.MATERIAL_PR2
 WHERE mp.NOMER_PR = CAST(? AS char(12))
 ORDER BY mp.MATERIAL_TIP, n.ITEM_ORDER, mp.ITEM_ID
"""

_MODEL_SQL = """
SELECT TOP 1 ITEM_ID FROM dbo.s_modeli
 WHERE MODEL = CAST(? AS char(25)) AND ART = CAST(? AS char(25)) AND ISFOLDER = 0
 ORDER BY ITEM_ID DESC
"""

# Карточка норм модели — тот же набор колонок, что у задания: норма карточки
# стоит и на месте нормы задания.
_CARD_SQL = """
SELECT n.TIP, RTRIM(sm.NAIM), RTRIM(ma.MATERIAL_ART),
       RTRIM(p1.NAIM), RTRIM(p2.NAIM), RTRIM(p3.NAIM), RTRIM(e.KEDIZM),
       n.NORMA, n.NORMA, RTRIM(n.PRIM), n.ON_RAZMER
  FROM dbo.s_norma n
  JOIN dbo.s_material sm ON sm.ITEM_ID = n.MATERIAL_ID
  LEFT JOIN dbo.s_edizm e ON e.ITEM_ID = sm.EDIZM_ID
  LEFT JOIN dbo.s_proper p1 ON p1.ITEM_ID = n.PROPER_ID1
  LEFT JOIN dbo.s_proper p2 ON p2.ITEM_ID = n.PROPER_ID2
  LEFT JOIN dbo.s_proper p3 ON p3.ITEM_ID = n.PROPER_ID3
  LEFT JOIN dbo.MATERIAL_ART ma ON ma.MATERIAL_ID = n.MATERIAL_ID
       AND ma.PROPER_ID1 = n.PROPER_ID1 AND ma.PROPER_ID2 = n.PROPER_ID2
 WHERE n.MODEL_ID = ?
 ORDER BY n.TIP, n.ITEM_ORDER, n.ITEM_ID
"""


def _s(v: Any) -> str:
    return str(v).strip() if v is not None else ""


def _num(v: Any) -> float | None:
    return float(v) if v is not None else None


def _row(r: tuple) -> dict[str, Any]:
    tip, name, art, p1, p2, p3, unit, norm, card_norm, prim, on_size = r
    return {
        # 1 — основной, 2 — вспомогательный (других значений в mat_plan и
        # s_norma нет, 05.10.2026).
        "tip": int(tip) if tip is not None else None,
        "name": _s(name),
        "material_art": _s(art),
        "prop1": _s(p1),
        "prop2": _s(p2),
        "prop3": _s(p3),
        "unit": _s(unit),
        "norm": _num(norm),
        "card_norm": _num(card_norm),
        "purpose": _s(prim),
        # ON_RAZMER = 1 стоит у норм с примечанием «ПО РАЗМЕРАМ» (каркас,
        # кольцо, застёжка, бретели у 512857-1): материал зависит от размера.
        "by_size": bool(on_size and int(on_size) == 1),
    }


def _norms_sync(model: str, articul: str, task: str) -> dict[str, Any]:
    conn = get_gpartner_conn()
    cur = conn.cursor()
    try:
        if task and len(task) <= _TASK_LEN:
            cur.execute(_TASK_SQL, task)
            rows = [_row(r) for r in cur.fetchall()]
            if rows:
                return {"source": "task", "task_number": task, "task_empty": False,
                        "model_item_id": None, "rows": rows}

        item_id = None
        if len(model) <= _MODEL_LEN and len(articul) <= _MODEL_LEN:
            cur.execute(_MODEL_SQL, model, articul)
            hit = cur.fetchone()
            item_id = int(hit[0]) if hit and hit[0] is not None else None
        rows = []
        if item_id is not None:
            cur.execute(_CARD_SQL, item_id)
            rows = [_row(r) for r in cur.fetchall()]
        return {
            "source": "card" if rows else None,
            "task_number": task or None,
            # Задание передано, а материалов по нему нет — интерфейс говорит,
            # что показана карточка модели, а не задание.
            "task_empty": bool(task),
            "model_item_id": item_id,
            "rows": rows,
        }
    finally:
        conn.close()


async def lisa_norms(model: str, articul: str, task_number: str | None) -> dict[str, Any]:
    model, articul, task = _s(model), _s(articul), _s(task_number)
    loop = asyncio.get_running_loop()
    return await loop.run_in_executor(None, _norms_sync, model, articul, task)
