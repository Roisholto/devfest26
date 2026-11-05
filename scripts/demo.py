#!/usr/bin/env python3
"""Record real local demo results, including a real agent crash.

Requires make start in another terminal, using the same sourced environment.
It adds simulated transactions; no database reset is performed.
"""
import collections
import html
import json
import os
from pathlib import Path
import signal
import subprocess
import time
from demo_client import INSTANCE, ask, wait, decide, evidence, request

out = Path("dist/fallback") / INSTANCE
out.mkdir(parents=True, exist_ok=True)
events = []
started = time.monotonic()

def record(label, value):
    print(label, json.dumps(value, ensure_ascii=False), flush=True)
    events.append([round(time.monotonic() - started, 3), "o", label + "\r\n" + json.dumps(value, indent=2, ensure_ascii=False).replace("\n", "\r\n") + "\r\n"])
    return value

def save(name, value):
    (out / (name + ".json")).write_text(json.dumps(value, indent=2, ensure_ascii=False))

def invocation_counts(workflow_id):
    path = Path(os.environ.get("MODEL_AUDIT_PATH", f".local/{INSTANCE}/model-invocations.jsonl"))
    counts = collections.Counter()
    for line in path.read_text().splitlines():
        event = json.loads(line)
        if event["workflow_id"] == workflow_id and event["event"] == "model_invocation_started":
            counts[event["activity_id"]] += 1
    return dict(counts)

record("MODE", request("GET", "/health"))

def payment(scenario, restart=False):
    before = evidence()
    rid = ask("Send ₦10,000 to John", scenario)
    pending = wait(rid)
    assert pending["phase"] == "awaiting_approval", pending
    record("PROPOSAL " + rid, pending)
    counts_before = invocation_counts(rid)
    if restart:
        pid_file = Path(f".local/{INSTANCE}/agent.pid")
        pid = int(pid_file.read_text())
        command = Path(f"/proc/{pid}/cmdline").read_bytes()
        assert b"devfest" in command and b"agent" in command, "refuse to stop an unrelated process"
        os.kill(pid, signal.SIGKILL)
        time.sleep(2)
        log = open(f".local/{INSTANCE}/agent-restarted.log", "ab", buffering=0)
        agent = subprocess.Popen(["./bin/devfest", "agent"], stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        pid_file.write_text(str(agent.pid))
        recovered = wait(rid)
        assert recovered["proposal"] == pending["proposal"]
        assert invocation_counts(rid) == counts_before, "completed model calls were invoked again"
        record("RESTART EVIDENCE", {"workflow_id": rid, "proposal_unchanged": True, "model_invocations_before": counts_before, "model_invocations_after": invocation_counts(rid)})
    record("APPROVAL", decide(rid, pending))
    result = wait(rid, terminal=True)
    assert result["phase"] == "succeeded", result
    after = evidence()
    assert after["debits"] == before["debits"] + 1
    assert after["balance_kobo"] == before["balance_kobo"] - 1000000
    bundle = {"recorded_demo": True, "mode": result["mode"], "request_id": rid, "proposal": pending, "result": result, "evidence_before": before, "evidence_after": after}
    record("RESULT " + scenario, bundle)
    return bundle

save("opening", payment("success"))
save("restart", payment("success", restart=True))
save("lost-response", payment("lost-response"))
save("last-transfer", wait(ask("Check my last transfer and retry it if it failed")))

with (out / "recording.cast").open("w") as f:
    f.write(json.dumps({"version": 2, "width": 110, "height": 36, "title": "Recorded DevFest payment demo — " + INSTANCE}) + "\n")
    for event in events:
        f.write(json.dumps(event, ensure_ascii=False) + "\n")

cards = []
for name in ("opening", "restart", "lost-response"):
    data = json.loads((out / (name + ".json")).read_text())
    summary = {"mode": data["mode"], "request_id": data["request_id"], "payment_key": data["result"]["transfer"]["payment_key"], "reference": data["result"]["transfer"]["reference"], "status": data["result"]["phase"], "debits_before": data["evidence_before"]["debits"], "debits_after": data["evidence_after"]["debits"], "balance_before": data["evidence_before"]["balance_kobo"], "balance_after": data["evidence_after"]["balance_kobo"]}
    cards.append(f'<section id="{name}"><h1>{name.replace("-", " ").title()}</h1><p>Recorded local run · {html.escape(INSTANCE)} · {html.escape(data["mode"])} mode</p><pre>{html.escape(json.dumps(summary, indent=2, ensure_ascii=False))}</pre><p>Full JSON and terminal recording are included alongside this page.</p></section>')
page = '<!doctype html><meta charset="utf-8"><title>Recorded DevFest demo</title><style>body{margin:0;background:#f0f0f0;color:#202124;font:24px Arial}section{min-height:680px;box-sizing:border-box;padding:45px 65px;border-bottom:2px solid #222}h1{margin:0 0 15px;font-size:45px}pre{font:19px monospace;background:#ffe8a1;border:2px solid #222;border-radius:25px;padding:25px;white-space:pre-wrap}p{font-size:21px}</style>' + "".join(cards)
(out / "index.html").write_text(page)
print("PASS: opening, agent crash, one-debit lost-response reconciliation, and safe last-transfer lookup. Recorded artifacts:", out)
