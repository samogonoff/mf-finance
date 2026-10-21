-- Свод «Бюджет по ЮЛ» (этапы 1.6, 3, 4; TPL-08): ручные корректировки итогов.
--
-- Свод считается из готовых форм (продажи и расходы), корректировка — отдельная запись «ЮЛ × строка свода × сумма в BYN × причина»:
-- расчёт не переписывается, итог = расчёт + активные корректировки. Снятая корректировка остаётся в журнале (revoked).
-- line_key: sales:<канал> или exp:<группа PL>.
CREATE TABLE IF NOT EXISTS pl_le_adjust (
    id            BIGSERIAL PRIMARY KEY,
    pl_id         BIGINT        NOT NULL REFERENCES pl_instance(id) ON DELETE CASCADE,
    legal_entity  TEXT          NOT NULL,
    line_key      TEXT          NOT NULL,
    amount_byn    NUMERIC(18,2) NOT NULL,
    reason        TEXT          NOT NULL,
    user_id       BIGINT        NOT NULL,
    created_at    TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    revoked       BOOLEAN       NOT NULL DEFAULT FALSE,
    revoked_by    BIGINT,
    revoked_at    TIMESTAMPTZ,
    revoke_reason TEXT          NOT NULL DEFAULT '',
    CONSTRAINT pl_le_adjust_reason_chk CHECK (btrim(reason) <> '')
);

CREATE INDEX IF NOT EXISTS pl_le_adjust_pl_idx ON pl_le_adjust (pl_id) WHERE NOT revoked;
