"""Углублённое исследование данных раздела: «почему такая маржа?».

Продолжение блока «Разбор ИИ» (`app/insights.py`). Тот даёт обзор по графикам
секции; здесь пользователь задаёт КОНКРЕТНЫЙ вопрос по срезу, и модель ведёт
многошаговое исследование: план → запрос к данным → чтение результата → следующий
запрос → вывод.

ПОЧЕМУ МОДЕЛЬ НЕ ПИШЕТ SQL
==========================
Схема витрины враждебна к генерации SQL: русские имена колонок с запятыми, а
`"Отпускная цена по уровню, руб"` — единственная без точки на конце (проверено
01.09.2026: внешний инструмент на этом спотыкается сразу). Упавший запрос — это
ещё повезло; хуже, когда он не падает, а тихо считает не то.

Поэтому модель НЕ генерирует SQL, а вызывает инструменты — параметризованные
запросы, написанные нами. Она решает только, какой инструмент вызвать и с какими
фильтрами. Отсюда: произвольного SQL в системе нет, read-only роль и валидатор
запросов не нужны, а числа гарантированно совпадают с дашбордом.

ЧИСЛА БЕРУТСЯ ИЗ ЯДРА ДАШБОРДА
==============================
Все инструменты — проекции `margin.dashboard()`, того же кода, что рисует
страницу «Маржа выпуска». Это не оптимизация, а требование: агент, считающий
маржу своей формулой, начнёт противоречить графику, на который смотрит человек.
Один вызов ядра отдаёт сразу плитки, месяцы, разрезы, матрицу и артикулы,
поэтому несколько инструментов обслуживаются одним запросом — в пределах одного
исследования результат кэшируется по фильтрам (`_Cache`).

ПРО ПРИЧИНЫ
===========
Обзорному блоку причинность запрещена: в агрегате графика причин нет. Здесь она
РАЗРЕШЕНА, но только подтверждённая запросом — модель обязана сослаться на шаг
исследования. Это не формальность: реальная причина аномальной маржи ЧНИ в
июне 2026 оказалась дефектом витрины (не выгрузилось сырьё), и увидеть её можно
только сравнив сырьё на штуку по месяцам — то есть инструментом, а не
рассуждением.
"""

from __future__ import annotations

import json
import logging
import time
from typing import Any


from app import insights, margin
from app.logship import log

# ─── Пределы. Три независимых ограничителя, а не один ───────────────────────
#
# Шаги ограничивают число обращений к МОДЕЛИ, вызовы инструментов — обращения к
# ВИТРИНЕ (за один шаг модель может попросить несколько), а бюджет времени
# закрывает случай, когда и то и другое в пределах нормы, но каждый вызов
# медленный. Любой из трёх исчерпан — исследование сворачивается и просит у
# модели вывод по уже собранному.

# Шесть — эмпирика: типовой путь «разрез → провал в подгруппу → структура →
# сравнение периодов → вывод» укладывается в четыре, запас на тупиковый шаг.
MAX_STEPS = 6

# Шесть шагов по три вызова дали бы восемнадцать обращений к витрине на один
# вопрос. Двенадцать — с запасом больше, чем нужно любому осмысленному пути.
MAX_TOOL_CALLS = 12

# Общий бюджет на исследование. Таймаут httpx ограничивает ОДИН вызов модели;
# без общего предела шесть медленных шагов держали бы запрос минутами, а
# пользователь всё это время смотрит на спиннер.
#
# Исследование выполняется ФОНОВОЙ задачей (POST стартует, GET опрашивает
# статус — см. миграцию 0047), поэтому таймаут прокси в бюджет больше не входит:
# HTTP-запросы короткие, а агент живёт отдельно от них. Раньше дефолт был 35 с
# именно из-за минутного proxy_read_timeout у `location /api/cost/`.
#
# 120 с — предел разумного ожидания человека у экрана, а не техническое
# ограничение. Меняется через COST_LLM_AGENT_BUDGET без правки кода.
def _time_budget() -> float:
    from app import llm_settings
    return float(llm_settings.int_value(
        "agent_budget_s", "COST_LLM_AGENT_BUDGET", 120))


# Сколько времени резервируется на ФИНАЛЬНЫЙ вызов модели (тот, что без
# инструментов, с выводом). Без резерва бюджет исчерпывался ровно перед самым
# долгим вызовом, и ответ всё равно не успевал прийти до обрыва прокси.
FINAL_CALL_RESERVE_S = 20.0

# Предел на историю, отправляемую модели. Результаты инструментов копятся, и к
# седьмому вызову промпт вырастал до ~24 тыс. токенов (замер 01.09.2026) — это
# и деньги, и риск упереться в контекст модели. При превышении самые старые
# результаты заменяются заглушкой: свежие данные для вывода важнее ранних.
MAX_HISTORY_CHARS = 60000

# Обрезка выдачи инструментов. Модель должна получить достаточно, чтобы увидеть
# картину, но не столько, чтобы утопить контекст: 300 строк матрицы в промпте
# бесполезны — по ним всё равно нельзя сделать вывод одним взглядом.
MAX_ROWS = 15

# Метрики, которые едут в модель. Полный ответ ядра — это сотни полей на строку
# (три периода × две валюты × десяток показателей); в промпте нужны единицы.
_KEY_METRICS = (
    "vol", "margin_byn", "margin_pct", "unit_cost_byn", "unit_price_byn",
    "unit_raw_byn", "min_per_unit", "calc_count",
)
_COMPARE_METRICS = ("margin_byn", "margin_pct", "unit_cost_byn", "unit_raw_byn", "vol")
_FACT_METRICS = (
    "fact_coverage_pct", "margin_pct_norm", "margin_pct_fact", "fact_dev_pp",
)

# Фильтры, которые модель имеет право задавать. Белый список — не только
# безопасность: ядро молча игнорирует незнакомые ключи, и опечатка модели
# превратилась бы в «данные по всему выпуску» под видом ответа про одну группу.
ALLOWED_FILTERS = ("year", "month", "brand_manager", "level01", "level02",
                   "level03", "level04", "level05", "model_name", "model",
                   "articul", "country", "season")


def _round(value: Any) -> Any:
    if isinstance(value, (int, float)) and not isinstance(value, bool):
        return round(float(value), 2)
    return value


def _pick(row: dict, keys: tuple[str, ...], suffixes: tuple[str, ...] = ("",)) -> dict:
    out: dict[str, Any] = {}
    for key in keys:
        for suffix in suffixes:
            value = row.get(f"{key}{suffix}")
            if value is not None:
                out[f"{key}{suffix}"] = _round(value)
    return out


def _clean_filters(raw: Any) -> dict[str, list[str]]:
    """Фильтры от модели → белый список. Скаляры приводим к списку."""
    if not isinstance(raw, dict):
        return {}
    out: dict[str, list[str]] = {}
    for key, value in raw.items():
        if key not in ALLOWED_FILTERS or value in (None, "", []):
            continue
        values = value if isinstance(value, list) else [value]
        cleaned = [str(v).strip() for v in values if str(v).strip()]
        if cleaned:
            out[key] = cleaned
    return out


class _Cache:
    """Кэш вызовов ядра в пределах одного исследования.

    Агент почти всегда спрашивает один и тот же срез под разными углами, а ядро
    отдаёт весь дашборд одним запросом. Без кэша каждый шаг стоил бы отдельный
    SQL на 1–3 секунды.
    """

    def __init__(self) -> None:
        self._data: dict[str, dict] = {}
        self.sql_calls = 0

    async def get(self, filters: dict, path: list[str] | None = None,
                  basis: str = "fact") -> dict:
        key = json.dumps([filters, path or [], basis], ensure_ascii=False, sort_keys=True)
        if key not in self._data:
            self._data[key] = await margin.dashboard(
                filters, matrix_path=path or [], cost_basis=basis)
            self.sql_calls += 1
        return self._data[key]


# ─── Инструменты ─────────────────────────────────────────────────────────────

TOOL_SPECS: list[dict] = [
    {
        "type": "function",
        "function": {
            "name": "metrics",
            "description": (
                "Показатели среза: выпуск, маржа, маржинальность, себестоимость и "
                "сырьё на штуку, минуты пошива. Сразу с сравнением: _pm — тот же "
                "срез в прошлом месяце, _py — в прошлом году. Плюс динамика по "
                "месяцам. Начинай исследование этим инструментом."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "filters": {
                        "type": "object",
                        "description": (
                            "Срез: year, month (двузначный, '06'), level01..level05, "
                            "brand_manager, model, articul, country, season. "
                            "Значения — точные, как в данных."
                        ),
                    },
                },
                "required": ["filters"],
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "breakdown",
            "description": (
                "Разрез среза по товарным группам Level 01 или по бренд-менеджерам: "
                "где сидит проблема. Возвращает маржу, маржинальность и выпуск по "
                "каждому значению."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "filters": {"type": "object"},
                    "by": {"type": "string", "enum": ["level01", "brand_manager"]},
                },
                "required": ["filters", "by"],
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "drill",
            "description": (
                "Проваливание по иерархии товарных групп: путь ['Девочкам'] даёт "
                "Level 02 внутри «Девочкам», ['Девочкам','MF life девочка'] — "
                "Level 03 внутри неё, и так до артикула и задания. Так находится "
                "подгруппа, которая тянет маржу вниз."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "filters": {"type": "object"},
                    "path": {
                        "type": "array",
                        "items": {"type": "string"},
                        "description": "Путь по уровням, начиная с Level 01",
                    },
                },
                "required": ["filters", "path"],
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "articles",
            "description": (
                "Артикулы среза: отпускная цена и три себестоимости единицы — "
                "плановая, нормативная, фактическая, — отклонения между ними и "
                "маржинальность единицы. sort='worst_margin' даёт позиции с худшей "
                "маржинальностью, sort='volume' — самые крупные по выпуску."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "filters": {"type": "object"},
                    "sort": {"type": "string", "enum": ["volume", "worst_margin"]},
                },
                "required": ["filters"],
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "fact_vs_norm",
            "description": (
                "Норматив против факта по срезу: маржинальность по нормативной и по "
                "фактической себестоимости, отклонение в процентных пунктах и ДОЛЯ "
                "ВЫПУСКА С ГОДНЫМ ФАКТОМ. Низкая доля означает, что «факт» посчитан "
                "без фактической ставки минуты и сравнивать его нельзя."
            ),
            "parameters": {
                "type": "object",
                "properties": {"filters": {"type": "object"}},
                "required": ["filters"],
            },
        },
    },
]


async def _tool_metrics(cache: _Cache, args: dict) -> dict:
    filters = _clean_filters(args.get("filters"))
    data = await cache.get(filters)
    tiles = data.get("tiles") or {}
    months = data.get("months") or []
    return {
        "срез": filters or "весь выпуск",
        "показатели": _pick(tiles, _KEY_METRICS),
        "прошлый_месяц": _pick(tiles, _COMPARE_METRICS, ("_pm",)),
        "прошлый_год": _pick(tiles, _COMPARE_METRICS, ("_py",)),
        "по_месяцам": [
            {"месяц": m.get("ym"), **_pick(m, ("margin_byn", "margin_pct",
                                               "unit_cost_byn", "unit_raw_byn", "vol"))}
            for m in months[:24]
        ],
    }


# Название строки в разрезах и матрице ядра лежит в `label` (в матрице рядом
# есть ещё и `name`, но он null) — проверено на живом ответе 01.09.2026.
_ROW_LABEL = "label"


async def _tool_breakdown(cache: _Cache, args: dict) -> dict:
    filters = _clean_filters(args.get("filters"))
    by = args.get("by") if args.get("by") in ("level01", "brand_manager") else "level01"
    data = await cache.get(filters)
    rows = data.get("by_level01" if by == "level01" else "by_bm") or []
    return {
        "срез": filters or "весь выпуск",
        "разрез_по": by,
        "строки": [
            {"значение": r.get(_ROW_LABEL),
             **_pick(r, ("vol", "margin_byn", "margin_pct", "unit_cost_byn",
                         "unit_raw_byn", "calc_count"))}
            for r in rows[:MAX_ROWS]
        ],
    }


async def _tool_drill(cache: _Cache, args: dict) -> dict:
    filters = _clean_filters(args.get("filters"))
    path = [str(p) for p in (args.get("path") or []) if str(p).strip()]
    data = await cache.get(filters, path=path)
    rows = data.get("matrix") or []
    meta = data.get("meta") or {}
    return {
        "срез": filters or "весь выпуск",
        "путь": path,
        "уровень_строк": meta.get("matrix_label"),
        "строки": [
            {"значение": r.get(_ROW_LABEL),
             **_pick(r, ("vol", "margin_byn", "margin_pct", "unit_cost_byn",
                         "unit_raw_byn", "min_per_unit", "calc_count"))}
            for r in rows[:MAX_ROWS]
        ],
    }


# Поля листа отклонений отличаются от разрезов: там нет ни margin_pct, ни
# unit_cost_byn — только три себестоимости единицы и отпускная цена.
_DEV_FIELDS = ("model", "articul", "name", "plan_id", "vol", "unit_price_byn",
               "unit_plan_byn", "unit_norm_byn", "unit_fact_byn",
               "dev_fact_norm_pct", "dev_norm_plan_pct", "plan_coverage_pct",
               "fact_coverage_pct")


def _unit_margin_pct(row: dict) -> float | None:
    """Маржинальность единицы по листу отклонений.

    Считаем здесь, а не просим у модели: в `deviations` готовой маржинальности
    нет, а без неё «худшие артикулы» не отсортировать. Себестоимость берём
    фактическую, где она есть, иначе нормативную — та же логика, что у базы
    `fact` в ядре.
    """
    price = row.get("unit_price_byn")
    cost = row.get("unit_fact_byn") or row.get("unit_norm_byn")
    if not price or cost is None:
        return None
    try:
        return (float(price) - float(cost)) / float(price) * 100.0
    except (TypeError, ValueError, ZeroDivisionError):
        return None


async def _tool_articles(cache: _Cache, args: dict) -> dict:
    filters = _clean_filters(args.get("filters"))
    sort = args.get("sort") if args.get("sort") in ("volume", "worst_margin") else "volume"
    data = await cache.get(filters)
    rows = list(data.get("deviations") or [])

    enriched = []
    for r in rows:
        item = {k: _round(v) for k, v in r.items() if k in _DEV_FIELDS and v is not None}
        pct = _unit_margin_pct(r)
        if pct is not None:
            item["unit_margin_pct"] = round(pct, 2)
        enriched.append(item)

    if sort == "worst_margin":
        # Артикулы без маржинальности в конец: иначе «худшими» окажутся строки,
        # у которых просто нет цены.
        enriched.sort(key=lambda i: (i.get("unit_margin_pct") is None,
                                     i.get("unit_margin_pct", 0)))

    meta = data.get("meta") or {}
    return {
        "срез": filters or "весь выпуск",
        "сортировка": sort,
        "всего_артикулов": len(rows),
        "показаны_не_все": bool(meta.get("deviations_truncated")),
        "артикулы": enriched[:MAX_ROWS],
    }


async def _tool_fact_vs_norm(cache: _Cache, args: dict) -> dict:
    filters = _clean_filters(args.get("filters"))
    data = await cache.get(filters)
    tiles = data.get("tiles") or {}
    months = data.get("months") or []
    return {
        "срез": filters or "весь выпуск",
        "итого": _pick(tiles, _FACT_METRICS),
        "по_месяцам": [
            {"месяц": m.get("ym"), **_pick(m, _FACT_METRICS)}
            for m in months[:24]
        ],
    }


_TOOLS = {
    "metrics": _tool_metrics,
    "breakdown": _tool_breakdown,
    "drill": _tool_drill,
    "articles": _tool_articles,
    "fact_vs_norm": _tool_fact_vs_norm,
}


# ─── Промпт ──────────────────────────────────────────────────────────────────

SYSTEM_PROMPT = """Ты аналитик производственной себестоимости на швейном предприятии.
Пользователь смотрит дашборд «Маржа выпуска» и задаёт вопрос по срезу. Твоя
задача — ИССЛЕДОВАТЬ данные инструментами и ответить по существу.

Разговор продолжается: если в задании есть раздел «Что уже выяснено в этом
разговоре», вопрос почти наверняка УТОЧНЯЮЩИЙ. Тогда:
- не начинай с нуля и не пересказывай прошлый ответ — отвечай на то, что
  спросили сейчас;
- «там», «в этой группе», «а почему» относятся к предмету прошлых раундов;
- уже выясненные числа можно использовать, но выдавать их за свежие нельзя: если
  вывод строится на них, перепроверь инструментом.

Как работать:
1. Начни с `metrics` по срезу вопроса — увидишь показатели и сравнение с прошлым
   месяцем и прошлым годом. В уточняющем раунде первый вызов выбирай по смыслу
   вопроса, а не по этому порядку.
2. Дальше сужай: `breakdown` и `drill` показывают, где именно сидит проблема,
   `articles` — конкретные позиции, `fact_vs_norm` — эффект фактической ставки
   минуты.
3. Делай 2–5 вызовов. Каждый следующий выбирай по тому, что показал предыдущий,
   а не по заранее составленному списку.
4. Когда картина ясна — дай ответ.

Правила вывода:
- Пиши ТОЛЬКО по-русски.
- Все числа — из результатов инструментов. Не считай сам, кроме простых
  разностей, и такие помечай словом «разница».
- ИДЕНТИФИКАТОРЫ КОПИРУЙ ПОБУКВЕННО: артикул, модель, номер плана, задания —
  ровно как в данных, со всеми суффиксами и дефисами. «26-45330Ц-7П-2» нельзя
  сокращать до «26-45330Ц-7П»: по обрезанному номеру позицию не найти, а можно
  попасть в чужую (проверено 02.09.2026 — заказчик не смог сверить артикул).
- ОТЛИЧАЙ УБЫТОК ОТ ОШИБКИ В ДАННЫХ. Себестоимость в разы выше отпускной цены
  или в разы выше плановой — это почти всегда дефект калькуляции в источнике, а
  не реальная торговля в минус. Так и формулируй: «похоже на ошибку в
  калькуляции, требует проверки», а не «продаётся в убыток».
- ПРИЧИНУ называй только если её подтвердил инструмент, и указывай, чем именно.
  Если данных о причине нет — так и скажи: «по этим данным причина не
  определяется», и перечисли, что стоит проверить вне дашборда.
- Никогда не выдумывай внешние обстоятельства (курс, подорожание сырья, действия
  сотрудников) — таких данных у тебя нет.
- Обязательно проверяй качество данных, прежде чем объявлять экономический вывод:
  * НЕЗАКРЫТЫЙ ПЕРИОД: последний месяц в `по_месяцам` может быть текущим и
    содержать всего пару дней. Признак — выпуск и `calc_count` на порядок ниже
    соседних месяцев. По такому месяцу НЕЛЬЗЯ говорить о падении: там меньше
    дней, а не хуже экономика. Скажи, что период не закрыт, и считай по
    закрытым;
  * `unit_raw_byn` (сырьё на штуку) резко ниже соседних месяцев — вероятно, в
    источнике недозаполнено сырьё, и маржа завышена искусственно;
  * низкая `fact_coverage_pct` — «факт» посчитан без фактической ставки минуты,
    сравнение факта с нормативом недостоверно;
  * малое `calc_count` — вывод построен на единичных калькуляциях.
- Проценты не складывай; разницу процентов называй в процентных пунктах (пп).
- Не давай указаний в стиле «нужно», «следует». Формулируй как наблюдения и
  «что проверить».

Финальный ответ — СТРОГО один JSON-объект без markdown:
{"answer": "2–4 предложения: главный вывод",
 "findings": [{"text": "наблюдение с числами", "evidence": "чем подтверждено",
               "kind": "neutral|attention|risk"}],
 "data_quality": "что с качеством данных среза, или пустая строка",
 "next_steps": ["что проверить дальше, вне дашборда"],
 "confidence": "high|medium|low"}

ПИШИ КОМПАКТНО, иначе ответ обрывается на середине и теряется целиком:
- answer до 400 символов;
- не больше 4 findings, text до 200 символов, evidence до 100 (назови
  инструмент и ключевое число, без пересказа);
- data_quality одной фразой до 200 символов;
- не больше 2 next_steps по 100 символов."""


def build_user_prompt(question: str, context: dict,
                      history: list[dict] | None = None) -> str:
    ctx = "\n".join(f"- {k}: {v}" for k, v in (context or {}).items()
                    if v not in (None, "", []))

    # Сводка прошлых раундов ветки — вопрос, вывод и какие данные для него
    # запрашивались. Сырые результаты инструментов сюда не попадают: они
    # устаревают вместе с витриной, а нужны будут — агент запросит заново.
    past = ""
    if history:
        blocks = []
        for round_ in history:
            lines = [f"Вопрос {round_['seq']}: {round_['question']}"]
            if round_.get("answer"):
                lines.append(f"Ответ: {round_['answer']}")
            for finding in (round_.get("findings") or [])[:4]:
                text = finding.get("text") if isinstance(finding, dict) else finding
                if text:
                    lines.append(f"  · {text}")
            tools = round_.get("tools") or []
            if tools:
                lines.append("Запрашивались данные: " + ", ".join(tools))
            blocks.append("\n".join(lines))
        past = ("Что уже выяснено в этом разговоре (используй, но НЕ выдавай "
                "старые числа за свежие — при необходимости перепроверь "
                "инструментами):\n\n" + "\n\n".join(blocks))

    return "\n\n".join(filter(None, [
        insights.GLOSSARY,
        "Срез, который открыт у пользователя на дашборде:\n" + ctx if ctx else "",
        past,
        f"Вопрос пользователя: {question}",
    ]))


# ─── Вызов модели ────────────────────────────────────────────────────────────

class AgentError(RuntimeError):
    """Исследование не удалось довести до ответа."""


async def _chat(messages: list[dict], with_tools: bool) -> dict:
    """Один вызов модели. Возвращает message из ответа."""
    payload: dict[str, Any] = {
        "model": insights._model(),
        "messages": messages,
        "max_tokens": 4000,
        "temperature": 0.2,
    }
    effort = insights.reasoning_effort()
    if effort:
        # То же, что в обзорном блоке: без этого агент тратил минуты на
        # внутренние рассуждения на каждом шаге и ловил ReadTimeout.
        payload["reasoning_effort"] = effort
    thinking = insights.thinking_param()
    if thinking:
        # У GLM (z.ai) размышления выключаются своим полем, а не через
        # reasoning_effort — отправляем оба, лишнее отвалится в post_chat.
        payload["thinking"] = thinking

    if with_tools:
        payload["tools"] = TOOL_SPECS
        payload["tool_choice"] = "auto"

    try:
        # post_chat сам повторяет запрос при сетевом таймауте: у провайдера они
        # рядовые, а один ReadTimeout иначе убивал исследование целиком.
        resp = await insights.post_chat(payload)
    except insights.InsightError as exc:
        raise AgentError(str(exc)) from exc

    if resp.status_code != 200:
        try:
            detail = str((resp.json().get("error") or {}).get("message") or "")[:200]
        except Exception:
            detail = resp.text[:200]
        raise AgentError(f"модель вернула {resp.status_code}: {detail}")

    body = resp.json()
    try:
        return {"message": body["choices"][0]["message"],
                "finish_reason": body["choices"][0].get("finish_reason"),
                "usage": body.get("usage") or {},
                "cost": body.get("cost")}
    except (KeyError, IndexError) as exc:
        raise AgentError("неожиданная структура ответа модели") from exc


async def _rescue_answer(messages: list[dict], trace: list[dict]) -> dict | None:
    """Попытка вытащить вывод из УЖЕ собранных данных после сбоя вызова.

    Зачем. 02.09.2026 агент собрал семь наборов данных за 86 секунд, после чего
    один ReadTimeout выбросил всю работу и деньги. Данные к тому моменту лежат в
    истории сообщений — из них можно получить ответ, даже если очередной вызов
    не удался.
    Как. Один короткий вызов БЕЗ описаний инструментов (они занимают львиную
    долю промпта) и с урезанной историей: спрашиваем только вывод по собранному.
    Не получилось — возвращаем None, вызывающий отдаст degraded как раньше.
    """
    if not trace:
        return None

    # Собранные данные подаём ТЕКСТОМ, а не куском истории. Первая попытка
    # отфильтровать историю по ролям выбросила assistant-реплики с tool_calls, и
    # tool-результаты остались без запросов, к которым относятся: модель решила,
    # что «все запросы завершились ошибкой», хотя данные лежали рядом.
    tool_contents = [m.get("content") or "" for m in messages
                     if m.get("role") == "tool"]
    collected: list[str] = []
    for step, content in zip(trace, tool_contents):
        args = json.dumps(step.get("args"), ensure_ascii=False)
        collected.append(f"Шаг {step.get('step')} — {step.get('tool')}({args}):\n{content}")

    # Урезаем с начала: последние шаги обычно и есть те, на которых строится
    # вывод. Ранние агент уже учёл, выбирая, куда копать дальше.
    budget = 25000
    kept: list[str] = []
    for block in reversed(collected):
        if budget - len(block) < 0:
            break
        kept.insert(0, block)
        budget -= len(block)

    if not kept:
        return None

    question_msg = next((m.get("content") for m in messages
                         if m.get("role") == "user"), "")
    rescue = [
        {"role": "system", "content": SYSTEM_PROMPT},
        {"role": "user", "content": question_msg},
        {"role": "user", "content":
            "Связь с моделью прервалась, продолжать исследование нельзя.\n\n"
            "ДАННЫЕ, КОТОРЫЕ УЖЕ СОБРАНЫ (это настоящие результаты запросов, "
            "используй их):\n\n" + "\n\n".join(kept) + "\n\n"
            "Дай финальный ответ в требуемом формате JSON по этим данным. "
            "Отметь в next_steps, что исследование не доведено до конца, и "
            "поставь confidence не выше medium."},
    ]

    try:
        out = await _chat(rescue, with_tools=False)
    except AgentError as exc:
        log(logging.WARNING, "insights.ask: спасти ответ не удалось",
            error=str(exc)[:200])
        return None

    message = out["message"]
    content = (message.get("content") or "").strip() or \
              (message.get("reasoning_content") or "").strip()
    if not content:
        return None

    parsed = _parse_final(content)
    if parsed is None or (not parsed.get("answer") and not parsed.get("findings")):
        return None

    # Понижаем уверенность принудительно: вывод сделан по неполному
    # исследованию, что бы модель о себе ни думала.
    if parsed.get("confidence") == "high":
        parsed["confidence"] = "medium"
    parsed["usage"] = out["usage"]
    parsed["rescued"] = True
    return parsed


def _trim_history(messages: list[dict], limit: int | None = None) -> None:
    """Сжать историю на месте, если она переросла предел.

    Заглушаем содержимое САМЫХ РАННИХ tool-сообщений: последние шаги несут те
    данные, по которым модель делает вывод, а ранние она уже пересказала в своих
    репликах. Сами сообщения не удаляем — у каждого tool-ответа есть
    tool_call_id, и без парного сообщения запрос к модели станет невалидным.
    """
    cap = limit if limit is not None else MAX_HISTORY_CHARS
    total = sum(len(m.get("content") or "") for m in messages)
    if total <= cap:
        return
    for msg in messages:
        if total <= cap:
            break
        if msg.get("role") != "tool":
            continue
        original = len(msg.get("content") or "")
        if original <= 200:
            continue
        msg["content"] = json.dumps(
            {"опущено": "результат раннего шага исключён из истории, "
                        "чтобы не переполнить контекст"}, ensure_ascii=False)
        total -= original - len(msg["content"])


def _repair_truncated_json(txt: str) -> str | None:
    """Достроить JSON, обрезанный на середине. None — восстановить не удалось.

    Ответ модели упирается в лимит токенов, и текст обрывается посреди строки
    («…почему «Пляжная коллекция» имеет маржу»). Раньше такой ответ не парсился,
    и сырой JSON уходил пользователю в поле answer, обрезанный по 2000 символов
    (жалоба заказчика 02.09.2026). Между «показать обрывок JSON» и «достроить и
    отдать содержимое» второе явно лучше: answer и первые findings в таком
    ответе уже полные, теряется только хвост.

    Способ — перебор, а не разовая догадка. Первая версия отрезала до
    «последнего безопасного места» и достраивала скобки, но упиралась в
    висящий ключ (`"next_steps":` без значения) и всё равно не парсилась.
    Здесь берём позиции, где закрылась строка или структура, идём от самой
    поздней к ранней и на каждой пробуем достроить и распарсить: первый успех
    и есть максимум сохранённого.
    """
    begin = txt.find("{")
    if begin < 0:
        return None
    txt = txt[begin:]

    # Позиции-кандидаты и состояние стека на каждой из них.
    cuts: list[tuple[int, tuple[str, ...]]] = []
    stack: list[str] = []
    in_str = False
    escaped = False
    for i, ch in enumerate(txt):
        if in_str:
            if escaped:
                escaped = False
            elif ch == "\\":
                escaped = True
            elif ch == '"':
                in_str = False
                cuts.append((i + 1, tuple(stack)))
            continue
        if ch == '"':
            in_str = True
        elif ch in "{[":
            stack.append("}" if ch == "{" else "]")
        elif ch in "}]":
            if stack and stack[-1] == ch:
                stack.pop()
                cuts.append((i + 1, tuple(stack)))

    # От самой поздней точки к ранней, но не бесконечно: дальше десятков
    # попыток смысла нет — значит текст не JSON вовсе.
    for cut, pending in reversed(cuts[-60:]):
        frag = txt[:cut].rstrip().rstrip(",")
        candidate = frag + "".join(reversed(pending))
        try:
            data = json.loads(candidate)
        except json.JSONDecodeError:
            continue
        if isinstance(data, dict):
            return candidate
    return None


def _parse_final(content: str) -> dict | None:
    """Финальный JSON исследования. None — разобрать не удалось.

    None лучше, чем сырой текст в поле ответа: пользователю нельзя показывать
    JSON, а вызывающий по None либо просит модель ответить короче, либо честно
    сообщает о сбое.
    """
    import re

    txt = re.sub(r"^```(?:json)?|```$", "", content.strip(), flags=re.MULTILINE).strip()

    # По порядку: как есть → вырезанный из окружающего текста → достроенный
    # после обрыва по лимиту токенов.
    candidates: list[str] = [txt]
    braced = re.search(r"\{.*\}", txt, re.DOTALL)
    if braced:
        candidates.append(braced.group(0))
    repaired = _repair_truncated_json(txt)
    if repaired:
        candidates.append(repaired)

    data = None
    for candidate in candidates:
        try:
            data = json.loads(candidate)
            break
        except json.JSONDecodeError:
            continue

    if not isinstance(data, dict):
        return None

    findings = []
    for f in (data.get("findings") or [])[:6]:
        if isinstance(f, str) and f.strip():
            findings.append({"text": f.strip(), "evidence": "", "kind": "neutral"})
        elif isinstance(f, dict) and f.get("text"):
            kind = str(f.get("kind") or "neutral").lower()
            findings.append({
                "text": str(f["text"]).strip(),
                "evidence": str(f.get("evidence") or "").strip(),
                "kind": kind if kind in ("neutral", "attention", "risk") else "neutral",
            })

    return {
        "answer": str(data.get("answer") or "").strip(),
        "findings": findings,
        "data_quality": str(data.get("data_quality") or "").strip(),
        "next_steps": [str(s).strip() for s in (data.get("next_steps") or [])[:5] if str(s).strip()],
        "confidence": str(data.get("confidence") or "").lower() or "medium",
    }


# ─── Кэш и журнал (cost_insight_questions, миграция 0046) ───────────────────

async def request_hash(question: str, context: dict,
                       thread_id: str | None = None) -> str:
    """Ключ кэша: вопрос + срез + модель + момент обновления витрины.

    Метка обновления обязательна. В обзорном блоке в хеш входят сами серии,
    поэтому изменение данных меняет ключ автоматически; здесь агент ходит за
    данными сам, и без метки один и тот же вопрос отдавал бы вывод, сделанный
    по прошлой версии витрины.
    """
    import hashlib

    from app.db import get_cache_status

    refreshed = ""
    try:
        status = await get_cache_status()
        if status and status.get("refreshed_at"):
            refreshed = str(status["refreshed_at"])
    except Exception as exc:
        # Без метки кэш станет чуть «липче», но работать не перестанет.
        log(logging.WARNING, "insights.ask: статус кэша недоступен",
            error=str(exc)[:200])

    # Ветка входит в ключ: одинаковый уточняющий вопрос («а почему?») в двух
    # разных разговорах означает разное, и кэш отдавал бы чужой ответ.
    blob = json.dumps(
        {"q": question.strip().lower(), "ctx": context,
         "model": insights._model(), "data": refreshed,
         "thread": thread_id or ""},
        ensure_ascii=False, sort_keys=True, separators=(",", ":"),
    )
    return hashlib.sha256(blob.encode("utf-8")).hexdigest()


def _load_json(value: Any) -> Any:
    return json.loads(value) if isinstance(value, str) else value


async def cached_answer(req_hash: str) -> dict | None:
    """Последнее ЗАВЕРШЁННОЕ успешное исследование по хешу. None — в кэше нет.

    Условие по статусу обязательно: у только что стартовавшей задачи degraded
    тоже FALSE, и без него кэш отдавал бы пустой результат работающей задачи.
    """
    from app.db import pool

    try:
        async with pool().acquire() as conn:
            row = await conn.fetchrow(
                "SELECT result, trace, model, steps, sql_calls, elapsed_ms, created_at "
                "FROM cost_insight_questions "
                "WHERE request_hash = $1 AND degraded = FALSE AND status = 'done' "
                "ORDER BY created_at DESC LIMIT 1",
                req_hash,
            )
    except Exception as exc:
        log(logging.WARNING, "insights.ask: кэш недоступен", error=str(exc)[:200])
        return None

    if not row:
        return None

    return {
        **(_load_json(row["result"]) or {}),
        "trace": _load_json(row["trace"]) or [],
        "model": row["model"],
        "steps": row["steps"],
        "sql_calls": row["sql_calls"],
        "elapsed_ms": row["elapsed_ms"],
        "cached": True,
        "cached_at": row["created_at"].isoformat() if row["created_at"] else None,
        "degraded": False,
        "error": None,
    }


# ─── Жизненный цикл задачи (асинхронный запуск, миграция 0047) ──────────────
#
# Строка создаётся сразу со status='running' и дописывается по ходу работы.
# Ошибки записи здесь НЕ глотаются, в отличие от журнала обзорного блока: если
# строку не создать, опрашивать будет нечего и пользователь останется со
# спиннером навсегда.

# Сколько исследований разрешено одновременно на всех. Ограничитель дешёвый, но
# нужный: без него достаточно подержать кнопку, чтобы запустить десятки агентов,
# каждый из которых стоит денег и держит соединение к витрине.
MAX_RUNNING_JOBS = 3

# Через сколько минут работающая задача считается брошенной. Задача живёт в
# asyncio-таске, и рестарт контейнера (деплой, OOM) убивает её, не трогая строку
# в БД: без уборки такая строка навсегда занимала бы слот из MAX_RUNNING_JOBS, а
# пользователю показывался бы вечный «идёт исследование».
STALE_JOB_MINUTES = 10


# Сколько прошлых раундов ветки уходит в контекст. Пять — компромисс: дальше
# сводка начинает весить больше, чем помогает, а уточнения обычно опираются на
# один-два предыдущих ответа.
MAX_HISTORY_ROUNDS = 5


async def thread_history(thread_id: str) -> list[dict]:
    """Сводка завершённых раундов ветки: вопрос, вывод, какие данные брали.

    Сырые результаты инструментов НЕ включаются — они устаревают вместе с
    витриной (обновление раз в три часа), и старое число, поданное как свежее,
    хуже отсутствия контекста.
    """
    from app.db import pool

    try:
        async with pool().acquire() as conn:
            rows = await conn.fetch(
                "SELECT seq, question, result, trace FROM cost_insight_questions "
                "WHERE thread_id = $1::uuid AND status = 'done' AND degraded = FALSE "
                "ORDER BY seq DESC LIMIT $2",
                thread_id, MAX_HISTORY_ROUNDS,
            )
    except Exception as exc:
        # Без истории уточнение станет самостоятельным вопросом — хуже, но
        # рабочее; валить исследование из-за этого нельзя.
        log(logging.WARNING, "insights.ask: история ветки недоступна",
            thread_id=thread_id, error=str(exc)[:200])
        return []

    out: list[dict] = []
    for row in reversed(rows):
        result = _load_json(row["result"]) or {}
        trace = _load_json(row["trace"]) or []
        out.append({
            "seq": row["seq"],
            "question": row["question"],
            "answer": result.get("answer"),
            "findings": result.get("findings") or [],
            # Только имена инструментов: аргументы прошлых шагов агенту не
            # нужны, а места занимают много.
            "tools": sorted({t.get("tool") for t in trace if t.get("tool")}),
        })
    return out


async def next_seq(thread_id: str) -> int:
    """Номер следующего раунда в ветке."""
    from app.db import pool

    async with pool().acquire() as conn:
        return (await conn.fetchval(
            "SELECT coalesce(max(seq), 0) + 1 FROM cost_insight_questions "
            "WHERE thread_id = $1::uuid", thread_id)) or 1


async def running_jobs() -> int:
    """Сколько исследований выполняется прямо сейчас (без брошенных)."""
    from app.db import pool

    async with pool().acquire() as conn:
        return await conn.fetchval(
            "SELECT count(*) FROM cost_insight_questions "
            "WHERE status = 'running' "
            f"  AND updated_at > now() - interval '{STALE_JOB_MINUTES} minutes'"
        ) or 0


async def reap_stale_jobs() -> int:
    """Пометить брошенные задачи как неудачные. Возвращает их число."""
    from app.db import pool

    async with pool().acquire() as conn:
        result = await conn.execute(
            "UPDATE cost_insight_questions "
            "   SET status = 'failed', degraded = TRUE, updated_at = now(),"
            "       error = 'исследование прервано (перезапуск сервиса)' "
            " WHERE status = 'running' "
            f"   AND updated_at <= now() - interval '{STALE_JOB_MINUTES} minutes'"
        )
        try:
            return int(str(result).rsplit(" ", 1)[-1])
        except ValueError:
            return 0


async def create_job(req_hash: str, email: str, question: str, context: dict,
                     thread_id: str | None = None,
                     seq: int = 1) -> tuple[str, str]:
    """Создать задачу со статусом running. Возвращает (job_id, thread_id).

    Без *thread_id* открывается новая ветка: её идентификатором становится
    сам job_id первого раунда — отдельная генерация ничего не добавила бы.
    """
    from app.db import pool

    async with pool().acquire() as conn:
        row = await conn.fetchrow(
            "INSERT INTO cost_insight_questions ("
            "  request_hash, email, question, context, model, status, job_id,"
            "  thread_id, seq"
            ") VALUES ($1,$2,$3,$4::jsonb,$5,'running',gen_random_uuid(),"
            "          coalesce($6::uuid, gen_random_uuid()),$7) "
            "RETURNING job_id, thread_id",
            req_hash, email, question[:2000],
            json.dumps(context or {}, ensure_ascii=False),
            insights._model(), thread_id, seq,
        )
        return str(row["job_id"]), str(row["thread_id"])


async def update_progress(job_id: str, step: int, tool: str,
                          sql_calls: int) -> None:
    """Отметить текущий шаг. Ошибку глотаем: прогресс — не результат."""
    from app.db import pool

    try:
        async with pool().acquire() as conn:
            await conn.execute(
                "UPDATE cost_insight_questions "
                "   SET progress = $2::jsonb, steps = $3, updated_at = now() "
                " WHERE job_id = $1::uuid",
                job_id,
                json.dumps({"step": step, "tool": tool, "sql_calls": sql_calls},
                           ensure_ascii=False),
                step,
            )
    except Exception as exc:
        log(logging.WARNING, "insights.ask: прогресс не записан",
            job_id=job_id, error=str(exc)[:200])


async def finish_job(job_id: str, result: dict) -> None:
    """Записать итог исследования и закрыть задачу."""
    from app.db import pool

    usage = result.get("usage") or {}
    # rescued обязан попасть в сохранённый результат: без него ответ, собранный
    # после сбоя по неполным данным, в интерфейсе выглядит как полноценный.
    payload = {k: result.get(k) for k in
               ("answer", "findings", "data_quality", "next_steps", "confidence",
                "rescued")}
    status = "failed" if result.get("degraded") else "done"
    try:
        async with pool().acquire() as conn:
            await conn.execute(
                "UPDATE cost_insight_questions SET"
                "  status = $2, result = $3::jsonb, trace = $4::jsonb,"
                "  steps = $5, sql_calls = $6, degraded = $7, error = $8,"
                "  prompt_tokens = $9, completion_tokens = $10, cost = $11,"
                "  elapsed_ms = $12, model = $13, updated_at = now() "
                "WHERE job_id = $1::uuid",
                job_id, status,
                json.dumps(payload, ensure_ascii=False),
                json.dumps(result.get("trace") or [], ensure_ascii=False),
                result.get("steps") or 0, result.get("sql_calls") or 0,
                bool(result.get("degraded")), str(result.get("error") or "")[:500],
                usage.get("prompt_tokens"), usage.get("completion_tokens"),
                str(usage.get("cost") or ""), result.get("elapsed_ms"),
                result.get("model") or insights._model(),
            )
    except Exception as exc:
        # Результат потерян — но задачу надо снять со running, иначе она займёт
        # слот до уборки, а пользователь будет ждать вечно.
        log(logging.ERROR, "insights.ask: итог не записан", job_id=job_id,
            error=str(exc)[:300])
        try:
            async with pool().acquire() as conn:
                await conn.execute(
                    "UPDATE cost_insight_questions "
                    "   SET status = 'failed', degraded = TRUE, updated_at = now(),"
                    "       error = 'результат не удалось сохранить' "
                    " WHERE job_id = $1::uuid", job_id)
        except Exception:
            pass


async def get_job(job_id: str) -> dict | None:
    """Состояние задачи для опроса. None — задачи с таким id нет."""
    from app.db import pool

    try:
        async with pool().acquire() as conn:
            row = await conn.fetchrow(
                "SELECT job_id, thread_id, seq, status, question, progress,"
                "       result, trace, model, steps, sql_calls, degraded, error,"
                "       elapsed_ms, cost, prompt_tokens, completion_tokens,"
                "       created_at, updated_at "
                "FROM cost_insight_questions WHERE job_id = $1::uuid",
                job_id,
            )
    except Exception as exc:
        log(logging.WARNING, "insights.ask: статус недоступен",
            job_id=job_id, error=str(exc)[:200])
        return None

    if not row:
        return None

    # Задача, чей процесс умер: строка осталась running, но её никто не двигает.
    # Показываем это как ошибку, а не как бесконечную работу.
    stale = (row["status"] == "running"
             and row["updated_at"] is not None
             and (time.time() - row["updated_at"].timestamp()) > STALE_JOB_MINUTES * 60)

    out: dict[str, Any] = {
        "job_id": str(row["job_id"]),
        "thread_id": str(row["thread_id"]),
        "seq": row["seq"],
        "status": "failed" if stale else row["status"],
        "question": row["question"],
        "progress": _load_json(row["progress"]) or {},
        "model": row["model"],
        "steps": row["steps"],
        "sql_calls": row["sql_calls"],
        "started_at": row["created_at"].isoformat() if row["created_at"] else None,
    }

    if out["status"] == "running":
        return out

    out.update(_load_json(row["result"]) or {})
    out["trace"] = _load_json(row["trace"]) or []
    out["degraded"] = bool(row["degraded"]) or stale
    out["error"] = ("исследование прервано (перезапуск сервиса)" if stale
                    else (row["error"] or None))
    out["elapsed_ms"] = row["elapsed_ms"]
    out["usage"] = {"prompt_tokens": row["prompt_tokens"],
                    "completion_tokens": row["completion_tokens"],
                    "cost": row["cost"]}
    return out


# ─── Агентный цикл ───────────────────────────────────────────────────────────

async def investigate(question: str, context: dict, on_progress: Any = None,
                      history: list[dict] | None = None) -> dict:
    """Многошаговое исследование вопроса по данным раздела.

    Возвращает ответ, наблюдения и ТРАССУ шагов: какой инструмент с какими
    фильтрами вызывался. Трасса обязательна в выдаче — без неё вывод модели
    невозможно проверить, а «ИИ сказал» в финансах уносится на совещание как
    факт.

    *on_progress* — необязательная корутина `(step, tool, sql_calls)`, которую
    зовут после каждого обращения к данным. Нужна асинхронному запуску, чтобы
    показывать в интерфейсе, чем агент занят прямо сейчас.
    """
    from app import llm_settings

    started = time.monotonic()
    # Правка в админке должна применяться к следующему исследованию, а не
    # после рестарта сервиса.
    await llm_settings.refresh()

    cache = _Cache()
    trace: list[dict] = []
    usage_total = {"prompt_tokens": 0, "completion_tokens": 0}
    costs: list[str] = []

    messages: list[dict] = [
        {"role": "system", "content": SYSTEM_PROMPT},
        {"role": "user", "content": build_user_prompt(question, context, history)},
    ]

    result: dict[str, Any] = {
        "question": question,
        "trace": trace,
        "model": insights._model(),
        "degraded": False,
        "error": None,
    }

    def finish(payload: dict | None = None) -> dict:
        if payload:
            result.update(payload)
        result["elapsed_ms"] = int((time.monotonic() - started) * 1000)
        result["steps"] = len(trace)
        result["sql_calls"] = cache.sql_calls
        result["usage"] = {**usage_total, "cost": ";".join(c for c in costs if c)}
        return result

    if not insights.llm_enabled():
        return finish({"degraded": True, "error": "исследование не настроено (COST_LLM_*)"})

    tool_calls_made = 0
    # Повтор «ответь короче» делаем один раз: если и он не разобрался,
    # дело не в длине, и второй заход только потратит время.
    retried_short = False

    for step in range(MAX_STEPS):
        # Инструменты отключаются, когда исчерпан ЛЮБОЙ из трёх лимитов: иначе
        # модель уходит в новый вызов вместо ответа и цикл упирается в предел
        # без вывода. Отключение — не отказ: модель делает вывод по собранному.
        spent = time.monotonic() - started
        last = (
            step == MAX_STEPS - 1
            or tool_calls_made >= MAX_TOOL_CALLS
            # Резерв на сам финальный вызов: иначе бюджет исчерпывается ровно
            # перед ним, и ответ не успевает прийти до обрыва прокси.
            or spent >= max(0.0, _time_budget() - FINAL_CALL_RESERVE_S)
        )
        if last and step > 0:
            messages.append({
                "role": "user",
                "content": "Лимит шагов исследования исчерпан. Дай финальный "
                           "ответ в требуемом формате JSON по уже собранным "
                           "данным; если чего-то не хватило — укажи это в "
                           "next_steps и понизь confidence.",
            })

        _trim_history(messages)
        try:
            out = await _chat(messages, with_tools=not last)
        except AgentError as exc:
            log(logging.WARNING, "insights.ask: шаг не удался", step=step,
                error=str(exc)[:200], model=insights._model(),
                collected=len(trace))
            # Данные уже собраны — пробуем получить вывод по ним, вместо того
            # чтобы выбросить минуту работы из-за одного сбойного вызова.
            rescued = await _rescue_answer(messages, trace)
            if rescued is not None:
                log(logging.INFO, "insights.ask: ответ восстановлен после сбоя",
                    collected=len(trace), error=str(exc)[:120])
                rescued["error"] = (f"исследование прервано ({exc}), вывод сделан "
                                    f"по собранным данным")
                return finish(rescued)
            return finish({"degraded": True, "error": str(exc)})

        usage = out["usage"]
        usage_total["prompt_tokens"] += usage.get("prompt_tokens") or 0
        usage_total["completion_tokens"] += usage.get("completion_tokens") or 0
        if out.get("cost"):
            costs.append(str(out["cost"]))

        message = out["message"]
        calls = message.get("tool_calls") or []

        if not calls:
            content = (message.get("content") or "").strip()
            if not content:
                content = (message.get("reasoning_content") or "").strip()
            if not content:
                return finish({"degraded": True,
                               "error": "модель вернула пустой ответ"})
            parsed = _parse_final(content)
            if parsed is not None:
                return finish(parsed)

            # Ответ не разобрался. Раньше сырой текст уходил пользователю в
            # поле answer, обрезанный по 2000 символов, — он видел кусок JSON
            # (жалоба заказчика 02.09.2026). Логируем и просим ответить ЕЩЁ РАЗ
            # и короче: обычно причина в упоре в лимит токенов на длинных
            # evidence и next_steps.
            log(logging.WARNING, "insights.ask: финальный ответ не разобран",
                finish_reason=out["finish_reason"], raw=content[:400],
                length=len(content), model=insights._model())

            if retried_short:
                return finish({"degraded": True, "error":
                               "модель не смогла вернуть ответ в нужном формате"})
            retried_short = True
            messages.append({
                "role": "user",
                "content": "Ответ не разобран — он оборвался. Повтори тот же "
                           "вывод СТРОГО в требуемом JSON, но КОРОТКО: не более "
                           "3 findings, evidence и next_steps до 100 символов, "
                           "data_quality одной фразой.",
            })
            continue

        # История обязана содержать сам запрос на вызов — иначе следующий вызов
        # модели не свяжет результаты с tool_call_id и начнёт заново.
        messages.append({
            "role": "assistant",
            "content": message.get("content") or "",
            "tool_calls": calls,
        })

        # Ограничение и на шаг (3), и на исследование в целом: без второго
        # шесть шагов дали бы восемнадцать обращений к витрине.
        # Ограничение и на шаг (3), и на исследование в целом. Отвечать НУЖНО на
        # каждый запрошенный вызов, даже отказом: у tool-сообщения есть
        # tool_call_id, и assistant-реплика с вызовами без парных ответов делает
        # следующий запрос к модели невалидным.
        allowed = max(0, min(3, MAX_TOOL_CALLS - tool_calls_made))
        for index, call in enumerate(calls):
            fn = call.get("function") or {}
            name = fn.get("name") or ""
            try:
                args = json.loads(fn.get("arguments") or "{}")
            except json.JSONDecodeError:
                args = {}

            if index >= allowed:
                payload: Any = {"ошибка": "лимит обращений к данным исчерпан, "
                                          "делай вывод по уже собранному"}
                messages.append({
                    "role": "tool",
                    "tool_call_id": call.get("id"),
                    "content": json.dumps(payload, ensure_ascii=False),
                })
                continue

            tool_calls_made += 1
            handler = _TOOLS.get(name)
            if handler is None:
                payload = {"ошибка": f"инструмента «{name}» нет"}
            else:
                try:
                    payload = await handler(cache, args)
                except Exception as exc:
                    # Ошибку инструмента возвращаем МОДЕЛИ, а не наверх: она
                    # обычно означает неверный фильтр, и модель исправляет его
                    # сама следующим шагом.
                    log(logging.WARNING, "insights.ask: инструмент упал",
                        tool=name, error=str(exc)[:200])
                    payload = {"ошибка": f"{type(exc).__name__}: {str(exc)[:200]}"}

            trace.append({"step": len(trace) + 1, "tool": name,
                          "args": args, "ok": "ошибка" not in payload})
            if on_progress is not None:
                # Прогресс — чтобы пользователь видел, что агент делает, а не
                # пустой спиннер минуту. Падение отчёта не должно валить
                # исследование: оно уже идёт и стоит денег.
                try:
                    await on_progress(len(trace), name, cache.sql_calls)
                except Exception as exc:
                    log(logging.WARNING, "insights.ask: отчёт о прогрессе упал",
                        error=str(exc)[:200])
            messages.append({
                "role": "tool",
                "tool_call_id": call.get("id"),
                "content": json.dumps(payload, ensure_ascii=False),
            })

    return finish({"degraded": True,
                   "error": f"исследование не завершилось за {MAX_STEPS} шагов"})
