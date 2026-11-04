-- Follow-up lab support. Separate delivery attempts from the financial effect.
CREATE TABLE IF NOT EXISTS receipt_attempts (
 payment_key text PRIMARY KEY REFERENCES transfers(payment_key),
 attempts integer NOT NULL DEFAULT 0
);
