-- 003 — денормализация ДОГОВОРА в fact_premaster (дебиторка 62).
-- CH не умеет джойнить MSSQL Objects/Docs, поэтому имя договора и срок/отсрочку
-- материализуем при заливке (см. go/internal/etl/extract.go). repo_clickhouse
-- отдаёт их в rawRow, просрочку считает общий BuildReport.
--
-- ВАЖНО: после применения нужен РЕ-BOOTSTRAP — у старых строк поля пустые
-- (ALTER только добавляет колонки со значением по умолчанию '').
--
-- contract_ref — СЫРАЯ 1С-ссылка субконто договора (как в MSSQL-пути ContractRef),
-- чтобы drill-down (через MSSQL composite) фильтровал по тому же ключу.
-- Все поля String, '' = договора/срока нет.
ALTER TABLE finance.fact_premaster
    ADD COLUMN IF NOT EXISTS contract_ref      String DEFAULT '',
    ADD COLUMN IF NOT EXISTS contract_name     String DEFAULT '',
    ADD COLUMN IF NOT EXISTS contract_delay    String DEFAULT '',
    ADD COLUMN IF NOT EXISTS contract_doc_date String DEFAULT '',
    ADD COLUMN IF NOT EXISTS contract_pay_date String DEFAULT '';
