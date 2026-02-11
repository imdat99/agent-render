-- Enable UUID extension (for gen_random_uuid() if PG < 13)
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Migrate agents table
-- Warning: This casting (USING id::uuid) assumes that the existing data in 'id' column
-- are valid UUID strings. If you have legacy integer IDs stored as text, this will fail.
-- You may need to truncate the table or update invalid IDs first.
ALTER TABLE agents ALTER COLUMN id TYPE uuid USING id::uuid;
ALTER TABLE agents ALTER COLUMN id SET DEFAULT gen_random_uuid();

-- Migrate jobs table
ALTER TABLE jobs ALTER COLUMN id TYPE uuid USING id::uuid;
ALTER TABLE jobs ALTER COLUMN id SET DEFAULT gen_random_uuid();

-- Migrate foreign keys
-- Note: 'user_id' is kept as text because it may contain non-UUID values (e.g., 'user-default').
-- If you strictly want all IDs as UUID, you must ensure user_id data is valid UUIDs.
ALTER TABLE jobs ALTER COLUMN agent_id TYPE uuid USING agent_id::uuid;
