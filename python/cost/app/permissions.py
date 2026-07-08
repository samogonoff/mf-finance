from __future__ import annotations

COST_PERMISSIONS: dict[str, str] = {
    "cost:view":           "Просмотр таблицы себестоимости",
    "cost:edit_price":     "Редактирование цен (save-changes / save-batch)",
    "cost:approve":        "Согласование калькуляций (финальное утверждение)",
    "cost:peo_mark":       "Отметки ПЭО (простановка статуса по строкам)",
    "cost:edit_materials": "Редактирование материалов (calculation drafts)",
    "cost:export":         "Экспорт в Excel",
    "cost:admin":          "Управление ролями и пользователями cost",
}
