# Superset config для Finance Cabinet.
#
# Монтируется в контейнер как /app/pythonpath/superset_config.py (см.
# docker-compose.superset.yml). ОДИН И ТОТ ЖЕ файл используется в dev и в prod —
# всё, что различается, приходит из переменных окружения. Не хардкодьте здесь
# ни секреты, ни хосты: каждая новая переменная обязана появиться
# в ../.env.example тем же коммитом (правило проекта, см. CLAUDE.md).

import os
from urllib.parse import urlparse

from cachelib.redis import RedisCache


def env(key: str, default: str = "") -> str:
    return os.environ.get(key, default)


def env_bool(key: str, default: bool = False) -> bool:
    raw = os.environ.get(key)
    if raw is None or raw == "":
        return default
    return raw.strip().lower() in {"1", "true", "yes", "on"}


def env_int(key: str, default: int) -> int:
    raw = os.environ.get(key)
    if raw is None or raw == "":
        return default
    return int(raw)


# ── Базовое ──────────────────────────────────────────────────────────────────

# Без SECRET_KEY Superset откажется стартовать с дефолтом в non-debug режиме.
# В dev значение приходит из .env, в prod — из docker config (см. ci-finance.yml).
SECRET_KEY = env("SUPERSET_SECRET_KEY", "dev-superset-secret-change-me")

# Метабаза самого Superset (дашборды, чарты, датасеты, пользователи).
# ВАЖНО: это НЕ аналитические данные — источники подключаются отдельно,
# через bootstrap_databases.py.
SQLALCHEMY_DATABASE_URI = env(
    "SUPERSET_METADATA_URI",
    "postgresql+psycopg2://superset:superset@postgres-superset:5432/superset",
)
SQLALCHEMY_TRACK_MODIFICATIONS = False

ROW_LIMIT = env_int("SUPERSET_ROW_LIMIT", 50_000)
SQL_MAX_ROW = env_int("SUPERSET_SQL_MAX_ROW", 100_000)
SUPERSET_WEBSERVER_TIMEOUT = env_int("SUPERSET_WEBSERVER_TIMEOUT", 300)

BABEL_DEFAULT_LOCALE = "ru"
LANGUAGES = {
    "ru": {"flag": "ru", "name": "Russian"},
    "en": {"flag": "us", "name": "English"},
}

# Публичный URL инстанса — нужен для permalink'ов, скриншотов и MCP-ссылок
# (explore link, SQL Lab link). Если соврать — агент выдаст нерабочие URL.
WEBDRIVER_BASEURL = env("SUPERSET_PUBLIC_URL", "http://localhost:8093/")
SUPERSET_WEBSERVER_ADDRESS = env("SUPERSET_INTERNAL_URL", "http://superset:8088")

# ── Кэш и результаты запросов (Redis) ────────────────────────────────────────

REDIS_URL = env("SUPERSET_REDIS_URL", "redis://redis-superset:6379")

CACHE_CONFIG = {
    "CACHE_TYPE": "RedisCache",
    "CACHE_DEFAULT_TIMEOUT": 300,
    "CACHE_KEY_PREFIX": "superset_",
    "CACHE_REDIS_URL": f"{REDIS_URL}/1",
}
DATA_CACHE_CONFIG = {**CACHE_CONFIG, "CACHE_KEY_PREFIX": "superset_data_"}
FILTER_STATE_CACHE_CONFIG = {**CACHE_CONFIG, "CACHE_KEY_PREFIX": "superset_filter_"}
EXPLORE_FORM_DATA_CACHE_CONFIG = {**CACHE_CONFIG, "CACHE_KEY_PREFIX": "superset_form_"}

# ── Async-запросы (Celery) ───────────────────────────────────────────────────
#
# Нужны, потому что источники медленные: ClickHouse-факты растут, а MSSQL —
# живой корпоративный OLAP. Синхронный запрос упирается в
# SUPERSET_WEBSERVER_TIMEOUT и рвётся на середине.
#
# Все подключения в assets/databases/ идут с allow_run_async: true, поэтому
# RESULTS_BACKEND и воркер ОБЯЗАТЕЛЬНЫ. Без них SQL Lab отвечает 500
# «Results backend is not configured» на любой запрос.

_redis = urlparse(REDIS_URL)
_redis_host = _redis.hostname or "redis-superset"
_redis_port = _redis.port or 6379


class CeleryConfig:
    broker_url = f"{REDIS_URL}/0"
    result_backend = f"{REDIS_URL}/2"
    imports = ("superset.sql_lab", "superset.tasks.scheduler")
    worker_prefetch_multiplier = 1
    task_acks_late = False


CELERY_CONFIG = CeleryConfig

# Отдельная БД Redis (3) под результаты запросов: они крупные и живут по своему
# TTL, мешать их с кэшом чартов не надо.
RESULTS_BACKEND = RedisCache(
    host=_redis_host,
    port=_redis_port,
    db=3,
    key_prefix="superset_results_",
    default_timeout=86400,
)

# ── Фичефлаги ────────────────────────────────────────────────────────────────

FEATURE_FLAGS = {
    # Виртуальные датасеты поверх SQL — основной способ описывать витрины,
    # не плодя вьюхи в ClickHouse.
    "ENABLE_TEMPLATE_PROCESSING": True,
    "DASHBOARD_RBAC": True,
    "HORIZONTAL_FILTER_BAR": True,
    # Кросс-фильтры и drill — нужны финансистам для ВГО/планов.
    "DRILL_TO_DETAIL": True,
    "DRILL_BY": True,
}

# ── MCP-сервис ───────────────────────────────────────────────────────────────
#
# MCP-сервер встроен в Superset (модуль superset.mcp_service, требует fastmcp).
# Запускается отдельным процессом: `superset mcp run --host 0.0.0.0 --port 5008`,
# эндпоинт http://<host>:<port>/mcp. Это тот же сервер, под который написаны
# скиллы preset-io/agent-skills — см. superset/README.md в этом каталоге.

MCP_SERVICE_HOST = env("MCP_SERVICE_HOST", "0.0.0.0")
MCP_SERVICE_PORT = env_int("MCP_SERVICE_PORT", 5008)

# RBAC оставляем включённым всегда: MCP-инструменты ходят под правами
# конкретного пользователя Superset, а не мимо них.
MCP_RBAC_ENABLED = env_bool("MCP_RBAC_ENABLED", True)

# В dev аутентификацию MCP выключаем и работаем от имени MCP_DEV_USERNAME.
# В prod ОБЯЗАТЕЛЬНО MCP_AUTH_ENABLED=1 + MCP_JWT_SECRET (HS256) либо
# MCP_JWKS_URI (RS256) — иначе любой, кто дотянулся до порта, получит права
# админа Superset.
MCP_AUTH_ENABLED = env_bool("MCP_AUTH_ENABLED", False)
MCP_DEV_USERNAME = env("MCP_DEV_USERNAME", "admin")

if MCP_AUTH_ENABLED:
    if env("MCP_JWT_SECRET"):
        MCP_JWT_SECRET = env("MCP_JWT_SECRET")
    if env("MCP_JWKS_URI"):
        MCP_JWKS_URI = env("MCP_JWKS_URI")
    if env("MCP_JWT_ISSUER"):
        MCP_JWT_ISSUER = env("MCP_JWT_ISSUER")
    if env("MCP_JWT_AUDIENCE"):
        MCP_JWT_AUDIENCE = env("MCP_JWT_AUDIENCE")

# ── Безопасность фасада ──────────────────────────────────────────────────────

# Talisman навешивает HTTPS-заголовки и CSP. В dev по http это ломает UI,
# поэтому по умолчанию выключен, а в prod включается через env.
TALISMAN_ENABLED = env_bool("SUPERSET_TALISMAN_ENABLED", False)
ENABLE_PROXY_FIX = env_bool("SUPERSET_ENABLE_PROXY_FIX", True)

# Роль по умолчанию для новых пользователей. Gamma = «смотреть, не ломать».
AUTH_USER_REGISTRATION_ROLE = env("SUPERSET_DEFAULT_ROLE", "Gamma")
