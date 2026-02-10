-- Increase default max_retries from 3 to 10
ALTER TABLE merge_queue ALTER COLUMN max_retries SET DEFAULT 10;

-- Update existing records that haven't been modified
UPDATE merge_queue SET max_retries = 10 WHERE max_retries = 3;
