#!/usr/bin/env python3
"""Verify the real two-process demo using an isolated PostgreSQL schema.

make demo-check tests the starter; make solution-check compiles the written
one-file solution in a temporary directory; make live-check also uses Gemini.
Requires the existing PostgreSQL and Temporal services, without resetting data.
"""
import argparse
import collections
import json
import os
from pathlib import Path
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.parse
import uuid


def free_address():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return "127.0.0.1:" + str(sock.getsockname()[1])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--live", action="store_true", help="use actual Gemini inference")
    parser.add_argument("--solution", action="store_true", help="compile the supplied exercise solution in isolation")
    options = parser.parse_args()
    mode = "live" if options.live else "scripted"
    if options.live and not (os.environ.get("GEMINI_API_KEY") or os.environ.get("GOOGLE_API_KEY")):
        raise SystemExit("Set GEMINI_API_KEY or GOOGLE_API_KEY in .env first")
    dsn = os.environ["PAYMENT_DATABASE_URL"]
    parsed = urllib.parse.urlsplit(dsn)
    if parsed.scheme not in ("postgres", "postgresql"):
        raise SystemExit("demo verification requires a PostgreSQL connection URL")
    psql = shutil.which("psql")
    if not psql:
        raise SystemExit("Install the PostgreSQL psql client to run demo verification")
    run_id = uuid.uuid4().hex[:12]
    schema = "live_check_" + run_id
    out = Path(".local/verification") / run_id
    out.mkdir(parents=True, mode=0o700)
    audit = out / "model-invocations.jsonl"
    original = os.environ.copy()
    root = Path.cwd()
    source_copy = tempfile.TemporaryDirectory(prefix="devfest-solution-", dir=root / ".local") if options.solution else None
    binary = root / "bin/devfest"
    if source_copy:
        workspace = Path(source_copy.name)
        for name in ("go.mod", "go.sum"):
            shutil.copy2(root / name, workspace / name)
        for name in ("cmd", "internal"):
            shutil.copytree(root / name, workspace / name)
        shutil.copy2(root / "docs/solutions/beneficiary.go.txt", workspace / "internal/agent/beneficiary.go")
        binary = workspace / "devfest"
        subprocess.run(["go", "build", "-o", str(binary), "./cmd/devfest"], cwd=workspace, check=True)
    # libpq doesn't expand a URI supplied through PGDATABASE. Pass a URI with
    # its password removed as --dbname, keeping the password in the environment.
    db_query = urllib.parse.parse_qsl(parsed.query, keep_blank_values=True)
    password = next((v for k, v in db_query if k == "password"), urllib.parse.unquote(parsed.password) if parsed.password is not None else original.get("PGPASSWORD", ""))
    db_host = parsed.hostname or ""
    if ":" in db_host:
        db_host = "[" + db_host + "]"
    if parsed.port:
        db_host += ":" + str(parsed.port)
    if parsed.username:
        db_host = parsed.username + "@" + db_host
    public_dsn = urllib.parse.urlunsplit(parsed._replace(netloc=db_host, query=urllib.parse.urlencode([(k, v) for k, v in db_query if k != "password"])))
    db_env = {**original, "PGPASSWORD": password}

    def schema_sql(sql):
        result = subprocess.run([psql, "--dbname=" + public_dsn, "-X", "-v", "ON_ERROR_STOP=1", "-q", "-c", sql],
                                env=db_env, capture_output=True, text=True, timeout=15)
        if result.returncode:
            raise RuntimeError("temporary PostgreSQL schema operation failed")

    query = [(k, v) for k, v in urllib.parse.parse_qsl(parsed.query, keep_blank_values=True) if k != "search_path"]
    query.append(("search_path", schema))
    scoped_dsn = urllib.parse.urlunsplit(parsed._replace(query=urllib.parse.urlencode(query)))
    env = {**original, "DATABASE_URL": scoped_dsn, "PAYMENT_DATABASE_URL": scoped_dsn,
           "MODEL_MODE": mode, "WORKSHOP_INSTANCE": "participant",
           "TEMPORAL_TASK_QUEUE": "devfest26-check-" + run_id,
           "API_ADDRESS": free_address(), "PAYMENT_ADDRESS": free_address(),
           "MODEL_AUDIT_PATH": str(audit)}
    # demo_client reads these settings at import time.
    os.environ.update(env)
    from demo_client import ask, wait, decide, evidence, request, PAYMENT

    processes = []
    logs = []
    results = []
    histories = []
    workflow_ids = []
    passed = False
    # Some Flash projects have five requests/minute; one agent turn can make
    # several model calls. Leave a full quota window between user requests.
    # Set LIVE_CHECK_PAUSE_SECONDS=0 only with sufficient project quota.
    pause = float(os.environ.get("LIVE_CHECK_PAUSE_SECONDS", "65")) if options.live else 0
    next_request = 0

    def paced_ask(prompt, scenario="success"):
        nonlocal next_request
        delay = next_request - time.monotonic()
        if delay > 0:
            print("Waiting %.0fs for the live model quota window" % delay, flush=True)
            time.sleep(delay)
        rid = ask(prompt, scenario)
        workflow_ids.append(rid)
        return rid

    def paced_wait(rid, **options):
        nonlocal next_request
        state = wait(rid, timeout=120, **options)
        next_request = time.monotonic() + pause
        return state

    def command(*args):
        result = subprocess.run([str(binary), *args], env=env, capture_output=True, timeout=110)
        if result.returncode:
            raise RuntimeError(args[0] + " failed; check configuration/preflight")

    def start(service):
        log = (out / (service + ".log")).open("ab", buffering=0)
        logs.append(log)
        proc = subprocess.Popen([str(binary), service], env=env, stdout=log, stderr=subprocess.STDOUT)
        processes.append(proc)
        return proc

    def check(label, condition):
        if not condition:
            raise AssertionError(label)
        results.append(label)
        print("PASS:", label, flush=True)

    def invoke(prompt):
        rid = paced_ask(prompt)
        state = paced_wait(rid)
        if state["phase"] in ("completed", "error"):
            histories.append(rid)
        check(mode + " response: " + prompt, state["mode"] == mode and state["phase"] != "error")
        return rid, state

    def invocations(rid):
        counts = collections.Counter()
        if audit.exists():
            for line in audit.read_text().splitlines():
                event = json.loads(line)
                if event["workflow_id"] == rid and event["event"] == "model_invocation_started":
                    counts[event["activity_id"]] += 1
        return dict(counts)

    schema_sql('CREATE SCHEMA "' + schema + '"')
    try:
        command("migrate")
        command("seed")
        command("preflight")
        provider = start("payment")
        agent = start("agent")
        deadline = time.monotonic() + 15
        while True:
            try:
                request("GET", "/health")
                request("GET", "/health", base=PAYMENT)
                break
            except (OSError, TimeoutError):
                if time.monotonic() >= deadline or any(p.poll() is not None for p in processes):
                    raise RuntimeError("live-check services failed to start")
                time.sleep(0.2)

        before = evidence()
        _, listing = invoke("List my beneficiaries")
        check("list uses a dynamic tool without approval", "list_beneficiaries" in listing["tools"] and listing["phase"] == "completed")
        _, balance = invoke("What is my balance?")
        check("balance uses a dynamic tool without approval", "check_balance" in balance["tools"] and balance["phase"] == "completed")
        check("reads make no database mutations", evidence() == before)

        if options.solution:
            _, added = invoke("Add Ayomide at GTBank, account 0123454821")
            bs = request("GET", "/beneficiaries?q=Ayomide", base=PAYMENT)
            check("add requires no extra approval and preserves the leading zero",
                  added["phase"] == "completed" and "add_beneficiary" in added["tools"] and len(bs) == 1 and bs[0]["account_number"] == "0123454821")
            before = evidence()
            invalid_id = paced_ask("Add Invalid at GTBank, account 123456789. Use these exact details.")
            invalid = paced_wait(invalid_id)
            histories.append(invalid_id)
            validation_response = invalid["phase"] == "completed" or (invalid["phase"] == "error" and "account_number" in invalid.get("error", ""))
            check("nine-digit account is rejected without mutation", invalid["mode"] == mode and validation_response and invalid.get("proposal") is None and evidence() == before)
        else:
            check("starter reads require no beneficiary exercise completion", "add_beneficiary" not in listing["tools"])

        before = evidence()

        rid, proposal = invoke("Send ₦1 to John")
        check("payment pauses for approval", proposal["phase"] == "awaiting_approval" and evidence() == before)
        decide(rid, proposal, approve=False)
        rejected = wait(rid, terminal=True)
        histories.append(rid)
        check("rejected proposal causes no debit", rejected["phase"] == "rejected" and evidence() == before)

        for scenario, restart in (("success", True), ("lost-response", False)):
            before = evidence()
            rid = paced_ask("Send ₦1 to John", scenario)
            proposal = paced_wait(rid)
            check(scenario + " pauses before any debit", proposal["phase"] == "awaiting_approval" and evidence() == before)
            calls = invocations(rid)
            if restart:
                agent.kill()  # Actual process crash: API and worker stop together.
                agent.wait(timeout=8)
                check("payment service stays healthy while agent is down", provider.poll() is None and request("GET", "/health", base=PAYMENT)["status"] == "ready")
                try:
                    request("GET", "/health")
                except OSError:
                    check("agent API is unavailable during the crash", True)
                else:
                    raise AssertionError("API should stop with the agent worker")
                agent = start("agent")
                recovered = wait(rid, timeout=30)
                check("agent crash preserves proposal and completed model calls", recovered["proposal"] == proposal["proposal"] and invocations(rid) == calls)
            decide(rid, proposal)
            result = wait(rid, terminal=True, timeout=120)
            after = evidence()
            check(scenario + " produces exactly one ₦1 debit", result["phase"] == "succeeded" and after["debits"] == before["debits"] + 1 and after["balance_kobo"] == before["balance_kobo"] - 100)
            check(scenario + " payment execution makes no additional model calls", invocations(rid) == calls)
            histories.extend([rid, rid + "-payment"])

        before = evidence()
        _, last = invoke("Check my last transfer and retry it if it failed")
        check("successful last transfer is not replaced", last["phase"] == "completed" and "get_last_transfer" in last["tools"] and evidence() == before)
        if options.solution and not options.live:
            subprocess.run([sys.executable, "scripts/exercise-check.py"], env=env, check=True)
            check("participant acceptance command passes with the written solution", True)
        for rid in histories:
            command("replay", rid)
        check("all " + str(len(histories)) + " workflow histories replay without model calls", True)
        (out / "result.json").write_text(json.dumps({"model": env.get("GEMINI_MODEL"), "mode": mode, "solution": options.solution, "checks": results, "histories": histories}, indent=2))
        passed = True
        print("Verification saved to", out / "result.json", flush=True)
    finally:
        if not passed and shutil.which("temporal"):
            # Leave completed histories intact; terminate only still-open runs
            # owned by this test so no pending approval outlives its schema.
            for rid in workflow_ids:
                try:
                    subprocess.run(["temporal", "workflow", "terminate", "--workflow-id", rid,
                                    "--reason", "Isolated live-check cleanup", "--address",
                                    env.get("TEMPORAL_ADDRESS", "127.0.0.1:7233")],
                                   capture_output=True, timeout=15)
                except (OSError, subprocess.TimeoutExpired):
                    pass  # Still stop processes and remove the isolated schema.
        for proc in reversed(processes):
            if proc.poll() is None:
                proc.terminate()
        for proc in processes:
            try:
                proc.wait(timeout=8)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait()
        for log in logs:
            log.close()
        schema_sql('DROP SCHEMA "' + schema + '" CASCADE')
        print("Temporary verification schema removed", flush=True)
        if source_copy:
            source_copy.cleanup()


if __name__ == "__main__":
    # Logs stay private because upstream error messages can contain HTTP details.
    os.umask(0o077)
    # Give the cleanup block a chance to stop children and drop its schema when
    # a terminal or automation cancels the run.
    def interrupted(signum, frame):
        raise SystemExit("Live check interrupted")
    signal.signal(signal.SIGTERM, interrupted)
    main()
