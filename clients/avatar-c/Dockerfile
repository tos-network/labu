FROM ubuntu:24.04 AS builder

ARG GIT_REPO=https://github.com/tos-network/avatar
ARG GIT_REF=main
ARG MACHINE=linux_gcc_x86_64
ARG EXTRAS=""

ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=UTC

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential gcc g++ clang llvm make cmake ninja-build git \
    libssl-dev liburing-dev curl wget ca-certificates pkg-config liblz4-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /source
RUN git clone --depth=1 --branch ${GIT_REF} ${GIT_REPO} /source/avatar
WORKDIR /source/avatar

RUN ./deps.sh fetch
RUN ./deps.sh install
RUN make MACHINE=${MACHINE} EXTRAS="${EXTRAS}" -j

FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=UTC

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl python3 && rm -rf /var/lib/apt/lists/*

ENV LAB_STATE_DIR=/state
ENV LAB_RPC_PORT=8080
ENV LAB_NETWORK=devnet

WORKDIR /avatar
COPY --from=builder /source/avatar/build/linux/gcc/x86_64/bin/avatar /usr/local/bin/avatar
COPY --from=builder /source/avatar/build/linux/gcc/x86_64/bin/avatar_state_import /usr/local/bin/avatar_state_import
COPY --from=builder /source/avatar/opt /avatar/opt
COPY --from=builder /source/avatar/config /avatar/config

ENV PATH="/avatar/opt/bin:${PATH}"
ENV LD_LIBRARY_PATH="/avatar/opt/lib:${LD_LIBRARY_PATH}"

RUN mkdir -p /state

COPY <<'PY' /usr/local/bin/adapter.py
#!/usr/bin/env python3
import json
import os
import shutil
import signal
import subprocess
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.request import Request, urlopen

STATE_DIR = os.environ.get("LAB_STATE_DIR", "/state")
RPC_PORT = int(os.environ.get("LAB_RPC_PORT", "8080"))
NETWORK = os.environ.get("LAB_NETWORK", "devnet")
AVATAR_PORT = int(os.environ.get("LAB_AVATAR_PORT", "8081"))

if not STATE_DIR.endswith("/"):
    STATE_DIR += "/"

avatar_proc = None
proc_lock = threading.Lock()

def start_avatar():
    global avatar_proc
    args = [
        "/usr/local/bin/avatar",
        "--network", NETWORK,
        "--dir-path", STATE_DIR,
        "--rpc-bind-address", f"0.0.0.0:{AVATAR_PORT}",
    ]
    avatar_proc = subprocess.Popen(args)

def stop_avatar():
    global avatar_proc
    if avatar_proc is None:
        return
    avatar_proc.terminate()
    try:
        avatar_proc.wait(timeout=5)
    except subprocess.TimeoutExpired:
        avatar_proc.kill()
    avatar_proc = None

def reset_state():
    with proc_lock:
        stop_avatar()
        if os.path.isdir(STATE_DIR):
            shutil.rmtree(STATE_DIR, ignore_errors=True)
        os.makedirs(STATE_DIR, exist_ok=True)
        start_avatar()

def rpc_call(method, params=None):
    payload = {"jsonrpc": "2.0", "id": 1, "method": method, "params": params or {}}
    data = json.dumps(payload).encode("utf-8")
    req = Request(f"http://127.0.0.1:{AVATAR_PORT}/json_rpc", data=data, headers={"Content-Type": "application/json"})
    with urlopen(req, timeout=10) as resp:
        body = resp.read()
    return json.loads(body.decode("utf-8"))

def state_digest():
    try:
        resp = rpc_call("get_top_block", {"include_txs": False})
        result = resp.get("result")
        if isinstance(result, dict) and "hash" in result:
            return result["hash"]
        if isinstance(result, str):
            return result
    except Exception:
        pass
    return ""

class Handler(BaseHTTPRequestHandler):
    def _json(self, code, payload):
        body = json.dumps(payload).encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/health":
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b"OK\n")
            return
        if self.path == "/state/digest":
            self._json(200, {"state_digest": state_digest()})
            return
        self.send_response(404)
        self.end_headers()

    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length) if length > 0 else b"{}"
        try:
            payload = json.loads(raw.decode("utf-8"))
        except Exception:
            payload = {}

        if self.path == "/state/reset":
            reset_state()
            self._json(200, {"success": True})
            return

        if self.path == "/state/load":
            with proc_lock:
                stop_avatar()
                os.makedirs(STATE_DIR, exist_ok=True)
                state_file = os.path.join(STATE_DIR, "pre_state.json")
                with open(state_file, "w", encoding="utf-8") as f:
                    json.dump(payload, f)
                cmd = [
                    "/usr/local/bin/avatar_state_import",
                    "--state-json", state_file,
                    "--dir-path", STATE_DIR,
                    "--network", NETWORK,
                ]
                result = subprocess.run(cmd, capture_output=True, text=True)
                start_avatar()
                if result.returncode != 0:
                    err = (result.stderr or result.stdout or "").strip()
                    self._json(500, {"success": False, "error": "state_load_failed", "details": err})
                    return
            self._json(200, {"success": True, "state_digest": state_digest()})
            return

        if self.path == "/tx/execute":
            wire_hex = payload.get("wire_hex", "")
            if not wire_hex:
                self._json(400, {"success": False, "error": "missing wire_hex"})
                return
            try:
                resp = rpc_call("submit_transaction", {"data": wire_hex})
                ok = "error" not in resp
                self._json(200, {"success": ok, "error_code": 0 if ok else 1, "state_digest": state_digest()})
            except Exception as e:
                self._json(500, {"success": False, "error": str(e)})
            return

        if self.path == "/block/execute":
            self._json(501, {"success": False, "error": "block_execute_unsupported"})
            return

        self.send_response(404)
        self.end_headers()

def main():
    start_avatar()
    def handle_sig(*_):
        stop_avatar()
        sys.exit(0)
    signal.signal(signal.SIGTERM, handle_sig)
    signal.signal(signal.SIGINT, handle_sig)
    server = HTTPServer(("0.0.0.0", RPC_PORT), Handler)
    server.serve_forever()

if __name__ == "__main__":
    main()
PY

RUN chmod +x /usr/local/bin/adapter.py

EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --retries=10 \
  CMD curl -f http://localhost:8080/health || exit 1

ENTRYPOINT ["python3", "/usr/local/bin/adapter.py"]
