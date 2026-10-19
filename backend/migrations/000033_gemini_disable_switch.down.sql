-- Removing the latch could undo an owner's emergency disable decision.
DO $$ BEGIN RAISE EXCEPTION 'Keep the forward Gemini disable-switch schema; do not erase the owner disable state'; END $$;
