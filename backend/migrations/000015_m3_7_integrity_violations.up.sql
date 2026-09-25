-- M3-7-C4: Persist structured check violations in PostgreSQL integrity_check_results.
ALTER TABLE integrity_check_results
    ADD COLUMN IF NOT EXISTS violations JSONB NOT NULL DEFAULT '[]'::jsonb;
