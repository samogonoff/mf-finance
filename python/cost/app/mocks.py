from __future__ import annotations

from typing import Any

# ── Filter options data ───────────────────────────────────────────────────────

BRAND_MANAGERS = [
    "КОЦУР С.А.",
    "ИВАНОВ И.И.",
    "ПЕТРОВА А.Н.",
    "СИДОРОВ В.К.",
]

# Иерархия: brand_manager → level01 → level02 → ...
# ID для уровней: строка, text — человекочитаемое имя.
_LEVELS: dict[str, list[dict[str, str]]] = {
    "level01": [
        {"id": "1", "text": "Одежда"},
        {"id": "2", "text": "Обувь"},
        {"id": "3", "text": "Аксессуары"},
    ],
    "level02": [
        {"id": "4", "text": "Верхняя"},
        {"id": "5", "text": "Низ"},
        {"id": "6", "text": "Плательная"},
    ],
    "level03": [
        {"id": "7", "text": "Куртки"},
        {"id": "8", "text": "Пальто"},
        {"id": "9", "text": "Жилеты"},
        {"id": "10", "text": "Брюки"},
        {"id": "11", "text": "Юбки"},
    ],
    "level04": [
        {"id": "12", "text": "Зима"},
        {"id": "13", "text": "Демисезон"},
        {"id": "14", "text": "Лето"},
    ],
    "level05": [
        {"id": "15", "text": "Базовая линия"},
        {"id": "16", "text": "Премиум"},
        {"id": "17", "text": "Лимитированная"},
    ],
}

CALC_SIGNS = ["ПКПСС", "КПСС", "ПФКСС", "ФКСС"]

# Привязка level → какие ID доступны для данного brand_manager
# (симулирует DWH-каскад: разные менеджеры работают с разными группами)
_BRAND_LEVEL_MAP: dict[str, dict[str, list[str]]] = {
    "КОЦУР С.А.": {
        "level01": ["1", "2"],          # Одежда, Обувь
        "level02": ["4", "5", "6"],
        "level03": ["7", "8", "9", "10", "11"],
        "level04": ["12", "13", "14"],
        "level05": ["15", "16", "17"],
    },
    "ИВАНОВ И.И.": {
        "level01": ["1", "3"],          # Одежда, Аксессуары
        "level02": ["4", "5"],
        "level03": ["7", "8", "9"],
        "level04": ["12", "14"],
        "level05": ["15", "16"],
    },
    "ПЕТРОВА А.Н.": {
        "level01": ["1"],               # Одежда
        "level02": ["4", "5", "6"],
        "level03": ["7", "8", "9", "10", "11"],
        "level04": ["12", "13"],
        "level05": ["15", "17"],
    },
    "СИДОРОВ В.К.": {
        "level01": ["2", "3"],          # Обувь, Аксессуары
        "level02": ["5", "6"],
        "level03": ["10", "11"],
        "level04": ["13", "14"],
        "level05": ["16", "17"],
    },
}

# Привязка level01_id → какие level02_id доступны
_LEVEL_CASCADE: dict[str, dict[str, list[str]]] = {
    "level01": {
        "1": ["4", "5", "6"],  # Одежда → Верхняя, Низ, Плательная
        "2": ["5", "6"],       # Обувь → Низ, Плательная
        "3": ["4"],            # Аксессуары → Верхняя
    },
    "level02": {
        "4": ["7", "8", "9"],    # Верхняя → Куртки, Пальто, Жилеты
        "5": ["10", "11"],       # Низ → Брюки, Юбки
        "6": ["7", "9", "11"],   # Плательная → Куртки, Жилеты, Юбки
    },
    "level03": {
        "7": ["12", "13"],       # Куртки → Зима, Демисезон
        "8": ["12"],             # Пальто → Зима
        "9": ["13", "14"],       # Жилеты → Демисезон, Лето
        "10": ["12", "13", "14"],# Брюки → все
        "11": ["13", "14"],      # Юбки → Демисезон, Лето
    },
    "level04": {
        "12": ["15", "16"],      # Зима → Базовая, Премиум
        "13": ["15", "16", "17"],# Демисезон → все
        "14": ["15", "17"],      # Лето → Базовая, Лимитированная
    },
    "level05": {},  # terminal level
}

LEVEL_KEYS = ["level01", "level02", "level03", "level04", "level05"]

# Lookup: level_key → {id: text} для перевода ID фильтров в текстовые значения строк
_LEVEL_TEXT: dict[str, dict[str, str]] = {
    key: {item["id"]: item["text"] for item in _LEVELS[key]}
    for key in LEVEL_KEYS
}

# ── Cascade propagation helpers ────────────────────────────────────────────────

def _propagate_down(from_idx: int, to_idx: int, selected_ids: set[str]) -> set[str]:
    current = set(selected_ids)
    for k in range(from_idx, to_idx):
        src_key = LEVEL_KEYS[k]
        cascade = _LEVEL_CASCADE.get(src_key, {})
        nxt: set[str] = set()
        for cid in current:
            nxt.update(cascade.get(cid, []))
        current = nxt
        if not current:
            break
    return current


def _propagate_up(from_idx: int, to_idx: int, selected_ids: set[str]) -> set[str]:
    current = set(selected_ids)
    for k in range(from_idx, to_idx, -1):
        src_key = LEVEL_KEYS[k - 1]
        cascade = _LEVEL_CASCADE.get(src_key, {})
        parents: set[str] = set()
        for parent_id, child_ids in cascade.items():
            if any(c in current for c in child_ids):
                parents.add(parent_id)
        current = parents
        if not current:
            break
    return current


def _get_available_ids(
    key: str,
    selected_bm: list[str],
    selected_levels: dict[str, list[str]],
    key_index: int,
) -> set[str]:
    ids = {item["id"] for item in _LEVELS[key]}

    if selected_bm:
        allowed_bm: set[str] = set()
        for bm in selected_bm:
            bm_levels = _BRAND_LEVEL_MAP.get(bm, {})
            if key in bm_levels:
                allowed_bm.update(bm_levels[key])
        if allowed_bm:
            ids &= allowed_bm

    # Только вышестоящие уровни (меньший индекс) фильтруют текущий — top-down
    for j in range(key_index):
        higher_selected = selected_levels.get(LEVEL_KEYS[j], [])
        if higher_selected:
            propagated = _propagate_down(j, key_index, set(higher_selected))
            ids &= propagated

    # Нижестоящие уровни НЕ фильтруют вышестоящие
    return ids


def _match_brand_manager(bm: str, selected_levels: dict[str, list[str]]) -> bool:
    bm_data = _BRAND_LEVEL_MAP.get(bm, {})
    for key, vals in selected_levels.items():
        if key not in LEVEL_KEYS:
            continue
        bm_vals = bm_data.get(key, [])
        if not any(v in bm_vals for v in vals):
            return False
    return True


# ── Filter-options with cascade ───────────────────────────────────────────────

def get_filter_options(params: dict[str, list[str]]) -> dict[str, Any]:
    """Возвращает фильтр-опции, отфильтрованные по выбранным значениям (каскад).

    params — словарь ключ → список значений (id для уровней, строка для brand_manager).
    """
    selected_bm = params.get("brand_manager", [])
    selected_levels: dict[str, list[str]] = {}
    for k in LEVEL_KEYS:
        v = params.get(k, [])
        if v:
            selected_levels[k] = v

    result: dict[str, Any] = {}

    # 1. Brand managers — НЕ фильтруются уровнями (top-down cascade)
    result["brand_manager"] = list(BRAND_MANAGERS)

    # 2. Level 01–05 — полный двусторонний многоуровневый каскад
    for i, key in enumerate(LEVEL_KEYS):
        available_ids = _get_available_ids(key, selected_bm, selected_levels, i)
        all_for_key = _LEVELS[key]
        result[key] = [
            item for item in all_for_key if item["id"] in available_ids
        ] or all_for_key

    # 3. calc_sign — статический
    result["calc_sign"] = list(CALC_SIGNS)

    return result


# ── Mock data rows ────────────────────────────────────────────────────────────

def _make_row(i: int, overrides: dict | None = None) -> dict[str, Any]:
    base = 1500 + i * 73
    cost = base * 0.62
    brand = ["КОЦУР С.А.", "ИВАНОВ И.И.", "ПЕТРОВА А.Н.", "СИДОРОВ В.К."][i % 4]
    row = {
        "Бренд-менеджер": brand,
        "Модель": f"M-{1000 + i:04d}",
        "Артикул": f"ART-{20000 + i:05d}",
        "Признак калькуляции": ["ПКПСС", "КПСС", "ПФКСС", "ФКСС"][i % 4],
        "дата расчета": f"2026-04-{(i % 28) + 1:02d}T00:00:00",
        "Уровень цен": ["Базовый розничный", "Премиум розничный", "Партнёрский"][i % 3],
        "Страна пр-ва": ["Беларусь", "Россия", "Турция", "Китай"][i % 4],
        "Семья": ["AURORA", "BOREAL", "CRAFT", "LINEA"][i % 4],
        "Сезон": ["Осень-Зима 2025", "Весна-Лето 2026", "Осень-Зима 2026"][i % 3],
        # Level поля — для фильтрации
        "Level 01": _LEVELS["level01"][i % 3]["text"],
        "Level 02": _LEVELS["level02"][i % 3]["text"],
        "Level 03": _LEVELS["level03"][i % 3]["text"],
        "Level 04": _LEVELS["level04"][i % 3]["text"],
        "Level 05": _LEVELS["level05"][i % 3]["text"],
        # Агрегированные поля
        "avg_Розничная цена по уровню, руб.": round(base * 1.1, 2),
        "avg_Отпускная цена по уровню, руб":  round(base, 2),
        "avg_Розничная цена по уровню, USD.": round(base * 1.1 / 92, 2),
        "avg_Отпускная цена по уровню, USD.": round(base / 92, 2),
        "avg_Пошив, руб.":   round(cost * 0.18, 2),
        "avg_Пошив, USD.":   round(cost * 0.18 / 92, 2),
        "avg_Раскрой, руб.": round(cost * 0.07, 2),
        "avg_Раскрой, USD.": round(cost * 0.07 / 92, 2),
        "sum_Основные материалы, руб.":      round(cost * 0.55, 2),
        "sum_Основные материалы, USD.":      round(cost * 0.55 / 92, 2),
        "sum_Вспомогательные материалы, руб.": round(cost * 0.08, 2),
        "sum_Вспомогательные материалы, USD.": round(cost * 0.08 / 92, 2),
        "avg_Декоры, руб.":      round(cost * 0.04, 2),
        "avg_Декоры, USD.":      round(cost * 0.04 / 92, 2),
        "avg_Вязание, руб.":     round(cost * 0.02, 2),
        "avg_Вязание, USD.":     round(cost * 0.02 / 92, 2),
    }
    if overrides:
        row.update(overrides)
    return row


def _all_rows() -> list[dict[str, Any]]:
    return [_make_row(i) for i in range(1, 145)]


def _match_filters(row: dict, payload: dict) -> bool:
    """Проверяет, подходит ли строка под фильтры из payload."""
    if payload.get("date_from"):
        date_val = (row.get("дата расчета") or "").split("T")[0]
        if date_val < payload["date_from"]:
            return False
    if payload.get("date_to"):
        date_val = (row.get("дата расчета") or "").split("T")[0]
        if date_val > payload["date_to"]:
            return False

    # MULTI_FILTER_COLUMNS mapping from routes.py
    col_map = {
        "level01": "Level 01", "level02": "Level 02", "level03": "Level 03",
        "level04": "Level 04", "level05": "Level 05",
        "brand_manager": "Бренд-менеджер", "country": "Страна пр-ва",
        "family": "Семья", "season": "Сезон",
        "calc_sign": "Признак калькуляции", "model": "Модель", "articul": "Артикул",
    }
    for key, col in col_map.items():
        vals = payload.get(key) or []
        if vals and "all" not in vals:
            # level01-level05 приходят как id (1,2,3), но в строках хранится text
            if key in _LEVEL_TEXT:
                vals = [_LEVEL_TEXT[key].get(v, v) for v in vals]
            row_val = str(row.get(col, "")).strip()
            if row_val not in vals:
                return False
    return True


# ── Public API ────────────────────────────────────────────────────────────────

# Legacy — используется для прямого импорта, но теперь не нужен
FILTER_OPTIONS: dict[str, Any] = get_filter_options({})

PRICE_LEVELS: list[dict[str, Any]] = [
    {"name": "Базовый розничный",  "price_type1": 1850.00, "price_type3": 2470.00},
    {"name": "Премиум розничный",  "price_type1": 2280.00, "price_type3": 3150.00},
    {"name": "Партнёрский",        "price_type1": 1620.00, "price_type3": 2120.00},
    {"name": "Опт",                "price_type1": 1450.00, "price_type3": 1880.00},
    {"name": "Распродажа",         "price_type1": 1290.00, "price_type3": 1690.00},
]


def aggregated(payload: dict | None = None) -> dict:
    """Агрегированные данные, опционально отфильтрованные."""
    rows = _all_rows()
    if payload:
        rows = [r for r in rows if _match_filters(r, payload)]
    # Пересчитываем себестоимость как сумму 6 компонентов (как в детализации)
    for row in rows:
        row["sum_Себестоимость, руб."] = round(
            float(row.get("avg_Пошив, руб.", 0) or 0)
            + float(row.get("avg_Раскрой, руб.", 0) or 0)
            + float(row.get("avg_Декоры, руб.", 0) or 0)
            + float(row.get("avg_Вязание, руб.", 0) or 0)
            + float(row.get("sum_Основные материалы, руб.", 0) or 0)
            + float(row.get("sum_Вспомогательные материалы, руб.", 0) or 0),
            2,
        )
        row["sum_Себестоимость, USD."] = round(
            float(row.get("avg_Пошив, USD.", 0) or 0)
            + float(row.get("avg_Раскрой, USD.", 0) or 0)
            + float(row.get("avg_Декоры, USD.", 0) or 0)
            + float(row.get("avg_Вязание, USD.", 0) or 0)
            + float(row.get("sum_Основные материалы, USD.", 0) or 0)
            + float(row.get("sum_Вспомогательные материалы, USD.", 0) or 0),
            2,
        )
    # Inject margin targets into each row
    for row in rows:
        l1 = (row.get("Level 01") or "").strip()
        row["target_margin_pct"] = _mock_margin_targets.get(l1)
    return {"data": rows, "count": len(rows)}


# ── Mock margin targets ─────────────────────────────────────────────────────

_mock_margin_targets: dict[str, float] = {}


def margin_targets() -> list[dict]:
    """Return all saved mock margin targets (one per level01 entry)."""
    result: list[dict] = []
    for entry in _LEVELS["level01"]:
        text = entry["text"]
        pct = _mock_margin_targets.get(text)
        result.append({
            "level1": text,
            "target_margin_pct": pct,
            "updated_at": None,
            "updated_by": None,
        })
    return result


def save_margin_targets(targets: list[dict], username: str) -> dict:
    """Save mock margin targets in memory."""
    for t in targets:
        level1 = (t.get("level1") or "").strip()
        pct = t.get("target_margin_pct")
        if level1:
            _mock_margin_targets[level1] = float(pct) if pct is not None else 0.0
    return {"success": True, "count": len(targets)}


def load_data(payload: dict) -> dict:
    """Сырые данные с пагинацией и фильтрацией."""
    rows = _all_rows()
    rows = [r for r in rows if _match_filters(r, payload)]
    total = len(rows)
    limit = min(payload.get("limit", 1000), 5000)
    offset = payload.get("offset", 0)
    page = rows[offset:offset + limit]
    return {"data": page, "count": len(page), "total": total, "offset": offset, "limit": limit}


# ── Mock cache endpoints ──────────────────────────────────────────────────────


def refresh_cache() -> dict:
    return {"status": "mock", "message": "Mock cache: no-op"}


def cache_status() -> dict:
    import datetime
    return {
        "refreshed_at": datetime.datetime.now().isoformat(),
        "row_count": 144,
        "is_refreshing": False,
        "error_message": None,
    }


# ── Mock details ──────────────────────────────────────────────────────────────


def details(model: str) -> dict:
    """Мок для details: возвращает 2 детальные строки."""
    rows = [
        {
            "Модель": model,
            "Артикул": f"ART-20001",
            "Наименование модели": f"{model} · образец",
            "Номер задания производства": f"JOB-{hash(model) % 10000:04d}",
            "дата расчета": "2026-04-15T00:00:00",
            "Дата выпуска": "2026-04-10T00:00:00",
            "Признак калькуляции": "ПКПСС",
            "Розничная цена, руб.": 2035.00,
            "Оптовая цена, руб.": 1850.00,
            "Осн. материалы, руб.": 520.00,
            "Вспом. материалы, руб.": 95.00,
            "Пошив, руб.": 280.00,
            "Раскрой, руб.": 95.00,
            "Декор, руб.": 38.00,
            "Вязание, руб.": 0.00,
            "Себестоимость, руб.": 1028.00,
            "Наценка, руб.": 822.00,
            "Наценка, %": 79.96,
            "Маржинальность, %": 44.43,
        },
        {
            "Модель": model,
            "Артикул": f"ART-20002",
            "Наименование модели": f"{model} · вариант 2",
            "Номер задания производства": f"JOB-{hash(model + 'v2') % 10000:04d}",
            "дата расчета": "2026-04-10T00:00:00",
            "Дата выпуска": "2026-04-05T00:00:00",
            "Признак калькуляции": "КПСС",
            "Розничная цена, руб.": 2420.00,
            "Оптовая цена, руб.": 2200.00,
            "Осн. материалы, руб.": 650.00,
            "Вспом. материалы, руб.": 110.00,
            "Пошив, руб.": 310.00,
            "Раскрой, руб.": 110.00,
            "Декор, руб.": 45.00,
            "Вязание, руб.": 0.00,
            "Себестоимость, руб.": 1225.00,
            "Наценка, руб.": 975.00,
            "Наценка, %": 79.59,
            "Маржинальность, %": 44.32,
        },
    ]
    return {"data": rows, "count": len(rows)}
