DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='cost_calc_version_rows' AND column_name='Декоры, наименование') THEN
    ALTER TABLE cost_calc_version_rows ADD COLUMN "Декоры, наименование" text;
  END IF;
END $$;
