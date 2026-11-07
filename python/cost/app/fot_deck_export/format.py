"""Форматирование чисел выгрузки «1:1 как в деке» (вкладка p17, задача 663620).

Единственное место с правилами типографики деки. Биндеры НЕ форматируют числа
сами, не считают знаки и не держат констант вида «  чел.» — только эти функции.

Каждая функция возвращает пару (текст, токен стиля):

  * текст — то, что печатается в ране/ячейке деки;
  * токен — как это красить: ``"up"`` (красный деки, рост/перевыполнение),
    ``"down"`` (зелёный, снижение/недобор), ``"neutral"`` (базовый цвет строки),
    ``"na"`` (приглушённый серый для «н/д» в колонках отклонений) или
    ``None`` — цвет не трогаем (подписи, прочие числа, KPI).

Токен считается по ОКРУГЛЁННОМУ значению, а не по сырому: дека показывает одно
значение, а УФ ставит по нему же. Иначе строка «−0,0» печаталась бы числом с
минусом и красилась зелёной как снижение, а «99,96 %» выглядело бы как «100,0%»
нейтральным. Поэтому −0.04 → «0,0» + down, 99.96 → «100,0%» + up.

Настройки формата приходят из manifest.json (`prototypes[proto].format`):
деку писали разные генераторы, и её слайды отличаются друг от друга. В своде и
в странах BY/RU значения KPI напечатаны без разрядов («7307,2»), у UZ — с
неразрывным пробелом («2 052,5»); «% укомпл.» в таблице BY — целым («99%»),
в остальных таблицах — с одним знаком («72,7%»). Поэтому каждая функция
принимает флаги формата, а значения флагов снимаются с самой деки при сборке
актива — в биндерах этих знаний нет.

Общее для всей деки:
  * разряды — неразрывный пробел (U+00A0), когда группировка включена;
  * десятичный разделитель — запятая;
  * минус — типографский U+2212, не дефис: «−153,9»;
  * пустое значение — «н/д» (так писал генератор деки), токен ``"na"``.
"""

from __future__ import annotations

import re

NBSP = " "  # неразрывный пробел — разделитель разрядов деки
MINUS = "−"  # типографский минус
NA = "н/д"  # пустое значение

#: Токены стиля, которые понимает движок/биндеры (см. docstring модуля).
TOKEN_UP = "up"
TOKEN_DOWN = "down"
TOKEN_NEUTRAL = "neutral"
TOKEN_NA = "na"
#: Ровно ноль — на листах 1–3/7–8 он НЕ светофор: круг «0,00» в деке серый,
#: ячейка таблицы остаётся базовым цветом строки. В p17 ноль — зелёный
#: (`delta` возвращает down), поэтому токен отдельный, а не переиспользован.
TOKEN_ZERO = "zero"

#: Формат по умолчанию: разряды группируются, проценты с одним знаком.
DEFAULT_FORMAT = {"kpi_group": True, "cell_group": True, "pct_digits": 1,
                  "total_pct_digits": 1, "note_tail_space": False,
                  # листы 1–3/7–8 деки: значения таблиц — млн с одним знаком
                  # (b1) либо целые тыс. (p15/p16); знаки «п.п.» на кругах — до
                  # двух, хвостовые нули срезаются («−0,35», но ноль — «0,00»)
                  "cell_digits": 1, "pp_digits": 2,
                  # знаки РОВНОГО нуля круга: у листов 7/8 «0,00», у листа 3
                  # деки «0,0» — точность своя у каждого листа
                  "pp_zero_digits": 2,
                  # пара «ПЧ/ФЧ» в карточках отделов: дека пишет её с ТОЧКОЙ
                  # («372.4/348.9») — это её типографика, а не наш формат
                  "hc_dot": True}


def _group(value: float, digits: int, group: bool = True) -> str:
    """Модуль числа: запятая в дробной части, разряды — NBSP (если включены)."""
    text = f"{abs(float(value)):,.{digits}f}"
    if not group:
        # без разрядов: разделитель тысяч долой, точка → запятая
        return text.replace(",", "").replace(".", ",")
    # запятая-разделитель разрядов → NBSP, точка → запятая; делаем в два шага,
    # чтобы не перепутать уже подставленную запятую с десятичной
    return text.replace(",", "\x00").replace(".", ",").replace("\x00", NBSP)


def _as_float(value) -> float | None:
    if value is None:
        return None
    try:
        n = float(value)
    except (TypeError, ValueError):
        return None
    if n != n or n in (float("inf"), float("-inf")):  # NaN/inf — как пустое
        return None
    return n


def _rounded(n: float, digits: int = 1) -> float:
    """Значение так, как его увидит читатель: округление до знаков деки.

    Через Decimal, а не format(): у Python round(2.5) = 2 (банковское), а дека
    округляет половину вверх — и «99,95 %» печатается как «100,0 %».
    """
    from decimal import ROUND_HALF_UP, Decimal
    quantum = Decimal(1).scaleb(-digits)
    return float(Decimal(repr(float(n))).quantize(quantum, rounding=ROUND_HALF_UP))


def num(value, group: bool = True) -> tuple[str, str | None]:
    """Число деки: «7 307,2» / «7307,2». Один знак после запятой.

    Токен — None: значения (численность, план, факт) в деке не красятся.
    """
    n = _as_float(value)
    if n is None:
        return NA, TOKEN_NA
    shown = _rounded(n, 1)
    sign = MINUS if shown < 0 else ""
    return sign + _group(shown, 1, group), None


def pct(value, digits: int = 1, bands: dict | None = None,
        light: bool = False) -> tuple[str, str | None]:
    """Процент укомплектованности: «96,6%» / «99%» + токен светофора деки.

    Число знаков — свойство слайда, а не значения: таблица BY печатает «99%»,
    а таблицы RU/UZ/KZ — «72,7%»/«100,0%» при тех же по природе данных
    (`format.pct_digits` из манифеста).

    Светофор трёхцветный по границам деки (`pct_bands`): ≤ down_max — недобор
    (down, зелёный), ≥ up_min — перевыполнение (up, красный), между — базовый
    цвет строки (neutral). Границы ВКЛЮЧИТЕЛЬНЫЕ и сравниваются с округлённым
    значением: ровно 97,0 % в деке зелёное, ровно 100,0 % — красное.
    """
    n = _as_float(value)
    if n is None:
        return NA, TOKEN_NA
    shown = _rounded(n, digits)
    text = (MINUS if shown < 0 else "") + _group(shown, digits, True) + "%"
    return text, _pct_token(shown, bands)


def _pct_token(shown: float, bands: dict | None) -> str | None:
    if not bands:
        # без границ — прежнее двухполосное правило (>= 100 красный)
        return TOKEN_UP if shown >= 100 else TOKEN_DOWN
    if shown <= bands["down_max"]:
        return TOKEN_DOWN
    if shown >= bands["up_min"]:
        return TOKEN_UP
    # между границами — базовый цвет строки
    return TOKEN_NEUTRAL


def delta(value, group: bool = True) -> tuple[str, str | None]:
    """Отклонение со знаком: «+30,5», «−153,9», ноль — «0,0» (без плюса).

    Знак текста и токен УФ берутся от ОКРУГЛЁННОГО значения: −0.04 печатается
    «0,0» (не «−0,0») и красится зелёным, как нулевое отклонение деки
    («Красильное», Δ25 = 0,0 — зелёное, но без плюса).
    """
    n = _as_float(value)
    if n is None:
        return NA, TOKEN_NA
    shown = _rounded(n, 1)
    if shown > 0:
        return "+" + _group(shown, 1, group), TOKEN_UP
    if shown < 0:
        return MINUS + _group(shown, 1, group), TOKEN_DOWN
    return _group(0.0, 1, group), TOKEN_DOWN


def kpi(key: str, value, cfg: dict | None = None) -> tuple[str, str | None]:
    """Значение карточки-показателя свода/страны (пять KPI деки)."""
    cfg = cfg or DEFAULT_FORMAT
    group = cfg["kpi_group"]
    return delta(value, group) if key.startswith("delta") else num(value, group)


def cell(key: str, value, cfg: dict | None = None,
         bands: dict | None = None) -> tuple[str, str | None]:
    """Ячейка строки таблицы по ключу колонки (COL_KEYS биндера p17)."""
    cfg = cfg or DEFAULT_FORMAT
    if key == "label":
        return ("" if value is None else str(value)), None
    if key == "staff_pct":
        return pct(value, cfg["pct_digits"], bands)
    if key.startswith("delta"):
        return delta(value, cfg["cell_group"])
    return num(value, cfg["cell_group"])


def total_value(key: str, value, cfg: dict | None = None,
                bands: dict | None = None) -> tuple[str, str | None]:
    """Значение колонки в строке/полосе итога (свои знаки процентов деки)."""
    cfg = cfg or DEFAULT_FORMAT
    if key == "label":
        return ("" if value is None else str(value)), None
    if key == "staff_pct":
        return pct(value, cfg["total_pct_digits"], bands)
    if key.startswith("delta"):
        return delta(value, cfg["cell_group"])
    return num(value, cfg["cell_group"])


def fmt_of(meta: dict | None) -> dict:
    """Настройки формата прототипа из манифеста (с дефолтами)."""
    out = dict(DEFAULT_FORMAT)
    if meta:
        out.update({k: v for k, v in meta.items() if k in DEFAULT_FORMAT})
    return out


# ─── Листы 1–3/7–8: деньги в млн/тыс., «п.п.» и пара ПЧ/ФЧ ───────────────────

def money(value, group: bool = True, digits: int = 1,
          plus: bool = True, nbsp: bool = True) -> tuple[str, str | None]:
    """Денежное значение листа: «147,1» / «7 300» / «+8,2».

    В отличие от `num`/`delta`, знаки после запятой задаёт прототип: у карточек
    и таблиц b1 это млн с одним знаком, у p15/p16 — целые тыс. (`cell_digits` из
    манифеста). Ноль печатается БЕЗ плюса («0,0»), как в деке, а не «+0,0».

    `plus=False` — значение без плюса у положительных: так дека печатает СУММЫ
    («87,0 млн»), плюс остаётся только у отклонений (динамика). Один и тот же
    формат, два смысла — поэтому флаг, а не вторая функция.

    Токен — ``None``: денежные значения на этих листах не красятся (цвет несут
    отдельные шейпы подписей-динамик и круги «п.п.»).

    `nbsp=False` — разряды обычным пробелом: так дека печатает пару ПЧ/ФЧ в
    карточке страны («ПЧ: 4 537,7»), тогда как панель численности того же
    листа держит там неразрывный. Разница видна только побайтово, на глаз
    строки одинаковы, поэтому флаг, а не «примерно так же».
    """
    n = _as_float(value)
    if n is None:
        return NA, TOKEN_NA
    shown = _rounded(n, digits)
    sign = MINUS if shown < 0 else ("+" if (plus and shown > 0) else "")
    text = _group(shown, digits, group)
    if group and not nbsp:
        text = text.replace(NBSP, " ")
    return sign + text, None


def signed(value, digits: int = 1, group: bool = True,
           token_zero: str = TOKEN_ZERO) -> tuple[str, str | None]:
    """Отклонение листов 1–3/7–8: знак + токен, НОЛЬ — не светофор.

    `delta` (p17) нулевое отклонение красит зелёным — так размечена таблица
    численности. На листах 1–3/7–8 ноль нейтрален: круг «0,00» деки серый
    (6B7280), ячейка таблицы остаётся базовым цветом строки, подпись «0,0» —
    тоже. Поэтому токен нуля отдельный (``TOKEN_ZERO``), а биндер решает, красить
    ли его вообще.
    """
    n = _as_float(value)
    if n is None:
        return NA, TOKEN_NA
    shown = _rounded(n, digits)
    if shown > 0:
        return "+" + _group(shown, digits, group), TOKEN_UP
    if shown < 0:
        return MINUS + _group(shown, digits, group), TOKEN_DOWN
    return _group(0.0, digits, group), token_zero


def pp(value, digits: int = 2, zero_digits: int | None = None
       ) -> tuple[str, str | None]:
    """Круг «доля ФОТ в выручке, п.п.»: «−0,35» / «+0,24» / «0,00» / «н/д».

    Хвостовые нули дека срезает до одного знака («−0,1», «+0,1»), но ровно ноль
    печатает ПОЛНЫМ числом знаков — и это число своё у каждого листа: на
    листах 7/8 это «0,00», а на листе 3 деки — «0,0» (проверено по дампу).
    Поэтому точность нуля приходит отдельным параметром (`zero_digits`), а
    `digits` задаёт точность расчёта: срезать хвост у нуля нельзя, иначе на
    листе 8 вместо «0,00» окажется «0,0».

    Токен — как у `signed` (ноль не светофор).
    """
    n = _as_float(value)
    if n is None:
        return NA, TOKEN_NA
    shown = _rounded(n, digits)
    if shown == 0:
        zd = digits if zero_digits is None else zero_digits
        return _group(0.0, zd, True), TOKEN_ZERO
    text = (MINUS if shown < 0 else "+") + _group(shown, digits, True)
    # хвостовой ноль срезается: «−0,10» → «−0,1», «+0,20» → «+0,2»
    if digits > 1 and text.endswith("0"):
        text = text[:-1]
    return text, (TOKEN_UP if shown > 0 else TOKEN_DOWN)


def hc_pair(plan, fact, digits: int = 1, dot: bool = False) -> str:
    """Пара «ПЧ/ФЧ» карточки отдела: «372.4/348.9» (дека — с точкой).

    Разделитель дробной части у пары свой: листы 7/8 пишут её ТОЧКОЙ, хотя все
    прочие числа листа — запятой. Разряды дека в паре не группирует (значения
    < 1000), поэтому NBSP здесь не ставится. Нет данных — «н/д» на позиции.
    """
    sep = "." if dot else ","
    def one(v) -> str:
        n = _as_float(v)
        if n is None:
            return NA
        shown = _rounded(n, digits)
        text = f"{abs(shown):.{digits}f}".replace(".", sep)
        return (MINUS if shown < 0 else "") + text
    return "%s/%s" % (one(plan), one(fact))


# ─── Подписи деки: шаблоны с прорезями ───────────────────────────────────────

#: Прорезь шаблона: ``{year}``, ``{prev}``, ``{y2}``, ``{p2}``, ``{vN}``.
_SLOT_RE = re.compile(r"\{([a-z]+\d*)\}")


def fill_label(slots: list[dict], *, year=None, prev=None, country=None,
               values=()) -> list[list[str]]:
    """Подпись-шаблон прототипа → абзацы готового текста ([[ран, …], …]).

    Шаблон снимается с деки при сборке актива (`scripts/build_fot_deck_template`):
    формулировки, разбивка на абзацы и раны — декины, а годы, страна и значения
    в нём стоят прорезями. Так правка деки (другая подпись, другой год) не
    требует правки кода биндера — это и есть «1:1 с декой» в части подписей.

    Прорези:
      * ``{year}`` / ``{prev}`` — годы отчётного и прошлого периода (полные);
      * ``{y2}`` / ``{p2}`` — их двузначные хвосты («1ПГ26»);
      * ``{country}`` — название страны листа («Беларусь»);
      * ``{vN}`` — N-е значение из `values` (нумерация сквозная по шейпу).

    Незаполненная прорезь — исключение, а не пустая строка: тихо отданная
    подпись «Доля ФОТ в выручке гр. (): » выглядит как баг вёрстки, и заметить
    её на слайде почти невозможно.
    """
    ctx = {"year": year, "prev": prev, "country": country,
           "y2": (str(year)[-2:] if year is not None else None),
           "p2": (str(prev)[-2:] if prev is not None else None)}
    numbered = {i: v for i, v in enumerate(values, 1)}
    out: list[list[str]] = []
    for slot in slots:
        para: list[str] = []
        for run in slot["runs"]:
            para.append(_fill_slots(run["text"], ctx, numbered))
        out.append(para)
    return out


def fill_header_labels(cells: list[dict], *, year=None, prev=None, country=None,
                       values=()) -> list[list[str]]:
    """Подписи шапки таблицы из манифеста → [колонка][абзац].

    В подписях колонок тоже стоят прорези: «Факт / {v1}ПГ {prev}» — номер
    полугодия и год. Поэтому `values` принимаются так же, как у подписей
    шейпов, и нумерация сквозная по всей шапке (дека нумеровала их подряд,
    слева направо).
    """
    numbered = {i: v for i, v in enumerate(values, 1)}
    ctx = {"year": year, "prev": prev, "country": country,
           "y2": (str(year)[-2:] if year else None),
           "p2": (str(prev)[-2:] if prev else None)}
    out: list[list[str]] = []
    for cell in cells:
        paras = []
        for text in cell.get("paragraphs") or []:
            paras.append(_fill_slots(text, ctx, numbered))
        out.append(paras)
    return out


def slot_names(text: str) -> list[str]:
    """Имена прорезей в шаблоне подписи: ``['v1', 'v2', 'year']``.

    Нужно биндеру, чтобы понять, что именно требует подпись: у пятого блока
    ленты KPI подпись полугодий несёт ещё и номер полугодия, а у года — нет.
    Считать это по тексту деки в биндере нельзя, поэтому спрашиваем шаблон.
    """
    return _SLOT_RE.findall(text or "")


def _fill_slots(text: str, ctx: dict, numbered: dict) -> str:
    """Подставляет прорези шаблона; неизвестная или пустая — RuntimeError."""
    na_values: set[int] = set()

    def sub(m: re.Match) -> str:
        key = m.group(1)
        if key.startswith("v") and key[1:].isdigit():
            idx = int(key[1:])
            if idx not in numbered:
                raise RuntimeError(
                    "fot_deck: подпись деки ждёт значение {v%d}, а биндер передал "
                    "их %d — состав подписи и данных разошёлся (шаблон: %r)"
                    % (idx, len(numbered), text))
            if str(numbered[idx]) == NA:
                na_values.add(idx)
            return str(numbered[idx])
        value = ctx.get(key)
        if value in (None, ""):
            raise RuntimeError(
                "fot_deck: в подписи деки не заполнена прорезь {%s} — "
                "биндер не передал %s (текст: %r)" % (key, key, text))
        return str(value)

    out = _SLOT_RE.sub(sub, text)
    # «н/д» уже означает «значения нет»: единица сразу за ним («н/д%») —
    # ложная точность, и дека так не печатает. Снимаем её, только если
    # ближайшая прорезь подставила NA (иначе «н/д» могло быть текстом деки).
    if na_values:
        out = re.sub(re.escape(NA) + r"(\s*)(%|млн|тыс\.[^ ]*)", NA, out)
    return out


__all__ = ["DEFAULT_FORMAT", "MINUS", "NA", "NBSP", "TOKEN_DOWN", "TOKEN_NA",
           "TOKEN_NEUTRAL", "TOKEN_UP", "TOKEN_ZERO", "cell", "delta",
           "fill_header_labels", "fill_label", "fmt_of", "hc_pair", "kpi",
           "money", "num", "pct", "pp", "signed", "slot_names", "total_value"]
