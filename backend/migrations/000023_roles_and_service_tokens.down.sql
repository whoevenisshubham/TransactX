DROP TABLE IF EXISTS control_idempotency_records;
DROP TABLE IF EXISTS operational_audit_logs;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('CUSTOMER', 'MERCHANT', 'OPS_ADMIN'));
