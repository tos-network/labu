#!/usr/bin/env python3
import argparse
import json
import os
import sys
from pathlib import Path
from urllib import request, error

import yaml


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
    files = sorted(
        p
        for p in vec_dir.rglob("*")
        if p.suffix in {".json", ".yaml", ".yml"}
    )
    vectors = []
    for path in files:
        raw = path.read_text()
        if path.suffix == ".json":
            data = json.loads(raw)
            for vec in data.get("test_vectors", []):
                vectors.append((path.name, vec))
            continue

        try:
            docs = list(yaml.safe_load_all(raw))
        except yaml.YAMLError as exc:
            print(f"[WARN] skip {path}: invalid YAML ({exc.__class__.__name__})", file=sys.stderr)
            continue
        for doc in docs:
            if not isinstance(doc, dict):
                continue
            for vec in doc.get("test_vectors", []):
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


def _is_valid_hex(value, length_bytes=None):
    if value is None:
        return False
    if not isinstance(value, str):
        return False
    v = value[2:] if value.startswith(("0x", "0X")) else value
    if len(v) % 2 != 0:
        return False
    try:
        bytes.fromhex(v)
    except ValueError:
        return False
    if length_bytes is not None and len(v) != length_bytes * 2:
        return False
    return True


def _maybe_skip_tx(tx_json):
    if not isinstance(tx_json, dict):
        return "tx is not object"
    sig = tx_json.get("signature")
    if sig and not _is_valid_hex(sig, length_bytes=64):
        return "signature must be 64 bytes"
    src = tx_json.get("source")
    if src and not _is_valid_hex(src, length_bytes=32):
        return "source must be 32 bytes"
    payload = tx_json.get("payload") or []
    if isinstance(payload, list):
        for item in payload:
            dest = item.get("destination")
            if dest and not _is_valid_hex(dest, length_bytes=32):
                return "destination must be 32 bytes"
            asset = item.get("asset")
            if asset and not _is_valid_hex(asset, length_bytes=32):
                return "asset must be 32 bytes"
    return None


def run_vector(base_url, vec):
    http_post_json(f"{base_url}/state/reset", {})
    pre_state = vec.get("pre_state")
    if pre_state is not None:
        load_res = http_post_json(f"{base_url}/state/load", pre_state)
    else:
        load_res = None

    exec_res = {}
    skipped = None
    inp = vec.get("input", {}) or {}
    kind = inp.get("kind") or "tx"
    wire_hex = inp.get("wire_hex") or ""
    rpc = inp.get("rpc")
    # RPC vectors use `input.rpc` and typically omit `input.kind`.
    if rpc and kind == "tx" and not wire_hex and not inp.get("tx"):
        kind = "rpc"
    tx_json = inp.get("tx")
    if kind == "tx":
        payload = {}
        if wire_hex:
            payload["wire_hex"] = wire_hex
        if tx_json:
            skipped = _maybe_skip_tx(tx_json)
            if skipped and not wire_hex:
                return None, None, load_res, skipped
            payload["tx"] = tx_json
        exec_res = http_post_json(f"{base_url}/tx/execute", payload)
    elif kind == "tx_roundtrip":
        if not wire_hex:
            return None, None, load_res, "tx_roundtrip missing wire_hex"
        payload = {"wire_hex": wire_hex}
        exec_res = http_post_json(f"{base_url}/tx/roundtrip", payload)
    elif kind == "block":
        payload = {"txs": []}
        txs = inp.get("txs") or []
        if not isinstance(txs, list) or not txs:
            return None, None, load_res, "block.txs missing or empty"
        for item in txs:
            if not isinstance(item, dict):
                return None, None, load_res, "block.txs entry must be object"
            item_wire = item.get("wire_hex") or ""
            item_tx = item.get("tx")
            if item_tx:
                skipped = _maybe_skip_tx(item_tx)
                if skipped and not item_wire:
                    return None, None, load_res, skipped
            payload["txs"].append({"wire_hex": item_wire, "tx": item_tx})
        exec_res = http_post_json(f"{base_url}/block/execute", payload)
    elif kind == "chain":
        blocks = inp.get("blocks") or []
        if not isinstance(blocks, list) or not blocks:
            return None, None, load_res, "chain.blocks missing or empty"
        payload = {"blocks": []}
        for b in blocks:
            if not isinstance(b, dict):
                return None, None, load_res, "chain.blocks entry must be object"
            out_block = {
                "id": b.get("id") or "",
                "parents": b.get("parents") or [],
                "height": b.get("height"),
                "timestamp_ms": b.get("timestamp_ms"),
                "txs": [],
            }
            txs = b.get("txs") or []
            if not isinstance(txs, list):
                return None, None, load_res, "chain block txs must be list"
            for item in txs:
                if not isinstance(item, dict):
                    return None, None, load_res, "chain block txs entry must be object"
                item_wire = item.get("wire_hex") or ""
                item_tx = item.get("tx")
                if item_tx:
                    skipped = _maybe_skip_tx(item_tx)
                    if skipped and not item_wire:
                        return None, None, load_res, skipped
                out_block["txs"].append({"wire_hex": item_wire, "tx": item_tx})
            payload["blocks"].append(out_block)
        exec_res = http_post_json(f"{base_url}/chain/execute", payload)
    elif kind == "rpc":
        if not isinstance(rpc, dict) or not rpc:
            return None, None, load_res, "rpc missing input.rpc object"
        exec_res = http_post_json(f"{base_url}/json_rpc", rpc)
    else:
        exec_res = http_get_json(f"{base_url}/state/digest")

    post_state = None
    expected = vec.get("expected") or {}
    if expected.get("post_state") is not None:
        post_state = http_get_json(f"{base_url}/state/export")
    return exec_res, post_state, load_res, skipped


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--vectors", required=True, help="vectors directory (JSON)")
    ap.add_argument("--base-url", default=os.environ.get("LABU_BASE_URL", "http://127.0.0.1:8080"))
    ap.add_argument("--dump", action="store_true", help="dump exec_res and post_state for each vector")
    args = ap.parse_args()

    vectors = load_vectors(args.vectors)
    if not vectors:
        print("no vectors found", file=sys.stderr)
        return 2

    failures = 0
    for fname, vec in vectors:
        name = vec.get("name", "<unnamed>")
        if vec.get("runnable") is False:
            print(f"[SKIP] {fname}/{name}: runnable=false")
            continue
        try:
            exec_res, post_state, load_res, skipped = run_vector(args.base_url, vec)
        except error.HTTPError as e:
            failures += 1
            print(f"[FAIL] {fname}/{name}: http {e.code}")
            continue
        except Exception as e:
            failures += 1
            print(f"[FAIL] {fname}/{name}: {e}")
            continue
        if vec.get("runnable") is False:
            print(f"[SKIP] {fname}/{name}: runnable=false")
            continue
        if skipped:
            print(f"[SKIP] {fname}/{name}: {skipped}")
            continue

        expected = vec.get("expected") or {}
        inp = vec.get("input", {}) or {}
        is_rpc = isinstance(inp.get("rpc"), dict) and bool(inp.get("rpc"))
        if args.dump:
            if load_res is not None:
                print(f"[DUMP] {fname}/{name}: state_load={json.dumps(load_res, sort_keys=True)}")
            print(f"[DUMP] {fname}/{name}: exec_res={json.dumps(exec_res, sort_keys=True)}")
            if post_state is not None:
                print(f"[DUMP] {fname}/{name}: post_state={json.dumps(post_state, sort_keys=True)}")

        if is_rpc:
            if "response" in expected and exec_res != expected.get("response"):
                failures += 1
                print(
                    f"[FAIL] {fname}/{name}: response mismatch expected {json.dumps(expected.get('response'), sort_keys=True)} got {json.dumps(exec_res, sort_keys=True)}"
                )
                continue
            print(f"[OK] {fname}/{name}")
            continue

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
