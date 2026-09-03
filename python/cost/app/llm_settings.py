"""Подключение к языковой модели: настройки из БД поверх окружения.

Провайдер, ключ и модель настраиваются на `/cost/admin/llm` и живут в таблице
`cost_llm_settings` (миграция 0051). Значение из таблицы ПЕРЕКРЫВАЕТ
одноимённую переменную `COST_LLM_*`, пустое поле означает «взять из
окружения». Отсюда: dev работает по `.env` как раньше, а прод настраивается
через интерфейс, без правки CI-переменной и деплоя стека.

ПОЧЕМУ КЭШ В ПАМЯТИ
===================
Настройки нужны в `insights._api_base()`, `_model()` и прочих — а они
СИНХРОННЫЕ и вызываются на каждом шаге агента. Асинхронное чтение БД оттуда не
сделать, поэтому значения держим в кэше процесса: `refresh()` (async) обновляет
его перед работой, `current()` (sync) отдаёт то, что есть. Кэш живёт `TTL_S`
секунд — этого хватает, чтобы правка в админке применилась почти сразу, и
достаточно, чтобы не ходить в БД на каждый вызов модели.

ПРО SSRF
========
Поле «адрес провайдера» — это управляемый пользователем URL, по которому сервер
сам сделает запрос. Без проверки администратор (или тот, кто получил его права)
заставил бы python-cost стучаться во внутреннюю сеть: postgres, go-api, OLAP,
метаданные облака. Поэтому `validate_api_base` требует https и публичный
адрес; приватные диапазоны и http разрешены только явным флагом
`COST_LLM_ALLOW_PRIVATE=1` — он нужен на dev для локальной Ollama.
"""

from __future__ import annotations

import ipaddress
import logging
import os
import socket
import time
from typing import Any
from urllib.parse import urlparse

from app.logship import log

# Сколько секунд живёт кэш настроек. Двадцать — компромисс: правка в админке
# подхватывается практически сразу, а на длинном исследовании (6 шагов, до двух
# минут) в БД сходим не больше пары раз.
TTL_S = 20.0

_FIELDS = ("enabled", "api_base", "api_key", "model", "reasoning",
           "timeout_s", "agent_budget_s")

_cache: dict[str, Any] | None = None
_cache_at = 0.0


def _env(name: str, default: str = "") -> str:
    return (os.environ.get(name) or default).strip()


def allow_private() -> bool:
    """Разрешены ли приватные адреса провайдера (dev с локальной Ollama)."""
    return _env("COST_LLM_ALLOW_PRIVATE") in ("1", "true", "yes")


# ─── Валидация адреса ────────────────────────────────────────────────────────

def validate_api_base(url: str) -> str:
    """Проверить адрес провайдера. Возвращает нормализованный URL.

    Бросает ValueError с внятным текстом — он попадёт прямо в интерфейс.
    """
    url = (url or "").strip().rstrip("/")
    if not url:
        raise ValueError("Адрес провайдера не задан")
    if len(url) > 300:
        raise ValueError("Адрес слишком длинный")

    parsed = urlparse(url)
    if parsed.scheme not in ("http", "https"):
        raise ValueError("Адрес должен начинаться с https://")
    if not parsed.hostname:
        raise ValueError("В адресе нет имени хоста")
    if parsed.scheme == "http" and not allow_private():
        raise ValueError("Разрешён только https (http — лишь для локального "
                         "провайдера при COST_LLM_ALLOW_PRIVATE=1)")

    # Белый список хостов, если задан: на проде он и должен быть задан.
    allowed = [h.strip().lower() for h in _env("COST_LLM_ALLOWED_HOSTS").split(",")
               if h.strip()]
    host = parsed.hostname.lower()
    if allowed and host not in allowed:
        raise ValueError(f"Хост «{host}» не в списке разрешённых "
                         f"(COST_LLM_ALLOWED_HOSTS): {', '.join(allowed)}")

    if allow_private():
        return url

    # Резолвим и проверяем КАЖДЫЙ адрес: имя может указывать на приватный IP
    # («internal.example.com» → 10.0.0.5) — одной проверки строки мало.
    try:
        infos = socket.getaddrinfo(host, parsed.port or
                                   (443 if parsed.scheme == "https" else 80),
                                   proto=socket.IPPROTO_TCP)
    except socket.gaierror as exc:
        raise ValueError(f"Хост «{host}» не разрешается в адрес: {exc}") from exc

    for info in infos:
        ip = ipaddress.ip_address(info[4][0])
        if (ip.is_private or ip.is_loopback or ip.is_link_local
                or ip.is_reserved or ip.is_multicast):
            raise ValueError(
                f"Хост «{host}» указывает на внутренний адрес {ip} — "
                f"запросы во внутреннюю сеть запрещены. Для локального "
                f"провайдера задайте COST_LLM_ALLOW_PRIVATE=1")
    return url


# ─── Чтение и кэш ────────────────────────────────────────────────────────────

async def refresh(force: bool = False) -> dict[str, Any]:
    """Перечитать настройки из БД в кэш процесса."""
    global _cache, _cache_at

    if not force and _cache is not None and (time.monotonic() - _cache_at) < TTL_S:
        return _cache

    from app.db import pool

    row = None
    try:
        async with pool().acquire() as conn:
            row = await conn.fetchrow(
                "SELECT enabled, api_base, api_key, model, reasoning, timeout_s,"
                "       agent_budget_s, updated_by, updated_at "
                "FROM cost_llm_settings WHERE id = 1")
    except Exception as exc:
        # Таблицы может не быть (миграция не накатана) или БД недоступна —
        # тогда работаем по окружению, как до появления админки.
        log(logging.WARNING, "llm_settings: настройки недоступны",
            error=str(exc)[:200])

    _cache = dict(row) if row else {}
    _cache_at = time.monotonic()
    return _cache


def current() -> dict[str, Any]:
    """Кэш настроек. Пустой словарь — работаем по окружению."""
    return _cache or {}


def invalidate() -> None:
    """Сбросить кэш — вызывается после сохранения в админке."""
    global _cache_at
    _cache_at = 0.0


# ─── Значения с приоритетом БД над окружением ────────────────────────────────

def value(field: str, env_name: str, default: str = "") -> str:
    """Строковое значение: БД → окружение → дефолт."""
    from_db = current().get(field)
    if isinstance(from_db, str) and from_db.strip():
        return from_db.strip()
    return _env(env_name, default)


def int_value(field: str, env_name: str, default: int) -> int:
    from_db = current().get(field)
    if isinstance(from_db, int) and from_db > 0:
        return from_db
    try:
        return int(float(_env(env_name, str(default))))
    except ValueError:
        return default


def enabled() -> bool:
    """Включён ли разбор.

    Галочка в админке имеет приоритет, но включённый без ключа разбор
    бессмысленен: кнопки в интерфейсе появятся, а каждый вызов будет падать.
    Поэтому наличие ключа проверяется всегда.
    """
    has_key = bool(value("api_key", "COST_LLM_API_KEY"))
    from_db = current().get("enabled")
    if isinstance(from_db, bool):
        return from_db and has_key
    return _env("COST_LLM_ENABLED") in ("1", "true", "yes") and has_key


def public_view() -> dict[str, Any]:
    """Настройки для интерфейса: ключ маской, видно, что откуда взято."""
    from app import insights

    db = current()
    key = value("api_key", "COST_LLM_API_KEY")
    updated_at = db.get("updated_at")

    def source(field: str, env_name: str) -> str:
        raw = db.get(field)
        if (isinstance(raw, str) and raw.strip()) or isinstance(raw, (bool, int)):
            return "админка"
        return "окружение" if _env(env_name) else "не задано"

    return {
        "enabled": enabled(),
        "api_base": insights._api_base(),
        "api_key_mask": ("•" * 8 + key[-4:]) if len(key) > 4 else ("задан" if key else ""),
        "api_key_set": bool(key),
        "model": insights._model(),
        "reasoning": insights.reasoning_effort(),
        "timeout_s": int(insights._timeout()),
        "agent_budget_s": int_value("agent_budget_s", "COST_LLM_AGENT_BUDGET", 120),
        "allow_private": allow_private(),
        "allowed_hosts": [h.strip() for h in _env("COST_LLM_ALLOWED_HOSTS").split(",")
                          if h.strip()],
        "sources": {
            "enabled": source("enabled", "COST_LLM_ENABLED"),
            "api_base": source("api_base", "COST_LLM_API_BASE"),
            "api_key": source("api_key", "COST_LLM_API_KEY"),
            "model": source("model", "COST_LLM_MODEL"),
            "reasoning": source("reasoning", "COST_LLM_REASONING"),
        },
        "updated_by": db.get("updated_by") or "",
        "updated_at": updated_at.isoformat() if updated_at else None,
    }


# ─── Сохранение ──────────────────────────────────────────────────────────────

REASONING_LEVELS = ("none", "low", "medium", "high")


async def save(payload: dict, email: str) -> dict[str, Any]:
    """Сохранить настройки. Бросает ValueError для интерфейса."""
    from app.db import pool

    api_base = str(payload.get("api_base") or "").strip()
    if api_base:
        api_base = validate_api_base(api_base)

    model = str(payload.get("model") or "").strip()[:120]
    reasoning = str(payload.get("reasoning") or "").strip().lower()
    if reasoning and reasoning not in REASONING_LEVELS:
        raise ValueError(f"Уровень размышлений должен быть одним из: "
                         f"{', '.join(REASONING_LEVELS)}")

    def positive(name: str, low: int, high: int) -> int | None:
        raw = payload.get(name)
        if raw in (None, "", 0):
            return None
        try:
            num = int(raw)
        except (TypeError, ValueError):
            raise ValueError(f"«{name}» должно быть целым числом") from None
        if not low <= num <= high:
            raise ValueError(f"«{name}» должно быть от {low} до {high}")
        return num

    timeout_s = positive("timeout_s", 5, 600)
    agent_budget_s = positive("agent_budget_s", 20, 900)

    enabled_raw = payload.get("enabled")
    enabled_val = bool(enabled_raw) if enabled_raw is not None else None

    # Пустой ключ в запросе означает «оставить прежний»: интерфейс никогда не
    # получает ключ целиком (только маску) и не может его вернуть.
    new_key = str(payload.get("api_key") or "").strip()

    async with pool().acquire() as conn:
        if new_key:
            await conn.execute(
                "UPDATE cost_llm_settings SET enabled=$1, api_base=$2, api_key=$3,"
                "  model=$4, reasoning=$5, timeout_s=$6, agent_budget_s=$7,"
                "  updated_by=$8, updated_at=now() WHERE id = 1",
                enabled_val, api_base, new_key, model, reasoning, timeout_s,
                agent_budget_s, email)
        else:
            await conn.execute(
                "UPDATE cost_llm_settings SET enabled=$1, api_base=$2, model=$3,"
                "  reasoning=$4, timeout_s=$5, agent_budget_s=$6,"
                "  updated_by=$7, updated_at=now() WHERE id = 1",
                enabled_val, api_base, model, reasoning, timeout_s,
                agent_budget_s, email)

    invalidate()
    await refresh(force=True)
    log(logging.INFO, "llm_settings: настройки изменены", user=email,
        model=value("model", "COST_LLM_MODEL"),
        api_base=value("api_base", "COST_LLM_API_BASE"),
        reasoning=value("reasoning", "COST_LLM_REASONING"),
        key_changed=bool(new_key))
    return public_view()
