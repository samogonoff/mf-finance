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
    return {"version_id": None, "snapshot_id": None, "snapshots": [], "rows": filtered}


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


def margin_deviations() -> dict:
    """Заглушка выгрузки листа отклонений: в моке дашборда листа нет, и
    выгрузка отдаёт пустую выборку той же формы, что в бою (margin.deviations)."""
    from app import margin

    return {
        "rows": [],
        "truncated": False,
        "row_limit": margin.DEVIATION_EXPORT_LIMIT,
        "cache_refreshed_at": "2026-08-12T06:00:00+00:00",
        "period": {"year": ["2026"], "month": []},
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


# ── Mock справочника моделей (S_MODELI) ──────────────────────────────────────

_MOCK_CATALOG_FOLDERS = [
    {"folder_id": 6, "path": "Женщинам\\Нижнее белье женщина\\Трусы женщина",
     "levels": ["Женщинам", "Нижнее белье женщина", "Трусы женщина"],
     "brand_manager": "КОЦУР С.А."},
    {"folder_id": 21, "path": "Носки&Колготки\\Носки мужские\\Классические мужские",
     "levels": ["Носки&Колготки", "Носки мужские", "Классические мужские"],
     "brand_manager": "ДАНЧУК Е.Ю."},
]

_MOCK_CATALOG_ROWS = [
    {
        "MODEL": "107K-1916", "ART": "25107K", "NAIM": "НОСКИ МУЖСКИЕ",
        "BRAND_FIO": "ДАНЧУК Е.Ю.",
        "FULL_PATH": "Носки&Колготки\\Носки мужские\\Классические мужские",
        "STATUS": "COLLECTION", "BRAND": "Mark Formelle", "COUNTRY": "Беларусь",
        "KEDIZM": "пара", "SOSTAV": "80% хлопок; 18% полиамид; 2% эластан",
        "NVID": "классические", "NASSORTIMENT": "14.31.10.300", "NSYRIE": "хлопок",
        "NGRUPPA": None, "KTNVED": "6115 95 000 0", "KOD_IKPU": "06115950000",
        "PRICE_ROZN": 6.9, "PRICE_OPT": 4.14, "PRICE_MOPT": 4.02,
        "PLAN_PRICE": 1.87, "NDS": 20.0, "RU_NDS": 20.0,
        "NORMA": 0.041, "NORMA_KROY": 0.0, "NORMA_POSHIV": 3.2,
        "MASSA_ED": 0.062, "IN_BOX": 200, "PURCHASED": 0, "PR_ARH": 0,
        "ITEM_ID": 140233, "PARENT_ID": 21,
    },
    {
        "MODEL": "1007-2C", "ART": "1007-2C", "NAIM": "ТРУСЫ ЖЕНСКИЕ",
        "BRAND_FIO": "КОЦУР С.А.",
        "FULL_PATH": "Женщинам\\Нижнее белье женщина\\Трусы женщина",
        "STATUS": "BASIC", "BRAND": "Mark Formelle", "COUNTRY": "Беларусь",
        "KEDIZM": "шт", "SOSTAV": "95% хлопок; 5% эластан",
        "NVID": "шорты", "NASSORTIMENT": "14.14.24.000", "NSYRIE": "хлопок",
        "NGRUPPA": None, "KTNVED": "6108 21 000 0", "KOD_IKPU": "06108210000",
        "PRICE_ROZN": 12.5, "PRICE_OPT": 7.5, "PRICE_MOPT": 7.2,
        "PLAN_PRICE": 3.4, "NDS": 20.0, "RU_NDS": 20.0,
        "NORMA": 0.118, "NORMA_KROY": 1.4, "NORMA_POSHIV": 5.6,
        "MASSA_ED": 0.048, "IN_BOX": 100, "PURCHASED": 0, "PR_ARH": 0,
        "ITEM_ID": 139001, "PARENT_ID": 6,
    },
]

# Колонки «полного» режима, которых нет в основном наборе — чтобы кнопка
# «показать все» в моке тоже что-то меняла.
_MOCK_CATALOG_EXTRA = {
    "MASSA_NET": 0.06, "KOMPLEKT": 1, "NO_RAZMER": 0, "SERTIFIKAT": "ТР ТС 017/2011",
    "GOST1": "ГОСТ 31408-2009", "GOST2": None, "PACK_L": 120, "PACK_W": 80, "PACK_H": 20,
    "IS_BAN": None, "DATEVRKV": "2026-02-11T09:14:22", "USERVRKV": "Лисица И.И.",
    "DATEKRKV": "2026-08-30T16:02:41", "USERKRKV": "Лисица И.И.",
    "PROPER_NAIM1": "с длинным рукавом", "PROPER_NAIM2": None, "PROPER_NAIM3": None,
}


def models_catalog_folders() -> dict:
    return {"folders": _MOCK_CATALOG_FOLDERS}


def models_catalog(payload: dict, limit: int, offset: int, all_columns: bool) -> dict:
    """Мок справочника: те же фильтры, что на сервере, но по двум строкам."""
    from app.db import MODELI_COLUMN_LABELS, MODELI_MAIN_COLUMNS

    rows = [dict(r) for r in _MOCK_CATALOG_ROWS]
    if all_columns:
        for r in rows:
            r.update(_MOCK_CATALOG_EXTRA)

    for field, column in (("model", "MODEL"), ("articul", "ART"), ("naim", "NAIM")):
        needle = (payload.get(field) or "").strip().lower()
        if needle:
            rows = [r for r in rows if needle in str(r.get(column) or "").lower()]

    bm = [v.strip() for v in (payload.get("brand_manager") or []) if v]
    if bm:
        rows = [r for r in rows if r.get("BRAND_FIO") in bm]
    for i in range(1, 6):
        vals = [v.strip() for v in (payload.get("level%02d" % i) or []) if v]
        if vals:
            rows = [
                r for r in rows
                if (str(r.get("FULL_PATH") or "").split(chr(92)) + ["", "", "", "", ""])[i - 1] in vals
            ]

    keys = list(MODELI_MAIN_COLUMNS)
    if all_columns:
        keys += [k for k in _MOCK_CATALOG_EXTRA if k not in keys]

    page = rows[offset:offset + limit]
    return {
        "data": [{k: r.get(k) for k in keys} for r in page],
        "total": len(rows),
        "count": len(page),
        "limit": limit,
        "offset": offset,
        "columns": [{"key": k, "label": MODELI_COLUMN_LABELS.get(k, k)} for k in keys],
    }


# ── ФОТ-дашборд «ФОТ и численность» (P9–P13) ──────────────────────────────────

# Контрольные числа шага 1 (сверка с PBI CFO_ALL_AGG на живом MSSQL, 13.10.2026).
# Мок отдаёт их в meta, чтобы фронт и шаги 2b/2c сверяли сборку с эталоном без VPN.
_FOT_MOCK_META = {
    "fot_2026": 217260.0,          # тыс BYN: ФОТ «Ожидание 2026» (факт 1ПГ + тактика 2ПГ)
    "fot_share_2026": 15.57,       # % к «Продажам без НДС», тот же срез
    "fot_share_2025": 15.97,       # % за факт 2025
    "headcount_2026_06_01": 7203.16,   # ставки, факт на 01.06.2026
    "headcount_plan_approved": 7353.2, # ставки, план «утверждено» (PlanChisl)
    "closed_period": "2026-08-31",
}

# Состояние fot_meta для моков; счётчики — с витрин шага 1 (13.10.2026).
_FOT_MOCK_STATE = {
    "closed_period": "2026-08-31",
    "fallback_closed": "2026-12-01",
    "refreshed_at": "2026-10-13T17:36:52+00:00",
    "refreshing_since": None,
    "source_counts": {
        "fot_monthly": 297962, "fot_headcount": 10660,
        "fot_headcount_plan": 492, "fot_cfo": 691,
    },
    "stale": False,
}


def fot_filter_options() -> dict:
    """Мок каскадных фильтров ФОТ-дашборда — ключи 1:1 с app.fot.filter_options().

    Статические списки (страны, сценарии, ВГО, месяцы) берём из модуля
    дашборда, а не переписываем сюда — копия расходилась бы при первой же
    правке набора.
    """
    from app import fot

    return {
        "directions": [
            {"id": "Магазины", "text": "Магазины"},
            {"id": "Производство", "text": "Производство"},
            {"id": "Складская логистика", "text": "Складская логистика"},
            {"id": "Отделы", "text": "Отделы"},
            {"id": "Розница РБ", "text": "Розница РБ"},
            {"id": "Розница РФ", "text": "Розница РФ"},
            {"id": "Розница КЗ", "text": "Розница КЗ"},
            {"id": "Маркетплейсы РФ", "text": "Маркетплейсы РФ"},
        ],
        "cfo_groups": [
            {"id": "Розничная сеть", "text": "Розничная сеть"},
            {"id": "Производство и логистика", "text": "Производство и логистика"},
        ],
        "cfo_subgroups": [
            {"id": "Магазины Минск", "text": "Магазины Минск"},
            {"id": "Магазины регионы РБ", "text": "Магазины регионы РБ"},
        ],
        "cfos": [
            {"id": "101", "text": "101 — Магазин Минск, Немига"},
            {"id": "2001", "text": "2001 — Цех пошива 1"},
        ],
        "legal_entities": [
            {"id": "ЧТУП «Марк Формэль»", "text": "ЧТУП «Марк Формэль»"},
            {"id": "ООО «Марк Формэль Рус»", "text": "ООО «Марк Формэль Рус»"},
        ],
        "countries": [{"id": c, "text": t} for c, t in fot.COUNTRY_OPTIONS],
        "scenarios": [{"id": s, "text": t} for s, t in fot.SCENARIO_OPTIONS],
        "intercompany": [{"id": i, "text": t} for i, t in fot.IC_OPTIONS],
        "years": [{"id": "2025", "text": "2025"}, {"id": "2026", "text": "2026"}],
        "months": [{"id": str(m), "text": str(m)} for m in range(1, 13)],
        "meta": dict(_FOT_MOCK_STATE),
    }


# ── P10/P11 «фиксированные полугодия»: зерно под контрольные числа ────────────

# Строки-«ЦФО» зерна: (страна, группа, подгруппа, направление, ЦФО, канал,
# код ЦФО, Факт 1ПГ26 в тыс.). Вес строки = её Факт 1ПГ26 / итог — все периоды
# масштабируются одинаково, поэтому итоги P10/P11 остаются контрольными, а
# направления блока «Отделы» P13 сходятся к контрольным числам слайдов 28–46:
# «Магазины» BY = 13 174, «Производство» BY = 32 123, «Складская логистика»
# BY = 6 064, «Отделы» (все страны) = 26 642 тыс факт 1ПГ26.
_FOT_HALF_SPECS = (
    ("BY", "Производство", "Производство Брест", "Производство",
     "2001 — Цех пошива 1", "Производство", "2001", 32123.00),
    ("BY", "Производство", "Логистика Брест", "Складская логистика",
     "2101 — Склад готовой продукции", "Отдел", "2101", 6064.00),
    ("BY", "Магазины", "Магазины Минск", "Магазины",
     "101 — Магазин Минск, Немига", "Розница РБ", "101", 13174.00),
    ("BY", "Отдел", "Отдел", "Отделы", "611 — Отдел планирования",
     "Отдел", "611", 5976.90),
    ("RU", "Отдел", "Отдел РФ", "Отделы", "612 — Отдел кадров РФ",
     "Отдел", "612", 8690.40),
    ("RU", "Отдел", "Отдел РФ", "Отделы", "613 — Отдел ИТ РФ",
     "Отдел", "613", 4888.35),
    ("RU", "Отдел", "Отдел РФ", "Отделы", "614 — Отдел логистики РФ",
     "Отдел", "614", 3258.90),
    ("KZ", "Отдел", "Отдел KZ", "Отделы", "615 — Отдел кадров KZ",
     "Отдел", "615", 2172.60),
    ("UZ", "Отдел", "Отдел UZ", "Отделы", "616 — Отдел кадров UZ",
     "Отдел", "616", 1654.85),
    ("RU", "Магазины", "Магазины РФ", "Розница РФ",
     "301 — Магазин Москва", "Розница РФ", "301", 9985.80),
    ("RU", "Маркетплейсы", "Маркетплейсы РФ", "Маркетплейсы РФ",
     "305 — Озон РФ", "Маркетплейсы", "305", 15209.70),
    ("KZ", "Магазины", "Магазины KZ", "Розница КЗ",
     "401 — Магазин Алматы", "Розница КЗ", "401", 3258.90),
    ("UZ", "Производство", "Пошив UZ", "Пошив, раскрой, печать",
     "501 — Цех Наманган", "Производство", "501", 2172.60),
)

# Контрольные итоги PBI в тыс. (факт 1ПГ25 = 87 000 + 2ПГ25 = 112 306 → 199 306;
# выручка 590 000 + 658 000 = 1 248 000): доли 15,57/15,97 % и Ожидание
# 217 260 тыс (108 630 факт 1ПГ26 + 108 630 тактика 2ПГ26). f25/rv_f25 в зерно
# не пишем — это суммы полугодий, как SUM FILTER за 12 месяцев в _halfyear_sql.
_FOT_HALF_TOTALS = {
    "f25_h1": 87000, "f25_h2": 112306,
    "b26_h1": 105000, "f26_h1": 108630, "b26_h2": 104000, "t26_h2": 108630,
    "rv_f25_h1": 590000, "rv_f25_h2": 658000,
    "rv_f26_h1": 745000, "rv_t26_h2": 650000,
}

# f25 у KZ и UZ занижен вдвое (срез компенсируется первой строкой BY — итоги
# остаются контрольными): динамика обеих стран улетает за +10 %, KZ получает
# жёлтый светофор (доля падает), UZ — красный (доля растёт): в моке видны все
# три цвета Δ-колонок, а не только зелёный.
_FOT_HALF_F25_CUT = {"401 — Магазин Алматы", "501 — Цех Наманган"}

# Знаменатели долей в тыс., ключи как в den из _halfyear_sql: группа — из
# контрольных долей; KZ — выручка по кодам 1006/1012; UZ — вся выручка UZ.
_FOT_HALF_DEN = {
    "dn_grp_rv_f25_h1": 590000, "dn_grp_rv_f25_h2": 658000, "dn_grp_rv_f25": 1248000,
    "dn_grp_rv_f26_h1": 745000, "dn_grp_rv_t26_h2": 650000,
    "dn_kz_rv_f25_h1": 38000, "dn_kz_rv_f25_h2": 42000, "dn_kz_rv_f25": 80000,
    "dn_kz_rv_f26_h1": 95000, "dn_kz_rv_t26_h2": 90000,
    "dn_uz_rv_f25_h1": 21000, "dn_uz_rv_f25_h2": 24000, "dn_uz_rv_f25": 45000,
    "dn_uz_rv_f26_h1": 30000, "dn_uz_rv_t26_h2": 25000,
}


def _fot_half_mock() -> tuple[list[dict], dict]:
    """Зерно P10/P11/P13 и знаменатели долей.

    Итоги заданы в тыс. (как в PBI) и умножаются на 1000 до сырых сумм
    витрины — сборщики app.fot (_t/_ms) дают те же контрольные числа. Вес
    строки выводится из её Факта 1ПГ26 (см. _FOT_HALF_SPECS), поэтому все
    периоды масштабируются одинаково и контрольные числа P13 по направлениям
    сходятся точно.
    """
    f26_total = _FOT_HALF_TOTALS["f26_h1"]
    grain = []
    for ctry, g1, g2, direction, cfon, chan, code, f26_t in _FOT_HALF_SPECS:
        w = f26_t / f26_total
        row = {"g1": g1, "g2": g2, "dir": direction, "cfon": cfon, "cfol": cfon,
               "chan": chan, "ctry": ctry, "cfo": code}
        for key, total in _FOT_HALF_TOTALS.items():
            row[key] = round(total * 1000.0 * w, 2)
        if cfon in _FOT_HALF_F25_CUT:
            for key in ("f25_h1", "f25_h2"):
                row[key] = round(row[key] * 0.5, 2)
        grain.append(row)
    # Срезанный f25 KZ/UZ компенсируем в первую строку: «Итого» остаётся ровно
    # контрольным (199 306 тыс за 2025), светофор стран при этом не весь зелёный.
    for key in ("f25_h1", "f25_h2"):
        deficit = round(_FOT_HALF_TOTALS[key] * 1000.0 - sum(r[key] for r in grain), 2)
        grain[0][key] = round(grain[0][key] + deficit, 2)
    for r in grain:
        r["f25"] = round(r["f25_h1"] + r["f25_h2"], 2)
        r["rv_f25"] = round(r["rv_f25_h1"] + r["rv_f25_h2"], 2)
    den = {k: v * 1000.0 for k, v in _FOT_HALF_DEN.items()}
    return grain, den


def _fot_half_mock_dashboard(tab: str, params: dict) -> dict:
    """Сборка P10/P11 мока теми же функциями, что и боевой ответ.

    app.fot._p10_assemble/_p11_assemble получают мок-зерно — формулы (доли,
    Δ, светофор) и форма ответа не могут разойтись с боем. P11 берёт и мок
    численности P12 (_fot_hc_mock) для панели и карточек стран. Срезы на
    числа не влияют (это мок): страна карточек — None, пути дрилла берутся
    из запроса.
    """
    from app import fot

    def _first(value, default: str) -> str:
        if isinstance(value, (list, tuple)):
            return str(value[0]) if value else default
        return str(value) if value not in (None, "") else default

    currency = _first(params.get("currency"), "BYN").upper()
    if currency not in fot.FOT_CURRENCIES:
        currency = "BYN"
    grain, den = _fot_half_mock()
    headcount = cards_by_country = None
    if tab == "p10":
        cards, tables, warning = fot._p10_assemble(currency, grain, den, None)
        methodology = fot.METHODOLOGY_P10
    else:
        cards, headcount, cards_by_country, tables, warning = fot._p11_assemble(
            currency, grain, den, _fot_hc_mock(), None,
            fot._parse_str_list(params.get("matrix_path")),
            fot._parse_str_list(params.get("channel_path")),
        )
        methodology = fot.METHODOLOGY_P11
    meta = {
        "tab": tab,
        "params": dict(params),
        "currency": currency,
        "periods": fot._periods_meta(),
        "closed_period": _FOT_MOCK_STATE["closed_period"],
        "stale": False,
        "refreshed_at": _FOT_MOCK_STATE["refreshed_at"],
        "methodology": methodology,
    }
    meta.update(_FOT_MOCK_META)
    if warning:
        meta["warning"] = warning
    out = {"cards": cards, "charts": [], "tables": tables, "meta": meta}
    if tab == "p11":
        out["headcount"] = headcount
        out["cards_by_country"] = cards_by_country
    return out


# ── P12 «Численность план и факт»: зерно под контрольные числа ────────────────

# Группа ЦФО × страна (ставки): суммы по столбцам — контрольные числа PBI
# (сверка 13.10.2026): план утв. = 7 353,2; факт 30.06.2025 = 7 205,9;
# факт 30.06.2026 = 7 203,16. Укомплектованность стран даёт все три цвета
# светофора: BY 98,8 % (зелёный), RU 93,2 % (жёлтый), UZ 45,6 % (красный).
_FOT_HC_MOCK = (
    # ctry, группа ЦФО, факт 1ПГ25, факт 1ПГ26, план утв., план тактика
    ("BY", "Магазины", 2455.10, 2450.30, 2480.00, 2500.00),
    ("BY", "Производство", 2975.30, 2980.45, 3050.00, 3070.00),
    ("BY", "Складская логистика", 638.40, 640.20, 645.00, 650.00),
    ("BY", "Отделы", 415.60, 412.55, 385.00, 420.00),
    ("RU", "Магазины", 385.20, 380.00, 395.00, 398.00),
    ("RU", "Производство", 122.10, 120.00, 125.00, 126.00),
    ("RU", "Отделы", 97.60, 96.40, 120.00, 94.00),
    ("KZ", "Магазины", 79.10, 78.60, 80.00, 81.00),
    ("KZ", "Отделы", 21.80, 21.30, 22.00, 22.00),
    ("UZ", "Магазины", 12.30, 19.86, 45.00, 45.50),
    ("UZ", "Производство", 3.40, 3.50, 6.20, 6.00),
)


def _fot_hc_mock() -> list[dict]:
    """Зерно P12: факт 2025/2026 + план по группа ЦФО × страна (ставки)."""
    return [
        {"g1": g1, "ctry": ctry, "q25": q25, "q26": q26, "pa": pa, "pt": pt}
        for ctry, g1, q25, q26, pa, pt in _FOT_HC_MOCK
    ]


# ── P13 «HR-бюджет и отделы»: статьи HR-ЦФО под контрольные числа ─────────────

# Факт 1ПГ26 по (код ЦФО, статья) в тыс. — контрольные числа PBI (13.10.2026):
# 224 — ЗП 592,29 / все статьи 727,3; шесть HR-ЦФО вместе — ЗП 990,5 / все
# статьи 1 132,7. Остальные периоды — фиксированные коэффициенты к Факту
# 1ПГ26 (в моке важна форма и Δ-светофор, а не бюджеты).
_FOT_HR_F26 = {
    ("224", "Заработная плата"): 592.29,
    ("224", "Прочие расходы на персонал"): 105.01,
    ("224", "Амортизация"): 30.00,
    ("604", "Заработная плата"): 180.00,
    ("604", "Прочие расходы на персонал"): 4.19,
    ("324", "Заработная плата"): 95.50,
    ("346", "Заработная плата"): 60.00,
    ("208", "Заработная плата"): 40.20,
    ("444", "Заработная плата"): 22.51,
    ("444", "Амортизация"): 3.00,
}

# (f25_h1, f25_h2, b26_h1, b26_h2) — доли Факта 1ПГ26; тактика 2ПГ26 — по коду,
# чтобы Δ год к 2025 дал и зелёные (604/208/346), и жёлтые (224/324/444)
# светофоры: красный у HR-таблицы не бывает (доли ФОТ нет).
_FOT_HR_SCALE = {"f25_h1": 0.88, "f25_h2": 0.94, "b26_h1": 0.97, "b26_h2": 1.03}
_FOT_HR_T26 = {"224": 1.05, "604": 1.00, "324": 1.02, "346": 0.95, "208": 1.00, "444": 1.08}


def _fot_hr_mock() -> list[dict]:
    """Строки HR-бюджета P13 (код ЦФО × статья) в сырых суммах витрины."""
    from app import fot

    hr_by_code = {code: ctry for ctry, codes in fot.HR_CFO_CODES.items() for code in codes}
    rows = []
    for (code, tmpl), f26 in _FOT_HR_F26.items():
        row = {"ctry": hr_by_code[code], "cfo": code, "tmpl": tmpl,
               "f26_h1": round(f26 * 1000.0, 2)}
        for key, k in _FOT_HR_SCALE.items():
            row[key] = round(f26 * k * 1000.0, 2)
        row["t26_h2"] = round(f26 * _FOT_HR_T26[code] * 1000.0, 2)
        rows.append(row)
    return rows


def _fot_first(value, default: str) -> str:
    """Первое значение repeated-параметра или дефолт (срезы мок игнорирует)."""
    if isinstance(value, (list, tuple)):
        return str(value[0]) if value else default
    return str(value) if value not in (None, "") else default


def _fot_mock_meta(tab: str, params: dict, currency: str, methodology: str) -> dict:
    """Общая meta вкладок P10–P13 мока: состояние витрин — из _FOT_MOCK_STATE,
    контрольные числа шага 1 — в meta (см. _FOT_MOCK_META)."""
    from app import fot

    meta = {
        "tab": tab,
        "params": dict(params),
        "currency": currency,
        "periods": fot._periods_meta(),
        "closed_period": _FOT_MOCK_STATE["closed_period"],
        "stale": False,
        "refreshed_at": _FOT_MOCK_STATE["refreshed_at"],
        "methodology": methodology,
    }
    meta.update(_FOT_MOCK_META)
    return meta


def _fot_p12_mock_dashboard(params: dict) -> dict:
    """P12 моком: боевой сборщик app.fot._p12_assemble на мок-зерне численности
    (контрольные суммы — в комментарии к _FOT_HC_MOCK). Срезы на числа не
    влияют (это мок)."""
    from app import fot

    currency = _fot_first(params.get("currency"), "BYN").upper()
    if currency not in fot.FOT_CURRENCIES:
        currency = "BYN"
    cards, tables, warning = fot._p12_assemble(_fot_hc_mock())
    meta = _fot_mock_meta("p12", params, currency, fot.METHODOLOGY_P12)
    if warning:
        meta["warning"] = warning
    return {"cards": cards, "charts": [], "tables": tables, "meta": meta}


def _fot_p17_mock_dashboard(params: dict) -> dict:
    """P17 моком: боевой сборщик app.fot._p17_assemble на мок-зерне численности
    (то же зерно, что у P12 — свод по всей группе компаний)."""
    from app import fot

    currency = _fot_first(params.get("currency"), "BYN").upper()
    if currency not in fot.FOT_CURRENCIES:
        currency = "BYN"
    cards, tables, warning = fot._p17_assemble(_fot_hc_mock())
    meta = _fot_mock_meta("p17", params, currency, fot.METHODOLOGY_P17)
    if warning:
        meta["warning"] = warning
    return {"cards": cards, "charts": [], "tables": tables, "meta": meta}


def _fot_p13_mock_dashboard(params: dict) -> dict:
    """P13 моком: боевой сборщик app.fot._p13_assemble. HR-статьи — свой мок
    под контрольные числа PBI, ФОТ отделов — зерно P10/P11 (его направления
    «Магазины/Производство/Складская логистика/Отделы» дают контрольные числа
    слайдов: ?direction=Магазины → BY 13 174 тыс факт 1ПГ26 и т.д.), численность
    карточек — суммы зерна P12. Из срезов мок применяет только направление —
    к зерну отделов; остальные на числа не влияют (это мок)."""
    from app import fot

    currency = _fot_first(params.get("currency"), "BYN").upper()
    if currency not in fot.FOT_CURRENCIES:
        currency = "BYN"
    grain, _den = _fot_half_mock()
    directions = set(fot._parse_str_list(params.get("direction")))
    if directions:
        grain = [r for r in grain if r.get("dir") in directions]
    hc_rows = _fot_hc_mock()
    cards, tables, warning = fot._p13_assemble(
        currency, _fot_hr_mock(), grain,
        {"q": round(sum(r["q26"] for r in hc_rows), 2)},
        {"pt": round(sum(r["pt"] for r in hc_rows), 2)})
    meta = _fot_mock_meta("p13", params, currency, fot.METHODOLOGY_P13)
    if warning:
        meta["warning"] = warning
    return {"cards": cards, "charts": [], "tables": tables, "meta": meta}


# ── P14 «ФОТ по направлениям бизнеса»: зерно под числа листа 4 деки ──────────

# Направления BY (тыс. BYN; выручка — в тех же тыс., ставки): ФОТ-периоды и
# численность сняты с листа 4 деки v16, выручка — собственная по ЦФО
# направлений (живые пропорции: розница РБ ~116 млн 1ПГ25, опт/прочее ~140,
# ИМ ~8, картонаж ~0,8; у производства своих продаж почти нет). Знаменатель
# «беспродажных» направлений (производство, отделы) — вся выручка среза —
# досчитывает боевой сборщик (fot._P14_OWN_REVENUE), поэтому их доли здесь
# получаются «как в деке» (~12 % / ~9 %), а не от собственной выручки.
# «Ожидание» — факт+тактика (формула витрины), с декой расходится на единицы
# тыс: у деки слагаемые из разных кэшей.
_FOT_P14_MOCK = (
    # direction, f25_h1, f25_h2, b26_h1, f26_h1, b26_h2, t26_h2,
    # rv_f25_h1, rv_f25_h2, rv_f25, rv_f26_h1, rv_t26_h2, pa, q26, q25
    ("Производство", 30880, 31817, 31130, 32123, 32202, 33210,
     2218, 2200, 4418, 2300, 2400, 2441.9, 2342.2, 2283.9),
    ("Производство картонажное", 101, 144, 190, 188, 193, 193,
     802, 900, 1702, 979, 1100, 11.0, 12.0, 11.0),
    ("Отделы", 23604, 26013, 25585, 26644, 25866, 27651,
     140300, 155000, 295300, 170000, 190000, 821.2, 792.3, 815.8),
    ("Магазины", 10929, 12237, 11440, 13174, 11725, 13191,
     115529, 128000, 243529, 131740, 155179, 1159.8, 1136.0, 1084.0),
    ("Интернет-магазин", 253, 267, 310, 260, 310, 259,
     8161, 9000, 17161, 10000, 12745, 12.0, 12.0, 10.0),
)


def _fot_p14_mock_payload() -> dict:
    """Ответ _p14_sql моком: зерно/выручка/численность по направлениям BY.

    ФОТ и выручка константы держат в тыс (как на листе деки), сюда приезжают
    сырыми суммами витрины (×1000) — те же единицы, что у SQL и боевых
    сборщиков; численность — ставки, без множителя."""
    grain, rev, h25, h26, plan = [], [], [], [], []
    for (label, f25_h1, f25_h2, b26_h1, f26_h1, b26_h2, t26_h2,
         rv1, rv2, rv3, rv4, rv5, pa, q26, q25) in _FOT_P14_MOCK:
        grain.append({"dir": label, "f25_h1": f25_h1 * 1000.0,
                      "f25_h2": f25_h2 * 1000.0,
                      "f25": (f25_h1 + f25_h2) * 1000.0, "b26_h1": b26_h1 * 1000.0,
                      "f26_h1": f26_h1 * 1000.0, "b26_h2": b26_h2 * 1000.0,
                      "t26_h2": t26_h2 * 1000.0})
        rev.append({"dir": label, "rv_f25_h1": rv1 * 1000.0, "rv_f25_h2": rv2 * 1000.0,
                    "rv_f25": rv3 * 1000.0, "rv_f26_h1": rv4 * 1000.0,
                    "rv_t26_h2": rv5 * 1000.0})
        h25.append({"dir": label, "q25": q25})
        h26.append({"dir": label, "q26": q26})
        plan.append({"dir": label, "pa": pa})
    return {"grain": grain, "rev": rev, "h25": h25, "h26": h26, "plan": plan}


def _fot_p14_mock_dashboard(params: dict) -> dict:
    """P14 моком: боевой сборщик app.fot._p14_assemble на зерне листа 4 деки
    (контрольные числа — в комментарии к _FOT_P14_MOCK). Срезы на числа не
    влияют (это мок): шапка листа — Беларусь BY."""
    from app import fot

    currency = _fot_first(params.get("currency"), "BYN").upper()
    if currency not in fot.FOT_CURRENCIES:
        currency = "BYN"
    cards, headcount, directions, tables, warning = fot._p14_assemble(
        currency, _fot_p14_mock_payload(), "BY")
    meta = _fot_mock_meta("p14", params, currency, fot.METHODOLOGY_P14)
    meta["slide"] = {"cc": "BY", "label": "Беларусь"}
    if warning:
        meta["warning"] = warning
    return {"cards": cards, "headcount": headcount, "directions": directions,
            "charts": [], "tables": tables, "meta": meta}


# ── P15/P16 «Отделы»: зерно под числа листов 7–8 деки ─────────────────────────

# Направления группы ЦФО «Отдел» среза BY (тыс. BYN, ставки) — колонки
# таблицы листа 7 декы v16. «Складская логистика» декы разложена на пару
# Складская + Транспортная (живая пропорция бюджета 5654/1322), боевой
# сборщик объединяет их (_P15_DEPT_MERGE) — контрольные 7300/7344/6976
# сохраняются. «Ожидание» декы — из CFO-свода и не равно факт+тактика
# (у Складской 14 731 против 14 644 у формулы витрины): мок держит факт и
# тактику декы, «Ожидание» собирается формулой витрины, как у P14.
_FOT_P15_MOCK = (
    # dir, f25_h1, f25_h2, b26_h1, f26_h1, b26_h2, t26_h2, pa, q26, q25
    ("Складская логистика", 5353, 5727, 5654, 6064, 5654, 6122, 328.0, 304.9, 332.2),
    ("Транспортная логистика", 1340, 1376, 1322, 1236, 1322, 1222, 44.4, 44.0, 48.2),
    ("HR", 522, 572, 579, 592, 585, 590, 24.5, 24.5, 25.0),
    ("Аналитики", 267, 393, 448, 480, 464, 377, 9.0, 9.0, 9.0),
    ("Аналитики BI", 0, 17, 46, 47, 46, 46, 1.0, 1.0, None),
    ("Генеральная администрация", 790, 879, 836, 1010, 836, 962, 14.3, 13.3, 13.0),
    ("Дизайнеры", 1274, 1247, 1388, 1230, 1388, 1261, 28.5, 26.0, 30.0),
    ("Закупки", 713, 705, 750, 709, 750, 703, 16.5, 17.5, 18.5),
    ("КБ", 816, 907, 1016, 916, 1016, 958, 25.5, 26.0, 23.0),
    ("КРУ", 524, 450, 472, 510, 472, 512, 24.5, 24.5, 22.5),
    ("Маркетинг", 1267, 1270, 1025, 1015, 1025, 1015, 29.0, 28.0, 28.3),
    ("ОКК", 0, 126, 139, 132, 152, 134, 7.0, 7.0, 6.0),
    ("Отдел IT", 1362, 1557, 1390, 1608, 1436, 2020, 30.5, 31.5, 28.5),
    ("Отдел ОТ", 147, 98, 106, 110, 106, 103, 4.0, 4.0, 3.3),
    ("Отдел планирования ЧНП", 292, 733, 556, 609, 556, 568, 12.0, 13.0, 10.0),
    ("Отдел подготовки производства", 336, 356, 351, 342, 351, 351, 11.0, 10.0, 11.0),
    ("ОУиР", 648, 721, 770, 628, 770, 770, 15.0, 13.0, 16.0),
    ("ОУиР ЧНП", 0, 0, 231, 198, 231, 201, 5.0, 5.0, 4.0),
    ("Строители", 2466, 2756, 1917, 2860, 1917, 2848, 94.5, 91.0, 92.5),
    ("Товародвижение", 456, 530, 548, 500, 541, 538, 14.0, 14.3, 13.0),
    ("УКК", 421, 391, 411, 401, 405, 409, 14.0, 16.3, 13.3),
    ("ФЭО и Бухгалтерия", 1685, 1776, 1812, 1964, 1812, 2073, 69.0, 68.5, 68.5),
    # у «Группы аудита…» строк численности нет — как на листе декы
    ("Группа аудита производственного качества", 0, 0, 0, 127, 0, 153, 0.0, None, None),
)

# Выручка среза BY (тыс.) — живые пропорции витрины (округлено): знаменатель
# кругов «доля ФОТ в выручке, п.п.» на карточках.
_FOT_P15_RV_MOCK = {
    "rv_f25_h1": 267400, "rv_f25_h2": 281000, "rv_f25": 548400,
    "rv_f26_h1": 274800, "rv_t26_h2": 322900,
}


def _fot_p15_mock_payload() -> dict:
    """Ответ _p15_sql моком: ФОТ/численность отделов и выручка среза.

    ФОТ константы держат в тыс (как на листе декы), сюда приезжают сырыми
    суммами витрины (×1000); численность — ставки, без множителя."""
    fot_rows, h25, h26, plan = [], [], [], []
    for (label, f25_h1, f25_h2, b26_h1, f26_h1, b26_h2, t26_h2,
         pa, q26, q25) in _FOT_P15_MOCK:
        fot_rows.append({"dir": label, "f25_h1": f25_h1 * 1000.0,
                         "f25_h2": f25_h2 * 1000.0,
                         "f25": (f25_h1 + f25_h2) * 1000.0,
                         "b26_h1": b26_h1 * 1000.0, "f26_h1": f26_h1 * 1000.0,
                         "b26_h2": b26_h2 * 1000.0, "t26_h2": t26_h2 * 1000.0})
        if q25 is not None:
            h25.append({"dir": label, "v": q25})
        if q26 is not None:
            h26.append({"dir": label, "v": q26})
        if pa:
            plan.append({"dir": label, "v": pa})
    return {"fot": fot_rows, "rev": {k: v * 1000.0 for k, v in _FOT_P15_RV_MOCK.items()},
            "h25": h25, "h26": h26, "plan": plan}


def _fot_p15_mock_dashboard(params: dict, tab: str) -> dict:
    """P15/P16 моком: боевые сборщики на общем зерне _p15_assemble (контрольные
    числа — в комментарии к _FOT_P15_MOCK). Срезы на числа не влияют (это
    мок): шапка листа — Беларусь BY."""
    from app import fot

    currency = _fot_first(params.get("currency"), "BYN").upper()
    if currency not in fot.FOT_CURRENCIES:
        currency = "BYN"
    (cards, headcount, departments, tables, trends, banners,
     warning) = fot._p15_assemble(currency, _fot_p15_mock_payload(), "BY")
    methodology = fot.METHODOLOGY_P15 if tab == "p15" else fot.METHODOLOGY_P16
    meta = _fot_mock_meta(tab, params, currency, methodology)
    meta["slide"] = {"cc": "BY", "label": "Беларусь"}
    if warning:
        meta["warning"] = warning
    out: dict = {"cards": cards, "headcount": headcount,
                 "cards_by_department": departments, "charts": [], "meta": meta}
    if tab == "p15":
        out["dept_trends"] = trends
        out["tables"] = tables
    else:
        out["dept_banners"] = banners
        out["tables"] = []
    return out


def fot_dashboard(tab: str, params: dict) -> dict:
    """Мок вкладки ФОТ-дашборда — форма ответа 1:1 с app.fot.dashboard().

    P9 всегда отдаёт один контрольный срез (июнь 2025, BYN): числа сняты с
    витрин шага 1 и PBI, чтобы фронт сверял рендер на них. Срезы из запроса
    приезжают в meta.params, на числа не влияют — это мок. P10–P16 собираются
    боевыми сборщиками app.fot на выдуманном зерне, подобранном под контрольные
    числа PBI и презентации (см. _fot_half_mock, _FOT_HC_MOCK, _FOT_HR_F26,
    _FOT_P14_MOCK, _FOT_P15_MOCK).
    """
    from app import fot

    if tab not in ("p9", "p10", "p11", "p12", "p13", "p14", "p15", "p16", "p17"):
        raise ValueError("fot: неизвестная вкладка %r" % (tab,))
    if tab in ("p10", "p11"):
        return _fot_half_mock_dashboard(tab, params)
    if tab == "p12":
        return _fot_p12_mock_dashboard(params)
    if tab == "p17":
        return _fot_p17_mock_dashboard(params)
    if tab == "p13":
        return _fot_p13_mock_dashboard(params)
    if tab == "p14":
        return _fot_p14_mock_dashboard(params)
    if tab in ("p15", "p16"):
        return _fot_p15_mock_dashboard(params, tab)

    def _first(value, default: str) -> str:
        if isinstance(value, (list, tuple)):
            return str(value[0]) if value else default
        return str(value) if value not in (None, "") else default

    scenario = _first(params.get("scenario_plan"), "тактика")
    scenario_label = dict(fot.SCENARIO_OPTIONS).get(scenario, scenario)
    currency = _first(params.get("currency"), "BYN").upper()
    if currency not in fot.FOT_CURRENCIES:
        currency = "BYN"

    # Контрольные числа июня-2025 (BYN): ФОТ/численность/выручка сняты с витрин
    # шага 1 и PBI, лимит — иллюстративный (якоря на лимит июня-2025 нет);
    # производные считаем здесь же, чтобы карточки, матрица и график не
    # расходились между собой.
    fot_fact = 16818034.73
    fot_limit = 17250000.00
    rev_fact = 104838622.76
    headcount = 6995.91

    cards = [
        {"key": "fot_fact", "label": "ФОТ факт за месяц",
         "value": round(fot_fact, 2), "unit": currency},
        {"key": "fot_limit", "label": "ФОТ лимит (%s)" % scenario_label,
         "value": round(fot_limit, 2), "unit": currency},
        {"key": "fot_share", "label": "ФОТ, % от выручки",
         "value": round(fot_fact / rev_fact * 100, 2), "unit": "%"},
        {"key": "headcount", "label": "Численность",
         "value": round(headcount, 2), "unit": "ставок"},
        {"key": "fot_per_head", "label": "ФОТ на сотрудника",
         "value": round(fot_fact / headcount, 2), "unit": currency},
    ]

    # Серия июня предшествующих месяцев подобрана так, что сумма ФОТ за
    # 2025-01..2025-06 равна контрольной карточке месяца.
    fot_by_month = [2700000.0, 2750000.0, 2800000.0, 2850000.0, 2900000.0, 2818034.73]
    hc_by_month = [6800.0, 6850.0, 6900.0, 6950.0, 6970.0, 6995.91]
    labels = ["2025-%02d" % m for m in range(1, len(fot_by_month) + 1)]
    charts = [{
        "key": "monthly",
        "label": "ФОТ и численность по месяцам",
        "type": "bar_line",
        "labels": labels,
        "series": [
            {"key": "fot_fact", "label": "ФОТ факт", "type": "bar",
             "unit": currency, "values": [round(v, 2) for v in fot_by_month]},
            {"key": "headcount", "label": "Численность", "type": "line", "axis": "y2",
             "unit": "ставок", "values": [round(v, 2) for v in hc_by_month]},
        ],
    }]

    def _row(label, hc, plan, fot_f, fot_l, rev):
        return {
            "label": label,
            "drill_value": label,
            "headcount": round(hc, 2),
            "headcount_plan": round(plan, 2),
            "headcount_dev": round(hc - plan, 2),
            "fot_fact": round(fot_f, 2),
            "fot_limit": round(fot_l, 2),
            "fot_share_pct": round(fot_f / rev_fact * 100, 2),
            "limit_share_pct": round(fot_l / rev_fact * 100, 2),
            "country_share_pct": round(fot_f / rev_fact * 100, 2),
            "fot_per_head": round(fot_f / hc, 2) if hc else None,
            "revenue_per_head": round(rev / hc, 2) if hc else None,
        }

    # Строки суммируются в карточные значения: розница+производство+логистика
    # + управление = ФОТ/численность/выручка месяца.
    rows = [
        _row("Розничная сеть", 3000.00, 3050.00, 7200000.00, 7400000.00, 45000000.00),
        _row("Производство", 2200.00, 2250.00, 5100000.00, 5250000.00, 33000000.00),
        _row("Логистика", 1000.00, 1010.00, 2300000.00, 2360000.00, 15000000.00),
        _row("Управление", 795.91, 800.00, 2218034.73, 2240000.00, 11838622.76),
    ]
    columns = [
        {"key": "label", "label": "Группа ЦФО"},
        {"key": "headcount", "label": "Численность", "unit": "ставок"},
        {"key": "headcount_plan", "label": "Числ. план (тактика)", "unit": "ставок"},
        {"key": "headcount_dev", "label": "Откл. от плана", "unit": "ставок"},
        {"key": "fot_fact", "label": "ФОТ факт", "unit": currency},
        {"key": "fot_limit", "label": "ФОТ лимит", "unit": currency},
        {"key": "fot_share_pct", "label": "ФОТ, % от выручки", "unit": "%"},
        {"key": "limit_share_pct", "label": "Лимит, % от выручки", "unit": "%"},
        {"key": "country_share_pct", "label": "% от выручки своей страны", "unit": "%"},
        {"key": "fot_per_head", "label": "ФОТ на сотрудника", "unit": currency},
        {"key": "revenue_per_head", "label": "Выручка на сотрудника", "unit": currency},
    ]
    tables = [{
        "key": "by_department",
        "label": "По подразделениям",
        "columns": columns,
        "rows": rows,
        "meta": {
            "dim": "group_cfo1",
            "level_label": "Группа ЦФО",
            "path": [],
            "can_drill": True,
            "truncated": False,
            "row_limit": fot.MATRIX_ROW_LIMIT,
        },
    }]

    meta = {
        "tab": "p9",
        "params": dict(params),
        "period": {"year": 2025, "month": 6, "month_date": "2025-06-01"},
        "currency": currency,
        "scenario_plan": scenario,
        "scenario_plan_label": scenario_label,
        "closed_period": _FOT_MOCK_STATE["closed_period"],
        "stale": False,
        "refreshed_at": _FOT_MOCK_STATE["refreshed_at"],
        "matrix": {
            "dim": "group_cfo1",
            "label": "Группа ЦФО",
            "path": [],
            "can_drill": True,
            "levels": [{"key": k, "label": lbl} for k, lbl in fot.MATRIX_LEVELS],
        },
        "methodology": fot.METHODOLOGY_P9,
    }
    meta.update(_FOT_MOCK_META)
    return {"cards": cards, "charts": charts, "tables": tables, "meta": meta}
