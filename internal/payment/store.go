package payment

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"devfest26/internal/domain"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	Pool  *pgxpool.Pool
	Limit int64
}

func Open(ctx context.Context, dsn string, limit int64) (*Store, error) {
	p, err := pgxpool.New(ctx, dsn)

	if err != nil {
		return nil, err
	}

	if err = p.Ping(ctx); err != nil {
		p.Close()

		return nil, err
	}

	return &Store{
		Pool:  p,
		Limit: limit,
	}, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.Pool.Begin(ctx)

	if err != nil {
		return err
	}

	defer tx.Rollback(ctx)

	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(260626)`); err != nil {
		return err
	}

	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY)`); err != nil {
		return err
	}

	es, err := migrations.ReadDir("migrations")

	if err != nil {
		return err
	}

	for _, e := range es {
		var exists bool

		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, e.Name()).Scan(&exists); err != nil {
			return err
		}

		if exists {
			continue
		}

		b, _ := migrations.ReadFile("migrations/" + e.Name())

		if _, err = tx.Exec(ctx, string(b)); err != nil {
			return err
		}

		if _, err = tx.Exec(ctx, `INSERT INTO schema_migrations VALUES ($1)`, e.Name()); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (s *Store) Seed(ctx context.Context) error {
	if _, err := s.Pool.Exec(ctx, `INSERT INTO accounts VALUES ('demo',10000000) ON CONFLICT DO NOTHING`); err != nil {
		return err
	}

	for _, b := range []domain.AddBeneficiary{{
		Name:          "John",
		BankCode:      "044",
		AccountNumber: "0012345678",
	}, {
		Name:          "Zainab",
		BankCode:      "011",
		AccountNumber: "0098765432",
	}} {
		if _, err := s.Add(ctx, "seed/"+b.Name, b); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) Add(ctx context.Context, key string, b domain.AddBeneficiary) (domain.Beneficiary, error) {
	var out domain.Beneficiary

	if err := domain.ValidateKey(key); err != nil {
		return out, err
	}

	if err := b.Validate(); err != nil {
		return out, err
	}

	b.Name = strings.TrimSpace(b.Name)
	tx, err := s.Pool.Begin(ctx)

	if err != nil {
		return out, err
	}

	defer tx.Rollback(ctx)

	hash := domain.Fingerprint(b)

	if _, err = tx.Exec(ctx, `INSERT INTO beneficiary_operations(operation_key,fingerprint) VALUES($1,$2) ON CONFLICT DO NOTHING`, key, hash); err != nil {
		return out, err
	}

	var existingHash string

	if err = tx.QueryRow(ctx, `SELECT fingerprint FROM beneficiary_operations WHERE operation_key=$1 FOR UPDATE`, key).Scan(&existingHash); err != nil {
		return out, err
	}

	if existingHash != hash {
		return out, domain.Conflict("operation key already belongs to different beneficiary details")
	}

	if _, err = tx.Exec(ctx, `INSERT INTO beneficiaries VALUES($1,'demo',$2,$3,$4) ON CONFLICT(account_id,bank_code,account_number) DO NOTHING`, domain.NewID(), b.Name, b.BankCode, b.AccountNumber); err != nil {
		return out, err
	}

	err = tx.QueryRow(ctx, `SELECT id,name,bank_code,account_number FROM beneficiaries WHERE account_id='demo' AND bank_code=$1 AND account_number=$2`, b.BankCode, b.AccountNumber).Scan(&out.ID, &out.Name, &out.BankCode, &out.AccountNumber)

	if err != nil {
		return out, err
	}

	if !strings.EqualFold(out.Name, b.Name) {
		return out, domain.Conflict("this bank account is already saved with another name")
	}

	if _, err = tx.Exec(ctx, `UPDATE beneficiary_operations SET beneficiary_id=$2 WHERE operation_key=$1`, key, out.ID); err != nil {
		return out, err
	}

	return out, tx.Commit(ctx)
}

func (s *Store) List(ctx context.Context, q string) ([]domain.Beneficiary, error) {
	if len(q) > 80 {
		return nil, domain.Invalid("search query is too long")
	}

	rows, err := s.Pool.Query(ctx, `SELECT id,name,bank_code,account_number FROM beneficiaries WHERE account_id='demo' AND strpos(lower(name),lower($1))>0 ORDER BY lower(name),id LIMIT 50`, q)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	out := []domain.Beneficiary{}

	for rows.Next() {
		var b domain.Beneficiary

		if err := rows.Scan(&b.ID, &b.Name, &b.BankCode, &b.AccountNumber); err != nil {
			return nil, err
		}

		out = append(out, b)
	}

	return out, rows.Err()
}

func (s *Store) Balance(ctx context.Context) (int64, error) {
	var n int64

	err := s.Pool.QueryRow(ctx, `SELECT balance_kobo FROM accounts WHERE id='demo'`).Scan(&n)

	return n, err
}

const transferColumns = `payment_key,reference,beneficiary_id,amount_kobo,currency,status,reason,scenario,created_at`

func scanTransfer(row pgx.Row) (domain.Transfer, error) {
	var t domain.Transfer

	err := row.Scan(&t.Key, &t.Reference, &t.BeneficiaryID, &t.AmountKobo, &t.Currency, &t.Status, &t.Reason, &t.Scenario, &t.CreatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return t, domain.NotFound("transaction not found")
	}

	return t, err
}

// Submit makes the provider itself safe under at-least-once Activity execution.
// Uncertainty after Commit is resolved by the caller querying the original key.
func (s *Store) Submit(ctx context.Context, in domain.SubmitTransfer) (domain.Transfer, bool, error) {
	var zero domain.Transfer

	if err := in.Validate(s.Limit); err != nil {
		return zero, false, err
	}

	tx, err := s.Pool.Begin(ctx)

	if err != nil {
		return zero, false, err
	}

	defer tx.Rollback(ctx)

	var beneficiaryExists bool

	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM beneficiaries WHERE id=$1 AND account_id='demo')`, in.BeneficiaryID).Scan(&beneficiaryExists); err != nil {
		return zero, false, err
	}

	if !beneficiaryExists {
		return zero, false, domain.Invalid("unknown beneficiary_id")
	}

	ready := 0

	if in.Scenario == "delayed" {
		ready = 8
	}

	if _, err = tx.Exec(ctx, `INSERT INTO transfers(payment_key,reference,account_id,beneficiary_id,amount_kobo,currency,status,scenario,ready_at) VALUES($1,$2,'demo',$3,$4,$5,'pending',$6,now()+$7*interval '1 second') ON CONFLICT(payment_key) DO NOTHING`, in.Key, "TX-"+domain.Fingerprint(in.Key)[:12], in.BeneficiaryID, in.AmountKobo, in.Currency, in.Scenario, ready); err != nil {
		return zero, false, err
	}

	t, err := scanTransfer(tx.QueryRow(ctx, `SELECT `+transferColumns+` FROM transfers WHERE payment_key=$1 FOR UPDATE`, in.Key))

	if err != nil {
		return zero, false, err
	}

	if t.BeneficiaryID != in.BeneficiaryID || t.AmountKobo != in.AmountKobo || t.Currency != in.Currency || t.Scenario != in.Scenario {
		return zero, false, domain.Conflict("payment key already belongs to different payment details")
	}

	var attempts int

	if err = tx.QueryRow(ctx, `UPDATE transfers SET attempts=attempts+1 WHERE payment_key=$1 RETURNING attempts`, in.Key).Scan(&attempts); err != nil {
		return zero, false, err
	}

	if t.Status == "pending" && in.Scenario == "unavailable" && attempts <= 2 {
		if err = tx.Commit(ctx); err != nil {
			return zero, false, err
		}

		return zero, false, &domain.Problem{
			Code:    "unavailable",
			Message: "simulated provider unavailability",
		}
	}

	if t.Status == "pending" {
		if err = settle(ctx, tx, &t); err != nil {
			return zero, false, err
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return zero, false, err
	}

	return t, in.Scenario == "lost-response" && attempts == 1, nil
}

func settle(ctx context.Context, tx pgx.Tx, t *domain.Transfer) error {
	var ready bool

	if err := tx.QueryRow(ctx, `SELECT ready_at<=now() FROM transfers WHERE payment_key=$1`, t.Key).Scan(&ready); err != nil {
		return err
	}

	if !ready {
		return nil
	}

	if t.Scenario == "decline" {
		t.Status = "failed"
		t.Reason = "simulated provider decline"
	} else {
		var balance int64

		if err := tx.QueryRow(ctx, `SELECT balance_kobo FROM accounts WHERE id='demo' FOR UPDATE`).Scan(&balance); err != nil {
			return err
		}

		if balance < t.AmountKobo {
			t.Status = "failed"
			t.Reason = "insufficient funds at execution"
		} else {
			if _, err := tx.Exec(ctx, `UPDATE accounts SET balance_kobo=balance_kobo-$1 WHERE id='demo'`, t.AmountKobo); err != nil {
				return err
			}

			if _, err := tx.Exec(ctx, `INSERT INTO debit_journal(payment_key,amount_kobo) VALUES($1,$2)`, t.Key, t.AmountKobo); err != nil {
				return err
			}

			t.Status = "succeeded"
		}
	}

	_, err := tx.Exec(ctx, `UPDATE transfers SET status=$2,reason=$3 WHERE payment_key=$1`, t.Key, t.Status, t.Reason)

	return err
}

func (s *Store) Get(ctx context.Context, key string) (domain.Transfer, error) {
	tx, err := s.Pool.Begin(ctx)

	if err != nil {
		return domain.Transfer{}, err
	}

	defer tx.Rollback(ctx)

	t, err := scanTransfer(tx.QueryRow(ctx, `SELECT `+transferColumns+` FROM transfers WHERE payment_key=$1 OR reference=$1 FOR UPDATE`, key))

	if err != nil {
		return t, err
	}

	// A transient submission failure is not permission for a status read to initiate a payment.
	if t.Status == "pending" && t.Scenario == "delayed" {
		if err = settle(ctx, tx, &t); err != nil {
			return t, err
		}
	}

	return t, tx.Commit(ctx)
}

func (s *Store) Last(ctx context.Context) (domain.Transfer, error) {
	return scanTransfer(s.Pool.QueryRow(ctx, `SELECT `+transferColumns+` FROM transfers WHERE account_id='demo' ORDER BY created_at DESC,payment_key DESC LIMIT 1`))
}

func (s *Store) Evidence(ctx context.Context) (map[string]any, error) {
	var balance, beneficiaries, transfers, debits, receipts, receiptAttempts int64

	if err := s.Pool.QueryRow(ctx, `SELECT (SELECT balance_kobo FROM accounts WHERE id='demo'),(SELECT count(*) FROM beneficiaries),(SELECT count(*) FROM transfers),(SELECT count(*) FROM debit_journal),(SELECT count(*) FROM receipts),(SELECT coalesce(sum(attempts),0) FROM receipt_attempts)`).Scan(&balance, &beneficiaries, &transfers, &debits, &receipts, &receiptAttempts); err != nil {
		return nil, err
	}

	return map[string]any{"balance_kobo": balance, "beneficiaries": beneficiaries, "transfers": transfers, "debits": debits, "receipts": receipts, "receipt_attempts": receiptAttempts}, nil
}

func (s *Store) Receipt(ctx context.Context, key string, failFirst int) error {
	t, err := s.Get(ctx, key)

	if err != nil {
		return err
	}

	if t.Status != "succeeded" {
		return domain.Invalid("only successful payments have receipts")
	}

	var attempt int

	if err := s.Pool.QueryRow(ctx, `INSERT INTO receipt_attempts VALUES($1,1) ON CONFLICT(payment_key) DO UPDATE SET attempts=receipt_attempts.attempts+1 RETURNING attempts`, t.Key).Scan(&attempt); err != nil {
		return err
	}

	if attempt <= failFirst {
		return &domain.Problem{
			Code:    "unavailable",
			Message: "simulated receipt delivery failure; payment remains successful",
		}
	}

	_, err = s.Pool.Exec(ctx, `INSERT INTO receipts VALUES($1,'payment_receipt',$2) ON CONFLICT DO NOTHING`, t.Key, fmt.Sprintf("%s sent: %s", domain.Money(t.AmountKobo), t.Reference))

	return err
}
