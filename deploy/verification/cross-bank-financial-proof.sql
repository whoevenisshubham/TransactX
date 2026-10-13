-- Read-only proof for the documented 100-paise BANK-A to BANK-B recovery scenario.
\set ON_ERROR_STOP on
WITH participant_operations AS (
 SELECT 'BANK-A' AS schema_bank, * FROM bank_a.operations WHERE payment_id=:'payment_id'
 UNION ALL SELECT 'BANK-B' AS schema_bank, * FROM bank_b.operations WHERE payment_id=:'payment_id'
), participant_entries AS (
 SELECT 'BANK-A' AS schema_bank, * FROM bank_a.ledger_entries WHERE payment_id=:'payment_id'
 UNION ALL SELECT 'BANK-B' AS schema_bank, * FROM bank_b.ledger_entries WHERE payment_id=:'payment_id'
)
SELECT json_build_object(
 'state',(SELECT state FROM payments WHERE id=:'payment_id'),
 'logicalPayments',(SELECT count(*) FROM payments WHERE initiated_by_user_id=:'sender_id' AND note=:'note'),
 'idempotencyRows',(SELECT count(*) FROM idempotency_records WHERE user_id=:'sender_id' AND key=:'key' AND payment_id=:'payment_id'),
 'centralTransactions',(SELECT count(*) FROM ledger_transactions WHERE payment_id=:'payment_id'),
 'centralEntries',(SELECT count(*) FROM ledger_entries e JOIN ledger_transactions t ON t.id=e.ledger_transaction_id WHERE t.payment_id=:'payment_id'),
 'correctCentralEntries',(SELECT count(*) FROM ledger_entries e JOIN ledger_transactions t ON t.id=e.ledger_transaction_id JOIN accounts a ON a.id=e.account_id WHERE t.payment_id=:'payment_id' AND e.amount_paise=100 AND ((e.entry_type='DEBIT' AND a.user_id=:'sender_id') OR (e.entry_type='CREDIT' AND a.user_id=:'receiver_id'))),
 'centralTotal',(SELECT sum(balance_paise) FROM accounts),
 'centralReconstructionErrors',(SELECT count(*) FROM accounts a WHERE a.balance_paise<>a.opening_balance_paise+coalesce((SELECT sum(CASE WHEN e.entry_type='CREDIT' THEN e.amount_paise ELSE -e.amount_paise END) FROM ledger_entries e WHERE e.account_id=a.id),0)),
 'centralSender',(SELECT balance_paise FROM accounts WHERE user_id=:'sender_id'),
 'centralReceiver',(SELECT balance_paise FROM accounts WHERE user_id=:'receiver_id'),
 'bankASender',(SELECT b.balance_paise FROM bank_a.accounts b JOIN accounts a ON a.bank_account_id=b.id WHERE a.user_id=:'sender_id'),
 'bankBReceiver',(SELECT b.balance_paise FROM bank_b.accounts b JOIN accounts a ON a.bank_account_id=b.id WHERE a.user_id=:'receiver_id'),
 'participantTotal',(SELECT coalesce((SELECT sum(balance_paise) FROM bank_a.accounts),0)+coalesce((SELECT sum(balance_paise) FROM bank_b.accounts),0)),
 'bankAOperations',(SELECT count(*) FROM participant_operations WHERE schema_bank='BANK-A'),
 'bankBOperations',(SELECT count(*) FROM participant_operations WHERE schema_bank='BANK-B'),
 'uniqueOperations',(SELECT count(DISTINCT operation_id) FROM participant_operations),
 'originalHoldRows',(SELECT count(*) FROM participant_operations WHERE schema_bank='BANK-A' AND operation_id=:'operation_id' AND operation_type='HOLD'),
 'bankAHoldEntries',(SELECT count(*) FROM participant_entries WHERE schema_bank='BANK-A' AND operation_id=:'operation_id' AND entry_type='HOLD'),
 'participantEntries',(SELECT count(*) FROM participant_entries),
 'correctParticipantEntries',(SELECT count(*) FROM participant_entries e JOIN accounts a ON a.bank_account_id=e.account_id WHERE e.amount_paise=100 AND e.currency='INR' AND ((e.schema_bank='BANK-A' AND a.user_id=:'sender_id' AND e.entry_type='HOLD') OR (e.schema_bank='BANK-B' AND a.user_id=:'receiver_id' AND e.entry_type IN ('PROVISIONAL_CREDIT','FINAL_CREDIT')))),
 'duplicateEffects',(SELECT count(*) FROM (SELECT schema_bank,operation_id,entry_type,account_id FROM participant_entries GROUP BY 1,2,3,4 HAVING count(*)<>1) d),
 'invalidPayloads',(SELECT count(*) FROM participant_operations p WHERE p.amount_paise IS NULL OR p.amount_paise<>100 OR p.currency IS NULL OR p.currency<>'INR' OR p.account_id IS NULL OR p.idempotency_key<>p.operation_id::text OR p.bank_id<>p.schema_bank OR p.status NOT IN ('CONFIRMED','FINAL') OR NOT EXISTS(SELECT 1 FROM accounts a JOIN banks b ON b.id=a.bank_id WHERE a.bank_account_id=p.account_id AND b.code=p.schema_bank AND a.user_id=CASE WHEN p.schema_bank='BANK-A' THEN :'sender_id'::uuid ELSE :'receiver_id'::uuid END)),
 'journalRows',(SELECT count(*) FROM payment_bank_operations WHERE payment_id=:'payment_id'),
 'journalMismatches',(SELECT count(*) FROM payment_bank_operations j JOIN banks b ON b.id=j.bank_id LEFT JOIN participant_operations p ON p.operation_id=j.operation_id WHERE j.payment_id=:'payment_id' AND (p.operation_id IS NULL OR p.schema_bank<>b.code OR p.amount_paise<>j.amount_paise OR p.currency<>j.currency)),
 'projectionRows',(SELECT count(*) FROM ops_payment_worklist w JOIN payments p ON p.id=w.payment_id WHERE w.payment_id=:'payment_id' AND w.current_state=p.state),
 'pendingDeliveries',(SELECT count(*) FROM outbox_deliveries d JOIN outbox_events e ON e.id=d.event_id WHERE e.aggregate_id=:'payment_id' AND d.status<>'delivered'),
 'outboxEvents',(SELECT count(*) FROM outbox_events WHERE aggregate_id=:'payment_id'),
 'inboxEvents',(SELECT count(*) FROM consumer_inbox i JOIN outbox_events e ON e.id=i.event_id WHERE e.aggregate_id=:'payment_id'),
 'recoveryStatus',(SELECT status FROM recovery_jobs WHERE payment_id=:'payment_id'));
