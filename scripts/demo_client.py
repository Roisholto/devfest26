"""Small stdlib-only REST client used by workshop acceptance scripts."""
import json
import os
import time
import urllib.request
import urllib.error
import uuid

INSTANCE = os.environ.get("WORKSHOP_INSTANCE", "participant")
API = "http://" + os.environ.get("API_ADDRESS", "127.0.0.1:8080" if INSTANCE == "presenter" else "127.0.0.1:8090")
PAYMENT = "http://" + os.environ.get("PAYMENT_ADDRESS", "127.0.0.1:8081" if INSTANCE == "presenter" else "127.0.0.1:8091")

def request(method, path, body=None, key=None, base=API):
    data = None if body is None else json.dumps(body).encode()
    headers = {"Content-Type": "application/json"}
    if key:
        headers["Idempotency-Key"] = key
    req = urllib.request.Request(base + path, data=data, headers=headers, method=method)
    with urllib.request.urlopen(req, timeout=12) as response:
        return json.load(response)

def ask(prompt, scenario="success", **options):
    key = "demo-" + uuid.uuid4().hex
    result = request("POST", "/requests", {"prompt": prompt, "scenario": scenario, **options}, key)
    return result["request_id"]

def wait(request_id, terminal=False, timeout=50):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            result = request("GET", "/requests/" + request_id)
            waiting = result["phase"] in ("reasoning", "executing", "notifying") or (terminal and result["phase"] in ("awaiting_approval", "scheduled"))
            if not waiting:
                return result
        except (urllib.error.URLError, TimeoutError):
            pass
        time.sleep(0.2)
    raise RuntimeError("request timed out: " + request_id)

def decide(request_id, state, approve=True):
    return request("POST", "/requests/" + request_id + "/decision", {"proposal_id": state["proposal"]["id"], "approve": approve}, "decision-" + state["proposal"]["id"] + str(approve))

def evidence():
    return request("GET", "/evidence", base=PAYMENT)
