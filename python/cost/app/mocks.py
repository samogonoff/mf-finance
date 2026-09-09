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

    # 4. plan_id — статический список
    result["plan_id"] = ["ПЛАН-A", "ПЛАН-B", "ПЛАН-C"]

    return result


# ── Mock data rows ────────────────────────────────────────────────────────────

def _make_row(i: int, overrides: dict | None = None) -> dict[str, Any]:
    base = 1500 + i * 73
    cost = base * 0.62
    brand = ["КОЦУР С.А.", "ИВАНОВ И.И.", "ПЕТРОВА А.Н.", "СИДОРОВ В.К."][i % 4]
    materials = ["Ткань основная", "Подкладка", "Фурнитура", "Нитки", "Утеплитель"]
    row = {
        "Бренд-менеджер": brand,
        "Модель": f"M-{1000 + i:04d}",
        "Артикул": f"ART-{20000 + i:05d}",
        "Признак калькуляции": ["ПКПСС", "КПСС", "ПФКСС", "ФКСС"][i % 4],
        "дата расчета": f"2026-04-{(i % 28) + 1:02d}T00:00:00",
        "Материал/техоперация/декор(признак)": ["материал", "техоперация", "декор"][i % 3],
        "Наименование": f"{materials[i % 5]} арт.{20000 + i:05d}",
        "артикул материала": f"MAT-{30000 + i:05d}",
        "свойство1": f"состав {['100% хлопок', 'полиэстер 100%', 'вискоза 100%', 'лён 100%', 'шерсть 100%'][i % 5]}",
        "свойство2": f"цвет {['чёрный', 'белый', 'синий', 'красный', 'зелёный'][i % 5]}",
        "свойство3": f"размер {['42', '44', '46', '48', '50'][i % 5]}",
        "Норма": round(0.5 + (i % 10) * 0.25, 2),
        "цена материала, руб.": round(80 + i * 3.5, 2),
        "цена материала, USD.": round((80 + i * 3.5) / 92, 2),
        "Уровень цен": ["Базовый розничный", "Премиум розничный", "Партнёрский"][i % 3],
        "Страна пр-ва": ["Беларусь", "Россия", "Турция", "Китай"][i % 4],
        "Семья": ["AURORA", "BOREAL", "CRAFT", "LINEA"][i % 4],
        "Сезон": ["Осень-Зима 2025", "Весна-Лето 2026", "Осень-Зима 2026"][i % 3],
        "PLAN_ID": ["ПЛАН-A", "ПЛАН-B", "ПЛАН-C"][i % 3],
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
        "sum_Пошив, руб.":   round(cost * 0.18, 2),
        "sum_Пошив, USD.":   round(cost * 0.18 / 92, 2),
        "sum_Раскрой, руб.": round(cost * 0.07, 2),
        "sum_Раскрой, USD.": round(cost * 0.07 / 92, 2),
        "sum_Основные материалы, руб.":      round(cost * 0.55, 2),
        "sum_Основные материалы, USD.":      round(cost * 0.55 / 92, 2),
        "sum_Вспомогательные материалы, руб.": round(cost * 0.08, 2),
        "sum_Вспомогательные материалы, USD.": round(cost * 0.08 / 92, 2),
        "sum_Декоры, руб.":      round(cost * 0.04, 2),
        "sum_Декоры, USD.":      round(cost * 0.04 / 92, 2),
        "sum_Вязание, руб.":     round(cost * 0.02, 2),
        "sum_Вязание, USD.":     round(cost * 0.02 / 92, 2),
        "avg_Ставка НДС": [20, 20, 20, 20, 10, 20, 20, 20, 0, 20][i % 10],
        "avg_Пошив, минуты": round(15 + (i % 20) * 2.5, 1),
        "avg_Раскрой, минуты": round(8 + (i % 15) * 1.8, 1),
        "avg_Курс на дату расчета": round(90 + (i % 11) * 0.5, 2),
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
        "plan_id": "PLAN_ID",
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
    {"id": 1, "name": "Базовый розничный",  "price_type1": 1850.00, "price_type3": 2470.00, "price_type4": 2800.00, "price_type5": 2500.00, "price_type6": 2300.00},
    {"id": 2, "name": "Премиум розничный",  "price_type1": 2280.00, "price_type3": 3150.00, "price_type4": 3500.00, "price_type5": 3200.00, "price_type6": 2900.00},
    {"id": 3, "name": "Партнёрский",        "price_type1": 1620.00, "price_type3": 2120.00, "price_type4": 2400.00, "price_type5": 2100.00, "price_type6": 1900.00},
    {"id": 4, "name": "Опт",                "price_type1": 1450.00, "price_type3": 1880.00, "price_type4": 2100.00, "price_type5": 1900.00, "price_type6": 1700.00},
    {"id": 5, "name": "Распродажа",         "price_type1": 1290.00, "price_type3": 1690.00, "price_type4": 1900.00, "price_type5": 1700.00, "price_type6": 1500.00},
]


def aggregated(payload: dict | None = None) -> dict:
    """Агрегированные данные, опционально отфильтрованные."""
    rows = _all_rows()
    if payload:
        rows = [r for r in rows if _match_filters(r, payload)]
    # Пересчитываем себестоимость как сумму 6 компонентов (как в детализации)
    for row in rows:
        row["sum_Себестоимость, руб."] = round(
            float(row.get("sum_Пошив, руб.", 0) or 0)
            + float(row.get("sum_Раскрой, руб.", 0) or 0)
            + float(row.get("sum_Декоры, руб.", 0) or 0)
            + float(row.get("sum_Вязание, руб.", 0) or 0)
            + float(row.get("sum_Основные материалы, руб.", 0) or 0)
            + float(row.get("sum_Вспомогательные материалы, руб.", 0) or 0),
            2,
        )
        row["sum_Себестоимость, USD."] = round(
            float(row.get("sum_Пошив, USD.", 0) or 0)
            + float(row.get("sum_Раскрой, USD.", 0) or 0)
            + float(row.get("sum_Декоры, USD.", 0) or 0)
            + float(row.get("sum_Вязание, USD.", 0) or 0)
            + float(row.get("sum_Основные материалы, USD.", 0) or 0)
            + float(row.get("sum_Вспомогательные материалы, USD.", 0) or 0),
            2,
        )
    # Inject margin targets into each row
    for row in rows:
        l1 = (row.get("Level 01") or "").strip()
        row["target_margin_pct"] = _mock_margin_targets.get(l1)
    # Inject price_rf/kz/uz from price levels
    pl_map = {pl["name"]: pl for pl in PRICE_LEVELS}
    for row in rows:
        pl_name = str(row.get("Уровень цен", "") or "").strip()
        matched = pl_map.get(pl_name)
        if matched:
            row["price_rf"] = matched.get("price_type4")
            row["price_kz"] = matched.get("price_type5")
            row["price_uz"] = matched.get("price_type6")
    # Inject planned_retail/planned_wholesale (mock: ПКПСС→null, КПСС→ПКПСС, ПФКСС→КПСС, ФКСС→ПФКСС)
    chain_map: dict[str, str] = {"КПСС": "ПКПСС", "ПФКСС": "КПСС", "ФКСС": "ПФКСС"}
    for row in rows:
        cs = str(row.get("Признак калькуляции", "") or "").strip()
        if cs in chain_map:
            src_cs = chain_map[cs]
            # Find first row with matching source calc_sign for same model+articul (+plan_id for ПФКСС/ФКСС)
            for src in rows:
                if src.get("Признак калькуляции") != src_cs:
                    continue
                if src.get("Модель") != row.get("Модель"):
                    continue
                if src.get("Артикул") != row.get("Артикул"):
                    continue
                if cs != "КПСС" and src.get("PLAN_ID") != row.get("PLAN_ID"):
                    continue
                row["planned_retail"] = src.get("avg_Розничная цена по уровню, руб.")
                row["planned_wholesale"] = src.get("avg_Отпускная цена по уровню, руб")
                break
    for row in rows:
        cs = str(row.get("Признак калькуляции", "") or "").strip()
        if cs not in ("КПСС", "ПФКСС"):
            continue
        if row.get("planned_retail") is not None and row.get("planned_wholesale") is not None:
            continue
        price_mopt = round(float(row.get("sum_Себестоимость, руб.", 0) or 0) * 1.25, 2)
        nnds = 20.0
        level1 = str(row.get("Level 01", "") or "").strip()
        mult = 1.3 if level1 in ("Девочкам", "Мальчикам") else 1.4
        if row.get("planned_wholesale") is None:
            row["planned_wholesale"] = price_mopt
        if row.get("planned_retail") is None:
            row["planned_retail"] = round(price_mopt * mult * (1 + nnds / 100), 2)
        row["planned_cost"] = round(price_mopt * 0.7, 2)
    return {"data": rows, "count": len(rows)}


def raw_rows(payload: dict) -> dict:
    """Мок для raw-rows: фильтрует _all_rows() по полям группировки."""
    rows = _all_rows()
    filtered = [
        r for r in rows
        if all(
            str(r.get(field, "")).strip() == str(value).strip()
            for field, value in payload.items()
            if value is not None and value != "" and value != "—"
        )
    ]
    return {"data": filtered, "count": len(filtered)}


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


# ── Mock МП constants (наценка МП / % расходов МП / скидка СПП) ────────────

_mock_mp_constants: list[dict] = []
_mock_mp_constants_next_id = 1


def list_mp_constants() -> list[dict]:
    """История mock-констант МП, самые новые сверху."""
    return sorted(
        _mock_mp_constants,
        key=lambda c: (c["effective_date"], c["created_at"]),
        reverse=True,
    )


def add_mp_constants(markup_mp, expense_pct_mp, spp_discount, effective_date, username: str) -> dict:
    global _mock_mp_constants_next_id
    import datetime as _dt
    row = {
        "id": _mock_mp_constants_next_id,
        "effective_date": (effective_date or _dt.date.today()).isoformat(),
        "markup_mp": float(markup_mp),
        "expense_pct_mp": float(expense_pct_mp),
        "spp_discount": float(spp_discount),
        "created_by": username,
        "created_at": _dt.datetime.now().isoformat(),
    }
    _mock_mp_constants.append(row)
    _mock_mp_constants_next_id += 1
    return {"success": True, "id": row["id"]}


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


def details(key: str, scope: str = "model") -> dict:
    """Мок для details: возвращает 2 детальные строки.

    `scope="model"` — обе строки одной модели с разными артикулами;
    `scope="articul"` — обе строки одного артикула с разными моделями;
    `scope="construction"` — разные и модели, и артикулы (режимы ЧНИ).
    """
    if scope == "articul":
        model, model2 = f"{key}-M1", f"{key}-M2"
        articul = articul2 = key
    elif scope == "construction":
        model, model2 = f"{key}-1001", f"{key}-1002"
        articul, articul2 = "ART-20001", "ART-20002"
    else:
        model = model2 = key
        articul, articul2 = "ART-20001", "ART-20002"
    rows = [
        {
            "Модель": model,
            "Артикул": articul,
            "Наименование модели": f"{model} · образец",
            "Номер задания производства": f"JOB-{hash(model) % 10000:04d}",
            "дата расчета": "2026-04-15T00:00:00",
            "Дата выпуска": "2026-04-10T00:00:00",
            "Страна пр-ва": "Беларусь",
            "Пошив, минуты": 14.5,
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
            "Модель": model2,
            "Артикул": articul2,
            "Наименование модели": f"{model2} · вариант 2",
            "Номер задания производства": f"JOB-{hash(model2 + 'v2') % 10000:04d}",
            "дата расчета": "2026-04-10T00:00:00",
            "Дата выпуска": "2026-04-05T00:00:00",
            "Страна пр-ва": "Узбекистан",
            "Пошив, минуты": 16.2,
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


# ── Mock versioning (Stream G) ─────────────────────────────────────────────

_mock_approvals_store: dict[tuple[str, str, str | None, str | None, str | None], str] = {}


def checkout_calculation(model, articul, calc_sign, plan_id, date, username) -> dict:
    rows = _all_rows()
    filtered = [
        r for r in rows
        if r.get("Модель") == model and r.get("Артикул") == articul
    ]
    for r in filtered:
        r["change_type"] = "original"
    return {"version_id": 1, "rows": filtered}


def save_version_draft(version_id, rows) -> dict:
    return {"success": True, "mock": True}


def submit_version(version_id, comment=None) -> dict:
    return {"success": True, "mock": True}


def approve_version(version_id, approved_by) -> dict:
    return {"success": True, "mock": True}


def reject_version(version_id, approved_by, comment=None) -> dict:
    return {"success": True, "mock": True}


def get_active_version(model, articul, calc_sign, plan_id, date) -> dict:
    return {"has_draft": False}


def delete_version(version_id) -> dict:
    return {"success": True, "mock": True}


def get_raw_cache_rows(model, articul, calc_sign, plan_id, date) -> dict:
    rows = _all_rows()
    filtered = [
        r for r in rows
        if r.get("Модель") == model and r.get("Артикул") == articul
    ]
    for r in filtered:
        r["change_type"] = "original"
    return {"version_id": None, "rows": filtered}


def list_versions(model, articul, calc_sign, plan_id, date) -> list[dict]:
    return []


def get_version_rows(version_id) -> dict:
    return {"version_id": version_id, "version": 1, "status": "draft", "rows": []}


def create_version(model, articul, calc_sign, plan_id, date, username, rows, status="draft") -> dict:
    return {"version_id": 999, "version": 1, "mock": True}


# ── Mock PEO approvals (Stream H) ──────────────────────────────────────────


def _approval_key(model, articul, calc_sign, plan_id, task_number=None) -> tuple:
    """Ключ пер-заданный — как UNIQUE в cost_calc_approvals (миграции 0023, 0038).

    Пустое задание приводим к '' — в БД колонка NOT NULL DEFAULT '', и mock
    должен вести себя так же, иначе в mock-режиме '' и None были бы двумя
    разными согласованиями одной калькуляции.
    """
    return (model or "", articul or "", calc_sign, plan_id, (task_number or "").strip())


def save_approvals_batch(approvals: list[dict]) -> dict:
    for a in approvals:
        key = _approval_key(
            a.get("model", ""), a.get("articul", ""), a.get("calc_sign"),
            a.get("plan_id"), a.get("task_number"),
        )
        _mock_approvals_store[key] = a.get("status", "pending")
    return {"success": True, "mock": True, "count": len(approvals)}


def revoke_approval(model, articul, calc_sign, plan_id, task_number=None) -> dict:
    _mock_approvals_store.pop(_approval_key(model, articul, calc_sign, plan_id, task_number), None)
    return {"success": True, "mock": True}


def revoke_approvals_batch(items: list[dict]) -> dict:
    count = 0
    for it in items:
        key = _approval_key(
            it.get("model", ""), it.get("articul", ""), it.get("calc_sign"),
            it.get("plan_id"), it.get("task_number"),
        )
        if _mock_approvals_store.pop(key, None) is not None:
            count += 1
    return {"success": True, "mock": True, "count": count}


def get_approval_status(filters: dict | None = None) -> dict:
    result = []
    for (m, a, cs, pid, tn), status in _mock_approvals_store.items():
        if filters:
            if filters.get("model") and m != filters["model"]:
                continue
            if filters.get("articul") and a != filters["articul"]:
                continue
        result.append({
            "model": m, "articul": a, "calc_sign": cs, "plan_id": pid,
            "task_number": tn, "status": status,
        })
    return {"data": result, "mock": True}


# ── Roles mocks ────────────────────────────────────────────────────────────────

_MOCK_ROLES = [
    {"id": 1, "name": "ПЭО", "permissions": ["cost:view", "cost:approve", "cost:export"], "is_system": True},
    {"id": 2, "name": "Бренд-менеджер", "permissions": ["cost:view", "cost:edit_price", "cost:export"], "is_system": True},
    {"id": 3, "name": "Калькулятор", "permissions": ["cost:view", "cost:edit_price", "cost:edit_materials", "cost:export"], "is_system": True},
    {"id": 4, "name": "Администратор", "permissions": ["cost:view", "cost:edit_price", "cost:approve", "cost:edit_materials", "cost:export", "cost:admin"], "is_system": False},
]

_MOCK_USER_ROLES = [
    {"id": 1, "email": "ivanova@mf.ru", "role_id": 1, "role_name": "ПЭО", "permissions": ["cost:view", "cost:approve", "cost:export"]},
    {"id": 2, "email": "petrov@mf.ru", "role_id": 2, "role_name": "Бренд-менеджер", "permissions": ["cost:view", "cost:edit_price", "cost:export"]},
]


def list_roles() -> list[dict]:
    return _MOCK_ROLES


def list_user_roles() -> list[dict]:
    return _MOCK_USER_ROLES


def my_roles_permissions(email: str) -> dict:
    user_roles = [r for r in _MOCK_USER_ROLES if r["email"] == email]
    permissions = set()
    for r in user_roles:
        permissions.update(r.get("permissions", []))
    return {"email": email, "roles": user_roles, "permissions": sorted(permissions)}


def commercial_dashboard() -> dict:
    """Заглушка дашборда коммерческой эффективности для COST_MOCK=1.

    Числа выдуманные, но структура и НАБОР ПОЛЕЙ совпадают с боевым ответом —
    иначе фронт в mock-режиме молча рисовал бы не то. Взвешенные показатели
    отдаются None: в реальности они пусты, пока не наполнен volume_pcs, и фронт
    обязан уметь это показать.

    Списки измерений, мер и фильтров берём из самого модуля дашборда, а не
    переписываем сюда: копия уже расходилась с боевым ответом при первой же
    правке набора фильтров.
    """
    from app import commercial

    months = ["2026-06", "2026-07", "2026-08"]
    return {
        "tiles": {
            "calc_count": 1234, "model_count": 210, "volume_total": None,
            "cost_byn": 13.32, "cost_usd": 4.62,
            "price_byn": 22.21, "price_usd": 7.71,
            "retail_byn": 36.11, "retail_usd": 12.54,
            "markup_byn": 8.89, "markup_usd": 3.09,
            "margin_pct": 40.03, "profit_pct": 66.74,
            "margin_pct_w": None, "profit_pct_w": None,
            "last_calc_date": "2026-08-12T00:00:00+00:00",
        },
        "months": [
            {"ym": m, "cost_byn": 12.0 + i, "cost_usd": 4.1 + i * 0.3,
             "price_byn": 20.0 + i * 2, "price_usd": 6.9 + i * 0.7,
             "retail_byn": 33.0 + i * 3, "retail_usd": 11.4 + i,
             "calc_count": 400 + i * 50}
            for i, m in enumerate(months)
        ],
        "structure": [
            {"label": name, "mat_main": 40.0 + i * 5, "mat_aux": 6.0,
             "sewing": 18.0, "cutting": 3.0, "decor": 2.0, "knitting": 0.0,
             "other": 14.0 + i, "cost_total": 83.0 + i * 6}
            # Верхний уровень иерархии — бренд-менеджеры (см. structure_dim ниже).
            for i, name in enumerate(("ИВАНОВА И.И.", "ПЕТРОВ П.П.", "СИДОРОВА А.А."))
        ],
        "ring": [
            {"label": "ТРУСЫ МУЖСКИЕ", "value": 520},
            {"label": "НОСКИ МУЖСКИЕ", "value": 410},
            {"label": "ЛЕГИНСЫ", "value": 304},
        ],
        "options": {
            "model_name": ["ТРУСЫ МУЖСКИЕ", "НОСКИ МУЖСКИЕ", "ЛЕГИНСЫ"],
            "model": ["417760-3", "583002", "447706"],
            "articul": ["A-1", "A-2"],
            "country": ["Беларусь", "Китай", "Узбекистан"],
            "calc_sign": ["ПКПСС", "КПСС", "ПФКСС", "ФКСС"],
            "season": ["SS2025", "SS2026", "AW2025"],
            "level01": ["Женщинам", "Мужчинам", "Детям"],
            "level02": ["Бельё", "Одежда"],
            "level03": ["Трусы", "Носки"],
            "brand_manager": ["Иванова И.И.", "Петров П.П."],
            "year": ["2026"],
            "month": ["06", "07", "08"],
        },
        "meta": {
            "options_truncated": [],
            "calc_total": 2000,
            "cache_refreshed_at": "2026-08-12T06:00:00+00:00",
            "dimension": "model_name", "dimension_label": "Наименование товара",
            "measure": "volume_pcs", "measure_label": "Выпуск, шт",
            "dimensions": [{"key": k, "label": v}
                           for k, v in commercial.DIMENSIONS.items()],
            "measures": [{"key": k, "label": v[0]}
                         for k, v in commercial.MEASURES.items()],
            "volume_sign": commercial.VOLUME_SIGN,
            "structure_dim": "brand_manager",
            "structure_label": "Бренд-менеджер",
            "structure_path": [],
            "structure_can_drill": True,
            "date_basis": commercial.DEFAULT_DATE_BASIS,
            "date_basis_label": commercial.DATE_BASES[commercial.DEFAULT_DATE_BASIS][0],
        },
    }


def margin_dashboard() -> dict:
    """Заглушка дашборда «Маржа выпуска» для COST_MOCK=1.

    Суммы выдуманные, производные показатели считаются той же функцией, что и
    в бою (margin._derive) — иначе мок расходился бы с боевым ответом набором
    ключей при первой же правке формул. Прошлый год у части строк пуст: фронт
    обязан уметь показывать сравнение, которого нет.
    """
    from app import margin

    def sums(vol, rev, cost, raw, k=1.0, prev=True):
        # k — множитель для сдвинутых периодов; prev=False — сравнения нет.
        row = {
            "vol": vol, "rev_b": rev, "rev_u": rev / 3.2, "cost_b": cost, "cost_u": cost / 3.2,
            "raw_b": raw, "raw_u": raw / 3.2, "sew_min": vol * 4.2, "sew_vol": vol * 0.6,
            "target_num": rev * 0.6 * 0.48, "target_den": rev * 0.6,
            # Норматив против факта: факт есть у 90% выпуска и на 3% дороже норматива.
            "vol_f": vol * 0.9, "rev_f_b": rev * 0.9, "rev_f_u": rev * 0.9 / 3.2,
            "cost_f_b": cost * 0.9 * 1.03, "cost_f_u": cost * 0.9 * 1.03 / 3.2,
            "cost_n_b": cost * 0.9, "cost_n_u": cost * 0.9 / 3.2,
        }
        for suffix, mult in (("_pm", 0.93 * k), ("_py", 0.81 * k)):
            for key in ("vol", "rev_b", "rev_u", "cost_b", "cost_u", "raw_b", "raw_u", "sew_min", "sew_vol"):
                row[key + suffix] = row[key] * mult if prev else None
            # Прошлого года по факту нет (до 2026 факт не вели) — как в бою.
            for key in ("vol_f", "rev_f_b", "rev_f_u", "cost_f_b", "cost_f_u", "cost_n_b", "cost_n_u"):
                row[key + suffix] = row[key] * mult if (prev and suffix == "_pm") else None
        return row

    months = [f"2026-{m:02d}" for m in range(1, 9)]
    return {
        "tiles": margin._derive({**sums(2_877_939, 39_356_047, 18_026_135, 9_100_000),
                                 "calc_count": 2376, "model_count": 640}),
        "months": [
            margin._derive({"ym": ym, **sums(2_800_000 + i * 90_000, 37e6 + i * 0.9e6, 17e6 + i * 0.3e6, 8.5e6)})
            for i, ym in enumerate(months)
        ],
        "by_bm": [
            margin._derive({"label": name, **sums(900_000 - i * 120_000, 12e6 - i * 1.6e6, 5.4e6 - i * 0.6e6, 2.7e6,
                                                   prev=i != 2)})
            for i, name in enumerate(("ИВАНОВА И.И.", "ПЕТРОВ П.П.", "СИДОРОВА А.А.", "КОЗЛОВ К.К."))
        ],
        "by_level01": [
            margin._derive({"label": name, **sums(1_200_000 - i * 300_000, 16e6 - i * 4e6, 7e6 - i * 1.5e6, 3.5e6)})
            for i, name in enumerate(("Носки&Колготки", "Женщинам", "Мужчинам", "Девочкам"))
        ],
        "matrix": [
            margin._derive({"label": name, "name": None,
                            **sums(1_200_000 - i * 300_000, 16e6 - i * 4e6, 7e6 - i * 1.5e6, 3.5e6)})
            for i, name in enumerate(("Носки&Колготки", "Женщинам", "Мужчинам", "Девочкам", ""))
        ],
        "options": {
            "model_name": ["ТРУСЫ МУЖСКИЕ", "НОСКИ МУЖСКИЕ", "ЛЕГИНСЫ"],
            "model": ["417760-3", "583002", "447706"],
            "articul": ["A-1", "A-2"],
            "country": ["Беларусь", "Узбекистан"],
            "season": ["SS2025", "SS2026", "AW2025"],
            "level01": ["Женщинам", "Мужчинам", "Носки&Колготки"],
            "level02": ["Бельё", "Одежда"],
            "level03": ["Трусы", "Носки"],
            "brand_manager": ["ИВАНОВА И.И.", "ПЕТРОВ П.П."],
            "year": ["2025", "2026"],
            "month": [f"{m:02d}" for m in range(1, 9)],
        },
        "meta": {
            "options_truncated": [],
            "matrix_truncated": False,
            "matrix_row_limit": margin.MATRIX_ROW_LIMIT,
            "calc_total": 30_000,
            "cache_refreshed_at": "2026-08-12T06:00:00+00:00",
            "volume_sign": margin.VOLUME_SIGN,
            "period": {"year": ["2026"], "month": []},
            "year_defaulted": True,
            "cost_basis": margin.DEFAULT_COST_BASIS,
            "cost_basis_label": margin.COST_BASES[margin.DEFAULT_COST_BASIS],
            "cost_bases": [{"key": k, "label": v} for k, v in margin.COST_BASES.items()],
            "matrix_dim": "level01",
            "matrix_label": "Level 01",
            "matrix_path": [],
            "matrix_can_drill": True,
            "matrix_levels": [{"key": k, "label": v} for k, v in margin.MATRIX_LEVELS],
        },
    }


# ── Мультипаки ────────────────────────────────────────────────────────────────
#
# Состав отдаётся статикой: в mock-режиме нет ни кэша калькуляций, ни таблиц
# состава, а редактор расчёта должен открываться и блок состава показывать.
# Сборка возвращает готовые суммы, но строк не генерирует — генерация опирается
# на строку-шаблон из cost_data_all, которой в моке нет.

_MULTIPACK_ITEMS = [
    {
        "src_model": "430A-2848", "src_articul": "B2-124430A",
        "src_calc_sign": "ПКПСС", "src_plan_id": "0",
        "pinned_calc_sign": False, "pinned_plan_id": False,
        "qty": 2.0, "position": 0, "found": True,
        "model_name": "НОСКИ ДЕТСКИЕ", "level01": "Носки&Колготки",
        "calc_date": "2026-09-01", "fx_rate": 3.05, "task": "",
        "tasks_total": 1, "src_rows": 23,
        "buckets": {
            "Основные материалы": {"rub": 0.62, "usd": 0.20},
            "Вспомогательные материалы": {"rub": 0.11, "usd": 0.04},
            "Пошив": {"rub": 0.0, "usd": 0.0},
            "Раскрой": {"rub": 0.0, "usd": 0.0},
            "Вязание": {"rub": 0.24, "usd": 0.08},
            "Декоры": {"rub": 0.03, "usd": 0.01},
        },
        "unit_rub": 1.0, "unit_usd": 0.33,
        "sum_rub": 2.0, "sum_usd": 0.66,
    },
    {
        "src_model": "430A-2930", "src_articul": "B2-125430A",
        "src_calc_sign": "ПКПСС", "src_plan_id": "0",
        "pinned_calc_sign": False, "pinned_plan_id": False,
        "qty": 1.0, "position": 1, "found": True,
        "model_name": "НОСКИ ДЕТСКИЕ", "level01": "Носки&Колготки",
        "calc_date": "2026-09-01", "fx_rate": 3.05, "task": "",
        "tasks_total": 1, "src_rows": 23,
        "buckets": {
            "Основные материалы": {"rub": 0.70, "usd": 0.23},
            "Вспомогательные материалы": {"rub": 0.12, "usd": 0.04},
            "Пошив": {"rub": 0.0, "usd": 0.0},
            "Раскрой": {"rub": 0.0, "usd": 0.0},
            "Вязание": {"rub": 0.26, "usd": 0.09},
            "Декоры": {"rub": 0.02, "usd": 0.01},
        },
        "unit_rub": 1.1, "unit_usd": 0.37,
        "sum_rub": 1.1, "sum_usd": 0.37,
    },
]


def multipack_state(model: str, articul: str) -> dict[str, Any]:
    return {
        "is_pack": True,
        "pack_id": 1,
        "model": (model or "").strip(),
        "articul": (articul or "").strip(),
        "pack_size": 3.0,
        "note": "мок-состав",
        "created_by": "mock@local",
        "updated_by": None,
        "updated_at": None,
        "items": _MULTIPACK_ITEMS,
        "qty_total": 3.0,
        "items_rub": 3.1,
        "items_usd": 1.03,
        "last_build": None,
        "stale": [],
        "warnings": [],
        "mock": True,
    }


def multipack_build(model: str, articul: str) -> dict[str, Any]:
    state = multipack_state(model, articul)
    return {
        "rows": [],
        "items_rub": 3.1, "items_usd": 1.03,
        "packaging_rub": 0.0, "packaging_usd": 0.0,
        "total_rub": 3.1, "total_usd": 1.03,
        "generated": 0, "kept": 0,
        "state": state,
        "mock": True,
    }


def multipack_candidates(query: str = "") -> list[dict[str, Any]]:
    rows = [
        {
            "model": it["src_model"], "articul": it["src_articul"],
            "calc_sign": it["src_calc_sign"], "plan_id": it["src_plan_id"],
            "model_name": it["model_name"], "level01": it["level01"],
            "calc_date": it["calc_date"], "task": it["task"],
            "tasks_total": it["tasks_total"], "unit_rub": it["unit_rub"],
        }
        for it in _MULTIPACK_ITEMS
    ]
    q = (query or "").strip().lower()
    if not q:
        return rows
    return [r for r in rows if q in r["model"].lower() or q in r["articul"].lower()
            or q in (r["model_name"] or "").lower()]
