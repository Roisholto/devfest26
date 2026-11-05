# Ten-minute exercise: one file, one new tool

Stay on `main`. Listing, search, balance, approved payments and recovery already work. John and Zainab are seeded, so the core demo does not depend on finishing this exercise.

Edit **only `internal/agent/beneficiary.go`**. It contains two TODOs: the tool declaration and the typed handler. No branch switching, checkpoint settings, worker registration or database work is required.

## Minutes 0–3: advertise the tool

Replace the empty `beneficiaryTools` slice with:

```go
var beneficiaryTools = []Spec{
    {
        Name:        "add_beneficiary",
        Description: "Save beneficiary details supplied by the user; no extra confirmation required.",
        Fields:      []string{"name", "bank_code", "account_number"},
        Required:    []string{"name", "bank_code", "account_number"},
    },
}
```

The supplied `Catalog()` appends this slice in a fixed order. The native Dynamic Activity dispatcher already routes the allowed name to `AddBeneficiary`. A catalog entry advertises the function to Gemini and allows the dispatcher to accept it; it does not register a static Activity.

## Minutes 3–5: implement the handler

Replace the placeholder method in the same file:

```go
func (r *Router) AddBeneficiary(ctx context.Context, key string, in domain.AddBeneficiary) (map[string]any, error) {
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

**Halfway hint:** both TODOs are in this one file. The model needs the declaration; the dispatcher needs the implementation.

## Minutes 5–8: run it

```bash
gofmt -w internal/agent/beneficiary.go
make restart-agent
```

Use another terminal for CLI commands:

```bash
./bin/pay ask --key add-ayo-1 'Add Ayomide at GTBank, account 0123454821'
./bin/pay wait REQUEST_ID
./bin/pay ask 'Search beneficiaries for Ayo'
./bin/pay wait REQUEST_ID
make exercise-check
```

Expect phase `completed`, a dynamic Activity named `add_beneficiary`, one saved beneficiary and no payment approval. The acceptance check creates a unique simulated practice beneficiary so it can verify the handler even after earlier runs. Repeating the same bank/account returns the existing beneficiary; conflicting details are rejected.

## Minutes 8–10: inspect validation and make a proposal

Accounts are strings of **exactly ten ASCII digits**, including the leading zero. Nine/eleven digits, whitespace, Unicode digits and numeric JSON are invalid. Never pad or truncate an account number. Bank codes supplied for the demo are GTBank `058`, Access `044` and FirstBank `011`; format validation does not verify a real bank account.

A direct service request is validated independently:

```bash
curl -i http://127.0.0.1:8091/beneficiaries \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: invalid-number' \
  -d '{"name":"Invalid","bank_code":"058","account_number":1234548210}'
```

Expect HTTP 400 with no mutation. Then ask `Send ₦100 to Ayomide`: preparing a proposal still cannot move money. Approve or reject through `bin/pay` to see the separate payment boundary.

## Complete solution

The [complete replacement file](solutions/beneficiary.go.txt) is the reference. To use it, save your progress first:

```bash
cp internal/agent/beneficiary.go .local/beneficiary-progress.go.txt
cp docs/solutions/beneficiary.go.txt internal/agent/beneficiary.go
make restart-agent
make exercise-check
```

`make solution-check` compiles this exact reference in a temporary copy and verifies it using real services without editing your working file. The presenter prepares a completed copy before the event. Participants who finish early can explore the written notification and scheduling practices.
