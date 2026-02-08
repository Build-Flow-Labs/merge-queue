DROP INDEX IF EXISTS idx_merge_queue_retry;
ALTER TABLE merge_queue DROP COLUMN IF EXISTS next_retry_at;
ALTER TABLE merge_queue DROP COLUMN IF EXISTS max_retries;
ALTER TABLE merge_queue DROP COLUMN IF EXISTS retry_count;
