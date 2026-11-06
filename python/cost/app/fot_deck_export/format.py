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

NBSP = " "  # неразрывный пробел — разделитель разрядов деки
MINUS = "−"  # типографский минус
NA = "н/д"  # пустое значение

#: Токены стиля, которые понимает движок/биндеры (см. docstring модуля).
TOKEN_UP = "up"
TOKEN_DOWN = "down"
TOKEN_NEUTRAL = "neutral"
TOKEN_NA = "na"

#: Формат по умолчанию: разряды группируются, проценты с одним знаком.
DEFAULT_FORMAT = {"kpi_group": True, "cell_group": True, "pct_digits": 1,
                  "total_pct_digits": 1, "note_tail_space": False}


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


__all__ = ["DEFAULT_FORMAT", "MINUS", "NA", "NBSP", "TOKEN_DOWN", "TOKEN_NA",
           "TOKEN_NEUTRAL", "TOKEN_UP", "cell", "delta", "fmt_of", "kpi", "num",
           "pct", "total_value"]
