"""
Тонкий клиент к Finance Go-API /internal/notifications.

Используется FastAPI-роутами раздела «Себестоимость», чтобы создавать
уведомления админам кабинета при значимых событиях (массовое сохранение
цен, ошибка импорта и т.п.).

ENV:
  FINANCE_INTERNAL_API     — базовый URL Go-API (например http://go-api:8080)
  INTERNAL_SERVICE_TOKEN   — статический токен (должен совпадать с тем,
                             что задан у Go-API; см. internalapi/handler.go)

Если хотя бы одна переменная пуста — клиент молчаливо no-op'ит, чтобы
сохранение цен не падало из-за конфигурационной проблемы уведомлений.
"""
from __future__ import annotations

import logging
import os
from typing import Any

import httpx

log = logging.getLogger("cost.notify")

_BASE = os.environ.get("FINANCE_INTERNAL_API", "").rstrip("/")
_TOKEN = os.environ.get("INTERNAL_SERVICE_TOKEN", "")
_TIMEOUT = httpx.Timeout(connect=2.0, read=4.0, write=4.0, pool=4.0)


def _enabled() -> bool:
    return bool(_BASE and _TOKEN)


async def notify_admins(
    title: str,
    message: str = "",
    *,
    type: str = "info",
    object_type: str | None = None,
    data: dict[str, Any] | None = None,
) -> None:
    """Создать уведомление для всех носителей ROLE_ADMIN основного кабинета.

    Сетевые ошибки не пробрасываются — пишем в лог и продолжаем.
    """
    if not _enabled():
        log.debug("notify_admins skipped: internal channel not configured")
        return

    payload: dict[str, Any] = {
        "role_target": "ROLE_ADMIN",
        "title": title,
        "message": message,
        "type": type,
    }
    if object_type:
        payload["object_type"] = object_type
    if data:
        payload["data"] = data

    try:
        async with httpx.AsyncClient(timeout=_TIMEOUT) as client:
            resp = await client.post(
                f"{_BASE}/internal/notifications",
                json=payload,
                headers={"X-Internal-Token": _TOKEN},
            )
            if resp.status_code >= 400:
                log.warning(
                    "notify_admins failed: %s %s",
                    resp.status_code,
                    resp.text[:200],
                )
    except httpx.HTTPError as exc:
        log.warning("notify_admins HTTP error: %s", exc)
