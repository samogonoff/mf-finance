"""
Отметка «нужна замена артикула» и рассылка о ней (пожелание № 4).

Экономист ставит в строке таблицы галочку «нужна замена артикула». Отметка
хранится по ключу калькуляции с заданием (как статус ПЭО), а при установке
уходит письмо следующим операторам. Кому — зависит от ассортимента:

  · ЧНИ (носки и Orodoro)  → сегмент 'chni'  — Кочеткова Т., Король Н.;
  · всё остальное          → сегмент 'other' — Дащинская А.А.

Адресаты лежат в `cost_articul_replace_recipients` (миграция 0052), а не в
коде: люди меняются чаще, чем деплоится сервис.

ПОЧТА
=====
Отправка — обычный SMTP из стандартной библиотеки (`smtplib`), настраивается
переменными COST_SMTP_* (см. .env.example). В проекте до сих пор почтового
канала не было (уведомления шли в кабинет и Б24 через Go-API), поэтому SMTP
здесь — новая зависимость от инфраструктуры. Без COST_SMTP_HOST отметка всё
равно ставится, а письмо помечается как `skipped` — и это видно в интерфейсе,
чтобы никто не думал, что рассылка идёт.

Отправка синхронная относительно запроса, но выполняется в пуле потоков
(smtplib блокирующий) с таймаутом: пользователь сразу видит результат —
«письмо ушло» или «не ушло, вот почему». Сбой почты не откатывает отметку:
она — источник истины, письмо — следствие; повторить рассылку можно, сняв и
поставив галочку снова, а история попыток остаётся в `cost_articul_replace_mail`.
"""
from __future__ import annotations

import asyncio
import json
import logging
import os
import smtplib
import ssl
from datetime import datetime, timezone
from email.message import EmailMessage
from typing import Any

import httpx

from app.db import acquire
from app.logship import log

# Критерий ЧНИ — верхний уровень номенклатуры. Слова «ЧНИ» в справочнике нет,
# по тексту его не найти. Orodoro — отдельное значение Level 01, а не подраздел
# «Носки&Колготки»: условие только по носкам потеряло бы 82 пары модель+артикул.
CHNI_LEVEL01 = ("Носки&Колготки", "Orodoro")

# Ключ отметки — модель, артикул, признак, план, задание. Пустые части хранятся
# как '' (не NULL), чтобы UNIQUE работал как ключ (урок миграции 0023).
ReplaceKey = tuple[str, str, str, str, str]


def _s(v: Any) -> str:
    return str(v or "").strip()


def _now_local() -> datetime:
    """Время для письма — минское: контейнер живёт в UTC, а читают письмо люди
    в Минске. Часовой пояс задаётся COST_MAIL_TZ, по умолчанию Europe/Minsk."""
    try:
        from zoneinfo import ZoneInfo
        return datetime.now(ZoneInfo(os.environ.get("COST_MAIL_TZ", "").strip() or "Europe/Minsk"))
    except Exception:
        return datetime.now(timezone.utc)


def make_key(model: Any, articul: Any, calc_sign: Any, plan_id: Any, task_number: Any) -> ReplaceKey:
    return (_s(model), _s(articul), _s(calc_sign), _s(plan_id), _s(task_number))


def segment_for(level01: Any) -> str:
    """Сегмент адресатов по Level 01. Пустой Level 01 уходит в 'other' — это
    осознанный остаточный принцип, а не ошибка (см. память о ЧНИ)."""
    return "chni" if _s(level01) in CHNI_LEVEL01 else "other"


# ── SMTP ──────────────────────────────────────────────────────────────────────

def smtp_config() -> dict[str, Any]:
    port_raw = os.environ.get("COST_SMTP_PORT", "").strip()
    timeout_raw = os.environ.get("COST_SMTP_TIMEOUT", "").strip()
    return {
        "host": os.environ.get("COST_SMTP_HOST", "").strip(),
        "port": int(port_raw) if port_raw.isdigit() else 587,
        "user": os.environ.get("COST_SMTP_USER", "").strip(),
        "password": os.environ.get("COST_SMTP_PASSWORD", ""),
        "from": os.environ.get("COST_SMTP_FROM", "").strip()
                or os.environ.get("COST_SMTP_USER", "").strip(),
        # 1 — STARTTLS (порт 587), 0 — без шифрования (внутренний релей),
        # ssl — неявный TLS (порт 465).
        "tls": (os.environ.get("COST_SMTP_STARTTLS", "1").strip().lower() or "1"),
        "timeout": float(timeout_raw) if timeout_raw.replace(".", "", 1).isdigit() else 10.0,
    }


def smtp_enabled() -> bool:
    return bool(smtp_config()["host"])


def _send_sync(cfg: dict[str, Any], msg: EmailMessage) -> None:
    """Блокирующая отправка; вызывается из пула потоков."""
    if cfg["tls"] == "ssl":
        ctx = ssl.create_default_context()
        server: smtplib.SMTP = smtplib.SMTP_SSL(cfg["host"], cfg["port"], timeout=cfg["timeout"], context=ctx)
    else:
        server = smtplib.SMTP(cfg["host"], cfg["port"], timeout=cfg["timeout"])
    try:
        server.ehlo()
        if cfg["tls"] == "1":
            server.starttls(context=ssl.create_default_context())
            server.ehlo()
        if cfg["user"]:
            server.login(cfg["user"], cfg["password"])
        server.send_message(msg)
    finally:
        try:
            server.quit()
        except Exception:
            pass


async def send_mail(recipients: list[str], subject: str, body: str) -> tuple[str, str | None]:
    """Возвращает (status, error): sent | skipped | error."""
    cfg = smtp_config()
    if not cfg["host"]:
        return "skipped", "SMTP не настроен (COST_SMTP_HOST пуст)"
    if not recipients:
        return "skipped", "нет активных адресатов в справочнике"
    if not cfg["from"]:
        return "error", "не задан отправитель (COST_SMTP_FROM или COST_SMTP_USER)"

    msg = EmailMessage()
    msg["Subject"] = subject
    msg["From"] = cfg["from"]
    msg["To"] = ", ".join(recipients)
    msg.set_content(body)

    loop = asyncio.get_running_loop()
    try:
        await asyncio.wait_for(
            loop.run_in_executor(None, _send_sync, cfg, msg),
            timeout=cfg["timeout"] + 5,
        )
        return "sent", None
    except asyncio.TimeoutError:
        return "error", f"SMTP не ответил за {cfg['timeout'] + 5:.0f} с"
    except Exception as exc:  # smtplib.SMTPException, OSError и прочее
        return "error", f"{type(exc).__name__}: {exc}"[:500]


# ── Битрикс (корпоративный портал, site_api) ─────────────────────────────────
#
# Тот же контракт, которым Go-API дублирует уведомления кабинета в Б24
# (go/internal/notifications/service.go, эталон — MP SiteApiNotifyService):
# POST JSON {id: "<b24_id>", message: "Finance: ..."} с Basic Auth, ответ
# {"success": bool, "error": str}. Имена переменных совпадают с корневым .env
# основного контура, но python-cost читает их из своего python/cost/.env.

BITRIX_MESSAGE_PREFIX = "Finance: "


def bitrix_config() -> dict[str, str]:
    return {
        "url": os.environ.get("SITE_API_NOTIFY_URL", "").strip(),
        "user": os.environ.get("SITE_API_NOTIFY_USER", "").strip(),
        "password": os.environ.get("SITE_API_NOTIFY_PASSWORD", ""),
    }


def bitrix_enabled() -> bool:
    cfg = bitrix_config()
    return bool(cfg["url"] and cfg["user"] and cfg["password"])


async def send_bitrix(b24_id: int, text: str) -> tuple[bool, str | None]:
    """Сообщение пользователю Битрикса по его ID на портале. (ок, причина)."""
    cfg = bitrix_config()
    if not bitrix_enabled():
        return False, "Битрикс не настроен (SITE_API_NOTIFY_URL/USER/PASSWORD)"
    if not b24_id or int(b24_id) <= 0:
        return False, "не задан ID в Битриксе"
    payload = {"id": str(int(b24_id)), "message": BITRIX_MESSAGE_PREFIX + text}
    try:
        async with httpx.AsyncClient(timeout=httpx.Timeout(10.0)) as client:
            resp = await client.post(
                cfg["url"], json=payload,
                auth=(cfg["user"], cfg["password"]),
                headers={"Accept": "application/json"},
            )
        if resp.status_code != 200:
            return False, f"HTTP {resp.status_code}"
        try:
            data = resp.json()
        except ValueError:
            return False, "портал ответил не JSON"
        if not isinstance(data, dict) or data.get("success") is not True:
            return False, (data.get("error") if isinstance(data, dict) else None) or "портал ответил success=false"
        return True, None
    except httpx.HTTPError as exc:
        return False, f"{exc.__class__.__name__}: {exc}"[:200]


# ── Справочник адресатов ──────────────────────────────────────────────────────

async def recipients(segment: str | None = None, *, active_only: bool = True) -> list[dict[str, Any]]:
    where: list[str] = []
    params: list[Any] = []
    if segment:
        params.append(segment)
        where.append(f"segment = ${len(params)}")
    if active_only:
        where.append("active")
    sql = "SELECT id, segment, email, name, b24_id, active FROM cost_articul_replace_recipients"
    if where:
        sql += " WHERE " + " AND ".join(where)
    sql += " ORDER BY segment, name, email"
    async with acquire() as conn:
        rows = await conn.fetch(sql, *params)
    return [dict(r) for r in rows]


# ── Отметки ───────────────────────────────────────────────────────────────────

async def flags_for(keys: list[ReplaceKey]) -> dict[ReplaceKey, dict[str, Any]]:
    """Отметки по переданным ключам — для наложения на строки /aggregated.

    Возвращаются и снятые отметки (needed=false): интерфейсу полезно показать,
    что замену когда-то отмечали и потом отменили.
    """
    uniq = list({k for k in keys if k[0] and k[1]})
    if not uniq:
        return {}
    async with acquire() as conn:
        rows = await conn.fetch(
            """SELECT model, articul, calc_sign, plan_id, task_number,
                      needed, comment, set_by, set_at, notified_at, notify_error
                 FROM cost_articul_replace
                WHERE (model, articul, calc_sign, plan_id, task_number) IN (
                      SELECT * FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[])
                )""",
            [k[0] for k in uniq], [k[1] for k in uniq], [k[2] for k in uniq],
            [k[3] for k in uniq], [k[4] for k in uniq],
        )
    out: dict[ReplaceKey, dict[str, Any]] = {}
    for r in rows:
        k = (r["model"], r["articul"], r["calc_sign"], r["plan_id"], r["task_number"])
        out[k] = {
            "replace_needed": bool(r["needed"]),
            "replace_comment": r["comment"] or "",
            "replace_set_by": r["set_by"],
            "replace_set_at": r["set_at"].isoformat() if r["set_at"] else None,
            "replace_notified_at": r["notified_at"].isoformat() if r["notified_at"] else None,
            "replace_notify_error": r["notify_error"],
        }
    return out


def apply_flags(rows: list[dict[str, Any]], flags: dict[ReplaceKey, dict[str, Any]]) -> None:
    for row in rows:
        k = make_key(row.get("Модель"), row.get("Артикул"), row.get("Признак калькуляции"),
                     row.get("PLAN_ID"), row.get("Номер задания производства"))
        f = flags.get(k)
        if f:
            row.update(f)
        else:
            row["replace_needed"] = False
            row["replace_set_by"] = None
            row["replace_set_at"] = None
            row["replace_notified_at"] = None
            row["replace_notify_error"] = None


def _compose_mail(key: ReplaceKey, context: dict[str, Any], set_by: str, comment: str) -> tuple[str, str]:
    model, articul, calc_sign, plan_id, task = key
    subject = f"Нужна замена артикула: {model} / {articul}"
    if calc_sign or plan_id:
        subject += f" ({calc_sign or '—'}, план {plan_id or '—'})"

    def line(label: str, value: Any) -> str:
        v = _s(value)
        return f"{label}: {v}\n" if v else ""

    body = (
        "В разделе «Себестоимость» отмечено, что по калькуляции нужна замена артикула.\n\n"
        + line("Модель", model)
        + line("Артикул", articul)
        + line("Наименование", context.get("model_name"))
        + line("Признак калькуляции", calc_sign)
        + line("План", plan_id)
        + line("№ задания", task)
        + line("Группа (Level 01)", context.get("level01"))
        + line("Страна производства", context.get("country"))
        + line("Бренд-менеджер", context.get("brand_manager"))
        + line("Комментарий", comment)
        + f"\nОтметил(а): {set_by}\n"
        + f"Когда: {_now_local().strftime('%d.%m.%Y %H:%M')}\n"
    )
    link = os.environ.get("COST_PUBLIC_URL", "").strip()
    if link:
        body += f"\nОткрыть раздел: {link.rstrip('/')}/cost\n"
    body += "\nПисьмо отправлено автоматически, отвечать на него не нужно.\n"
    return subject, body


async def set_flag(
    key: ReplaceKey,
    *,
    needed: bool,
    comment: str,
    set_by: str,
    context: dict[str, Any],
) -> dict[str, Any]:
    """Поставить или снять отметку. При установке — разослать письмо.

    Письмо уходит при каждой установке галочки (в том числе повторной после
    снятия): так экономист может повторить рассылку, если первая не дошла.
    Снятие галочки писем не рассылает — адресаты работают по факту замены, а
    «отбой» в требовании не описан.
    """
    model, articul, calc_sign, plan_id, task = key
    ctx_json = json.dumps(context or {}, ensure_ascii=False)
    async with acquire() as conn:
        row = await conn.fetchrow(
            """INSERT INTO cost_articul_replace
                   (model, articul, calc_sign, plan_id, task_number, needed, comment, context, set_by)
               VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9)
               ON CONFLICT ON CONSTRAINT uq_articul_replace_key DO UPDATE
                  SET needed = EXCLUDED.needed,
                      comment = EXCLUDED.comment,
                      context = EXCLUDED.context,
                      set_by = EXCLUDED.set_by,
                      set_at = now(),
                      notified_at = NULL,
                      notify_error = NULL,
                      updated_at = now()
               RETURNING id""",
            model, articul, calc_sign, plan_id, task, needed, comment or None, ctx_json, set_by,
        )
        replace_id = row["id"]

    result: dict[str, Any] = {
        "id": replace_id,
        "replace_needed": needed,
        "replace_comment": comment or "",
        "replace_set_by": set_by,
        "replace_set_at": datetime.now(timezone.utc).isoformat(),
        "replace_notified_at": None,
        "replace_notify_error": None,
        "mail": None,
    }
    if not needed:
        return result

    segment = segment_for(context.get("level01"))
    people = await recipients(segment)
    subject, body = _compose_mail(key, context, set_by, comment)

    # Канал 1 — почта, всем адресатам сегмента одним письмом.
    emails = [p["email"] for p in people]
    mail_status, mail_error = await send_mail(emails, subject, body)

    # Канал 2 — сообщение в Битрикс напрямую через корпоративный портал, тем
    # адресатам, у кого в справочнике задан ID в Битриксе. Через кабинет
    # (Go-API /internal/notifications) слать нельзя: эти люди в Finance Cabinet
    # не заходят, номера пользователя кабинета у них не будет никогда. Поэтому
    # идём тем же контрактом site_api, которым Go дублирует уведомления в Б24.
    bitrix_targets = [p for p in people if p.get("b24_id")]
    bitrix_ok: list[str] = []
    bitrix_failed: list[str] = []
    bitrix_error: str | None = None
    if bitrix_targets:
        if not bitrix_enabled():
            bitrix_status = "skipped"
            bitrix_error = "Битрикс не настроен (SITE_API_NOTIFY_URL/USER/PASSWORD)"
            bitrix_failed = [str(p["b24_id"]) for p in bitrix_targets]
        else:
            text = f"{subject}\n\n{body}"
            for p in bitrix_targets:
                ok, err = await send_bitrix(int(p["b24_id"]), text)
                (bitrix_ok if ok else bitrix_failed).append(str(p["b24_id"]))
                if not ok and err and not bitrix_error:
                    bitrix_error = err
            bitrix_status = "sent" if not bitrix_failed else "error"
            if bitrix_failed and bitrix_error is None:
                bitrix_error = "не доставлено"
    else:
        bitrix_status = "skipped"
        bitrix_error = "у адресатов не заданы ID в Битриксе"

    any_sent = mail_status == "sent" or bool(bitrix_ok)
    failures: list[str] = []
    if mail_status != "sent":
        failures.append(f"почта — {mail_error}")
    if bitrix_targets and bitrix_failed:
        failures.append(f"Битрикс — {bitrix_error or 'не доставлено'}"
                        + (f" (ID {', '.join(bitrix_failed)})" if bitrix_ok else ""))
    notify_error = "; ".join(failures) if failures else None

    async with acquire() as conn:
        await conn.execute(
            """INSERT INTO cost_articul_replace_mail
                   (replace_id, segment, channel, recipients, subject, status, error)
               VALUES ($1, $2, 'email', $3::text[], $4, $5, $6)""",
            replace_id, segment, emails, subject, mail_status, mail_error,
        )
        if bitrix_targets:
            await conn.execute(
                """INSERT INTO cost_articul_replace_mail
                       (replace_id, segment, channel, recipients, subject, status, error)
                   VALUES ($1, $2, 'bitrix', $3::text[], $4, $5, $6)""",
                replace_id, segment, [str(p["b24_id"]) for p in bitrix_targets], subject,
                bitrix_status, bitrix_error,
            )
        # notified_at — хотя бы один канал доставил; notify_error — что не дошло.
        # Так строка с дошедшим сообщением в Битрикс, но без почты, не выглядит
        # «ничего не отправлено», а причина недоставки всё равно видна.
        await conn.execute(
            """UPDATE cost_articul_replace
                  SET notified_at = CASE WHEN $2 THEN now() ELSE NULL END,
                      notify_error = $3
                WHERE id = $1""",
            replace_id, any_sent, notify_error,
        )
    result["replace_notified_at"] = datetime.now(timezone.utc).isoformat() if any_sent else None
    result["replace_notify_error"] = notify_error

    log(
        logging.INFO if not failures else logging.WARNING,
        "рассылка о замене артикула",
        model=model, articul=articul, calc_sign=calc_sign, plan_id=plan_id, task=task,
        segment=segment, recipients=emails, mail_status=mail_status, mail_error=mail_error,
        bitrix_status=bitrix_status, bitrix_ok=bitrix_ok, bitrix_failed=bitrix_failed,
        bitrix_error=bitrix_error, user=set_by,
    )
    result["mail"] = {
        "segment": segment,
        "recipients": emails,
        # Общий статус для фронта: «дошло хотя бы одним каналом» или нет.
        "status": "sent" if any_sent else "error" if "error" in (mail_status, bitrix_status) else "skipped",
        "error": notify_error,
        "email": {"status": mail_status, "error": mail_error, "recipients": emails},
        "bitrix": {"status": bitrix_status, "error": bitrix_error,
                   "sent": bitrix_ok, "failed": bitrix_failed},
    }
    return result
