CREATE TABLE IF NOT EXISTS accounts (
 id text PRIMARY KEY, balance_kobo bigint NOT NULL CHECK (balance_kobo >= 0)
);
CREATE TABLE IF NOT EXISTS beneficiaries (
 id text PRIMARY KEY,
 account_id text NOT NULL REFERENCES accounts(id),
 name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
 bank_code text NOT NULL CHECK (bank_code IN ('058','044','011')),
 account_number text NOT NULL CHECK (account_number COLLATE "C" ~ '^[0-9]{10}$'),
 UNIQUE(account_id, bank_code, account_number)
);
CREATE TABLE IF NOT EXISTS beneficiary_operations (
 operation_key text PRIMARY KEY, fingerprint text NOT NULL,
 beneficiary_id text REFERENCES beneficiaries(id)
);
CREATE TABLE IF NOT EXISTS transfers (
 payment_key text PRIMARY KEY,
 reference text NOT NULL UNIQUE,
 account_id text NOT NULL REFERENCES accounts(id),
 beneficiary_id text NOT NULL REFERENCES beneficiaries(id),
 amount_kobo bigint NOT NULL CHECK (amount_kobo > 0),
 currency text NOT NULL CHECK (currency = 'NGN'),
 status text NOT NULL CHECK (status IN ('pending','succeeded','failed')),
 reason text NOT NULL DEFAULT '',
 scenario text NOT NULL CHECK (scenario IN ('success','decline','unavailable','delayed','lost-response')),
 attempts integer NOT NULL DEFAULT 0,
 ready_at timestamptz NOT NULL DEFAULT now(),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS debit_journal (
 payment_key text PRIMARY KEY REFERENCES transfers(payment_key),
 amount_kobo bigint NOT NULL CHECK (amount_kobo > 0),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS receipts (
 payment_key text NOT NULL REFERENCES transfers(payment_key),
 kind text NOT NULL,
 content text NOT NULL,
 PRIMARY KEY (payment_key, kind)
);
