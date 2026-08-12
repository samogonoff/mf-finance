#!/usr/bin/env python3
"""Сборка переводов Superset из .po: оба слоя, с отбраковкой битых строк.

ЗАЧЕМ ЭТО НУЖНО
===============
Официальный образ apache/superset поставляет переводы ТОЛЬКО в исходниках:
в `superset/translations/<locale>/LC_MESSAGES/` лежит `messages.po`, а
скомпилированных файлов нет ни одного (проверено на 6.1.0 — ни `.mo`, ни
`.json` во всём каталоге). Поэтому интерфейс остаётся английским, сколько бы
ни выставляли `BABEL_DEFAULT_LOCALE` и `LANGUAGES` в superset_config.py.

У Superset ДВА независимых слоя перевода, и нужны оба:

  1. Бэкенд (Flask-Babel) читает `messages.mo`.
  2. Фронтенд (React) читает `messages.json` в формате Jed 1.x — см.
     `superset/translations/utils.py::get_language_pack`. Если файла нет,
     функция молча логирует ошибку и отдаёт английский пакет.

Основная часть интерфейса — React, поэтому без `.json` перевод почти не виден.

ПОЧЕМУ НЕ pybabel И НЕ po2json
==============================
`pybabel compile` на русском каталоге Superset 6.1.0 падает: 13 строк имеют
несовместимые плейсхолдеры (например, в переводе появился `%(error)s`, которого
нет в оригинале). Это дефекты апстримного перевода, а не нашей сборки.

Игнорировать их нельзя: битый плейсхолдер — это исключение при подстановке
в рантайме, то есть страница, которая падает вместо того, чтобы показать текст.
Поэтому такие строки ОТБРАКОВЫВАЮТСЯ и остаются английскими; сколько именно —
скрипт печатает, чтобы регресс в апстриме было видно.

`.json` в апстриме собирает npm-пакет `po2json` (`--domain superset
--format jed1.x --fuzzy`, см. `superset-frontend/scripts/po2json.sh`), но в
production-образе Node нет, а тот же формат собирается из `babel`, который
уже стоит как зависимость Flask-Babel.

ФОРМАТ JED 1.X
==============
    {
      "domain": "superset",
      "locale_data": {
        "superset": {
          "": {"domain": "superset", "lang": "ru", "plural_forms": "..."},
          "Dashboards": [null, "Дашборды"],
          "%s row": ["%s rows", "%s строка", "%s строки", "%s строк"]
        }
      }
    }

Первый элемент массива — msgid множественного числа либо null, дальше формы
перевода. Ключ с контекстом склеивается через EOT (0x04), как в gettext.
"""

from __future__ import annotations

import json
import sys
from pathlib import Path

from babel.messages.mofile import write_mo
from babel.messages.pofile import read_po

DOMAIN = "superset"
# Разделитель «контекст + msgid» из gettext — EOT (0x04), как у po2json.
CONTEXT_SEP = "\x04"


def load_catalog(po_path: Path, locale: str):
    """Читает каталог и выбрасывает строки с битыми плейсхолдерами."""
    with po_path.open(encoding="utf-8") as fp:
        catalog = read_po(fp, locale=locale, domain=DOMAIN)

    broken: list[tuple] = []
    for message, errors in catalog.check():
        if errors and message.id:
            broken.append((message.id, message.context))

    for msg_id, context in broken:
        catalog.delete(msg_id, context=context)

    return catalog, len(broken)


def build_pack(catalog, locale: str) -> tuple[dict, int]:
    entries: dict[str, list] = {}

    for message in catalog:
        if not message.id:
            continue  # заголовок каталога

        if isinstance(message.id, (list, tuple)):
            plural = message.id[1]
            forms = list(message.string or ())
            # Половинчатый перевод множественного числа хуже английского:
            # часть форм осталась бы пустыми строками в интерфейсе.
            if not any(forms):
                continue
            key, value = message.id[0], [plural, *forms]
        else:
            text = message.string or ""
            if not text:
                continue
            key, value = message.id, [None, text]

        if message.context:
            key = f"{message.context}{CONTEXT_SEP}{key}"

        entries[key] = value

    header = {"domain": DOMAIN, "lang": locale}
    if catalog.plural_expr:
        header["plural_forms"] = (
            f"nplurals={catalog.num_plurals}; plural={catalog.plural_expr};"
        )

    pack = {"domain": DOMAIN, "locale_data": {DOMAIN: {"": header, **entries}}}
    return pack, len(entries)


def main() -> int:
    if len(sys.argv) < 3:
        print(
            "использование: build_translations.py <каталог translations> <локаль>…",
            file=sys.stderr,
        )
        return 2

    root = Path(sys.argv[1])
    failed = False

    for locale in sys.argv[2:]:
        po_path = root / locale / "LC_MESSAGES" / "messages.po"
        if not po_path.is_file():
            print(f"нет файла {po_path}", file=sys.stderr)
            failed = True
            continue

        catalog, broken = load_catalog(po_path, locale)
        total = sum(1 for m in catalog if m.id)

        mo_path = po_path.with_suffix(".mo")
        with mo_path.open("wb") as fp:
            write_mo(fp, catalog, use_fuzzy=True)

        pack, translated = build_pack(catalog, locale)
        json_path = po_path.with_suffix(".json")
        json_path.write_text(
            json.dumps(pack, ensure_ascii=False, separators=(",", ":")),
            encoding="utf-8",
        )

        print(
            f"{locale}: переведено {translated} из {total} строк, "
            f"отбраковано битых {broken}; "
            f"messages.mo {mo_path.stat().st_size // 1024} КБ, "
            f"messages.json {json_path.stat().st_size // 1024} КБ"
        )

    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
