package payment

import (
	"context"
	"fmt"
	"net/http"
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

	s := &Store{
		Pool:  p,
		Limit: domain.DefaultLimitKobo,
	}

	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	if err = s.Seed(ctx); err != nil {
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
	in := domain.AddBeneficiaryRequest{
		Name:          "Tolu",
		BankCode:      "058",
		AccountNumber: "0123456789",
	}

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
	bs, _ := s.List(ctx, "John")
	in := domain.SubmitTransferRequest{
		Key:           "one-payment",
		BeneficiaryID: bs[0].ID,
		AmountKobo:    1000000,
		Currency:      "NGN",
		Scenario:      "success",
	}

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
	bs, _ := s.List(ctx, "John")

	var wg sync.WaitGroup

	for i := range 2 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_, _, err := s.Submit(ctx, domain.SubmitTransferRequest{
				Key:           fmt.Sprintf("withdraw-%d", i),
				BeneficiaryID: bs[0].ID,
				AmountKobo:    7000000,
				Currency:      "NGN",
				Scenario:      "success",
			})

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

	bs, _ := s.List(ctx, "John")
	in := domain.SubmitTransferRequest{
		Key:           "lost",
		BeneficiaryID: bs[0].ID,
		AmountKobo:    1000000,
		Currency:      "NGN",
		Scenario:      "lost-response",
	}

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

func TestBeneficiaryLookupByIDBeyondListLimit(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	for i := range 50 {
		_, err := s.Add(ctx, fmt.Sprintf("saved-%d", i), domain.AddBeneficiaryRequest{
			Name:          fmt.Sprintf("Saved %02d", i),
			BankCode:      "058",
			AccountNumber: fmt.Sprintf("%010d", i+1),
		})

		if err != nil {
			t.Fatal(err)
		}
	}

	target, err := s.Add(ctx, "last-recipient", domain.AddBeneficiaryRequest{
		Name:          "Zzz Recipient",
		BankCode:      "058",
		AccountNumber: "0123454821",
	})

	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(Handler(s))

	defer server.Close()

	client := httpx.NewClient(server.URL)

	var listed []domain.Beneficiary

	if err := client.Do(ctx, "GET", "/beneficiaries", "", nil, &listed); err != nil {
		t.Fatal(err)
	}

	if len(listed) != 50 {
		t.Fatalf("expected the capped list, got %d entries", len(listed))
	}

	for _, beneficiary := range listed {
		if beneficiary.ID == target.ID {
			t.Fatal("test recipient must be outside the first page")
		}
	}

	var found domain.Beneficiary

	if err := client.Do(ctx, "GET", "/beneficiaries/"+target.ID, "", nil, &found); err != nil {
		t.Fatal(err)
	}

	if found != target {
		t.Fatalf("ID lookup changed recipient details: got %+v, want %+v", found, target)
	}
}

func TestProviderLookupsStayWithinDemoAccount(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if _, err := s.Pool.Exec(ctx, `INSERT INTO accounts(id,balance_kobo) VALUES('other',1000000)`); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Pool.Exec(ctx, `INSERT INTO beneficiaries(id,account_id,name,bank_code,account_number) VALUES('other-recipient','other','Other','058','0123456789')`); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Pool.Exec(ctx, `INSERT INTO transfers(payment_key,reference,account_id,beneficiary_id,amount_kobo,currency,status,scenario) VALUES('other-payment','TX-other','other','other-recipient',100,'NGN','failed','decline')`); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(Handler(s))

	defer server.Close()

	client := httpx.NewClient(server.URL)

	for _, path := range []string{"/beneficiaries/missing", "/beneficiaries/other-recipient", "/transfers/other-payment", "/transfers/TX-other"} {
		response, err := client.HTTP.Get(server.URL + path)

		if err != nil {
			t.Fatal(err)
		}

		response.Body.Close()

		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("%s returned HTTP %d instead of 404", path, response.StatusCode)
		}
	}
}

func TestReceiptRetryCountValidation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	before, err := s.Evidence(ctx)

	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(Handler(s))

	defer server.Close()

	client := httpx.NewClient(server.URL)

	for _, value := range []string{"garbage", "1.5", "-1", "3"} {
		response, err := client.HTTP.Post(server.URL+"/receipts/missing?fail_first="+value, "application/json", nil)

		if err != nil {
			t.Fatal(err)
		}

		response.Body.Close()

		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("fail_first=%s returned HTTP %d instead of 400", value, response.StatusCode)
		}
	}

	after, err := s.Evidence(ctx)

	if err != nil {
		t.Fatal(err)
	}

	for key, value := range before {
		if after[key] != value {
			t.Fatalf("invalid retry count changed %s: before=%v after=%v", key, value, after[key])
		}
	}
}
