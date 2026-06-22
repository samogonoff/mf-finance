ALTER TABLE finance.fact_premaster
    DROP COLUMN IF EXISTS contract_ref,
    DROP COLUMN IF EXISTS contract_name,
    DROP COLUMN IF EXISTS contract_delay,
    DROP COLUMN IF EXISTS contract_doc_date,
    DROP COLUMN IF EXISTS contract_pay_date;
