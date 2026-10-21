"""ПФКСС на основании плановой калькуляции КПСС/ПКПСС (задача Б24 664398).

Заказчик, 20.10.2026: при замене артикула экономист вручную имитировал
создание ПФКСС из плановой калькуляции, меняя признак. Теперь это операция:
калькуляция КПСС или ПКПСС копируется в новую с признаком ПФКСС. План и
задание переносятся из исходника как есть, пустые остаются пустыми. Носки
(ЧНИ) — операция недоступна. Конвейер общий для калькуляций приложения:
строки пишутся в cost_manual_calc (витрина cost_data_all, как в /raw-rows).

Заказчик, 21.10.2026: у плановой при замене артикула план/задание часто
пусты — экономист подставляет их вручную (значения смотрит в ПФКСС
первоначального артикула, автопоиска нет). Override меняет только PLAN_ID
и «Номер задания производства» новой ПФКСС; остальное — как из исходника.

Переделка 21.10 (вводная заказчика): правка плана/задания живёт в САМОЙ
созданной ПФКСС (update_pfkss_plan), а не в окне калькуляции-основания;
при создании из основания план/задание переносятся из исходника как есть.
Override в API создания сохранён — он проверен и остаётся для совместимости.
"""

from __future__ import annotations

import datetime
import logging
import uuid

from app.articul_replace import CHNI_LEVEL01
from app.db import _cache_columns, acquire, get_active_version
from app.logship import log

# Источник — КПСС или ПКПСС; результат — всегда ПФКСС (строковое значение
# сверено с purchase.INVOICE_CALC_SIGN). ФКСС не участвует — история факта, в
# DWH она не пишется, как и в CALC_SIGN_COPY_ALLOWED.
PFKSS_FROM_SOURCE_SIGNS = ("КПСС", "ПКПСС")
PFKSS_CALC_SIGN = "ПФКСС"


class PfkssAlreadyExists(ValueError):
    """ПФКСС с этим ключом уже есть в витрине — 409, перезапись запрещена."""


async def get_plan_tasks(conn, plan_id: str) -> list[dict]:
    """Непустые задания плана и число калькуляций каждого (664398, 21.10).

    Один запрос питает GET /plan-tasks (подпись «калькуляций: N» у задания в
    селекте) и валидацию override-задания в create_pfkss_from_source — списки
    не разойдутся. Пустой или несуществующий план — пустой список, не ошибка.
    """
    if not (plan_id or "").strip():
        return []
    rows = await conn.fetch(
        """SELECT trim(COALESCE("Номер задания производства", '')) AS task,
                  count(DISTINCT (trim("Модель"), trim("Артикул"),
                                  trim(COALESCE("Признак калькуляции", '')),
                                  "дата расчета")) AS calc_count
             FROM cost_data_all
            WHERE trim(COALESCE("PLAN_ID", '')) = $1
              AND trim(COALESCE("Номер задания производства", '')) != ''
            GROUP BY 1
            ORDER BY 1""",
        plan_id.strip(),
    )
    return [{"task_number": r["task"], "calc_count": r["calc_count"]} for r in rows]


async def create_pfkss_from_source(
    *, model: str, articul: str, plan_id: str, calc_sign: str,
    task_number: str, calc_date: str, username: str,
    override_plan_id: str = "", override_task_number: str = "",
) -> dict:
    """Скопировать строки калькуляции КПСС/ПКПСС в новую ПФКСС.

    Источник строк — то, что пользователь видит в таблице: витрина
    cost_data_all по ключу, поверх — активная версия, как в /raw-rows.
    «дата расчета» новой калькуляции — сегодня, признак — ПФКСС; остальные
    колонки переносятся из исходника без изменений. Исключение — PLAN_ID и
    «Номер задания производства»: при пустых значениях исходника экономист
    может подставить их вручную (override_plan_id/override_task_number),
    тогда дубль-ключ и эти две колонки берутся из override.
    """
    model = (model or "").strip()
    articul = (articul or "").strip()
    plan_id = (plan_id or "").strip()
    calc_sign = (calc_sign or "").strip()
    task = (task_number or "").strip()
    if not model or not articul:
        raise ValueError("не указаны модель и артикул")
    if calc_sign not in PFKSS_FROM_SOURCE_SIGNS:
        raise ValueError(
            "создать ПФКСС можно только из КПСС или ПКПСС, а не "
            f"«{calc_sign or '—'}»"
        )
    # Фронт (AGG_GROUP_FIELDS «дата расчета») шлёт дату всегда; пустая — это
    # ошибка идентификации, а не повод подменять её сегодняшней. Нормализуем
    # один раз здесь и дальше работаем с объектом date.
    if not calc_date:
        raise ValueError("дата расчета исходной калькуляции обязательна")
    try:
        parsed_date = datetime.date.fromisoformat(str(calc_date).split("T")[0])
    except ValueError:
        raise ValueError("дата расчета исходной калькуляции не распознана")

    # 21.10: план/задание исходника могут быть пусты (замена артикула) — тогда
    # их подставляет экономист руками. Задание без плана не выбирается: список
    # заданий существует только у плана. Override плана без задания допустим —
    # получится ПФКСС с планом, но без задания.
    override_plan = (override_plan_id or "").strip()
    override_task = (override_task_number or "").strip()
    if override_task and not override_plan:
        raise ValueError("для выбора задания укажите план")
    # Целевые план/задание новой ПФКСС: override или перенос из исходника.
    target_plan = override_plan or plan_id
    target_task = override_task if override_plan else task

    batch = uuid.uuid4()
    today = datetime.date.today()
    user = (username or "system").strip() or "system"
    reason = f"создана на основании {calc_sign}, задача 664398"
    if override_plan:
        reason += ", план/задание подставлены вручную"

    async with acquire() as conn:
        # Источник строк: активная (draft/pending) версия перекрывает витрину —
        # иначе скопировалась бы просроченная раскладка материалов. get_active_version
        # берёт своё соединение из пула, поэтому зовём его до транзакции.
        ver = await get_active_version(
            model, articul, calc_sign, plan_id or None, None, task or None,
        )
        if ver and ver["rows"]:
            from_sql = "FROM cost_calc_version_rows WHERE version_id = $1"
            from_params = [ver["version"]["id"]]
        else:
            # «дата расчета» — часть ключа исходной калькуляции; фильтруем ею
            # всегда и явно (не её отсутствие = «любая»), иначе соседняя дата
            # того же ключа смешается в выборке.
            from_sql = (
                "FROM cost_data_all"
                ' WHERE trim("Модель") = $1 AND trim("Артикул") = $2'
                " AND trim(COALESCE(\"Признак калькуляции\", '')) = $3"
                " AND trim(COALESCE(\"PLAN_ID\", '')) = $4"
                " AND trim(COALESCE(\"Номер задания производства\", '')) = $5"
                ' AND "дата расчета" = $6::timestamptz'
            )
            from_params = [model, articul, calc_sign, plan_id, task, parsed_date]
        n = len(from_params)

        async with conn.transaction():
            # Гонки двух запросов с одним ключом не редкость — экономист открыл
            # две вкладки и нажал кнопку там и там. advisory-лок берём по
            # уникальному ключу будущей ПФКСС: pfkss::<model>::<articul>::
            # ПФКСС::<plan>::<task> — признак ЦЕЛЕВОЙ, а не источника: два
            # параллельных создания (из КПСС и из ПКПСС) в один целевой ключ
            # должны сериализоваться одним локом. План/задание — целевые (с
            # override). Кладём первой командой транзакции, до любых проверок —
            # иначе dup-проверка ниже успеет увидеть «нет дубля» на обеих
            # сессиях, и получим две вставки.
            lock_key = (
                f"pfkss::{model}::{articul}::{PFKSS_CALC_SIGN}::{target_plan or ''}"
                f"::{target_task or ''}"
            )
            await conn.execute(
                "SELECT pg_advisory_xact_lock(hashtext($1))", lock_key,
            )

            # Ручное задание обязано принадлежать выбранному плану — сверяем
            # с тем же списком, что отдаёт GET /plan-tasks (get_plan_tasks):
            # селект и валидация видят одну и ту же выборку заданий.
            if override_task:
                plan_task_numbers = {
                    t["task_number"] for t in await get_plan_tasks(conn, override_plan)
                }
                if override_task not in plan_task_numbers:
                    raise ValueError(
                        f"задание «{override_task}» отсутствует среди заданий"
                        f" плана «{override_plan}»"
                    )

            # ЧНИ — по Level 01 строк источника (критерий как у сегментов
            # articul_replace): носки и Orodoro операцией не обслуживаются.
            levels = await conn.fetch(
                f"SELECT DISTINCT COALESCE(trim(\"Level 01\"), '') AS l1 {from_sql}",
                *from_params,
            )
            if any(r["l1"] in CHNI_LEVEL01 for r in levels):
                raise ValueError(
                    "для носков (ЧНИ) операция недоступна — замена артикула"
                    " у них идёт своим порядком"
                )

            # Дубль не перезаписываем. cost_data_all покрывает и источник, и
            # созданные в приложении калькуляции, и ПФКСС по приходу. Ключ —
            # целевой (override), задание в ключе строгое: ПФКСС под другим
            # заданием того же плана — другая калькуляция, копированию она
            # не мешает.
            dup = await conn.fetchval(
                """SELECT 1 FROM cost_data_all
                    WHERE trim("Модель") = $1 AND trim("Артикул") = $2
                      AND trim(COALESCE("Признак калькуляции", '')) = $3
                      AND trim(COALESCE("PLAN_ID", '')) = $4
                      AND trim(COALESCE("Номер задания производства", '')) = $5
                    LIMIT 1""",
                model, articul, PFKSS_CALC_SIGN, target_plan, target_task,
            )
            if dup is not None:
                raise PfkssAlreadyExists(
                    "калькуляция ПФКСС по этому ключу (модель, артикул, план"
                    f" «{target_plan or '—'}», задание «{target_task or '—'}»)"
                    " уже есть"
                )

            # Вставка — как _apply_version_rows_to_cache: признак, «дата
            # расчета», PLAN_ID и задание — новые (последние два — целевые:
            # из override либо перенос из исходника), остальные колонки — как
            # в строках источника. Список колонок читаем из схемы
            # cost_data_cache на лету, как create_manual_calc: перечислять
            # имена в коде значит разойтись со схемой после первой же
            # миграции. Пустой план/задание пишем как NULL — как их видит
            # витрина (trim(COALESCE(..., ''))).
            cols = await _cache_columns(conn)
            col_list = ", ".join('"' + c + '"' for c in cols)
            select_parts = []
            for c in cols:
                if c == "Признак калькуляции":
                    select_parts.append(f"${n + 1}")
                elif c == "дата расчета":
                    select_parts.append(f"${n + 2}::timestamptz")
                elif c == "PLAN_ID":
                    select_parts.append(f"${n + 8}")
                elif c == "Номер задания производства":
                    select_parts.append(f"${n + 9}")
                else:
                    select_parts.append(f'"{c}"')
            inserted = await conn.fetch(
                f"""INSERT INTO cost_manual_calc ({col_list}, batch_id, source_calc_sign,
                                                  source_task, created_by, reason)
                    SELECT {", ".join(select_parts)},
                           ${n + 3}, ${n + 4}, ${n + 5}, ${n + 6}, ${n + 7}
                      {from_sql}
                    RETURNING id""",
                *from_params, PFKSS_CALC_SIGN, today, batch, calc_sign,
                task or None, user, reason,
                target_plan or None, target_task or None,
            )
            if not inserted:
                raise ValueError("исходная калькуляция не найдена в данных")

    log(logging.INFO, "создана ПФКСС на основании плановой калькуляции",
        model=model, articul=articul, source_calc_sign=calc_sign,
        plan_id=target_plan or None, task_number=target_task or None,
        source_plan_id=plan_id or None, source_task=task or None,
        rows=len(inserted), batch_id=str(batch), user=user)
    return {
        "batch_id": str(batch),
        "rows": len(inserted),
        "calc_sign": PFKSS_CALC_SIGN,
        "plan_id": target_plan or None,
        "task_number": target_task or None,
    }


async def is_manual_pfkss(conn, model: str, articul: str,
                          plan_id: str, task_number: str) -> bool:
    """ПФКСС с этим ключом создана в вебе (664398, переделка 21.10).

    Блок правки плана/задания в окне «Исходные строки» показываем только
    для «своих» калькуляций: cost_manual_calc пишется только приложением,
    ПФКСС по приходу и плановые исходники живут в кэше источника и
    приложением не менялись. Сравнение ключа — то же trim/COALESCE, что
    в dup-проверке создания.
    """
    found = await conn.fetchval(
        """SELECT 1 FROM cost_manual_calc
            WHERE trim("Модель") = $1 AND trim("Артикул") = $2
              AND trim(COALESCE("Признак калькуляции", '')) = $3
              AND trim(COALESCE("PLAN_ID", '')) = $4
              AND trim(COALESCE("Номер задания производства", '')) = $5
            LIMIT 1""",
        (model or "").strip(), (articul or "").strip(), PFKSS_CALC_SIGN,
        (plan_id or "").strip(), (task_number or "").strip(),
    )
    return found is not None


async def update_pfkss_plan(
    *, model: str, articul: str,
    current_plan_id: str, current_task_number: str,
    new_plan_id: str, new_task_number: str, username: str,
) -> dict:
    """Переписать план и задание уже созданной в вебе ПФКСС (664398).

    Переделка 21.10: правка живёт в самой ПФКСС, а не при создании из
    основания. Текущий ключ адресует строки в cost_manual_calc; новое
    задание обязано принадлежать новому плану. Дубль — если под НОВЫМ
    ключом уже лежит ДРУГАЯ ПФКСС — 409: ключ уникален в витрине, и
    перевод на занятый ключ тихо склеил бы две калькуляции.
    """
    model = (model or "").strip()
    articul = (articul or "").strip()
    cur_plan = (current_plan_id or "").strip()
    cur_task = (current_task_number or "").strip()
    new_plan = (new_plan_id or "").strip()
    new_task = (new_task_number or "").strip()
    if not model or not articul:
        raise ValueError("не указаны модель и артикул")
    # «0» — служебное «нет плана» у выгрузок на новый артикул: план у
    # ПФКСС обязателен, сохранять «нет плана» нечего. Задание без плана
    # не выбирается — список заданий существует только у плана.
    if not new_plan or new_plan == "0":
        raise ValueError("укажите настоящий номер плана: план у ПФКСС обязателен")

    user = (username or "system").strip() or "system"
    async with acquire() as conn:
        async with conn.transaction():
            # Сериализация с созданием ПФКСС в тот же ключ: лок по НОВОМУ
            # ключу (как в create_pfkss_from_source) — иначе параллельное
            # создание и этот UPDATE не увидят друг друга до вставки.
            lock_key = (
                f"pfkss::{model}::{articul}::{PFKSS_CALC_SIGN}::{new_plan}"
                f"::{new_task}"
            )
            await conn.execute(
                "SELECT pg_advisory_xact_lock(hashtext($1))", lock_key,
            )

            # Новое задание обязано принадлежать новому плану — сверяем
            # с тем же списком, что отдаёт GET /plan-tasks (get_plan_tasks):
            # селект и валидация видят одну и ту же выборку заданий.
            if new_task:
                plan_task_numbers = {
                    t["task_number"] for t in await get_plan_tasks(conn, new_plan)
                }
                if new_task not in plan_task_numbers:
                    raise ValueError(
                        f"задание «{new_task}» отсутствует среди заданий"
                        f" плана «{new_plan}»"
                    )

            rows = await conn.fetch(
                """UPDATE cost_manual_calc
                      SET "PLAN_ID" = $3, "Номер задания производства" = $4
                    WHERE trim("Модель") = $1 AND trim("Артикул") = $2
                      AND trim(COALESCE("Признак калькуляции", '')) = $5
                      AND trim(COALESCE("PLAN_ID", '')) = $6
                      AND trim(COALESCE("Номер задания производства", '')) = $7
                    RETURNING id""",
                model, articul, new_plan, new_task or None,
                PFKSS_CALC_SIGN, cur_plan, cur_task,
            )
            if not rows:
                raise ValueError("ПФКСС не найдена среди созданных в вебе")

            # Дубль: cost_data_all — view над cost_manual_calc, изменения
            # видны в той же транзакции. Считаем строки под НОВЫМ ключом:
            # если их больше, чем перевели мы (свой no-op-повтор — не
            # дубль), под ним уже лежит ДРУГАЯ ПФКСС (по приходу или
            # исходник в кэше) — откатываемся, иначе склеим две калькуляции.
            total = await conn.fetchval(
                """SELECT count(*) FROM cost_data_all
                    WHERE trim("Модель") = $1 AND trim("Артикул") = $2
                      AND trim(COALESCE("Признак калькуляции", '')) = $3
                      AND trim(COALESCE("PLAN_ID", '')) = $4
                      AND trim(COALESCE("Номер задания производства", '')) = $5""",
                model, articul, PFKSS_CALC_SIGN, new_plan, new_task,
            )
            if total > len(rows):
                raise PfkssAlreadyExists(
                    "под этим ключом (модель, артикул, план"
                    f" «{new_plan}», задание «{new_task or '—'}»)"
                    " уже есть другая ПФКСС"
                )

    log(logging.INFO, "обновлены план и задание ПФКСС",
        model=model, articul=articul,
        plan_id=new_plan or None, task_number=new_task or None,
        source_plan_id=cur_plan or None, source_task=cur_task or None,
        rows=len(rows), user=user)
    return {
        "rows": len(rows),
        "plan_id": new_plan or None,
        "task_number": new_task or None,
    }


