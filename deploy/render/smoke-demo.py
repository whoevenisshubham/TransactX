"""CI-only real-container smoke. Never targets an owner/source database."""
import json
import os
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

BASE = "http://127.0.0.1:10000"
ORIGIN = "https://demo.example.org"


def sql(query):
    parsed = urllib.parse.urlsplit(os.environ["DATABASE_URL"])
    assert parsed.hostname == "127.0.0.1"
    assert parsed.path.startswith("/transactx_disposable_")
    env = dict(os.environ, PGHOST=parsed.hostname, PGPORT=str(parsed.port or 5432),
               PGDATABASE=parsed.path[1:], PGUSER=urllib.parse.unquote(parsed.username),
               PGPASSWORD=urllib.parse.unquote(parsed.password))
    return subprocess.check_output(["psql", "-X", "-w", "-At", "-v", "ON_ERROR_STOP=1",
                                    "-c", query], env=env, text=True).strip()


def request(path, payload=None, token=None, key=None, expected=(200,)):
    headers = {"Origin": ORIGIN, "Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    if key:
        headers["Idempotency-Key"] = key
    req = urllib.request.Request(BASE + path,
                                 data=None if payload is None else json.dumps(payload).encode(),
                                 headers=headers)
    try:
        response = urllib.request.urlopen(req, timeout=60)
    except urllib.error.HTTPError as exc:
        response = exc
    with response:
        body = response.read()
        assert response.status in expected, (path, response.status, body[:300])
        assert response.headers.get("Access-Control-Allow-Origin") == ORIGIN
        return json.loads(body).get("data")


def login(identifier):
    return request("/api/auth/login", {"identifier": identifier,
                   "password": "Disposable-demo-password-2026!"})["token"]


def main():
    assert os.environ.get("GITHUB_ACTIONS") == "true", "CI-only smoke"
    assert sql("SELECT count(*) FROM public.payments") == "0"
    for _ in range(40):
        try:
            urllib.request.urlopen(BASE + "/healthz", timeout=2).close()
            break
        except OSError:
            time.sleep(1)
    else:
        raise AssertionError("gateway did not start")
    # Liveness must not spawn a pool or keep the database awake.
    assert sql("SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid()") == "0"
    alice, viewer = login("alice@transactx"), login("viewer@transactx")
    payload = {"recipient": "bob@transactx", "amountPaise": 100, "currency": "INR"}
    key = str(uuid.uuid4())
    payment = request("/api/payments", payload, alice, key, (200, 201, 202))
    duplicate = request("/api/payments", payload, alice, key, (200, 201, 202))
    assert duplicate["id"] == payment["id"]
    for _ in range(30):
        payment = request("/api/payments/" + payment["id"], token=alice)
        if payment["state"] == "COMPLETED":
            break
        time.sleep(1)
    assert payment["state"] == "COMPLETED"
    job = request("/api/ops/investigations", {"paymentId": payment["id"],
                  "question": "Explain this payment using its recorded evidence."},
                  viewer, str(uuid.uuid4()), (202,))
    for _ in range(40):
        report = request("/api/ops/investigations/" + job["investigationId"], token=viewer)
        if report["status"] == "completed":
            break
        time.sleep(1)
    assert report["status"] == "completed" and report["report"] is not None
    for _ in range(20):
        if sql("SELECT count(*) FROM ops_payment_worklist WHERE current_state='COMPLETED'") == "1":
            break
        time.sleep(1)
    assert sql("SELECT count(*) FROM payments") == "1"
    assert sql("SELECT count(*) FROM ops_payment_worklist WHERE current_state='COMPLETED'") == "1"
    assert sql("SELECT coalesce(sum(CASE WHEN entry_type='DEBIT' THEN amount_paise ELSE -amount_paise END),0) FROM ledger_entries") == "0"
    assert sql("SELECT count(*) FROM payment_bank_operations WHERE status='SUCCEEDED'") == "4"
    assert sql("SELECT count(*) FROM bank_a.operations") == "2"
    assert sql("SELECT count(*) FROM bank_b.operations") == "2"
    assert sql("SELECT sum(balance_paise) FROM public.accounts") == "1100000"
    assert sql("SELECT (SELECT sum(balance_paise) FROM bank_a.accounts)+(SELECT sum(balance_paise) FROM bank_b.accounts)") == "1100000"
    request("/api/auth/register", {}, expected=(403,))
    # No open browser/SSE here. Real idle stop must close ALL four pools.
    time.sleep(100)
    assert sql("SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid()") == "0"
    request("/api/accounts", token=alice)
    assert sql("SELECT count(*) FROM payments") == "1"
    print("PASS: real container payment, idempotency, investigator, projection, conservation, idle pools and wake")


if __name__ == "__main__":
    main()
