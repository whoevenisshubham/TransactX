-- Shared durable emergency stop; config/provider remain disabled by default.
-- This latch does not alter billing, approvals, counters or financial records.
ALTER TABLE gemini_free_admission ADD COLUMN disabled BOOLEAN NOT NULL DEFAULT FALSE;
