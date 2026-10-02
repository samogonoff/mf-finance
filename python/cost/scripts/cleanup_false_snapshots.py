"""
Разовая чистка ложных снимков источника — записей cost_calc_versions со
status='original' и created_by='refresh', которые обновление кэша создавало до
исправления db._snapshot_changed_sources (02.10.2026).

Откуда они. Частичное обновление не перезаливает строки старше отсечки (два
месяца), и у ключа с активной версией в кэше лежат строки самой версии. Обновление
сравнивало их с последним снимком и делало из них «снимок источника» с признаком
🔄; следующее полное обновление давало снимок с настоящими строками, и так по
кругу — по паре снимков на каждое суточное полное обновление. Копии
(cost_manual_calc) обновление не перезаливает вовсе, и у копии с версией такой
снимок появлялся после первого же обновления. На проде с выката 0057 (17.09.2026)
так у 532431-2 / 26-46218ПП-9 (ПКПСС) набралось 16 снимков при неизменном
источнике, и признак 🔄 горел у ключей, источник которых не менялся.

Что считается ложным. Снимки ключа задания просматриваются по порядку номеров;
снимки, сделанные пользователем (первый снимок ключа), не трогаются никогда.
Снимок обновления удаляется, если он
  • «строки версии» — его отпечаток равен отпечатку строк одной из версий ключа,
    созданных раньше снимка, как они лежат в кэше на дату снимка
    (pending/approved/archived/rejected; черновик на кэш не ложится);
  • «повтор» — строки и дата расчёта совпадают с последним оставленным снимком:
    источник не менялся.
Остальные снимки обновления — настоящие изменения источника (как у 121884,
где CostHistory дописал строки позже), они остаются. Снимок, на который
ссылается сборка мультипака, не удаляется.

Что ещё делает с ключом:
  • версии, сделанные от удалённого снимка, переводит на последний оставленный
    снимок перед ним — каким источник был на самом деле (иначе FK
    source_version_id обнулил бы основание, и признак 🔄 у версии пропал бы
    навсегда);
  • последнему оставленному снимку ставит отпечаток удалённого повтора — он
    посчитан по строкам источника. Если повтора нет, а отпечаток снимка равен
    отпечатку версии (его дописало первое обновление после 0057, когда в кэше
    лежали строки версии), отпечаток обнуляется: следующее обновление допишет
    его по числу строк, не создавая снимка.

Отчёт показывает по каждому ключу решение для каждого снимка и признак 🔄 до и
после. По умолчанию ничего не меняется — всё выполняется в транзакции с откатом,
поэтому цифры отчёта точные. --apply фиксирует.

Запускать ПОСЛЕ выката исправления _snapshot_changed_sources (иначе ближайшее
обновление кэша наплодит снимки заново) и не во время обновления кэша — скрипт
это проверяет. Повторный запуск безопасен: удалять второй раз нечего.

На проде переменные окружения контейнера python-cost читает entrypoint из .env,
а docker exec его минует — поэтому запуск через него:
    docker exec -it <контейнер python-cost> /entrypoint.sh python -m scripts.cleanup_false_snapshots
    docker exec -it <контейнер python-cost> /entrypoint.sh python -m scripts.cleanup_false_snapshots --apply
Локально (переменные уже в окружении контейнера):
    docker exec -it swarm-python-cost-1 python -m scripts.cleanup_false_snapshots
"""

from __future__ import annotations

import argparse
import asyncio
import sys

import asyncpg

from app import db


class _DryRun(Exception):
    """Выход из транзакции с откатом без --apply."""


# Статусы версий, строки которых накладывались на кэш хотя бы раз: активные и
# бывшие активными. Черновик на кэш не ложится, 'original' — сам снимок.
_APPLIED_STATUSES = ["pending", "approved", "archived", "rejected"]


def _row_text(alias: str, date_sql: str | None) -> str:
    """Текст строки для отпечатка — в точности как в db._source_fingerprint.

    date_sql — выражение даты расчёта; None — строка без даты (для сравнения
    содержимого снимков между собой: дату снимка сравниваем отдельно).
    """
    parts = []
    for c in db._FINGERPRINT_COLUMNS:
        if c == "дата расчета":
            if date_sql is not None:
                parts.append(f"coalesce(trim(({date_sql})::text), '')")
        elif c in db._CACHE_UNSCALED:
            parts.append(f'coalesce(round({alias}."{c}", 4)::text, \'\')')
        else:
            parts.append(f'coalesce(trim({alias}."{c}"::text), \'\')')
    return "concat_ws('|', " + ", ".join(parts) + ")"


_KEY_SQL = """model = $1 AND articul = $2
              AND calc_sign IS NOT DISTINCT FROM $3
              AND plan_id IS NOT DISTINCT FROM $4"""


async def _snapshots(conn, key) -> list[dict]:
    """Снимки ключа задания по порядку номеров, с числом строк и подписью
    содержимого (отпечаток строк без даты)."""
    rows = await conn.fetch(
        f"""SELECT v.id, v.snapshot_no, v.created_by, v.created_at,
                   v."дата расчета" AS d, v.source_hash,
                   (SELECT count(*) FROM cost_calc_version_rows r
                     WHERE r.version_id = v.id) AS n,
                   (SELECT md5(string_agg(t, '||' ORDER BY t))
                      FROM (SELECT {_row_text("r", None)} AS t
                              FROM cost_calc_version_rows r
                             WHERE r.version_id = v.id) s) AS sig,
                   EXISTS (SELECT 1 FROM cost_multipack_build b
                            WHERE b.version_id = v.id) AS pinned
              FROM cost_calc_versions v
             WHERE {_KEY_SQL} AND task_number IS NOT DISTINCT FROM $5
               AND status = 'original'
             ORDER BY v.snapshot_no NULLS FIRST, v.id""",
        *key,
    )
    return [dict(r) for r in rows]


async def _applied_versions(conn, key) -> list[tuple[int, object]]:
    """(id, created_at) версий, которые накладывались на строки этого задания:
    того же задания или легаси-версии с пустым заданием (как db._task_match_sql)."""
    task_sql, task_params = db._task_match_sql(key[4], 6)
    rows = await conn.fetch(
        f"""SELECT id, created_at FROM cost_calc_versions
             WHERE {_KEY_SQL} AND status = ANY($5::text[]){task_sql}""",
        *key[:4], _APPLIED_STATUSES, *task_params,
    )
    return [(r["id"], r["created_at"]) for r in rows]


async def _version_fingerprints(conn, key, version_ids: list[int], d) -> set[str]:
    """Отпечатки, которые давали строки этих версий, лёжа в кэше на дату d.

    Версия накладывается на кэш как есть (те же типы колонок), с датой расчёта
    ключа, поэтому её отпечаток — тот же текст строк с этой датой. Текст даты
    берём из кэша (timestamptz печатается в часовом поясе сессии), а если такой
    даты в кэше уже нет — полночь d в поясе сессии.
    """
    if not version_ids:
        return set()
    rows = await conn.fetch(
        f"""WITH ts AS (
                SELECT DISTINCT "дата расчета" AS ts FROM cost_data_all
                 WHERE "Модель" = $1 AND "Артикул" = $2
                   AND "Признак калькуляции" IS NOT DISTINCT FROM $3
                   AND "PLAN_ID" IS NOT DISTINCT FROM $4
                   AND "дата расчета"::date = $5::date
                UNION
                SELECT $5::date::timestamptz
            )
            SELECT md5(string_agg(t, '||' ORDER BY t)) AS fp
              FROM (SELECT ts.ts, r.version_id, {_row_text("r", "ts.ts")} AS t
                      FROM ts CROSS JOIN cost_calc_version_rows r
                     WHERE r.version_id = ANY($6::bigint[])) x
             GROUP BY x.ts, x.version_id""",
        *key[:4], d, version_ids,
    )
    return {r["fp"] for r in rows if r["fp"]}


async def _active_base(conn, key) -> tuple[int | None, int | None]:
    """(id активной версии, id её снимка-основания) — как в
    db.get_source_change_flags: последняя pending/approved версия задания."""
    row = await conn.fetchrow(
        f"""SELECT id, source_version_id FROM cost_calc_versions
             WHERE {_KEY_SQL} AND task_number IS NOT DISTINCT FROM $5
               AND status IN ('pending', 'approved')
             ORDER BY created_at DESC LIMIT 1""",
        *key,
    )
    return (row["id"], row["source_version_id"]) if row else (None, None)


def _flag(base_id: int | None, latest_id: int | None) -> bool:
    """Горит ли 🔄: основание активной версии — не последний снимок."""
    return base_id is not None and latest_id is not None and base_id != latest_id


async def _clean_key(conn, key, stats: dict) -> list[str]:
    """Решения по одному ключу задания. Возвращает строки отчёта (пустой
    список — менять у ключа нечего)."""
    snaps = await _snapshots(conn, key)
    if not snaps:
        return []
    versions = await _applied_versions(conn, key)
    fps_cache: dict = {}

    async def version_fps(d, before=None) -> set[str]:
        """Отпечатки версий на дату d. *before* — только версии, созданные
        раньше: ложный снимок копирует уже наложенную версию, а версия,
        сделанная ОТ снимка без правок, могла бы совпасть с настоящим."""
        ids = tuple(sorted(i for i, created in versions if before is None or created < before))
        if (d, ids) not in fps_cache:
            fps_cache[(d, ids)] = await _version_fingerprints(conn, key, list(ids), d)
        return fps_cache[(d, ids)]

    kept: list[dict] = []
    decisions: list[tuple[dict, str]] = []
    deleted: dict[int, int] = {}          # id удалённого → id снимка, на который переводим версии
    repeat_hash: dict[int, str] = {}      # id оставленного → отпечаток его последнего повтора
    for s in snaps:
        if s["created_by"] != "refresh" or not kept:
            kept.append(s)
            decisions.append((s, "оставлен: снимок пользователя"
                              if s["created_by"] != "refresh" else "оставлен: первый снимок"))
            continue
        prev = kept[-1]
        if s["source_hash"] and s["source_hash"] in await version_fps(s["d"], s["created_at"]):
            reason = "строки версии"
        elif s["sig"] == prev["sig"] and s["d"] == prev["d"]:
            reason = f"повтор №{prev['snapshot_no']}"
            if s["source_hash"]:
                repeat_hash[prev["id"]] = s["source_hash"]
        else:
            kept.append(s)
            decisions.append((s, "оставлен: источник изменился"))
            continue
        if s["pinned"]:
            kept.append(s)
            decisions.append((s, f"оставлен ({reason}): на него ссылается сборка мультипака"))
            stats["pinned"] += 1
            continue
        deleted[s["id"]] = prev["id"]
        decisions.append((s, f"удалить: {reason}"))

    last = kept[-1]
    new_hash = last["source_hash"]
    hash_note = None
    if last["id"] in repeat_hash:
        if repeat_hash[last["id"]] != last["source_hash"]:
            new_hash = repeat_hash[last["id"]]
            hash_note = "отпечаток — из удалённого повтора (по строкам источника)"
    elif last["source_hash"] and last["source_hash"] in await version_fps(last["d"]):
        new_hash = None
        hash_note = "отпечаток был отпечатком версии — обнулён, обновление допишет его по числу строк"

    if not deleted and hash_note is None:
        return []

    active_id, base_before = await _active_base(conn, key)
    flag_before = _flag(base_before, snaps[-1]["id"])

    rebased = 0
    for d_id, target in deleted.items():
        rebased += int((await conn.execute(
            "UPDATE cost_calc_versions SET source_version_id = $2 WHERE source_version_id = $1",
            d_id, target,
        )).split()[-1])
    if deleted:
        await conn.execute(
            "DELETE FROM cost_calc_versions WHERE id = ANY($1::bigint[])", list(deleted),
        )
    if hash_note is not None:
        await conn.execute(
            "UPDATE cost_calc_versions SET source_hash = $2 WHERE id = $1", last["id"], new_hash,
        )
    _, base_after = await _active_base(conn, key)
    flag_after = _flag(base_after, last["id"])

    stats["keys"] += 1
    stats["deleted"] += len(deleted)
    stats["deleted_version"] += sum(1 for _, r in decisions if r == "удалить: строки версии")
    stats["kept_changes"] += sum(1 for _, r in decisions if r == "оставлен: источник изменился")
    stats["hash_fixed"] += int(hash_note is not None)
    stats["rebased"] += rebased
    stats["flag_removed"] += int(flag_before and not flag_after)
    stats["flag_left"] += int(flag_after)

    model, articul, calc_sign, plan_id, task = key
    out = [f"{model} / {articul} / {calc_sign} / план {plan_id} / задание {task or '—'}: "
           f"снимков {len(snaps)}, удалить {len(deleted)}; "
           f"🔄 {'да' if flag_before else 'нет'} → {'да' if flag_after else 'нет'}"]
    for s, reason in decisions:
        out.append(f"    №{s['snapshot_no']} {s['created_by']} {s['created_at']:%d.%m %H:%M} UTC, "
                   f"дата расчёта {s['d']}, строк {s['n']} — {reason}")
    if hash_note:
        out.append(f"    №{last['snapshot_no']}: {hash_note}")
    if rebased:
        out.append(f"    версий переведено на оставленный снимок: {rebased}")
    if flag_after:
        out.append(f"    🔄 останется: активная версия {active_id} сделана от снимка "
                   f"{base_after}, последний снимок — №{last['snapshot_no']} (id {last['id']})")
    return out


async def main() -> None:
    parser = argparse.ArgumentParser(description="Чистка ложных снимков источника")
    parser.add_argument("--apply", action="store_true",
                        help="зафиксировать изменения (без флага — только отчёт, с откатом)")
    args = parser.parse_args()

    await db.init_pool()
    try:
        async with db.pool().acquire() as conn:
            status = await conn.fetchrow(
                "SELECT is_refreshing, refreshing_since FROM cost_cache_status WHERE id = 1")
            if status and status["is_refreshing"]:
                print(f"Идёт обновление кэша (с {status['refreshing_since']}) — запустите после него.")
                sys.exit(1)

            stats = dict.fromkeys(
                ("keys", "deleted", "deleted_version", "kept_changes", "hash_fixed",
                 "rebased", "flag_removed", "flag_left", "pinned"), 0)
            report: list[str] = []
            try:
                async with conn.transaction():
                    # Обновление кэша и сохранение версий пишут в эту таблицу —
                    # на время чистки (секунды) запись в неё ждёт. Если таблицу
                    # держит идущее обновление, лучше упасть, чем ждать его.
                    await conn.execute("SET LOCAL lock_timeout = '10s'")
                    await conn.execute("LOCK TABLE cost_calc_versions IN EXCLUSIVE MODE")
                    keys = await conn.fetch(
                        """SELECT DISTINCT model, articul, calc_sign, plan_id, task_number
                             FROM cost_calc_versions WHERE status = 'original'
                            ORDER BY 1, 2, 3, 4, 5""")
                    for k in keys:
                        report += await _clean_key(conn, tuple(k), stats)
                    if not args.apply:
                        raise _DryRun
            except _DryRun:
                pass
            except asyncpg.LockNotAvailableError:
                print("Таблицу версий держит другая транзакция (обычно обновление кэша) — "
                      "запустите позже. Ничего не изменено.")
                sys.exit(1)

            print("\n".join(report) if report else "Ложных снимков нет.")
            mode = "ПРИМЕНЕНО" if args.apply else "только отчёт, ничего не изменено (--apply — применить)"
            verb = "удалено" if args.apply else "к удалению"
            print(f"\n[{mode}] ключей с изменениями: {stats['keys']} из {len(keys)}; "
                  f"снимков {verb}: {stats['deleted']} (из них строки версии: "
                  f"{stats['deleted_version']}, повторы: {stats['deleted'] - stats['deleted_version']}); "
                  f"оставлено настоящих изменений источника: {stats['kept_changes']}; "
                  f"отпечатков исправлено: {stats['hash_fixed']}; версий переведено: {stats['rebased']}; "
                  f"🔄 снимется: {stats['flag_removed']}, останется: {stats['flag_left']}"
                  + (f"; оставлено из-за мультипака: {stats['pinned']}" if stats["pinned"] else ""))
    finally:
        await db.close_pool()


if __name__ == "__main__":
    asyncio.run(main())
