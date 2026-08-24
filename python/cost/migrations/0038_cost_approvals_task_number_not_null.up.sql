-- 0038 — согласования ПЭО: пустое задание хранится как '' вместо NULL
--
-- Зачем. UNIQUE (model, articul, calc_sign, plan_id, task_number) из 0023 в
-- Postgres по умолчанию NULLS DISTINCT: две записи с task_number IS NULL не
-- конфликтуют. Поэтому для калькуляций без номера задания (ПКПСС и всё, где
-- источник задание не заполняет) ON CONFLICT DO UPDATE не срабатывал вообще —
-- каждое согласование добавляло НОВУЮ строку вместо обновления существующей.
-- Проверено опытом на dev: два upsert'а с task_number IS NULL дают 2 записи,
-- с заполненным заданием — 1.
--
-- Чем это ломало интерфейс. В /aggregated статус считается как
-- BOOL_AND(ca.status = 'approved'), поэтому при дублях с разными статусами
-- согласованная калькуляция показывалась отклонённой (approved + rejected), а с
-- парой approved + pending — вообще бесцветной. Ровно это увидел пользователь
-- 24.08.2026: «согласовала данные модели, загрузила новые — согласование на
-- предыдущие исчезло, зелёные кружочки стали снова бесцветными».
--
-- Вторая половина той же проблемы — в кэше поле задания заполнено пустой
-- строкой, а не NULL (на dev: 1 018 854 строк заполнено, 2 582 пустой строкой,
-- 2 NULL), а join сравнивал через IS NOT DISTINCT FROM без COALESCE. Это
-- правится в коде (routes.py); здесь приводим к одному виду хранение.

-- 1. Схлопнуть дубли, накопившиеся из-за NULLS DISTINCT. Побеждает последняя
--    запись: сначала по времени решения, затем по updated_at, затем по id.
DELETE FROM cost_calc_approvals a
USING (
    SELECT id,
           ROW_NUMBER() OVER (
               PARTITION BY model, articul, COALESCE(calc_sign, ''),
                            COALESCE(plan_id, ''), COALESCE(task_number, '')
               ORDER BY approved_at DESC NULLS LAST, updated_at DESC, id DESC
           ) AS rn
    FROM cost_calc_approvals
) d
WHERE a.id = d.id AND d.rn > 1;

-- 2. NULL → пустая строка. Строго после шага 1: иначе UPDATE упёрся бы в
--    UNIQUE, как только два NULL-ряда с одинаковыми остальными полями стали
--    бы двумя ''.
UPDATE cost_calc_approvals SET task_number = '' WHERE task_number IS NULL;

-- 3. Закрепить инвариант. С NOT NULL действующий UNIQUE начинает работать и для
--    строк без задания — повторное согласование обновляет запись, а не плодит.
ALTER TABLE cost_calc_approvals ALTER COLUMN task_number SET DEFAULT '';
ALTER TABLE cost_calc_approvals ALTER COLUMN task_number SET NOT NULL;
