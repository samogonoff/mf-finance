-- 0032 — номер задания производства входит в ключ версии калькуляции.
--
-- Нестыковка. Главная таблица агрегирует с учётом задания —
-- "Номер задания производства" есть в AGG_GROUP_FIELDS, то есть пользователь
-- видит СТРОКУ НА ЗАДАНИЕ. А ключ версии его не учитывал:
-- (model, articul, calc_sign, plan_id). Поэтому версия, созданная из строки
-- одного задания, применялась ко ВСЕМ заданиям этой модели+артикула в плане.
-- По данным КПСС: 104 из 428 ключей охватывают несколько заданий (до 4).
--
-- Согласование ПЭО эту нестыковку уже не имеет: у cost_calc_approvals ключ
-- (model, articul, calc_sign, plan_id, task_number). Версии были единственным
-- исключением — приводим к той же конвенции.
--
-- ПКПСС ("новая разработка") — особый случай: там В ИСТОЧНИКЕ нет ни номера
-- плана, ни номера задания (проверено: все 2040 строк с пустыми обоими полями).
-- Для них ключ естественно вырождается, task_number = ''. Поэтому колонка
-- NOT NULL DEFAULT '' а не NULL: в PostgreSQL NULL'ы в UNIQUE считаются
-- различными, и уникальность «одна версия на ключ» с NULL просто не работала бы.
--
-- Добавление поля в ключ только ДРОБИТ группы, никогда не сливает, поэтому
-- конфликтов уникальности эта миграция создать не может (в отличие от 0027,
-- которая поле из ключа убирала и упала в CI на дубликатах).
--
-- Легаси-версии, охватывающие несколько заданий, остаются с task_number = ''.
-- Это трактуется как «версия старого формата, распространяется на все задания
-- ключа»: код ищет сначала точное совпадение по заданию, и лишь при отсутствии
-- берёт ''-версию. При следующем сохранении такая версия получает конкретное
-- задание и нестыковка сама рассасывается. Разделять их автоматически не стали:
-- у части строк это чужая незавершённая работа, а дробление необратимо.

ALTER TABLE cost_calc_versions
    ADD COLUMN IF NOT EXISTS task_number text NOT NULL DEFAULT '';

-- Версиям, все строки которых относятся к ОДНОМУ заданию, проставляем его.
-- Многозадачные (COUNT(DISTINCT ...) > 1) намеренно не трогаем — остаются ''.
UPDATE cost_calc_versions v
SET task_number = t.tn
FROM (
    SELECT version_id,
           MIN(trim(COALESCE("Номер задания производства", ''))) AS tn
    FROM cost_calc_version_rows
    GROUP BY version_id
    HAVING COUNT(DISTINCT trim(COALESCE("Номер задания производства", ''))) = 1
) t
WHERE v.id = t.version_id
  AND v.task_number = '';

ALTER TABLE cost_calc_versions
    DROP CONSTRAINT cost_calc_versions_model_articul_calc_sign_plan_id_version_key;
-- Имя намеренно короткое: полное по образцу прежнего вылезало за лимит
-- PostgreSQL в 63 байта и молча усекалось, а DROP в down-миграции полагался бы
-- на то же усечение.
ALTER TABLE cost_calc_versions
    ADD CONSTRAINT cost_calc_versions_key_task_version_key
        UNIQUE (model, articul, calc_sign, plan_id, task_number, version);

-- Неизменяемый снимок источника ("original") тоже становится пер-заданным.
DROP INDEX idx_ver_one_original;
CREATE UNIQUE INDEX idx_ver_one_original
    ON cost_calc_versions (model, articul, calc_sign, plan_id, task_number)
    WHERE status = 'original';
