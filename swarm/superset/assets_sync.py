#!/usr/bin/env python3
"""Синхронизация ассетов Superset между инстансом и git-деревом.

ЗАЧЕМ ЭТОТ СКРИПТ СУЩЕСТВУЕТ
============================
Дашборды, чарты и датасеты Superset живут в его метабазе Postgres, а не в
файлах. «Настроил в UI» = изменения есть только на твоей машине: в git не
попадут и на прод не уедут. Этот скрипт делает метабазу производной от git.

Рабочий цикл:

    1. dev: правим дашборд руками в UI на http://localhost:8093
    2. dev: make superset-assets-export   → YAML-дерево в swarm/superset/assets/
    3. dev: git diff — видно ровно то, что изменилось (читаемый YAML)
    4. git commit / push / merge request
    5. prod: make superset-assets-import (или тот же вызов из CI)

Обратный порядок тоже валиден: правку YAML руками можно накатить в свой dev
инстанс через import и посмотреть результат в UI.

ПОЧЕМУ API, А НЕ CLI
====================
У `superset import-directory` внутри ImportExamplesCommand: он ходит с
ignore_permissions=True и, что важнее, ТРАНСПИЛИРУЕТ SQL виртуальных датасетов
под целевой диалект. Для наших ClickHouse-датасетов это тихая порча запросов.
Эндпоинты /api/v1/assets/{export,import}/ используют ImportAssetsCommand —
без транспиляции, сопоставление строго по uuid.

СЕКРЕТЫ
=======
В git коммитятся YAML подключений с плейсхолдерами ${VAR}, не строки
соединения. Подстановка идёт из окружения в момент импорта, во временную
копию — отрендеренный YAML на диск рядом с git-деревом не попадает.
"""

from __future__ import annotations

import argparse
import io
import os
import re
import sys
import zipfile
from pathlib import Path

import requests

PLACEHOLDER_RE = re.compile(r"\$\{([A-Z0-9_]+)\}")

# Каталоги бандла в порядке, в котором их обрабатывает ImportAssetsCommand.
ASSET_DIRS = ("databases", "queries", "datasets", "charts", "dashboards")

# databases/ — единственный каталог, где git авторитетнее инстанса: там наши
# плейсхолдеры, а экспорт вернул бы sqlalchemy_uri с замаскированным паролем
# (XXXXXXXXXX) и затёр бы их.
EXPORT_SKIP_DIRS = ("databases",)


class SupersetClient:
    """Минимальный клиент: логин, CSRF, экспорт, импорт."""

    def __init__(self, base_url: str, username: str, password: str) -> None:
        self.base_url = base_url.rstrip("/")
        self.session = requests.Session()
        self.access_token = ""
        self.csrf_token = ""
        self._login(username, password)

    def _url(self, path: str) -> str:
        return f"{self.base_url}{path}"

    def _login(self, username: str, password: str) -> None:
        resp = self.session.post(
            self._url("/api/v1/security/login"),
            json={
                "username": username,
                "password": password,
                "provider": "db",
                "refresh": True,
            },
            timeout=60,
        )
        if resp.status_code != 200:
            raise SystemExit(
                f"логин в Superset не прошёл ({resp.status_code}): {resp.text[:500]}"
            )
        self.access_token = resp.json()["access_token"]

        # CSRF-токен нужен для любого POST за @protect(). Заодно на этом
        # запросе Session получает cookie сессии — без неё токен не примут.
        csrf = self.session.get(
            self._url("/api/v1/security/csrf_token/"),
            headers={"Authorization": f"Bearer {self.access_token}"},
            timeout=60,
        )
        if csrf.status_code != 200:
            raise SystemExit(
                f"не удалось получить CSRF-токен ({csrf.status_code}): {csrf.text[:500]}"
            )
        self.csrf_token = csrf.json()["result"]

    def export_bundle(self) -> zipfile.ZipFile:
        resp = self.session.get(
            self._url("/api/v1/assets/export/"),
            headers={"Authorization": f"Bearer {self.access_token}"},
            timeout=600,
        )
        if resp.status_code != 200:
            raise SystemExit(
                f"экспорт не удался ({resp.status_code}): {resp.text[:500]}"
            )
        return zipfile.ZipFile(io.BytesIO(resp.content))

    def import_bundle(self, blob: bytes, overwrite: bool) -> None:
        resp = self.session.post(
            self._url("/api/v1/assets/import/"),
            headers={
                "Authorization": f"Bearer {self.access_token}",
                "X-CSRFToken": self.csrf_token,
                "Referer": self.base_url,
            },
            files={"bundle": ("assets.zip", blob, "application/zip")},
            data={"overwrite": "true" if overwrite else "false"},
            timeout=600,
        )
        if resp.status_code != 200:
            raise SystemExit(
                f"импорт не удался ({resp.status_code}): {resp.text[:2000]}"
            )


def strip_bundle_root(name: str) -> str:
    """Экспорт кладёт всё в папку assets_export_<timestamp>/ — срезаем её."""
    parts = name.split("/", 1)
    return parts[1] if len(parts) == 2 else name


def cmd_export(client: SupersetClient, assets_dir: Path) -> int:
    bundle = client.export_bundle()

    written, skipped_dbs = 0, []
    seen: set[Path] = set()

    for entry in bundle.namelist():
        if entry.endswith("/"):
            continue
        rel = strip_bundle_root(entry)
        top = rel.split("/", 1)[0]

        if top in EXPORT_SKIP_DIRS:
            skipped_dbs.append(Path(rel).name)
            continue
        if top not in ASSET_DIRS and rel != "metadata.yaml":
            continue
        # metadata.yaml в git уже лежит с нужным type: assets — не перезаписываем
        # его тем, что вернул экспорт (там будет свой timestamp = шум в diff).
        if rel == "metadata.yaml":
            continue

        target = assets_dir / rel
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(bundle.read(entry))
        seen.add(target)
        written += 1

    # Удаляем то, что исчезло из инстанса: иначе удалённый в UI чарт останется
    # в git навсегда и вернётся на прод следующим импортом.
    removed = 0
    for sub in ASSET_DIRS:
        if sub in EXPORT_SKIP_DIRS:
            continue
        sub_dir = assets_dir / sub
        if not sub_dir.is_dir():
            continue
        for existing in sub_dir.rglob("*.yaml"):
            if existing not in seen:
                existing.unlink()
                removed += 1

    print(f"экспортировано файлов: {written}, удалено устаревших: {removed}")

    # Подключение, созданное в UI, в git не попадёт — про это надо знать сразу,
    # а не при разборе упавшего импорта на проде.
    known = {p.name for p in (assets_dir / "databases").glob("*.yaml")}
    unknown = [name for name in skipped_dbs if name not in known]
    if unknown:
        print(
            "\n⚠ в инстансе есть подключения, которых нет в git:\n  "
            + "\n  ".join(sorted(unknown))
            + "\n  Опишите их в assets/databases/ вручную (с ${ПЛЕЙСХОЛДЕРОМ}"
            " в sqlalchemy_uri и зашитым uuid), иначе на прод они не уедут.",
            file=sys.stderr,
        )
    return 0


def render(text: str, source: str, strict: bool) -> str | None:
    """Подставляет ${VAR} из окружения. None — если файл надо пропустить."""
    missing: list[str] = []

    def repl(match: re.Match[str]) -> str:
        name = match.group(1)
        value = os.environ.get(name, "")
        if not value:
            missing.append(name)
        return value

    rendered = PLACEHOLDER_RE.sub(repl, text)
    if missing:
        names = ", ".join(sorted(set(missing)))
        if strict:
            raise SystemExit(
                f"{source}: не заданы переменные окружения: {names}. "
                "Заполните их в .env (см. .env.example) или исключите источник."
            )
        print(f"пропускаю {source}: не заданы {names}", file=sys.stderr)
        return None
    return rendered


def cmd_import(client: SupersetClient, assets_dir: Path, overwrite: bool) -> int:
    strict = os.environ.get("SUPERSET_ASSETS_STRICT", "1").strip().lower() not in {
        "0",
        "false",
        "no",
    }

    buf = io.BytesIO()
    included, skipped = 0, 0

    with zipfile.ZipFile(buf, "w", zipfile.ZIP_DEFLATED) as bundle:
        metadata = assets_dir / "metadata.yaml"
        if not metadata.is_file():
            raise SystemExit(f"нет {metadata} — бандл без манифеста не примут")
        bundle.writestr("assets/metadata.yaml", metadata.read_text(encoding="utf-8"))

        for sub in ASSET_DIRS:
            sub_dir = assets_dir / sub
            if not sub_dir.is_dir():
                continue
            for path in sorted(sub_dir.rglob("*.yaml")):
                rel = path.relative_to(assets_dir).as_posix()
                rendered = render(
                    path.read_text(encoding="utf-8"), rel, strict=strict
                )
                if rendered is None:
                    skipped += 1
                    continue
                bundle.writestr(f"assets/{rel}", rendered)
                included += 1

    if included == 0:
        raise SystemExit("в бандле нет ни одного ассета — импортировать нечего")

    print(f"в бандле файлов: {included}, пропущено: {skipped}")
    client.import_bundle(buf.getvalue(), overwrite=overwrite)
    print("импорт завершён")
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("export", "import"))
    parser.add_argument(
        "--assets-dir",
        default=os.environ.get("SUPERSET_ASSETS_DIR", "/app/assets"),
        help="каталог git-дерева ассетов (в контейнере это /app/assets)",
    )
    parser.add_argument(
        "--base-url",
        default=os.environ.get("SUPERSET_INTERNAL_URL", "http://superset:8088"),
    )
    parser.add_argument(
        "--username", default=os.environ.get("SUPERSET_ADMIN_USERNAME", "admin")
    )
    parser.add_argument(
        "--no-overwrite",
        action="store_true",
        help="не перезаписывать существующие объекты (по умолчанию перезаписываем)",
    )
    args = parser.parse_args()

    password = os.environ.get("SUPERSET_ADMIN_PASSWORD", "")
    if not password:
        raise SystemExit("не задан SUPERSET_ADMIN_PASSWORD")

    assets_dir = Path(args.assets_dir)
    if not assets_dir.is_dir():
        raise SystemExit(f"нет каталога ассетов: {assets_dir}")

    client = SupersetClient(args.base_url, args.username, password)

    if args.action == "export":
        return cmd_export(client, assets_dir)
    return cmd_import(client, assets_dir, overwrite=not args.no_overwrite)


if __name__ == "__main__":
    sys.exit(main())
