-- 0071 — «Неактуальная модель/артикул»: охват сужен до калькуляции
-- (задача Б24 668461, обращение Журавской М.М. 26.10.2026)
--
-- Статус жил на паре модель+артикул (0056) и ложился на все калькуляции
-- изделия. У одного плана бывает несколько выгрузок ПФКСС с разными заданиями,
-- для ценообразования берётся максимальная себестоимость — неактуальной надо
-- помечать конкретную калькуляцию с меньшей себестоимостью, иначе серым
-- заливается и «ценовая» строка. Поэтому ключ расширяется до калькуляции:
-- модель + артикул + признак + план + задание — как у статуса ПЭО и отметки
-- «нужна замена артикула» (пустые части храним как '', не NULL — урок 0023).
-- Имя статуса в интерфейсе не меняется.
--
-- Старые пары, стоящие в проде с 09.09, разворачиваем на калькуляции из
-- cost_data_cache: вид таблицы у пользователей не меняется, автор и время
-- отметки сохраняются. Пары, которых в кэше нет (источник уже перезалит),
-- остаются строками с пустым ключом — безвредны, снимаются галочкой.

ALTER TABLE cost_obsolete
    ADD COLUMN IF NOT EXISTS calc_sign   TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS plan_id     TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS task_number TEXT NOT NULL DEFAULT '';

ALTER TABLE cost_obsolete DROP CONSTRAINT IF EXISTS uq_obsolete_key;
ALTER TABLE cost_obsolete
    ADD CONSTRAINT uq_obsolete_key
    UNIQUE (model, articul, calc_sign, plan_id, task_number);

-- Развёртывание старых пар (строки с пустыми calc_sign/plan_id/task_number)
-- на все их калькуляции, известные кэшу. Кэш обрезаем тем же strip'ом, каким
-- make_key готовит ключи строк (app/obsolete.py).
INSERT INTO cost_obsolete
       (model, articul, calc_sign, plan_id, task_number,
        obsolete, comment, set_by, set_at, created_at, updated_at)
SELECT DISTINCT
       o.model, o.articul,
       COALESCE(btrim(c."Признак калькуляции"), ''),
       COALESCE(btrim(c."PLAN_ID"), ''),
       COALESCE(btrim(c."Номер задания производства"), ''),
       o.obsolete, o.comment, o.set_by, o.set_at, now(), now()
  FROM cost_obsolete o
  JOIN cost_data_cache c
    ON btrim(COALESCE(c."Модель", ''))  = o.model
   AND btrim(COALESCE(c."Артикул", '')) = o.articul
 WHERE o.calc_sign = '' AND o.plan_id = '' AND o.task_number = ''
ON CONFLICT ON CONSTRAINT uq_obsolete_key DO NOTHING;

-- Пару, развернувшуюся хотя бы на одну конкретную калькуляцию, убираем — её
-- данные теперь в копиях. Пары без единой калькуляции в кэше оставляем: они
-- нечему не мешают и хранят историю отметки.
DELETE FROM cost_obsolete o
 WHERE o.calc_sign = '' AND o.plan_id = '' AND o.task_number = ''
   AND EXISTS (
       SELECT 1
         FROM cost_obsolete c
        WHERE c.model = o.model
          AND c.articul = o.articul
          AND (c.calc_sign, c.plan_id, c.task_number) <> ('', '', '')
   );

DROP INDEX IF EXISTS ix_obsolete_active;
CREATE INDEX IF NOT EXISTS ix_obsolete_active
    ON cost_obsolete (model, articul, calc_sign, plan_id, task_number) WHERE obsolete;
