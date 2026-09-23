-- 0059 — баг-трекер: паритет с MP (Version20260916120000BugTrackerAutomation).
--
-- url — адрес страницы отдельной колонкой (раньше жил только в route.url):
--   по нему ищут в админке и строят топ «проблемных» страниц в метриках.
-- claimed_by + claimed_at — замок автоматического разбора: автоматизация берёт
--   баг в работу одним UPDATE с условием на пустой замок, поэтому два прогона не
--   разберут один репорт дважды. Живёт ровно столько, сколько статус in_progress.
-- mr_iid / mr_url — подготовленный merge request. Отдельными полями, а не в
--   admin_comment: по mr_iid ищут баги, которые закрывает выкаченный MR.

ALTER TABLE bug_reports ADD COLUMN IF NOT EXISTS url        TEXT NOT NULL DEFAULT '';
ALTER TABLE bug_reports ADD COLUMN IF NOT EXISTS claimed_by TEXT;
ALTER TABLE bug_reports ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ;
ALTER TABLE bug_reports ADD COLUMN IF NOT EXISTS mr_iid     INT;
ALTER TABLE bug_reports ADD COLUMN IF NOT EXISTS mr_url     TEXT;

-- Старые репорты: url переносим из route.url, куда его всегда клал фронт.
UPDATE bug_reports SET url = COALESCE(route->>'url', '') WHERE url = '' AND route ? 'url';

CREATE INDEX IF NOT EXISTS idx_bug_reports_mr_iid ON bug_reports (mr_iid) WHERE mr_iid IS NOT NULL;
-- Очередь разбора: new + без замка, старые сверху.
CREATE INDEX IF NOT EXISTS idx_bug_reports_queue  ON bug_reports (created_at) WHERE status = 'new' AND claimed_by IS NULL;
