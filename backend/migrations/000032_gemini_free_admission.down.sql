-- Keep durable admission/history when any real attempt may have happened.
DO $$ BEGIN IF EXISTS(SELECT 1 FROM gemini_free_attempts) THEN
 RAISE EXCEPTION 'Gemini attempts exist; retain durable accounting and migrate forward';
END IF; END $$;
DROP TABLE gemini_free_attempts;
DROP TABLE gemini_free_daily;
DROP TABLE gemini_free_admission;
