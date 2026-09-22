"""
Отметка «нужна замена артикула» и уведомление о ней (пожелание № 4).

Экономист ставит в строке таблицы галочку «нужна замена артикула». Отметка
хранится по ключу калькуляции с заданием (как статус ПЭО), а при установке
уходит сообщение следующим операторам. Кому — зависит от ассортимента:

  · ЧНИ (носки и Orodoro)  → сегмент 'chni'  — Кочеткова Т., Король Н.;
  · всё остальное          → сегмент 'other' — Дащинская А.А.

Адресаты лежат в `cost_articul_replace_recipients` (миграция 0052), а не в
коде: люди меняются чаще, чем деплоится сервис.

КАНАЛ — БИТРИКС
===============
Единственный канал — сообщение в корпоративный Битрикс через портал (site_api),
адресату по его `b24_id` из справочника. Почтовый канал (SMTP) был в первой
версии и убран 08.09.2026 по решению заказчика: почтовой инфраструктуры у
проекта нет, а Битрикс у адресатов есть. Колонка `email` в справочнике
остаётся как идентификатор человека и подпись в интерфейсе.

Без SITE_API_NOTIFY_URL/USER/PASSWORD отметка всё равно ставится, а сообщение
помечается как `skipped` — и это видно в интерфейсе, чтобы никто не думал,
что уведомления идут.

Сбой доставки не откатывает отметку: она — источник истины, сообщение —
следствие; повторить рассылку можно, сняв и поставив галочку снова, а история
попыток остаётся в `cost_articul_replace_mail` (имя таблицы историческое,
`channel` = 'bitrix').
"""
from __future__ import annotations

import json
import logging
import os
from datetime import datetime, timezone
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
    """Время в сообщении — минское: контейнер живёт в UTC, а читают сообщение
    люди в Минске. Часовой пояс задаётся COST_MAIL_TZ, по умолчанию Europe/Minsk."""
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
        return False, "не настроен (SITE_API_NOTIFY_URL/USER/PASSWORD)"
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


def _compose_message(key: ReplaceKey, context: dict[str, Any], set_by: str, comment: str) -> str:
    """Текст сообщения в Битрикс: заголовок и карточка калькуляции."""
    model, articul, calc_sign, plan_id, task = key
    title = f"Нужна замена артикула: {model} / {articul}"
    if calc_sign or plan_id:
        title += f" ({calc_sign or '—'}, план {plan_id or '—'})"

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
    return f"{title}\n\n{body}"


async def set_flag(
    key: ReplaceKey,
    *,
    needed: bool,
    comment: str,
    set_by: str,
    context: dict[str, Any],
) -> dict[str, Any]:
    """Поставить или снять отметку. При установке — уведомить адресатов в Битриксе.

    Сообщение уходит при каждой установке галочки (в том числе повторной после
    снятия): так экономист может повторить рассылку, если первая не дошла.
    Снятие галочки ничего не рассылает — адресаты работают по факту замены, а
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
        "notify": None,
    }
    if not needed:
        return result

    segment = segment_for(context.get("level01"))
    people = await recipients(segment)
    text = _compose_message(key, context, set_by, comment)

    # Сообщение в Битрикс напрямую через корпоративный портал, тем адресатам,
    # у кого в справочнике задан ID в Битриксе. Через кабинет (Go-API
    # /internal/notifications) слать нельзя: эти люди в Finance Cabinet не
    # заходят, номера пользователя кабинета у них не будет никогда. Поэтому
    # идём тем же контрактом site_api, которым Go дублирует уведомления в Б24.
    targets = [p for p in people if p.get("b24_id")]
    sent_ok: list[str] = []
    failed: list[str] = []
    error: str | None = None
    if not people:
        status = "skipped"
        error = "нет активных адресатов в справочнике"
    elif not targets:
        status = "skipped"
        error = "у адресатов не заданы ID в Битриксе"
    elif not bitrix_enabled():
        status = "skipped"
        error = "не настроен (SITE_API_NOTIFY_URL/USER/PASSWORD)"
        failed = [str(p["b24_id"]) for p in targets]
    else:
        for p in targets:
            ok, err = await send_bitrix(int(p["b24_id"]), text)
            (sent_ok if ok else failed).append(str(p["b24_id"]))
            if not ok and err and not error:
                error = err
        status = "sent" if not failed else "error"
        if failed and error is None:
            error = "не доставлено"

    any_sent = bool(sent_ok)
    notify_error: str | None = None
    if status != "sent":
        notify_error = f"Битрикс — {error or 'не доставлено'}"
        if sent_ok and failed:
            notify_error += f" (ID {', '.join(failed)})"

    # Адресаты без b24_id сообщение не получат; про них тоже стоит сказать в
    # интерфейсе, иначе экономист считает, что оповестил всех из подсказки.
    without_id = [p["name"] or p["email"] for p in people if not p.get("b24_id")]
    if targets and without_id:
        extra = f"без ID в Битриксе: {', '.join(without_id)}"
        notify_error = f"{notify_error}; {extra}" if notify_error else extra

    async with acquire() as conn:
        await conn.execute(
            """INSERT INTO cost_articul_replace_mail
                   (replace_id, segment, channel, recipients, subject, status, error)
               VALUES ($1, $2, 'bitrix', $3::text[], $4, $5, $6)""",
            replace_id, segment, [str(p["b24_id"]) for p in targets],
            text.split("\n", 1)[0], status, notify_error,
        )
        # notified_at — хотя бы один адресат получил сообщение; notify_error —
        # что не дошло и почему.
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
        logging.INFO if status == "sent" and not without_id else logging.WARNING,
        "рассылка о замене артикула",
        model=model, articul=articul, calc_sign=calc_sign, plan_id=plan_id, task=task,
        segment=segment, recipients=[p["email"] for p in people],
        bitrix_status=status, bitrix_ok=sent_ok, bitrix_failed=failed,
        bitrix_error=error, without_b24_id=without_id, user=set_by,
    )
    result["notify"] = {
        "segment": segment,
        "channel": "bitrix",
        # Кого оповещали — подписи для интерфейса (имя, иначе e-mail).
        "recipients": [p["name"] or p["email"] for p in targets],
        # sent — дошло всем адресатам с ID; error — хотя бы одному не дошло;
        # skipped — не отправляли (канал не настроен или некому).
        "status": status,
        "error": notify_error,
        "sent": sent_ok,
        "failed": failed,
    }
    return result
