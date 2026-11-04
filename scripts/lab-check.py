#!/usr/bin/env python3
"""Acceptance checks for independent take-home labs (use solution/starter branch)."""
from datetime import datetime, timedelta, timezone
import os
from pathlib import Path
import signal
import subprocess
import sys
import time
from demo_client import INSTANCE, ask, wait, decide, evidence

lab = sys.argv[1] if len(sys.argv) > 1 else "notifications"
saved = wait(ask("Add Ayomide at GTBank, account 0123454821"))
assert saved["phase"] == "completed", saved
before = evidence()
if lab == "notifications":
    rid = ask("Send ₦100 to Ayomide")
    pending = wait(rid)
    assert pending["phase"] == "awaiting_approval", pending
    decide(rid, pending)
    result = wait(rid, terminal=True)
    after = evidence()
    assert result["phase"] == "succeeded", result
    assert after["debits"] == before["debits"] + 1
    assert after["receipts"] == before["receipts"] + 1, "implement the notification Activity call"
    assert after["receipt_attempts"] == before["receipt_attempts"] + 3
    print("PASS: three delivery attempts; one receipt; one payment debit")
elif lab == "scheduling":
    due = datetime.now(timezone.utc) + timedelta(seconds=18)
    rid = ask("Schedule ₦100 to Ayomide at " + due.isoformat(timespec="seconds"))
    pending = wait(rid)
    assert pending["phase"] == "awaiting_approval" and pending["proposal"].get("execute_at"), pending
    decide(rid, pending)
    scheduled = wait(rid)
    assert scheduled["phase"] == "scheduled", scheduled
    assert evidence()["debits"] == before["debits"], "early debit"
    pid_file = Path(f".local/{INSTANCE}/worker.pid")
    pid = int(pid_file.read_text())
    command = Path(f"/proc/{pid}/cmdline").read_bytes()
    assert b"devfest" in command and b"worker" in command
    os.kill(pid, signal.SIGTERM)
    time.sleep(2)
    log = open(f".local/{INSTANCE}/worker-lab.log", "ab", buffering=0)
    proc = subprocess.Popen(["./bin/devfest", "worker"], stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    pid_file.write_text(str(proc.pid))
    result = wait(rid, terminal=True)
    assert datetime.now(timezone.utc) >= due - timedelta(seconds=1)
    assert result["phase"] == "succeeded", result
    assert evidence()["debits"] == before["debits"] + 1
    print("PASS: no early debit; durable scheduled wait survives restart; one eventual debit")
else:
    raise SystemExit("choose notifications or scheduling")
