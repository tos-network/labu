#!/usr/bin/env python3
import argparse
import json
import os
import sys
from pathlib import Path
from urllib import request, error


def http_post_json(url, payload):
    data = json.dumps(payload).encode("utf-8")
    req = request.Request(url, data=data, headers={"Content-Type": "application/json"})
    with request.urlopen(req, timeout=60) as resp:
        body = resp.read()
        return json.loads(body.decode("utf-8")) if body else {}


def http_get_json(url):
    with request.urlopen(url, timeout=60) as resp:
        body = resp.read()
        return json.loads(body.decode("utf-8")) if body else {}


def load_vectors(vec_dir):
    vec_dir = Path(vec_dir)
    files = sorted(p for p in vec_dir.rglob("*") if p.suffix == ".json")
    vectors = []
    for path in files:
        data = json.loads(path.read_text())
        for vec in data.get("test_vectors", []):
            vectors.append((path.name, vec))
    return vectors


def normalize_accounts(raw):
    out = {}
    if not isinstance(raw, list):
        return out
    for item in raw:
        if not isinstance(item, dict):
            continue
        addr = item.get("address")
        if addr:
            out[addr] = item
    return out


def compare_post_state(expected, actual):
    exp_global = expected.get("global_state") if expected else None
    act_global = actual.get("global_state") if actual else None
    if exp_global:
        for field in ["total_supply", "total_burned", "total_energy", "block_height", "timestamp"]:
            if field in exp_global:
                if not act_global or str(act_global.get(field)) != str(exp_global.get(field)):
                    return False, f"global_state.{field} mismatch: expected={exp_global.get(field)} got={act_global.get(field)}"

    exp_accounts = normalize_accounts(expected.get("accounts", []))
    act_accounts = normalize_accounts(actual.get("accounts", []))
    for addr, exp in exp_accounts.items():
        act = act_accounts.get(addr)
        if act is None:
            return False, f"missing account {addr} in actual state"
        for field in ["balance", "nonce", "frozen", "energy", "flags", "data"]:
            if field in exp:
                if str(act.get(field)) != str(exp.get(field)):
                    return False, f"account {addr} {field} mismatch: expected={exp.get(field)} got={act.get(field)}"
    return True, ""


def run_vector(base_url, vec):
    http_post_json(f"{base_url}/state/reset", {})
    pre_state = vec.get("pre_state")
    if pre_state is not None:
        load_res = http_post_json(f"{base_url}/state/load", pre_state)
    else:
        load_res = None

    exec_res = {}
    inp = vec.get("input", {}) or {}
    kind = inp.get("kind") or "tx"
    wire_hex = inp.get("wire_hex") or ""
    tx_json = inp.get("tx")
    if kind == "tx":
        payload = {}
        if wire_hex:
            payload["wire_hex"] = wire_hex
        if tx_json:
            payload["tx"] = tx_json
        exec_res = http_post_json(f"{base_url}/tx/execute", payload)
    else:
        exec_res = http_get_json(f"{base_url}/state/digest")

    post_state = None
    expected = vec.get("expected") or {}
    if expected.get("post_state") is not None:
        post_state = http_get_json(f"{base_url}/state/export")
    return exec_res, post_state, load_res


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--vectors", required=True, help="vectors directory (JSON)")
    ap.add_argument("--base-url", default=os.environ.get("LAB_BASE_URL", "http://127.0.0.1:8080"))
    ap.add_argument("--dump", action="store_true", help="dump exec_res and post_state for each vector")
    args = ap.parse_args()

    vectors = load_vectors(args.vectors)
    if not vectors:
        print("no vectors found", file=sys.stderr)
        return 2

    failures = 0
    for fname, vec in vectors:
        name = vec.get("name", "<unnamed>")
        try:
            exec_res, post_state, load_res = run_vector(args.base_url, vec)
        except error.HTTPError as e:
            failures += 1
            print(f"[FAIL] {fname}/{name}: http {e.code}")
            continue
        except Exception as e:
            failures += 1
            print(f"[FAIL] {fname}/{name}: {e}")
            continue

        expected = vec.get("expected") or {}
        if args.dump:
            if load_res is not None:
                print(f"[DUMP] {fname}/{name}: state_load={json.dumps(load_res, sort_keys=True)}")
            print(f"[DUMP] {fname}/{name}: exec_res={json.dumps(exec_res, sort_keys=True)}")
            if post_state is not None:
                print(f"[DUMP] {fname}/{name}: post_state={json.dumps(post_state, sort_keys=True)}")

        if "success" in expected and exec_res.get("success") != expected.get("success"):
            failures += 1
            print(f"[FAIL] {fname}/{name}: success expected {expected.get('success')} got {exec_res.get('success')}")
            continue
        if "error_code" in expected and exec_res.get("error_code") != expected.get("error_code"):
            failures += 1
            print(f"[FAIL] {fname}/{name}: error_code expected {expected.get('error_code')} got {exec_res.get('error_code')}")
            continue
        if expected.get("state_digest") and exec_res.get("state_digest") and exec_res.get("state_digest") != expected.get("state_digest"):
            failures += 1
            print(f"[FAIL] {fname}/{name}: state_digest expected {expected.get('state_digest')} got {exec_res.get('state_digest')}")
            continue
        if expected.get("post_state") is not None:
            ok, reason = compare_post_state(expected.get("post_state") or {}, post_state or {})
            if not ok:
                failures += 1
                print(f"[FAIL] {fname}/{name}: {reason}")
                continue
        print(f"[OK] {fname}/{name}")

    if failures:
        print(f"failures: {failures}")
        return 1
    print("all ok")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
