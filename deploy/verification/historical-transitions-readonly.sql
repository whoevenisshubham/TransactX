-- Read-only forensic inventory. Supply credentials privately, never on the CLI.
-- psql -X -v ON_ERROR_STOP=1 -f historical-transitions-readonly.sql
-- The caller must connect to the explicitly authorized affected database.
BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout = '15s';
SELECT current_database() AS inspected_database;
SELECT count(*) AS surviving_payments FROM public.payments;
SELECT count(*) AS surviving_transitions FROM public.payment_state_transitions;
SELECT count(*) AS payments_without_recorded_history
FROM public.payments p
WHERE NOT EXISTS (SELECT 1 FROM public.payment_state_transitions t WHERE t.payment_id=p.id);
SELECT tgname, tgenabled FROM pg_trigger
WHERE tgrelid='public.payment_state_transitions'::regclass AND NOT tgisinternal;
SELECT count(*) AS histories_with_broken_adjacency FROM (
  SELECT payment_id, from_state,
    lag(to_state) OVER(PARTITION BY payment_id ORDER BY sequence_number) AS previous_to
  FROM public.payment_state_transitions
) history WHERE previous_to IS NOT NULL AND previous_to<>from_state;
COMMIT;
-- Counts/gaps do not identify deleted originals. Sequence gaps can also result
-- from rolled-back transactions. Payments predating migration 16 can legitimately
-- have no transition history. This report never establishes a restored-row count.
