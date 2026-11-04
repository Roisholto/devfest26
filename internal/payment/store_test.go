package payment

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"devfest26/internal/domain"
	"devfest26/internal/httpx"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL for real PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_" + domain.NewID()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := &Store{Pool: p, Limit: domain.DefaultLimitKobo}
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.Seed(ctx, true); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		p.Close()
		_, _ = admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
	})
	return s
}
func TestConcurrentBeneficiaryCreationAndConflict(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	in := domain.AddBeneficiary{Name: "Tolu", BankCode: "058", AccountNumber: "0123456789"}
	var wg sync.WaitGroup
	ids := make(chan string, 20)
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, err := s.Add(ctx, fmt.Sprintf("op-%d", i), in)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- b.ID
		}()
	}
	wg.Wait()
	close(ids)
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("duplicate beneficiary")
		}
	}
	bad := in
	bad.Name = "Other"
	if _, err := s.Add(ctx, "op-0", bad); err == nil {
		t.Fatal("changed-key payload accepted")
	}
	bs, _ := s.List(ctx, "tOL")
	if len(bs) != 1 {
		t.Fatalf("search returned %v", bs)
	}
}
func TestConcurrentPaymentIsOneDebit(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	bs, _ := s.List(ctx, "Ayomide")
	in := domain.SubmitTransfer{Key: "one-payment", BeneficiaryID: bs[0].ID, AmountKobo: 1000000, Currency: "NGN", Scenario: "success"}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, _, err := s.Submit(ctx, in)
			if err != nil || got.Status != "succeeded" {
				t.Errorf("submit: %v %v", got, err)
			}
		}()
	}
	wg.Wait()
	e, _ := s.Evidence(ctx)
	if e["debits"] != int64(1) || e["balance_kobo"] != int64(9000000) {
		t.Fatalf("evidence: %v", e)
	}
	in.AmountKobo++
	if _, _, err := s.Submit(ctx, in); err == nil {
		t.Fatal("changed payment accepted")
	}
}
func TestConcurrentWithdrawalsCannotOverspend(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	bs, _ := s.List(ctx, "Ayomide")
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := s.Submit(ctx, domain.SubmitTransfer{Key: fmt.Sprintf("withdraw-%d", i), BeneficiaryID: bs[0].ID, AmountKobo: 7000000, Currency: "NGN", Scenario: "success"})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	e, _ := s.Evidence(ctx)
	if e["debits"] != int64(1) || e["balance_kobo"] != int64(3000000) {
		t.Fatalf("overspend: %v", e)
	}
}
func TestLostResponseAndDirectHTTPValidation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	server := httptest.NewServer(Handler(s))
	defer server.Close()
	c := httpx.NewClient(server.URL)
	before, _ := s.Evidence(ctx)
	for _, payload := range []string{`{"name":"Bad","bank_code":"058","account_number":1234567890}`, `{"name":"Bad","bank_code":"058","account_number":"123456789"}`, `{"name":"Bad","bank_code":"999","account_number":"0123454821"}`, `{"name":"Bad","bank_code":"058","account_number":"0123454821","approved":true}`} {
		r := httptest.NewRequest("POST", "/beneficiaries", strings.NewReader(payload))
		r.Header.Set("Idempotency-Key", "bad")
		w := httptest.NewRecorder()
		Handler(s).ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("accepted invalid payload (%d): %s", w.Code, payload)
		}
	}
	after, _ := s.Evidence(ctx)
	if before["beneficiaries"] != after["beneficiaries"] {
		t.Fatal("invalid operation mutated data")
	}
	bs, _ := s.List(ctx, "Ayomide")
	in := domain.SubmitTransfer{Key: "lost", BeneficiaryID: bs[0].ID, AmountKobo: 1000000, Currency: "NGN", Scenario: "lost-response"}
	var out domain.Transfer
	if err := c.Do(ctx, "POST", "/transfers", "", in, &out); err == nil {
		t.Fatal("response was not lost")
	}
	if err := c.Do(ctx, "GET", "/transfers/lost", "", nil, &out); err != nil || out.Status != "succeeded" {
		t.Fatalf("reconcile: %v %v", out, err)
	}
	if err := c.Do(ctx, "POST", "/transfers", "", in, &out); err != nil {
		t.Fatal(err)
	}
	e, _ := s.Evidence(ctx)
	if e["debits"] != int64(1) {
		t.Fatalf("duplicate debit: %v", e)
	}
}
func TestDatabaseConstraintAndRollback(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.Pool.Exec(ctx, `INSERT INTO beneficiaries VALUES('bad','demo','Bad','058','123')`); err == nil {
		t.Fatal("database accepted bad account")
	}
	tx, _ := s.Pool.Begin(ctx)
	_, _ = tx.Exec(ctx, `UPDATE accounts SET balance_kobo=0`)
	_ = tx.Rollback(ctx)
	n, _ := s.Balance(ctx)
	if n != 10000000 {
		t.Fatal("rollback changed balance")
	}
}
