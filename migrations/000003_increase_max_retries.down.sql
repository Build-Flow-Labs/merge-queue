-- Revert max_retries default to 3
ALTER TABLE merge_queue ALTER COLUMN max_retries SET DEFAULT 3;
