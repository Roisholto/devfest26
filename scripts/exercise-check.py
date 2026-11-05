#!/usr/bin/env python3
"""Check the participant's completed tool through the real running agent/provider."""
import urllib.error
import uuid
from demo_client import ask, wait, request, evidence, PAYMENT

# A fresh account forces the handler to run even after earlier practice runs.
name = "Practice" + uuid.uuid4().hex[:8]
account = "0" + str(uuid.uuid4().int % 1_000_000_000).zfill(9)
prompt = f"Add {name} at GTBank, account {account}"
before = evidence()
saved = wait(ask(prompt))
assert saved["phase"] == "completed" and "add_beneficiary" in saved["tools"], "Complete both TODOs in internal/agent/beneficiary.go first"
again = wait(ask(prompt))
assert again["phase"] == "completed", again
matches = request("GET", "/beneficiaries?q=" + name, base=PAYMENT)
assert len(matches) == 1 and matches[0]["account_number"] == account, matches
after_valid = evidence()
assert after_valid["beneficiaries"] == before["beneficiaries"] + 1

bad = wait(ask("Add Invalid at GTBank, account 123454821"))
assert bad["phase"] == "completed" or (bad["phase"] == "error" and "account_number" in bad.get("error", "")), bad
try:
    request("POST", "/beneficiaries", {"name": "Invalid", "bank_code": "058", "account_number": 1234548210}, "invalid-number", base=PAYMENT)
    raise AssertionError("numeric account number accepted")
except urllib.error.HTTPError as error:
    assert error.code == 400, error.code
assert evidence() == after_valid, "invalid operation caused a mutation"
print("PASS: dynamic addition; no extra approval; search; leading zero; idempotent duplicate; invalid model/direct-service inputs cause no mutation")
