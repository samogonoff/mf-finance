-- 0057 — несколько неизменяемых снимков источника на ключ задания и
-- «основание» версии калькуляции.
--
-- Инцидент 16.09.2026 (задание 121884 / М26.3.1675 / план 9075): версия
-- калькуляции была создана, когда CostHistory ещё не содержал строк основных
-- материалов. Версия — снимок строк, и при каждом обновлении кэша
-- _apply_version_rows_to_cache заменяет собой строки источника по ключу, так
-- что появившиеся позже 7 строк основных материалов в кэш «не доезжали».
-- Замороженный 'original'-снимок (миграция 0026) прятал их и в «Исходных
-- данных» редактора.
--
-- Решение заказчика (16.09.2026): исходные строки — всегда строки CostHistory,
-- но с историей. Снимков источника на ключ становится несколько: первый по
-- прежнему делается при первом сохранении калькуляции, следующие создаёт
-- обновление кэша, когда строки источника по ключу с активными версиями
-- отличаются от последнего снимка (db._snapshot_changed_sources). Все снимки
-- неизменяемы; из снимка можно только создать версию калькуляции. Версия
-- запоминает, от какого снимка сделана (source_version_id): если у активной
-- версии основание старее последнего снимка — таблица показывает признак
-- «источник изменился», экономист сам смотрит новый снимок и делает версию.
-- Кнопки «перенести правки» нет — так решено.

ALTER TABLE cost_calc_versions
    ADD COLUMN IF NOT EXISTS snapshot_no INTEGER,
    ADD COLUMN IF NOT EXISTS source_hash TEXT,
    ADD COLUMN IF NOT EXISTS source_version_id BIGINT
        REFERENCES cost_calc_versions(id) ON DELETE SET NULL;

COMMENT ON COLUMN cost_calc_versions.snapshot_no IS
    'Порядковый номер снимка источника внутри ключа задания (только status=original)';
COMMENT ON COLUMN cost_calc_versions.source_hash IS
    'Отпечаток строк источника, с которых снят снимок (см. db._source_fingerprint). NULL у снимков до 0057';
COMMENT ON COLUMN cost_calc_versions.source_version_id IS
    'Снимок источника, от которого сделана версия калькуляции (основание)';

-- Существующие снимки — первые в своих ключах.
UPDATE cost_calc_versions SET snapshot_no = 1
 WHERE status = 'original' AND snapshot_no IS NULL;

-- Существующие версии сделаны от единственного снимка своего ключа.
UPDATE cost_calc_versions v
   SET source_version_id = o.id
  FROM cost_calc_versions o
 WHERE o.status = 'original'
   AND v.status <> 'original'
   AND v.source_version_id IS NULL
   AND o.model = v.model AND o.articul = v.articul
   AND o.calc_sign IS NOT DISTINCT FROM v.calc_sign
   AND o.plan_id IS NOT DISTINCT FROM v.plan_id
   AND o.task_number = v.task_number;

-- Уникальность номера версии — только для версий калькуляции: у всех снимков
-- version = 0, и они различаются snapshot_no.
ALTER TABLE cost_calc_versions
    DROP CONSTRAINT IF EXISTS cost_calc_versions_key_task_version_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_ver_key_task_version
    ON cost_calc_versions (model, articul, calc_sign, plan_id, task_number, version)
    WHERE status <> 'original';

DROP INDEX IF EXISTS idx_ver_one_original;
CREATE UNIQUE INDEX IF NOT EXISTS idx_ver_snapshot_no
    ON cost_calc_versions (model, articul, calc_sign, plan_id, task_number, snapshot_no)
    WHERE status = 'original';

CREATE INDEX IF NOT EXISTS idx_ver_source_version
    ON cost_calc_versions (source_version_id);
