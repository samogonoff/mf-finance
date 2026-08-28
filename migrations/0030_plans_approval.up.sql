-- 0030 — лист согласования этапов (pl_approval).
-- Таблица отсутствовала: pgStore.RecordApproval писал в неё с самого VS12, ошибка
-- глушилась в Service.StageAction (`_ = s.store.RecordApproval`), поэтому история
-- решений по этапам не сохранялась вообще. Создаём + расширяем полями ТЗ:
-- комментарий (обязателен при возврате), целевой этап возврата и аннулирование
-- (revoked) — при возврате согласования от целевого этапа и выше не удаляются,
-- а помечаются. См. ТЗ МП §2.3, ТЗ Розница §2.3.

CREATE TABLE IF NOT EXISTS pl_approval (
    id             BIGSERIAL PRIMARY KEY,
    pl_id          BIGINT NOT NULL REFERENCES pl_instance(id) ON DELETE CASCADE,
    stage_id       BIGINT REFERENCES pl_stage_instance(id) ON DELETE SET NULL,
    stage_code     TEXT NOT NULL DEFAULT '',      -- дубль кода этапа: живёт после удаления stage_instance
    legal_entity   TEXT NOT NULL DEFAULT '',      -- разрез согласования (этап 3 — по ЮЛ)
    user_id        BIGINT REFERENCES users(id),
    decision       TEXT NOT NULL,                 -- approve|return|submit|start|auto_skipped
    target_stage   TEXT NOT NULL DEFAULT '',      -- целевой этап возврата
    comment        TEXT NOT NULL DEFAULT '',      -- обязателен при decision='return' (проверка в коде)
    revoked        BOOLEAN NOT NULL DEFAULT FALSE,
    revoked_reason TEXT NOT NULL DEFAULT '',
    decided_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_pl_approval_pl ON pl_approval(pl_id, stage_code);
CREATE INDEX IF NOT EXISTS idx_pl_approval_ts ON pl_approval(decided_at DESC);
