#!/usr/bin/env python3
"""Verify the real three-process demo using an isolated PostgreSQL schema.

Run make check to compile the written beneficiary solution in a temporary
copy and exercise actual Gemini, Temporal and PostgreSQL boundaries.
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
    parser.add_argument("--solution", action="store_true", help="compile the supplied exercise solution in isolation")
    options = parser.parse_args()
    if not os.environ.get("GEMINI_API_KEY"):
        raise SystemExit("Set GEMINI_API_KEY in .env first")
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
    audit = (out / "model-invocations.jsonl").resolve()
    original = os.environ.copy()
    root = Path.cwd()
    source_copy = tempfile.TemporaryDirectory(prefix="devfest-solution-", dir=root / ".local") if options.solution else None
    workspace = root
    if source_copy:
        workspace = Path(source_copy.name)
        for name in ("go.mod", "go.sum"):
            shutil.copy2(root / name, workspace / name)
        for name in ("cmd", "internal"):
            shutil.copytree(root / name, workspace / name)
        shutil.copy2(root / "docs/solutions/beneficiary.go.txt", workspace / "internal/agent/beneficiary.go")
        # Run the exact Catalog function supplied in the exercise guide.
        tools_path = workspace / "internal/agent/tools.go"
        tools_source = tools_path.read_text()
        catalog_start = tools_source.index("func Catalog() []ToolSpec {")
        catalog_end = tools_source.index("\n}", catalog_start) + 2
        catalog_solution = (root / "docs/solutions/catalog.go.txt").read_text().rstrip()
        tools_path.write_text(tools_source[:catalog_start] + catalog_solution + tools_source[catalog_end:])
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
           "TEMPORAL_TASK_QUEUE": "devfest26-check-" + run_id,
           "API_ADDRESS": free_address(), "PAYMENT_ADDRESS": free_address(),
           "MODEL_AUDIT_PATH": str(audit)}
    # demo_client reads these settings at import time.
    os.environ.update(env)
    from demo_client import ask, wait, evidence, request, PAYMENT

    processes = []
    logs = []
    results = []
    histories = []
    workflow_ids = []
    passed = False
    # Some Flash projects have five requests/minute; one agent turn can make
    # several model calls. Leave a full quota window between user requests.
    # Set CHECK_PAUSE_SECONDS=0 only with sufficient project quota.
    pause = float(os.environ.get("CHECK_PAUSE_SECONDS", "65"))
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

    def command(service, *args):
        result = subprocess.run(["go", "run", "./cmd/" + service, *args], cwd=workspace, env=env, capture_output=True, timeout=110)
        if result.returncode:
            raise RuntimeError(args[0] + " failed; check configuration/preflight")

    def start(service):
        log = (out / (service + ".log")).open("ab", buffering=0)
        logs.append(log)
        # The API/provider must operate without any model credential.
        service_env = env if service == "worker" else {**env, "GEMINI_API_KEY": ""}
        proc = subprocess.Popen(["go", "run", "./cmd/" + service], cwd=workspace, env=service_env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        processes.append(proc)
        return proc

    def check(label, condition):
        if not condition:
            raise AssertionError(label)
        results.append(label)
        print("PASS:", label, flush=True)

    def cli(*args):
        result = subprocess.run(["go", "run", "./cmd/pay", *args], cwd=workspace,
                                env=env, capture_output=True, text=True, timeout=170)
        if result.returncode:
            raise RuntimeError("CLI command failed: " + args[0])
        return json.loads(result.stdout)

    def invoke(prompt):
        nonlocal next_request
        delay = next_request - time.monotonic()
        if delay > 0:
            print("Waiting %.0fs for the live model quota window" % delay, flush=True)
            time.sleep(delay)
        state = cli("ask", prompt)
        rid = state["request_id"]
        workflow_ids.append(rid)
        next_request = time.monotonic() + pause
        if state.get("phase") in ("completed", "error"):
            histories.append(rid)
        check("CLI inline Gemini response: " + prompt,
              state.get("model") == env.get("GEMINI_MODEL", "gemini-3.6-flash")
              and state.get("phase") != "error" and not state.get("wait_timed_out"))
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
        command("payment", "migrate")
        command("payment", "seed")
        command("payment", "preflight")
        command("worker", "preflight")
        provider = start("payment")
        api = start("api")
        worker = start("worker")
        deadline = time.monotonic() + 60
        while True:
            try:
                request("GET", "/health")
                request("GET", "/health", base=PAYMENT)
                break
            except (OSError, TimeoutError):
                if time.monotonic() >= deadline or any(p.poll() is not None for p in processes):
                    raise RuntimeError("verification services failed to start")
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
            check("nine-digit account is rejected without mutation", invalid["model"] == env.get("GEMINI_MODEL", "gemini-3.6-flash") and validation_response and invalid.get("proposal") is None and evidence() == before)
        else:
            check("starter reads require no beneficiary exercise completion", "add_beneficiary" not in listing["tools"])

        before = evidence()

        rid, proposal = invoke("Send ₦1 to John")
        check("payment pauses for approval", proposal["phase"] == "awaiting_approval" and evidence() == before)
        rejected = cli("reject", rid)
        check("CLI rejection returns its outcome inline", rejected["request_id"] == rid and rejected["decision_status"] == "accepted" and not rejected.get("wait_timed_out"))
        histories.append(rid)
        check("rejected proposal causes no debit", rejected["phase"] == "rejected" and evidence() == before)

        for scenario, restart in (("success", True), ("lost-response", False)):
            before = evidence()
            rid = paced_ask("Send ₦1 to John", scenario)
            proposal = paced_wait(rid)
            check(scenario + " pauses before any debit", proposal["phase"] == "awaiting_approval" and evidence() == before)
            calls = invocations(rid)
            if restart:
                # Kill only the worker's own go-run process group, including
                # its child executable. The API and provider remain running.
                os.killpg(worker.pid, signal.SIGKILL)
                worker.wait(timeout=8)
                check("API stays healthy while worker is down", api.poll() is None and request("GET", "/health")["status"] == "ready")
                check("payment service stays healthy while worker is down", provider.poll() is None and request("GET", "/health", base=PAYMENT)["status"] == "ready")
                pending = cli("ask", "--timeout", "1s", "List my beneficiaries")
                queued = pending["request_id"]
                workflow_ids.append(queued)
                check("CLI timeout returns a resumable request while worker is down",
                      pending.get("wait_timed_out") and pending.get("request_key")
                      and pending.get("resume_command") == "go run ./cmd/pay wait " + queued)
                check("API queues a new request without a worker", bool(queued) and not invocations(queued) and evidence() == before)
                worker = start("worker")
                recovered = wait(rid, timeout=60)
                check("worker crash preserves proposal and completed model calls", recovered["proposal"] == proposal["proposal"] and invocations(rid) == calls)
                queued_result = cli("wait", queued)
                histories.append(queued)
                check("queued request executes when worker returns", queued_result["phase"] == "completed" and "list_beneficiaries" in queued_result["tools"])
            result = cli("approve", rid)
            check(scenario + " CLI approval returns payment outcome inline",
                  result["request_id"] == rid and result["decision_status"] == "accepted"
                  and not result.get("wait_timed_out"))
            after = evidence()
            check(scenario + " produces exactly one ₦1 debit", result["phase"] == "succeeded" and after["debits"] == before["debits"] + 1 and after["balance_kobo"] == before["balance_kobo"] - 100)
            check(scenario + " payment execution makes no additional model calls", invocations(rid) == calls)
            histories.extend([rid, rid + "-payment"])

        before = evidence()
        _, last = invoke("Check my last transfer and retry it if it failed")
        check("successful last transfer is not replaced", last["phase"] == "completed" and "get_last_transfer" in last["tools"] and evidence() == before)

        # These are the actual independently launched production HTTP servers.
        api_logs = (out / "api.log").read_text()
        payment_logs = (out / "payment.log").read_text()
        check("agent API logs request metadata", "HTTP request method=POST path=/requests status=202" in api_logs)
        check("payment API logs request metadata", "HTTP request method=GET path=/balance status=200" in payment_logs)
        check("transfer preparation reads the recipient by ID",
              any("HTTP request method=GET path=/beneficiaries/" in line
                  and "status=200" in line for line in payment_logs.splitlines()))
        check("lost-response log records hijack without an HTTP success",
              any("HTTP request method=POST path=/transfers status=0" in line
                  and "hijacked=true" in line for line in payment_logs.splitlines()))

        for rid in histories:
            command("worker", "replay", rid)
        check("all " + str(len(histories)) + " workflow histories replay without model calls", True)
        (out / "result.json").write_text(json.dumps({"model": env.get("GEMINI_MODEL", "gemini-3.6-flash"), "solution": options.solution, "checks": results, "histories": histories}, indent=2))
        passed = True
        print("Verification saved to", out / "result.json", flush=True)
    finally:
        if not passed and shutil.which("temporal"):
            # Leave completed histories intact; terminate only still-open runs
            # owned by this test so no pending approval outlives its schema.
            for rid in workflow_ids:
                try:
                    subprocess.run(["temporal", "workflow", "terminate", "--workflow-id", rid,
                                    "--reason", "Isolated verification cleanup", "--address",
                                    env.get("TEMPORAL_ADDRESS", "127.0.0.1:7233")],
                                   capture_output=True, timeout=15)
                except (OSError, subprocess.TimeoutExpired):
                    pass  # Still stop processes and remove the isolated schema.
        for proc in reversed(processes):
            if proc.poll() is None:
                os.killpg(proc.pid, signal.SIGINT)
        for proc in processes:
            try:
                proc.wait(timeout=8)
            except subprocess.TimeoutExpired:
                os.killpg(proc.pid, signal.SIGKILL)
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
