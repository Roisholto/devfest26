# Checkpoint and practice branches

`main` is the complete core reference. The checkpoint history is linear, with a separate commit and branch for each advancement. Common infrastructure is supplied in the first checkpoint; stages enable progressively more agent capabilities. All checkpoints compile.

| Branch | Working capability / next action |
|---|---|
| `workshop/00-ready` | Services, schema, seeds and preflight; runtime-ready response |
| `workshop/01-read-tools` | ADK, native dynamic listing/search/lookup/balance |
| `workshop/02-beneficiary-exercise` | Complete backend; implement beneficiary tool/catalog |
| `workshop/03-beneficiary-solution` | Complete addition and validation examples |
| `workshop/04-approved-payments` | Proposal, approval, payment and status tools |
| `workshop/05-recovery` | Failure scenarios and reconciliation |
| `main` | Final reference, slides and full guides |

Inspect each advancement with `git diff workshop/01-read-tools..workshop/02-beneficiary-exercise` (substitute consecutive names). The `internal/agent/checkpoint.go` constants are teaching checkpoints, never model-controlled policy. Tool names are dynamically dispatched at every enabled stage. Internal payment/model Activities remain separate.

Start personal work without changing the supplied branch:

```bash
git switch -c my-beneficiary workshop/02-beneficiary-exercise
unset WORKSHOP_CHECKPOINT
source scripts/env.sh
make build
```

Stop services and finish active workflows before changing code checkpoints. Re-source a new checkpoint prefix in every CLI/service shell. Prefixes isolate task queues and workflow IDs; presenter/participant databases and ports isolate data. Checkpoint switches retain that instance's saved beneficiaries, so the newly added Ayomide can be used in a later payment checkpoint. The initial presenter/participant defaults should be set from fresh shells to avoid retained DSN/address overrides.

Commit unfinished work before switching to a new personal branch from a solution. Do not use destructive reset/clean commands.

Independent extensions from main:

- `practice/01-notifications-start` → `practice/01-notifications-solution`
- `practice/02-scheduled-transfers-start` → `practice/02-scheduled-transfers-solution`

Each lab's acceptance check intentionally fails until its missing feature is implemented. They do not depend on one another and do not add content to the live hour.
