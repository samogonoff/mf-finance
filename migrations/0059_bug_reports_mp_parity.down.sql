DROP INDEX IF EXISTS idx_bug_reports_queue;
DROP INDEX IF EXISTS idx_bug_reports_mr_iid;
ALTER TABLE bug_reports DROP COLUMN IF EXISTS mr_url;
ALTER TABLE bug_reports DROP COLUMN IF EXISTS mr_iid;
ALTER TABLE bug_reports DROP COLUMN IF EXISTS claimed_at;
ALTER TABLE bug_reports DROP COLUMN IF EXISTS claimed_by;
ALTER TABLE bug_reports DROP COLUMN IF EXISTS url;
