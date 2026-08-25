-- 0037 — журнал действий по заданию: прозрачная передача работы.
--
-- Что было: у задания хранился только ТЕКУЩИЙ делегат (pl_task.delegate_user_id).
-- Кто передал, когда, зачем и с каким сроком — нигде. После пары передач по
-- цепочке ответить на вопрос «почему это задание у него и кто его туда отдал»
-- было невозможно: в интерфейсе виден лишь конечный держатель, а в аудите
-- модуля действий по заданиям не было вовсе.
--
-- Требование ТЗ и UX-ревизии: «срок + комментарий при делегировании», видимость
-- «кто кого замещает» и цепочки передач (ux-redesign §7c — числилось
-- недоделанным). Плюс общий принцип обеих новых редакций ТЗ: любое движение
-- работы объясняется комментарием и остаётся в истории (ср. возврат этапа —
-- §2.3, где комментарий обязателен, а решения не удаляются).

-- Срок и авторство делегирования — на самом задании (быстрый доступ для списка).
ALTER TABLE pl_task ADD COLUMN IF NOT EXISTS due_at         TIMESTAMPTZ;
ALTER TABLE pl_task ADD COLUMN IF NOT EXISTS delegated_by   BIGINT REFERENCES users(id);
ALTER TABLE pl_task ADD COLUMN IF NOT EXISTS delegated_at   TIMESTAMPTZ;
ALTER TABLE pl_task ADD COLUMN IF NOT EXISTS delegate_note  TEXT NOT NULL DEFAULT '';

-- Журнал: одна строка на каждое действие с заданием.
CREATE TABLE IF NOT EXISTS pl_task_event (
    id          BIGSERIAL PRIMARY KEY,
    task_id     BIGINT NOT NULL REFERENCES pl_task(id) ON DELETE CASCADE,
    -- start|delegate|submit|accept|return|reopen|assign
    action      TEXT   NOT NULL,
    actor_id    BIGINT REFERENCES users(id),     -- кто сделал
    target_id   BIGINT REFERENCES users(id),     -- кому передали (delegate/assign)
    status_from TEXT   NOT NULL DEFAULT '',
    status_to   TEXT   NOT NULL DEFAULT '',
    comment     TEXT   NOT NULL DEFAULT '',
    due_at      TIMESTAMPTZ,                     -- срок, названный при передаче
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_pl_task_event_task ON pl_task_event(task_id, created_at);
CREATE INDEX IF NOT EXISTS idx_pl_task_event_actor ON pl_task_event(actor_id, created_at DESC);
