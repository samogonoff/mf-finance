-- 0049 — генерация заданий: крупные и мелкие МП раздельно + защита от дублей.
--
-- Найдено проходом 07.09.2026: периоды несовместимы между собой. В 2026-06
-- розница шла ОДНИМ заданием на 211 ЦФО всех четырёх стран, а МП были разделены
-- на large/small; в 2026-08/09/10 наоборот — розница разделена по странам
-- (миграция 0038), а МП собраны в одно задание на 15 ЦФО (миграция 0029).
-- Ни один период не соответствует требованиям полностью.
--
-- 1) Крупные и мелкие МП — независимые подзадачи (ТЗ §2.2 и звонок: «возврат
--    мелких МП не должен останавливать крупные»). Возвращаем два шаблона.
--    Карточки форм и так раздельные — одним заданием было только задание.
--
-- 2) Дубли. В 2026-06 на этапе 1.1 висели два задания «Маркетплейсы large» на
--    те же 4 ЦФО (одно с исполнителем, одно без), в списке «Все задания» — по
--    два набора «Товарооборот розницы · РБ/РФ/KZ/UZ». Причина в генерации: она
--    сносила только НЕтронутые (pending) задания и вставляла новые, поэтому
--    каждое повторное нажатие «Генерировать» плодило копии тронутых.
--    Уникальный индекс делает это невозможным на уровне БД, а генерация
--    переходит на upsert (task_store.go).

-- ── 1. Шаблон МП снова разделён на large и small ────────────────────────────
UPDATE pl_task_template
   SET title      = 'Маркетплейсы large',
       cfo_filter = '{"segment":"large"}'
 WHERE form_code = 'TPL-MP' AND stage_code = '1.1' AND title = 'Маркетплейсы';

INSERT INTO pl_task_template (stage_code, form_code, title, cfo_filter, task_role, group_by, sort_order)
SELECT '1.1', 'TPL-MP', 'Маркетплейсы small', '{"segment":"small"}', 'fill', 'position', 25
 WHERE NOT EXISTS (
     SELECT 1 FROM pl_task_template
      WHERE form_code = 'TPL-MP' AND stage_code = '1.1' AND title = 'Маркетплейсы small');

-- ── 2. Снять дубли перед уникальным индексом ────────────────────────────────
-- Оставляем ОДНО задание на (период, шаблон, должность, ЮЛ) — то, у которого
-- больше следов работы: назначенный исполнитель, делегат, непустой статус.
-- Порядок сравнения делает выбор детерминированным на любых данных.
DELETE FROM pl_task t
 WHERE EXISTS (
     SELECT 1 FROM pl_task k
      WHERE k.pl_id = t.pl_id
        AND k.template_id IS NOT DISTINCT FROM t.template_id
        AND COALESCE(k.position_id, 0) = COALESCE(t.position_id, 0)
        AND k.legal_entity = t.legal_entity
        AND (
             (k.assignee_user_id IS NOT NULL) > (t.assignee_user_id IS NOT NULL)
          OR ((k.assignee_user_id IS NOT NULL) = (t.assignee_user_id IS NOT NULL)
              AND (k.delegate_user_id IS NOT NULL) > (t.delegate_user_id IS NOT NULL))
          OR ((k.assignee_user_id IS NOT NULL) = (t.assignee_user_id IS NOT NULL)
              AND (k.delegate_user_id IS NOT NULL) = (t.delegate_user_id IS NOT NULL)
              AND (k.status <> 'pending') > (t.status <> 'pending'))
          OR ((k.assignee_user_id IS NOT NULL) = (t.assignee_user_id IS NOT NULL)
              AND (k.delegate_user_id IS NOT NULL) = (t.delegate_user_id IS NOT NULL)
              AND (k.status <> 'pending') = (t.status <> 'pending')
              AND k.id < t.id)
        )
 );

CREATE UNIQUE INDEX IF NOT EXISTS uq_pl_task_slot
    ON pl_task (pl_id, template_id, COALESCE(position_id, 0), legal_entity);

-- ── 3. Служебный текст из названий заданий ──────────────────────────────────
-- «· исполнитель не назначен» и «· без ТОПа» стояли в САМОМ названии, рядом с
-- отдельным бейджем того же смысла. Исполнителю это ничего не говорит.
UPDATE pl_task
   SET title = regexp_replace(title, ' · (исполнитель не назначен|без ТОПа)$', '')
 WHERE title ~ ' · (исполнитель не назначен|без ТОПа)$';
