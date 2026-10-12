-- Migration 000025 down: Revert worklist projection and consumer inbox

DROP TABLE IF EXISTS ops_payment_worklist CASCADE;
DROP TABLE IF EXISTS consumer_inbox CASCADE;
