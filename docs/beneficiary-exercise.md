# Ten-minute exercise: add Ayomide

Start on `workshop/02-beneficiary-exercise`. Listing, case-insensitive beneficiary search, the native dynamic dispatcher, service validation, PostgreSQL persistence and the typed HTTP client are already supplied.

```bash
git switch -c my-beneficiary workshop/02-beneficiary-exercise
source scripts/env.sh
make build
make preflight
make start
```

In a second terminal source the same environment. Before coding, `List my beneficiaries` and `Search beneficiaries for Ayo` work; Ayomide is absent in a fresh participant database. `add_beneficiary` is the missing tool. Preserve any existing participant data; the exercise also works if Ayomide was added on an earlier run.

## Minutes 0–3: implement the handler

Edit `internal/agent/beneficiary.go`. The supplied `AddBeneficiary` argument type has `Name`, `BankCode`, and **string** `AccountNumber`. Validate before calling the service:

```go
func (r *Router) AddBeneficiary(
    ctx context.Context, key string, in domain.AddBeneficiary,
) (map[string]any, error) {
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

The key comes from workflow/Activity identity and is stable across Activity retries. The payment service validates again and the database enforces uniqueness. Your handler never asks for an additional yes/no confirmation.

## Minutes 3–5: publish the tool

In `Catalog()` in `internal/agent/tools.go`, add this entry to the initial ordered `[]Spec`:

```go
{
    Name: "add_beneficiary",
    Description: "Save the complete beneficiary details supplied by the user; no extra confirmation required.",
    Fields: []string{"name", "bank_code", "account_number"},
    Required: []string{"name", "bank_code", "account_number"},
},
```

The dispatcher already has the typed `add_beneficiary` case. Do **not** add `RegisterActivity` or register a function under that name. `RegisterDynamicActivity(router.Execute, ...)` is already the single worker registration for model tools. Adding to the catalog advertises the declaration to Gemini and admits that name through the allowlist.

**Halfway hint:** the model needs the catalog entry to select the tool; the worker needs the handler to carry out the request. Both are necessary. Neither replaces payment-service validation.

## Minutes 5–8: build and try it

Stop the running services with Ctrl-C, then:

```bash
gofmt -w internal/agent/beneficiary.go internal/agent/tools.go
make build
make start
```

In the CLI terminal:

```bash
./bin/pay ask --key add-ayo-1 'Add Ayomide at GTBank, account 0123454821'
./bin/pay wait REQUEST_ID
./bin/pay ask --key search-ayo-1 'Search beneficiaries for Ayo'
./bin/pay wait REQUEST_ID
make exercise-check
```

Expected: phase `completed`, `tools` contains `add_beneficiary`, no pending proposal/approval, a saved beneficiary with the leading zero preserved, and one dynamic Activity named `add_beneficiary` in Temporal's UI. The acceptance script repeats the addition, checks case-insensitive search and tests invalid inputs.

## Minutes 8–10: inspect the boundaries

Valid account number: `"0123454821"`. Nine digits, whitespace, Unicode digits and numeric JSON values are invalid. Never pad or truncate an account number. Bank codes: `058` GTBank, `044` Access, `011` FirstBank. These are simulated details; formatting is not bank-account verification.

Call the payment service directly to prove it enforces the rule independently:

```bash
curl -i "http://$PAYMENT_ADDRESS/beneficiaries" \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: invalid-number' \
  -d '{"name":"Invalid","bank_code":"058","account_number":1234548210}'
```

Expected HTTP 400 and unchanged evidence. Adding the same bank/account with different beneficiary details returns HTTP 409 rather than overwriting it. Identical additions return the saved record. An invalid operation has no mutation; rejecting a later payment does not undo a previously successful addition.

## Completion and recovery

- Tool appears in the catalog.
- A valid instruction saves one beneficiary.
- Invalid account numbers fail.
- Repeating the addition creates no duplicate.
- Temporal shows the native dynamic tool Activity.

Full code is in `workshop/03-beneficiary-solution`; the snippets above are also the solution. If behind, save your work first:

```bash
git add internal/agent/beneficiary.go internal/agent/tools.go
git commit -m 'Save beneficiary practice progress'
# Stop services, then use a new branch rather than discarding changes:
git switch -c beneficiary-reference workshop/03-beneficiary-solution
```

Unset/re-source `WORKSHOP_CHECKPOINT` after switching, rebuild and restart. Continue to `workshop/04-approved-payments` or the completed `main` reference to send a payment to the beneficiary you added. The presenter uses an independent reference environment; you do not need to finish every checkpoint during the hour.
