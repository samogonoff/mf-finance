"""Разбор блока графиков раздела «Себестоимость» языковой моделью.

Блок «Разбор ИИ» ставится под ЛОГИЧЕСКОЙ СЕКЦИЕЙ страницы (например «Маржа
выпуска» целиком, а не под каждой карточкой): так модель видит динамику маржи
и динамику маржинальности вместе и может их сопоставить — по одному графику
такое наблюдение не построить.

Принцип, на котором держится весь модуль: **арифметику считает код, а модель
только интерпретирует посчитанное**. `compute_facts` детерминированно
вытаскивает из каждой серии максимумы, минимумы, изменение от начала к концу,
доли и выбросы; в промпт уходят готовые числа, и модель получает прямой запрет
считать самостоятельно. Поэтому:

* цифры в тексте не выдумываются — их посчитал Python;
* при недоступной модели блок не пустой: те же факты отдаются как есть;
* числа в ответе сверяются с входными (`_verify_numbers`), несошедшиеся
  помечаются — UI показывает их как расчёт модели, а не как данные.

Причинно-следственные утверждения запрещены промптом: в срезе нет данных о
причинах, а «маржа упала из-за подорожания сырья» уносится на совещание как
факт.
"""

from __future__ import annotations

import hashlib
import json
import logging
import os
import re
import time
from typing import Any

import httpx

from app.logship import log

# ─── Конфигурация ────────────────────────────────────────────────────────────

def _env(name: str, default: str = "") -> str:
    return (os.environ.get(name) or default).strip()


def llm_enabled() -> bool:
    """Настроен ли разбор. Без ключа блок не показывается — не дразним кнопкой.

    Значения берутся из настроек админки (`cost_llm_settings`), а при пустых
    полях — из окружения; см. app/llm_settings.py.
    """
    from app import llm_settings
    return llm_settings.enabled()


def _api_key() -> str:
    from app import llm_settings
    return llm_settings.value("api_key", "COST_LLM_API_KEY")


def _api_base() -> str:
    from app import llm_settings
    return llm_settings.value(
        "api_base", "COST_LLM_API_BASE", "https://opencode.ai/zen/v1").rstrip("/")


def _model() -> str:
    """Модель для разбора.

    Дефолт — БЕЗ режима размышлений, и это принципиально. Замер 01.09.2026 на
    блоке «Маржа выпуска» (2 графика, 13 фактов):
      deepseek-v4-flash  7 с,  0.00045 $  — стабильно
      glm-5             28–50 с, 0.0078 $ — половина вызовов падает, «думающая»
                                            модель съедает весь лимит токенов на
                                            рассуждения и до JSON не доходит
                                            (finish_reason=length)
    """
    from app import llm_settings
    return llm_settings.value("model", "COST_LLM_MODEL", "deepseek-v4-flash")


def _timeout() -> float:
    from app import llm_settings
    return float(llm_settings.int_value("timeout_s", "COST_LLM_TIMEOUT", 60))


# Границы входа. Серии под графиками — это десятки чисел на карточку; всё, что
# кардинально больше, означает, что шлют сырую выгрузку, а не данные графиков.
MAX_CHARTS = 8
MAX_LABELS = 200
MAX_DATASETS_PER_CHART = 12
MAX_VALUES_TOTAL = 1500

# ─── Глоссарий раздела ───────────────────────────────────────────────────────
# Смыслы, которых нет ни в схеме, ни в подписях графика. Без них модель
# смешивает план с фактом и усредняет проценты.

GLOSSARY = """Термины раздела «Себестоимость» (Markformelle):
- Признак калькуляции: ПКПСС, КПСС, ПФКСС — ПЛАНОВЫЕ расчёты; ФКСС — ФАКТ.
  Плановые и фактические показатели нельзя складывать и усреднять вместе.
- Цена в разделе задаётся на связку план + модель + артикул + признак
  калькуляции, а не на модель целиком.
- Level 01..Level 05 — иерархия товарных групп, от верхнего уровня к нижнему.
- ЧНИ — чулочно-носочные изделия: группы Level 01 «Носки&Колготки» и «Orodoro».
- Маржинальность = маржа / выпуск в отпускных ценах, в процентах.
- «пп» — процентные пункты (разница двух процентов), не проценты.
- Суммы бывают в BYN и USD; показатели в разных валютах несопоставимы.
- Норматив — расчёт по нормативной ставке минуты, факт — по фактической."""

# ─── Детерминированный расчёт фактов ─────────────────────────────────────────

def _nums(values: list[Any]) -> list[float]:
    out: list[float] = []
    for v in values:
        if isinstance(v, bool) or v is None:
            continue
        if isinstance(v, (int, float)):
            out.append(float(v))
    return out


def _fmt(x: float) -> str:
    """Число для промпта: без экспоненты и без хвоста нулей.

    Через `%g` суммы маржи в десятки миллионов превращались в «2.60315e+07» —
    модель такое читает плохо, а человек в списке фактов ещё хуже. Крупные
    значения округляем до целого: копейки в сумме за месяц смысла не несут.
    """
    if abs(x) >= 10000:
        return f"{round(x):d}"
    if x == int(x):
        return str(int(x))
    return f"{x:.2f}".rstrip("0").rstrip(".")


def _lbl(value: Any) -> str:
    """Подпись точки или пустая строка.

    Отсутствующую подпись нельзя подставлять в текст: в фактах появлялось
    «максимум 26031500 (None)», и модель принимала None за название месяца.
    """
    if value is None:
        return ""
    text = str(value).strip()
    return "" if text.lower() in ("none", "null", "nan", "undefined") else text


def _is_relative(series_name: str) -> bool:
    """Серия относительная (проценты, пункты, темпы)?

    Для таких сумма и «доля в сумме» лишены смысла: сложение процентов по
    месяцам ничего не означает, а модель послушно повторяла посчитанное — в
    выдаче появлялось «доля июня в сумме маржинальности 13,47%». Определяем по
    подписи серии: она приходит с графика и у нас всегда несёт единицу
    измерения («Маржинальность, %», «Отклонение, пп», «Темп роста к прошлому
    году, %»).
    """
    low = series_name.lower()
    return "%" in low or "пп" in low.split() or "темп" in low


_MONTH_WORDS = ("янв", "фев", "мар", "апр", "май", "июн", "июл", "авг",
                "сен", "окт", "ноя", "дек")
_PERIOD_RE = re.compile(r"(^|\D)(19|20)\d{2}(\D|$)")


def _is_time_series(labels: list[str]) -> bool:
    """Похожи ли подписи на периоды (месяцы, годы)?

    Нужно детектору незакрытого периода: «последняя точка аномально мала» имеет
    смысл только на временной оси. На разрезе по бренд-менеджерам он выдал
    «последняя точка (МАСЛЕННИКОВА А.) почти наверняка не закрыта» — там просто
    самый маленький из категорий, и это норма, а не неполные данные
    (проверено 03.09.2026 на коммерческом дашборде).
    """
    if len(labels) < 3:
        return False
    hits = sum(1 for lbl in labels
               if lbl and (_PERIOD_RE.search(lbl)
                           or lbl.strip().lower()[:3] in _MONTH_WORDS))
    # Больше половины подписей похожи на периоды — считаем ряд временным.
    return hits * 2 > len(labels)


def chart_facts(chart: dict) -> list[str]:
    """Факты по одному графику — посчитанные, а не угаданные."""
    facts: list[str] = []
    title = str(chart.get("title") or "график")
    series = chart.get("series") or {}
    labels = [_lbl(x) for x in (series.get("labels") or [])]
    datasets = series.get("datasets") or []
    # Детектор незакрытого периода имеет смысл только на временной оси.
    time_series = _is_time_series(labels)

    for ds in datasets[:MAX_DATASETS_PER_CHART]:
        name = str(ds.get("label") or "серия")
        data = _nums(list(ds.get("data") or []))
        if not data:
            continue

        pairs = list(zip(labels, data)) if len(labels) == len(data) else [("", v) for v in data]
        hi_lbl, hi = max(pairs, key=lambda p: p[1])
        lo_lbl, lo = min(pairs, key=lambda p: p[1])
        total = sum(data)
        relative = _is_relative(name)

        head = (f"[{title}] «{name}»: точек {len(data)}, максимум {_fmt(hi)}"
                + (f" ({hi_lbl})" if hi_lbl else "")
                + f", минимум {_fmt(lo)}"
                + (f" ({lo_lbl})" if lo_lbl else ""))
        if relative:
            # Для процентов сумма бессмысленна, а невзвешенное среднее по
            # периодам вводит в заблуждение — даём медиану как устойчивую
            # характеристику уровня.
            ordered = sorted(data)
            median = ordered[len(ordered) // 2]
            head += f", медиана {_fmt(median)} (сумма для этой серии не считается)"
        else:
            head += f", сумма {_fmt(total)}, среднее {_fmt(total / len(data))}"
        facts.append(head)

        # Изменение от первой точки к последней — для временных рядов это то,
        # что человек хочет прочитать первым.
        if len(data) >= 2 and data[0] != 0:
            delta = data[-1] - data[0]
            pct = delta / abs(data[0]) * 100
            facts.append(
                f"[{title}] «{name}»: от начала ({_fmt(data[0])}"
                + (f", {labels[0]}" if labels else "")
                + f") к концу ({_fmt(data[-1])}"
                + (f", {labels[-1]}" if labels else "")
                + f") изменение {_fmt(delta)} ({_fmt(pct)}%)"
            )

        neg = [(lbl, v) for lbl, v in pairs if v < 0]
        if neg:
            where = ", ".join(f"{lbl or '?'}: {_fmt(v)}" for lbl, v in neg[:5])
            facts.append(f"[{title}] «{name}»: отрицательные значения — {where}")

        # Незакрытый последний период. На дашборде маржи текущий месяц идёт
        # рядом с закрытыми, и в сентябре с двумя рабочими днями маржа
        # оказывается в десятки тысяч раз меньше январской. Без этой пометки
        # модель читала такое как обвал и писала «падение на 99,99%» — вывод
        # бессмысленный, потому что сравниваются два дня с месяцем.
        #
        # Определяем по данным серии, без знания календаря: последняя точка
        # аномально мала относительно медианы остальных. Только для серий,
        # где все значения одного знака и не процентные по смыслу — для доли
        # или темпа роста «мало» не означает «неполный период».
        if len(data) >= 4 and all(v >= 0 for v in data) and time_series:
            rest = sorted(data[:-1])
            median = rest[len(rest) // 2]
            last = data[-1]
            if median > 0 and last < median * 0.2:
                share = last / median * 100
                facts.append(
                    f"[{title}] «{name}»: ВНИМАНИЕ, последняя точка"
                    + (f" ({labels[-1]})" if labels else "")
                    + f" = {_fmt(last)}, это {_fmt(share)}% от медианы остальных "
                      f"({_fmt(median)}) — период почти наверняка НЕ ЗАКРЫТ и "
                      f"несопоставим с полными периодами"
                )

        # Доля лидера считается только для неотрицательных АБСОЛЮТНЫХ серий: на
        # серии со знаком «доля от суммы» лишена смысла, а на процентной —
        # вдвойне (доля процента в сумме процентов ничего не значит).
        if len(data) >= 3 and total > 0 and not relative and all(v >= 0 for v in data):
            facts.append(
                f"[{title}] «{name}»: доля наибольшего значения в сумме "
                f"{_fmt(hi / total * 100)}%" + (f" ({hi_lbl})" if hi_lbl else "")
            )

    # Сопоставление двух серий по одинаковым подписям — «текущий против
    # прошлого года» и «норматив против факта» рисуются именно так.
    if len(datasets) >= 2:
        a, b = datasets[0], datasets[1]
        da, db = _nums(list(a.get("data") or [])), _nums(list(b.get("data") or []))
        if da and len(da) == len(db):
            diffs = [x - y for x, y in zip(da, db)]
            lbls = labels if len(labels) == len(diffs) else [""] * len(diffs)
            mx_lbl, mx = max(zip(lbls, diffs), key=lambda p: abs(p[1]))
            facts.append(
                f"[{title}] разница «{a.get('label')}» − «{b.get('label')}»: "
                f"наибольшая по модулю {_fmt(mx)}"
                + (f" ({mx_lbl})" if mx_lbl else "")
                + f", средняя {_fmt(sum(diffs) / len(diffs))}"
            )

    return facts


def compute_facts(charts: list[dict]) -> list[str]:
    """Факты по всем графикам блока, в порядке карточек на странице."""
    facts: list[str] = []
    for chart in charts:
        facts.extend(chart_facts(chart))
    return facts


def _all_numbers(charts: list[dict], facts: list[str]) -> list[float]:
    """Числа, которые модель имеет право называть: из серий и из фактов."""
    pool: list[float] = []
    for chart in charts:
        for ds in (chart.get("series") or {}).get("datasets") or []:
            pool.extend(_nums(list(ds.get("data") or [])))
    for f in facts:
        pool.extend(float(m) for m in re.findall(r"-?\d+(?:\.\d+)?", f.replace(" ", "")))
    return pool


_NUM_RE = re.compile(r"-?\d[\d\s ]*(?:[.,]\d+)?")

# Модель пишет типографский минус, а не ASCII-дефис: «−51.07 пп». Без
# нормализации знак терялся, число сравнивалось как положительное и уходило в
# «непроверенные» — ложная тревога на совершенно корректном наблюдении.
_MINUS_CHARS = str.maketrans({"−": "-", "–": "-", "—": "-"})

# Тире МЕЖДУ цифрами — это диапазон («13510541–17513683»), а не минус. Правка
# знака без этой оговорки делала второе число отрицательным, и оно тут же
# помечалось непроверенным. Разделитель вырезаем до нормализации знаков.
_RANGE_DASH_RE = re.compile(r"(?<=\d)\s*[–—]\s*(?=\d)")

# Знак СЛОВОМ. «минус 14,2 пп» модель берёт из фактов, где посчитано «-14.2»,
# но валидатор видел «14,2» без знака, в пуле такого числа не было, и
# совершенно корректное наблюдение помечалось непроверенным (08.09.2026, после
# того как наблюдениям разрешили содержать точку сравнения — а значит и
# разницу, которую код уже считает сам).
_WORD_SIGN_RE = re.compile(r"\b(минус|плюс)\s+(?=[\d−–—-]*\d)", re.IGNORECASE)


def _normalize_signs(text: str) -> str:
    text = _RANGE_DASH_RE.sub(" … ", text)
    text = _WORD_SIGN_RE.sub(lambda m: "-" if m.group(1).lower() == "минус" else "", text)
    return text.translate(_MINUS_CHARS)


# Слова, при которых число без знака означает отрицательную величину.
_DOWNWARD_WORDS = ("ниже", "меньше", "хуже", "отстава", "разрыв",
                   "отклонен", "падени", "снижени", "просад", "недоб",
                   "отрицатель", "потер", "убыт")


def _close(a: float, b: float) -> bool:
    """Совпадение с допуском: 1 % относительной или 0,05 абсолютной."""
    return abs(a - b) <= max(0.05, abs(b) * 0.01)


def _verify_numbers(text: str, pool: list[float]) -> list[str]:
    """Числа из текста, которых нет во входных данных.

    Допуск: 1 % относительной или 0,05 абсолютной погрешности — модель законно
    округляет «19,94» до «19,9». Годы и мелкие целые (счётчики, «2 из 5») не
    проверяются: они не являются показателями.
    """
    normalized = _normalize_signs(text)
    unverified: list[str] = []
    for m in _NUM_RE.finditer(normalized):
        raw = m.group(0)
        cleaned = raw.replace(" ", "").replace(" ", "").replace(",", ".")
        try:
            val = float(cleaned)
        except ValueError:
            continue
        if val == int(val) and abs(val) <= 3000:
            continue

        if any(_close(val, p) for p in pool):
            continue

        # Знак, выраженный СЛОВОМ НАПРАВЛЕНИЯ: «на 14,2 пп ниже норматива» —
        # это отрицательная разница, посчитанная в фактах как «-14.2».
        # Слово «минус» снимает _normalize_signs, а «ниже»/«отставание»
        # остаются, и корректное наблюдение помечалось непроверенным
        # (08.09.2026, после того как наблюдениям разрешили точку
        # сравнения). Отрицательное соответствие принимаем ТОЛЬКО при
        # таком маркере рядом: сверка по модулю без него перестала бы
        # ловить перевёрнутый знак, а «маржа +14,2 пп» вместо «-14,2 пп» —
        # не опечатка, а противоположный вывод.
        window = normalized[max(0, m.start() - 60):m.end() + 60].lower()
        if (any(w in window for w in _DOWNWARD_WORDS)
                and any(p < 0 and _close(-val, p) for p in pool)):
            continue

        unverified.append(raw.strip())
    return unverified


# ─── Промпт ──────────────────────────────────────────────────────────────────

SYSTEM_PROMPT = """Ты аналитик производственной себестоимости на швейном предприятии.
Тебе дают факты, ПОСЧИТАННЫЕ по данным блока графиков одного раздела дашборда,
и ты пишешь короткие наблюдения для финансиста.

Жёсткие правила:
1. Пиши ТОЛЬКО по-русски.
2. Используй ТОЛЬКО числа из раздела «Посчитанные факты». Не считай сам, не
   складывай, не выводи новых величин.
3. НЕ объясняй причины. В данных нет информации о причинах: запрещено писать
   «из-за», «потому что», «вследствие», предполагать подорожание сырья, курс,
   действия сотрудников.
4. Не давай указаний и рекомендаций («нужно», «следует», «рекомендую»).
5. Не смешивай плановые признаки калькуляции с фактом (ФКСС).
6. Проценты не складывай; разницу процентов называй в процентных пунктах (пп).
7. НЕЗАКРЫТЫЙ ПЕРИОД. Если в фактах есть пометка «период почти наверняка НЕ
   ЗАКРЫТ», по этой точке НЕЛЬЗЯ делать вывод о падении, ухудшении или обвале:
   там просто меньше дней. Первым наблюдением скажи, что последний период
   неполный и в сравнение не годится, а остальные наблюдения строй по закрытым
   периодам.
8. ОХВАТ ДАННЫХ. Смотри в контексте пункт «Что показано на графиках»: если там
   сказано, что графики динамики игнорируют фильтр месяца, не пиши «за
   выбранный период» — пиши «за месяцы года, показанные на графике».
9. От 3 до 5 наблюдений, каждое до 350 символов. Наблюдение — это не
   констатация числа, а число с точкой сравнения: с соседним периодом,
   соседней категорией, медианой или нормативом, и что из этого следует.
   «Маржа в июне 41,2 %» — плохо; «маржа в июне 41,2 % против 52,7 % в
   мае, минимум из показанных месяцев» — хорошо.
10. Больше цени наблюдения, СВЯЗЫВАЮЩИЕ разные графики блока, чем пересказ
    одного графика.
11. Если факты не показывают ничего содержательного, верни одно наблюдение об
    этом. Не придумывай значимость.

Отвечай СТРОГО одним JSON-объектом без markdown:
{"observations":[{"text":"...","kind":"neutral|attention|risk"}]}
kind: neutral — констатация, attention — стоит посмотреть, risk — тревожный
признак (падение, отрицательные значения, разрыв норматив/факт)."""


def build_user_prompt(block_title: str, charts: list[dict], context: dict,
                      facts: list[str]) -> str:
    ctx_lines = [f"- {k}: {v}" for k, v in (context or {}).items() if v not in (None, "", [])]
    chart_lines = []
    for chart in charts:
        line = f"- {chart.get('title')}"
        if chart.get("note"):
            line += f" — {chart['note']}"
        chart_lines.append(line)

    return "\n\n".join(filter(None, [
        GLOSSARY,
        f"Раздел дашборда: {block_title}",
        "Графики блока:\n" + "\n".join(chart_lines) if chart_lines else "",
        "Контекст среза:\n" + "\n".join(ctx_lines) if ctx_lines else "",
        "Посчитанные факты (единственный источник чисел):\n"
        + "\n".join(f"- {f}" for f in facts),
    ]))


# ─── Вызов модели ────────────────────────────────────────────────────────────

class InsightError(RuntimeError):
    """Модель недоступна или ответила неразборчиво."""


# Сетевой таймаут провайдера — не приговор, а рядовое явление: 02.09.2026 при
# общей заторможенности opencode zen один ReadTimeout убивал и обзор блока, и
# исследование на 86 секунд собранных данных. Один повтор снимает большинство
# таких случаев; больше не делаем, чтобы не удваивать ожидание пользователя на
# реально мёртвом провайдере.
RETRY_ON_TIMEOUT = 1
RETRY_PAUSE_S = 1.5


def reasoning_effort() -> str:
    """Уровень внутренних размышлений модели. Пусто — параметр не отправляем.

    ДЕФОЛТ «none» — САМАЯ ВАЖНАЯ НАСТРОЙКА ЗДЕСЬ. Замер 02.09.2026 на промпте
    агентского размера (~6,4 тыс. токенов), deepseek-v4-flash:

        без параметра        17–127 с, генерация 1448–9571 токенов
        reasoning_effort=none  2–3 с, генерация 24–28 токенов

    В 27 раз быстрее. `max_tokens` размышления НЕ ограничивает: провайдер при
    `max_tokens=300` возвращал completion под 9,6 тыс. токенов. Именно этим и
    объяснялись ReadTimeout — модель думала дольше таймаута, а канал до провайдера
    при этом идеален (TCP 1 мс, TLS 395 мс, прокси в контейнере нет).

    Оставить размышления можно, задав low/medium/high — но тогда поднимайте и
    COST_LLM_TIMEOUT, иначе вернутся обрывы.
    """
    from app import llm_settings
    return llm_settings.value("reasoning", "COST_LLM_REASONING", "none")


def thinking_param() -> dict[str, Any] | None:
    """Параметр отключения размышлений для провайдеров, где своя ручка.

    `reasoning_effort=none` понимают не все. У GLM через z.ai размышления
    включены по умолчанию и выключаются отдельным полем
    `thinking: {"type": "disabled"}` (GLM-4.5 и выше), а `reasoning_effort`
    поддержан только с GLM-5.2. Отправляем оба: лишнее поле провайдер либо
    игнорирует, либо отвергает — и тогда post_chat выбросит его сам.
    """
    return {"type": "disabled"} if reasoning_effort() == "none" else None


# Необязательные поля запроса, которые провайдеры поддерживают вразнобой.
# При отказе (400/404/422) выбрасываются ПО ОДНОМУ, и запрос повторяется: так
# сборка работает и там, где поле не знают, без правки кода под провайдера.
DROPPABLE_FIELDS = ("thinking", "reasoning_effort", "response_format")


async def post_chat(payload: dict, timeout: float | None = None) -> httpx.Response:
    """POST в /chat/completions с повтором на таймауте и откатом полей.

    Повторяем сетевые сбои (таймаут, обрыв) и отказы из-за неподдерживаемого
    необязательного поля. Остальные коды возвращаются как есть: их смысл
    разбирает вызывающий, а повтор там бесполезен (429 — лимит) или вреден.
    """
    import asyncio

    headers = {
        "Authorization": f"Bearer {_api_key()}",
        "Content-Type": "application/json",
    }
    url = f"{_api_base()}/chat/completions"
    body = dict(payload)
    last: Exception | None = None
    network_attempts = 0

    # Попыток хватает на: сетевые повторы + по одному откату каждого поля.
    for _ in range(RETRY_ON_TIMEOUT + len(DROPPABLE_FIELDS) + 1):
        try:
            # trust_env=False: на машинах разработчиков задан системный прокси,
            # который перехватывает исходящие вызовы и роняет их.
            async with httpx.AsyncClient(timeout=timeout or _timeout(),
                                         trust_env=False) as client:
                resp = await client.post(url, json=body, headers=headers)
        except (httpx.TimeoutException, httpx.TransportError) as exc:
            last = exc
            network_attempts += 1
            if network_attempts > RETRY_ON_TIMEOUT:
                break
            log(logging.WARNING, "insights: повтор запроса к модели",
                attempt=network_attempts, error=type(exc).__name__, model=_model())
            await asyncio.sleep(RETRY_PAUSE_S)
            continue

        if resp.status_code in (400, 404, 422):
            dropped = next((f for f in DROPPABLE_FIELDS if f in body), None)
            if dropped is not None:
                log(logging.INFO, "insights: провайдер не принял поле",
                    field=dropped, status=resp.status_code, model=_model())
                body.pop(dropped)
                continue

        return resp

    raise InsightError(f"модель недоступна: {type(last).__name__}")


def _parse_observations(content: str) -> list[dict]:
    """Достаём JSON из ответа, терпя markdown-обёртку и текст вокруг."""
    txt = re.sub(r"^```(?:json)?|```$", "", content.strip(), flags=re.MULTILINE).strip()
    try:
        data = json.loads(txt)
    except json.JSONDecodeError:
        m = re.search(r"\{.*\}", txt, re.DOTALL)
        if not m:
            raise InsightError("модель ответила не JSON")
        try:
            data = json.loads(m.group(0))
        except json.JSONDecodeError as exc:
            raise InsightError("модель ответила не JSON") from exc

    items = data.get("observations") if isinstance(data, dict) else data
    if not isinstance(items, list):
        raise InsightError("в ответе модели нет observations")

    out: list[dict] = []
    for it in items[:6]:
        if isinstance(it, str) and it.strip():
            out.append({"text": it.strip(), "kind": "neutral"})
        elif isinstance(it, dict) and it.get("text"):
            kind = str(it.get("kind") or "neutral").lower()
            out.append({
                "text": str(it["text"]).strip(),
                "kind": kind if kind in ("neutral", "attention", "risk") else "neutral",
            })
    if not out:
        raise InsightError("модель вернула пустой список наблюдений")
    return out


async def call_llm(system: str, user: str) -> tuple[list[dict], dict]:
    """POST в OpenAI-совместимый /chat/completions. Возвращает (наблюдения, usage).

    Лимит токенов щедрый намеренно. «Думающие» модели (glm-5, kimi-k2.x) сначала
    пишут рассуждение и только потом ответ: на 1200 токенах glm-5 упиралась в
    лимит посреди размышлений (`finish_reason=length`) и JSON не выдавала вовсе.
    """
    payload: dict[str, Any] = {
        "model": _model(),
        "messages": [
            {"role": "system", "content": system},
            {"role": "user", "content": user},
        ],
        "max_tokens": 4000,
        "temperature": 0.2,
        # Гарантия формата на стороне провайдера — надёжнее просьбы в промпте.
        # Поддерживают не все: при отказе post_chat повторит запрос без поля.
        "response_format": {"type": "json_object"},
    }
    effort = reasoning_effort()
    if effort:
        # Размышления модели max_tokens не ограничивает: без этого параметра
        # deepseek-v4-flash генерировала до 9,6 тыс. токенов и упиралась в
        # таймаут. См. докстринг reasoning_effort().
        payload["reasoning_effort"] = effort
    thinking = thinking_param()
    if thinking:
        payload["thinking"] = thinking
    resp = await post_chat(payload)

    if resp.status_code in (400, 404, 422) and "response_format" in payload:
        # Провайдер или модель не знают про json_object — пробуем без него,
        # формат тогда держится только промптом и парсером.
        log(logging.INFO, "insights: провайдер не принял response_format",
            model=_model(), status=resp.status_code)
        payload.pop("response_format")
        resp = await post_chat(payload)

    if resp.status_code != 200:
        try:
            detail = str((resp.json().get("error") or {}).get("message") or "")[:200]
        except Exception:
            detail = resp.text[:200]
        raise InsightError(f"модель вернула {resp.status_code}: {detail}")

    body = resp.json()
    try:
        message = body["choices"][0]["message"]
    except (KeyError, IndexError) as exc:
        raise InsightError("неожиданная структура ответа модели") from exc

    # У «думающих» моделей (glm-5, kimi-k2.x) содержательный ответ иногда
    # целиком уезжает в reasoning_content, а content приходит пустым. Без этого
    # фолбэка разбор падал с «модель ответила не JSON» на пустой строке.
    content = (message.get("content") or "").strip()
    if not content:
        content = (message.get("reasoning_content") or "").strip()

    if not content:
        raise InsightError("модель вернула пустой ответ")

    finish = (body["choices"][0] or {}).get("finish_reason")
    try:
        observations = _parse_observations(content)
    except InsightError:
        # Сырой ответ в лог: без него причина «не JSON» неотлаживаема, а
        # воспроизвести её на том же срезе получается не всегда.
        log(logging.WARNING, "insights: ответ модели не разобран",
            model=_model(), raw=content[:400], finish_reason=finish)
        if finish == "length":
            # Отдельный текст: «не JSON» тут вводит в заблуждение — модель не
            # ошиблась форматом, а не уложилась в лимит токенов.
            raise InsightError(
                "модель не уложилась в лимит токенов — попробуйте модель без "
                "режима размышлений (deepseek-v4-flash, minimax-m2.5)")
        raise

    usage = body.get("usage") or {}
    return observations, {
        "prompt_tokens": usage.get("prompt_tokens"),
        "completion_tokens": usage.get("completion_tokens"),
        "cost": body.get("cost"),
    }


# ─── Валидация входа ─────────────────────────────────────────────────────────

def validate_charts(charts: Any) -> list[dict]:
    """Проверка формы и размера блока графиков. Бросает ValueError с внятным текстом."""
    if not isinstance(charts, list) or not charts:
        raise ValueError("charts должен быть непустым массивом графиков")
    if len(charts) > MAX_CHARTS:
        raise ValueError(f"слишком много графиков в блоке: {len(charts)} > {MAX_CHARTS}")

    total_values = 0
    clean: list[dict] = []
    for chart in charts:
        if not isinstance(chart, dict):
            raise ValueError("каждый график должен быть объектом {title, series}")
        series = chart.get("series")
        if not isinstance(series, dict):
            raise ValueError("у графика нет series {labels, datasets}")

        labels = series.get("labels") or []
        datasets = series.get("datasets") or []
        if not isinstance(labels, list) or not isinstance(datasets, list):
            raise ValueError("labels и datasets должны быть массивами")
        if not datasets:
            raise ValueError(f"у графика «{chart.get('title')}» нет серий данных")
        if len(labels) > MAX_LABELS:
            raise ValueError(f"слишком много подписей: {len(labels)} > {MAX_LABELS}")
        if len(datasets) > MAX_DATASETS_PER_CHART:
            raise ValueError(f"слишком много серий: {len(datasets)} > {MAX_DATASETS_PER_CHART}")

        clean_ds: list[dict] = []
        for ds in datasets:
            if not isinstance(ds, dict):
                raise ValueError("каждая серия должна быть объектом {label, data}")
            data = ds.get("data")
            if not isinstance(data, list):
                raise ValueError("data серии должен быть массивом")
            total_values += len(data)
            clean_ds.append({"label": str(ds.get("label") or "серия")[:120], "data": data})

        clean.append({
            "title": str(chart.get("title") or "график")[:200],
            "note": str(chart.get("note") or "")[:400],
            "series": {"labels": [str(x)[:80] for x in labels], "datasets": clean_ds},
        })

    if total_values > MAX_VALUES_TOTAL:
        raise ValueError(f"слишком много значений: {total_values} > {MAX_VALUES_TOTAL}")
    return clean


def request_hash(block: str, block_title: str, context: dict, charts: list[dict]) -> str:
    """Ключ кэша: тот же блок на тех же данных не пересчитывается."""
    blob = json.dumps(
        {"block": block, "title": block_title, "context": context,
         "charts": charts, "model": _model()},
        ensure_ascii=False, sort_keys=True, separators=(",", ":"),
    )
    return hashlib.sha256(blob.encode("utf-8")).hexdigest()


# ─── Кэш и журнал (cost_insight_runs, миграция 0045) ─────────────────────────
# Таблица закрывает и кэш, и аудит. Обе операции намеренно НЕ валят разбор при
# ошибке БД: пользователю важнее получить наблюдения, чем строгий журнал.

async def cached_run(req_hash: str) -> dict | None:
    """Последний успешный разбор по хешу. None — в кэше нет."""
    from app.db import pool

    try:
        async with pool().acquire() as conn:
            row = await conn.fetchrow(
                "SELECT observations, facts, model, elapsed_ms, created_at "
                "FROM cost_insight_runs "
                "WHERE request_hash = $1 AND degraded = FALSE "
                "ORDER BY created_at DESC LIMIT 1",
                req_hash,
            )
    except Exception as exc:
        log(logging.WARNING, "insights: кэш недоступен", error=str(exc)[:200])
        return None

    if not row:
        return None

    def _load(value: Any) -> Any:
        return json.loads(value) if isinstance(value, str) else value

    return {
        "observations": _load(row["observations"]),
        "facts": _load(row["facts"]),
        "model": row["model"],
        "elapsed_ms": row["elapsed_ms"],
        "cached": True,
        "cached_at": row["created_at"].isoformat() if row["created_at"] else None,
        "degraded": False,
        "error": None,
    }


async def record_run(req_hash: str, block: str, block_title: str, email: str,
                     context: dict, result: dict) -> None:
    """Запись запуска в журнал. Ответы из кэша сюда не попадают."""
    from app.db import pool

    usage = result.get("usage") or {}
    try:
        async with pool().acquire() as conn:
            await conn.execute(
                "INSERT INTO cost_insight_runs ("
                "  request_hash, block, block_title, email, model, observations,"
                "  facts, context, degraded, error, prompt_tokens,"
                "  completion_tokens, cost, elapsed_ms"
                ") VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8::jsonb,$9,$10,"
                "          $11,$12,$13,$14)",
                req_hash, block, block_title, email, result.get("model") or "",
                json.dumps(result.get("observations") or [], ensure_ascii=False),
                json.dumps(result.get("facts") or [], ensure_ascii=False),
                json.dumps(context or {}, ensure_ascii=False),
                bool(result.get("degraded")), str(result.get("error") or "")[:500],
                usage.get("prompt_tokens"), usage.get("completion_tokens"),
                str(usage.get("cost") or ""), result.get("elapsed_ms"),
            )
    except Exception as exc:
        log(logging.WARNING, "insights: журнал недоступен", block=block,
            error=str(exc)[:200])


# ─── Основной сценарий ───────────────────────────────────────────────────────

async def analyze(block: str, block_title: str, charts: list[dict],
                  context: dict) -> dict:
    """Разбор блока графиков. Всегда возвращает факты, наблюдения — если получилось."""
    from app import llm_settings

    started = time.monotonic()
    # Кэш настроек живёт секунды: правка в админке применяется без рестарта.
    await llm_settings.refresh()
    facts = compute_facts(charts)

    result: dict[str, Any] = {
        "block": block,
        "facts": facts,
        "observations": [],
        "model": _model(),
        "degraded": False,
        "error": None,
    }

    def finish() -> dict:
        result["elapsed_ms"] = int((time.monotonic() - started) * 1000)
        return result

    if not facts:
        result["degraded"] = True
        result["error"] = "в графиках блока нет числовых значений"
        return finish()

    if not llm_enabled():
        result["degraded"] = True
        result["error"] = "разбор не настроен (COST_LLM_*)"
        return finish()

    try:
        observations, usage = await call_llm(
            SYSTEM_PROMPT, build_user_prompt(block_title, charts, context, facts))
    except InsightError as exc:
        # Модель отвалилась — отдаём посчитанные факты, блок остаётся полезным.
        result["degraded"] = True
        result["error"] = str(exc)
        log(logging.WARNING, "insights: разбор не удался", block=block,
            error=str(exc)[:200], model=_model())
        return finish()

    pool = _all_numbers(charts, facts)
    for obs in observations:
        bad = _verify_numbers(obs["text"], pool)
        obs["unverified_numbers"] = bad
        obs["numbers_ok"] = not bad

    result["observations"] = observations
    result["usage"] = usage
    return finish()
