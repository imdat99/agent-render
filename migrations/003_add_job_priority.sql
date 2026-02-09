-- Add priority column to jobs table
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS priority INTEGER DEFAULT 0;

-- Create index on priority for faster sorting
CREATE INDEX IF NOT EXISTS idx_jobs_priority ON jobs(priority DESC);

-- Update existing jobs to have default priority
UPDATE jobs SET priority = 0 WHERE priority IS NULL;
