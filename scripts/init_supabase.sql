-- PicoClaw Supabase PostgreSQL Schema
-- Run this in the Supabase SQL Editor (Dashboard -> SQL Editor -> New Query)
-- to initialize the persistent memory tables for PicoClaw.

-- 1. Sessions Table: Stores session state, conversation summaries, and metadata scopes.
CREATE TABLE IF NOT EXISTS picoclaw_sessions (
    session_key TEXT PRIMARY KEY,
    summary TEXT DEFAULT '',
    scope JSONB,
    aliases TEXT[] DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- 2. Messages Table: Stores chronological conversation history with full provider messages (tool calls, roles, content).
CREATE TABLE IF NOT EXISTS picoclaw_messages (
    id BIGSERIAL PRIMARY KEY,
    session_key TEXT NOT NULL REFERENCES picoclaw_sessions(session_key) ON DELETE CASCADE,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    raw_message JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- 3. Indexes for fast retrieval
CREATE INDEX IF NOT EXISTS idx_picoclaw_messages_session_id ON picoclaw_messages(session_key, id ASC);
CREATE INDEX IF NOT EXISTS idx_picoclaw_sessions_updated_at ON picoclaw_sessions(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_picoclaw_sessions_aliases ON picoclaw_sessions USING GIN (aliases);

-- 4. Auto-update timestamp trigger for picoclaw_sessions
CREATE OR REPLACE FUNCTION update_picoclaw_sessions_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_picoclaw_sessions_updated_at ON picoclaw_sessions;
CREATE TRIGGER trg_picoclaw_sessions_updated_at
BEFORE UPDATE ON picoclaw_sessions
FOR EACH ROW
EXECUTE FUNCTION update_picoclaw_sessions_updated_at();

-- 5. Row Level Security (RLS)
-- If your service uses the Supabase 'service_role' key (recommended for backend daemons),
-- service_role automatically bypasses RLS.
-- If you use the 'anon' key, enable these permissive policies:
ALTER TABLE picoclaw_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE picoclaw_messages ENABLE ROW LEVEL SECURITY;

CREATE POLICY "Allow all access to picoclaw_sessions"
    ON picoclaw_sessions
    FOR ALL
    USING (true)
    WITH CHECK (true);

CREATE POLICY "Allow all access to picoclaw_messages"
    ON picoclaw_messages
    FOR ALL
    USING (true)
    WITH CHECK (true);
