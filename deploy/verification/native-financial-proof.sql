-- Read-only native recovery proof. Supply psql variables documented in deploy/README.md.
\set ON_ERROR_STOP on
SELECT json_build_object(
'paymentState',(select state from payments where id=:'payment_id'),
'paymentAmount',(select amount_paise from payments where id=:'payment_id'),
'logicalPayments',(select count(*) from payments where initiated_by_user_id=:'sender_user_id' and note=:'payment_note'),
'idempotencyRows',(select count(*) from idempotency_records where user_id=:'sender_user_id' and key=:'idempotency_key' and payment_id=:'payment_id'),
'ledgerTransactions',(select count(*) from ledger_transactions where payment_id=:'payment_id'),
'ledgerEntries',(select count(*) from ledger_entries le join ledger_transactions lt on lt.id=le.ledger_transaction_id where lt.payment_id=:'payment_id'),
'correctLedgerEntries',(select count(*) from ledger_entries le join ledger_transactions lt on lt.id=le.ledger_transaction_id join accounts a on a.id=le.account_id where lt.payment_id=:'payment_id' and le.amount_paise=100 and ((le.entry_type='DEBIT' and a.user_id=:'sender_user_id') or (le.entry_type='CREDIT' and a.user_id=:'receiver_user_id'))),
'centralTotal',(select sum(balance_paise) from accounts),
'centralReconstructionErrors',(select count(*) from accounts a where a.balance_paise <> a.opening_balance_paise + coalesce((select sum(case when le.entry_type='CREDIT' then le.amount_paise else -le.amount_paise end) from ledger_entries le where le.account_id=a.id),0)),
'centralSender',(select balance_paise from accounts where user_id=:'sender_user_id'),
'centralReceiver',(select balance_paise from accounts where user_id=:'receiver_user_id'),
'participantTotal',(select sum(balance_paise) from bank_a.accounts),
'participantSender',(select b.balance_paise from bank_a.accounts b join accounts a on a.bank_account_id=b.id where a.user_id=:'sender_user_id'),
'participantReceiver',(select b.balance_paise from bank_a.accounts b join accounts a on a.bank_account_id=b.id where a.user_id=:'receiver_user_id'),
'participantOperations',(select count(*) from bank_a.operations where payment_id=:'payment_id'),
'otherParticipantOperations',(select count(*) from bank_b.operations where payment_id=:'payment_id'),
'participantUniqueOperations',(select count(distinct operation_id) from bank_a.operations where payment_id=:'payment_id'),
'participantPayloadErrors',(select count(*) from bank_a.operations b where payment_id=:'payment_id' and (amount_paise<>100 or currency<>'INR' or account_id not in (select bank_account_id from accounts where user_id in (:'sender_user_id',:'receiver_user_id')))),
'originalHoldRows',(select count(*) from bank_a.operations where payment_id=:'payment_id' and operation_id=:'operation_id' and operation_type='HOLD' and amount_paise=100 and currency='INR' and account_id=(select bank_account_id from accounts where user_id=:'sender_user_id')),
'participantLedgerRows',(select count(*) from bank_a.ledger_entries where payment_id=:'payment_id'),
'participantLedgerDuplicates',(select count(*) from (select operation_id,entry_type,account_id from bank_a.ledger_entries where payment_id=:'payment_id' group by operation_id,entry_type,account_id having count(*)<>1) d),
'correctParticipantEntries',(select count(*) from bank_a.ledger_entries b join accounts a on a.bank_account_id=b.account_id where payment_id=:'payment_id' and amount_paise=100 and currency='INR' and ((entry_type='HOLD' and a.user_id=:'sender_user_id') or (entry_type in ('PROVISIONAL_CREDIT','FINAL_CREDIT') and a.user_id=:'receiver_user_id'))),
'journalRows',(select count(*) from payment_bank_operations where payment_id=:'payment_id'),
'journalParticipantMismatch',(select count(*) from payment_bank_operations j left join bank_a.operations b on b.operation_id=j.operation_id where j.payment_id=:'payment_id' and (b.operation_id is null or b.payment_id<>j.payment_id or b.amount_paise<>j.amount_paise or b.currency<>j.currency)),
'projectionRows',(select count(*) from ops_payment_worklist w join payments p on p.id=w.payment_id where w.payment_id=:'payment_id' and w.current_state=p.state),
'pendingDeliveries',(select count(*) from outbox_deliveries d join outbox_events e on e.id=d.event_id where e.aggregate_id=:'payment_id' and d.status<>'delivered'),
'outboxEvents',(select count(*) from outbox_events where aggregate_id=:'payment_id'),
'inboxEvents',(select count(*) from consumer_inbox i join outbox_events e on e.id=i.event_id where e.aggregate_id=:'payment_id'),
'recoveryJob',(select row_to_json(j) from (select status,lease_epoch,attempt_count,last_reason_code from recovery_jobs where payment_id=:'payment_id') j));

SELECT json_build_object('nullOrInvalidPayloads',(select count(*) from bank_a.operations where payment_id=:'payment_id' and (account_id IS NULL OR amount_paise IS NULL OR currency IS NULL OR amount_paise<>100 OR currency<>'INR' OR idempotency_key<>operation_id::text OR status NOT IN ('CONFIRMED','FINAL'))),'bankIdentityMismatch',(select count(*) from payment_bank_operations j join bank_a.operations b on b.operation_id=j.operation_id join banks bank on bank.id=j.bank_id where j.payment_id=:'payment_id' and bank.code<>b.bank_id),'confirmAccountMismatch',(select count(*) from bank_a.operations b join bank_a.operations h on h.operation_id=b.hold_id where b.payment_id=:'payment_id' and b.operation_type='CONFIRM_HOLD' and (b.account_id<>h.account_id OR b.amount_paise<>h.amount_paise OR b.currency<>h.currency)),'activeCorpusVersions',(select count(*) from corpus_versions where status='active'),'activeCorpusChunks',(select count(*) from runbook_chunks c join corpus_versions v on v.version=c.corpus_version where v.status='active'),'manifestHash',(select manifest_sha256 from corpus_versions where status='active'));
