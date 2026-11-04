#!/usr/bin/env python3
"""Acceptance checks for the participant's add_beneficiary implementation."""
import urllib.error
from demo_client import ask, wait, request, evidence, PAYMENT

before = evidence()
saved = wait(ask("Add Ayomide at GTBank, account 0123454821"))
assert saved["phase"] == "completed" and "add_beneficiary" in saved["tools"], saved
again = wait(ask("Add Ayomide at GTBank, account 0123454821"))
assert again["phase"] == "completed", again
matches = request("GET", "/beneficiaries?q=ayOM", base=PAYMENT)
assert len(matches) == 1 and matches[0]["account_number"] == "0123454821", matches
after_valid = evidence()
assert after_valid["beneficiaries"] - before["beneficiaries"] in (0, 1)

bad = wait(ask("Add Invalid at GTBank, account 123454821"))
assert bad["phase"] == "error" and "add_beneficiary" not in bad["tools"], bad
try:
    request("POST", "/beneficiaries", {"name": "Invalid", "bank_code": "058", "account_number": 1234548210}, "invalid-number", base=PAYMENT)
    raise AssertionError("numeric account number accepted")
except urllib.error.HTTPError as error:
    assert error.code == 400, error.code
assert evidence() == after_valid, "invalid operation caused a mutation"
print("PASS: dynamic tool works; no approval; search works; leading zero preserved; repeated addition is idempotent; invalid tool/direct-service requests do not mutate data")
