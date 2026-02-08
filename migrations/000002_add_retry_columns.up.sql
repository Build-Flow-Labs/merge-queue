-- Add retry tracking columns
ALTER TABLE merge_queue ADD COLUMN IF NOT EXISTS retry_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE merge_queue ADD COLUMN IF NOT EXISTS max_retries INTEGER NOT NULL DEFAULT 3;
ALTER TABLE merge_queue ADD COLUMN IF NOT EXISTS next_retry_at TIMESTAMPTZ;

-- Index for finding items ready for retry
CREATE INDEX IF NOT EXISTS idx_merge_queue_retry ON merge_queue(status, next_retry_at)
    WHERE status = 'failed' AND next_retry_at IS NOT NULL;
