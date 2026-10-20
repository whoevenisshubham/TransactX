CREATE TABLE gemini_free_admission (
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),
 lease_owner UUID,
 busy_until TIMESTAMPTZ NOT NULL DEFAULT 'epoch'
);
INSERT INTO gemini_free_admission(singleton) VALUES(TRUE);
CREATE TABLE gemini_free_daily (
 day DATE PRIMARY KEY,
 attempts INTEGER NOT NULL CHECK(attempts BETWEEN 0 AND 10)
);
CREATE TABLE gemini_free_attempts (
 id UUID PRIMARY KEY,
 day DATE NOT NULL REFERENCES gemini_free_daily(day),
 kind TEXT NOT NULL CHECK(kind IN ('countTokens','generateContent')),
 model TEXT NOT NULL,
 started_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 completed_at TIMESTAMPTZ,
 status TEXT NOT NULL CHECK(status IN ('pending','completed','failed','uncertain')),
 request_id TEXT,
 input_tokens BIGINT CHECK(input_tokens>=0),
 output_tokens BIGINT CHECK(output_tokens>=0)
);
