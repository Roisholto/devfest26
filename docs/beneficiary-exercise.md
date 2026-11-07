# Ten-minute exercise: one new tool

Stay on `main`. Listing, search, balance, approved payments and recovery already work. John and Zainab are seeded, so the core demo does not depend on finishing this exercise.

Make two small edits: add the tool declaration to **`Catalog()` in `internal/agent/tools.go`**, then implement **`AddBeneficiary` in `internal/agent/beneficiary.go`**. No branch switching, checkpoint settings, worker registration or database work is required.

## Minutes 0–3: advertise the tool

In `internal/agent/tools.go`, append this `ToolSpec` entry to the slice returned by `Catalog()`, after the supplied tools:

```go
{
    Name:        "add_beneficiary",
    Description: "Save beneficiary details supplied by the user; no extra confirmation required.",
    Fields:      []string{"name", "bank_code", "account_number"},
    Required:    []string{"name", "bank_code", "account_number"},
},
```

`Catalog()` is the single ordered list of model-visible tools. The supplied Dynamic Activity dispatcher already decodes the request and calls `AddBeneficiary`. A catalog entry advertises the function to Gemini and allows the dispatcher to accept it; it does not register a static Activity. Finish existing requests before changing the catalog.

## Minutes 3–5: implement the handler

In `internal/agent/beneficiary.go`, replace the placeholder method:

```go
func (r *ToolDispatcher) AddBeneficiary(ctx context.Context, key string, in domain.AddBeneficiaryRequest) (map[string]any, error) {
    if err := in.Validate(); err != nil {
        return nil, err
    }

    var out domain.Beneficiary

    if err := r.Client.Do(ctx, "POST", "/beneficiaries", key, in, &out); err != nil {
        return nil, err
    }

    return toMap(out), nil
}
```

The stable key comes from workflow/Activity identity. The service validates again and PostgreSQL enforces uniqueness. The user's add instruction authorizes saving complete details; this tool does not require another approval prompt.

**Halfway hint:** `Catalog()` supplies the declaration; `AddBeneficiary` supplies the implementation. The dispatcher already connects the name to the method.

## Minutes 5–8: run it

```bash
gofmt -w internal/agent/tools.go internal/agent/beneficiary.go
# Press Ctrl-C in the running worker terminal, then:
make worker
```

Use another terminal for CLI commands:

```bash
go run ./cmd/pay ask --key add-ayo-1 'Add Ayomide at GTBank, account 0123454821'
go run ./cmd/pay ask 'Search beneficiaries for Ayo'
source scripts/env.sh && python3 scripts/exercise-check.py
```

`ask` returns the answer directly; use the returned `resume_command` only if its wait times out. Expect phase `completed`, a dynamic Activity named `add_beneficiary`, one saved beneficiary and no payment approval. The acceptance check creates a unique simulated practice beneficiary so it can verify the handler even after earlier runs. Repeating the same bank/account returns the existing beneficiary; conflicting details are rejected.

## Minutes 8–10: inspect validation and make a proposal

Accounts are strings of **exactly ten ASCII digits**, including the leading zero. Nine/eleven digits, whitespace, Unicode digits and numeric JSON are invalid. Never pad or truncate an account number. Bank codes supplied for the demo are GTBank `058`, Access `044` and FirstBank `011`; format validation does not verify a real bank account.

A direct service request is validated independently:

```bash
curl -i http://127.0.0.1:8091/beneficiaries \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: invalid-number' \
  -d '{"name":"Invalid","bank_code":"058","account_number":1234548210}'
```

Expect HTTP 400 with no mutation. Then ask `Send ₦100 to Ayomide`: preparing a proposal still cannot move money. Approve or reject through `go run ./cmd/pay` to see the separate payment boundary.

## Complete solution

The [complete Catalog function](solutions/catalog.go.txt) and [beneficiary handler file](solutions/beneficiary.go.txt) are the reference code. To use them, save your progress first:

```bash
cp internal/agent/tools.go .local/tools-progress.go.txt
cp internal/agent/beneficiary.go .local/beneficiary-progress.go.txt
```

Replace **only `Catalog()`** in `internal/agent/tools.go` with the function in `docs/solutions/catalog.go.txt`. Copy the complete handler file, then restart the worker:

```bash
cp docs/solutions/beneficiary.go.txt internal/agent/beneficiary.go
gofmt -w internal/agent/tools.go internal/agent/beneficiary.go
# Press Ctrl-C in the running worker terminal, then:
make worker
source scripts/env.sh && python3 scripts/exercise-check.py
```

`make check` compiles these exact references in a temporary copy and verifies them using real services without editing your working files. If you finish early, explore the written notification and scheduling practices.
