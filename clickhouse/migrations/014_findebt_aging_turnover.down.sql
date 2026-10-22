DROP TABLE IF EXISTS finance.fact_findebt_turnover;
ALTER TABLE finance.fact_findebt_ccy DROP COLUMN IF EXISTS sum30_d;
ALTER TABLE finance.fact_findebt_ccy DROP COLUMN IF EXISTS sum60_d;
ALTER TABLE finance.fact_findebt_ccy DROP COLUMN IF EXISTS sum90_d;
ALTER TABLE finance.fact_findebt_ccy DROP COLUMN IF EXISTS sum999_d;
ALTER TABLE finance.fact_findebt_ccy DROP COLUMN IF EXISTS sum30_k;
ALTER TABLE finance.fact_findebt_ccy DROP COLUMN IF EXISTS sum60_k;
ALTER TABLE finance.fact_findebt_ccy DROP COLUMN IF EXISTS sum90_k;
ALTER TABLE finance.fact_findebt_ccy DROP COLUMN IF EXISTS sum999_k;
