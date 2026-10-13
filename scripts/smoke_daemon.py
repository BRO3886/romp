#!/usr/bin/env python3
"""Exercise built clients and the real daemon in isolated local state."""
import argparse
import json
import os
import pathlib
import signal
import socket
import sqlite3
import subprocess
import tempfile
import time

root = pathlib.Path(__file__).resolve().parent.parent
parser = argparse.ArgumentParser()
parser.add_argument("--launchd", action="store_true")
args = parser.parse_args()
romp, rompd = root / "bin/romp", root / "bin/rompd"


def command(argv, **kwargs):
    return subprocess.run([str(v) for v in argv], check=True, text=True,
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE, **kwargs).stdout


def ready(env):
    for _ in range(100):
        result = subprocess.run([str(romp), "daemon", "status"], env=env,
                                text=True, capture_output=True)
        if result.returncode == 0:
            return json.loads(result.stdout)
        time.sleep(0.1)
    raise RuntimeError("daemon did not become ready")


def prepare_shutdown(path):
    with socket.socket(socket.AF_UNIX) as conn:
        conn.settimeout(10)
        conn.connect(str(path))
        stream = conn.makefile("rwb")
        for request in [
            {"id": 1, "method": "initialize", "params": {
                "client_name": "smoke", "client_version": "v1",
                "protocol_min": 1, "protocol_max": 1, "capabilities": []}},
            {"id": 2, "method": "daemon.prepareShutdown", "params": {}},
        ]:
            stream.write((json.dumps(dict(jsonrpc="2.0", **request)) + "\n").encode())
            stream.flush()
            response = json.loads(stream.readline())
            assert "error" not in response, response


with tempfile.TemporaryDirectory(prefix="rompd-smoke-", dir="/tmp") as temp:
    temp = pathlib.Path(temp)
    import shutil
    daemon_binary = temp / "bin/rompd"
    daemon_binary.parent.mkdir()
    shutil.copy2(rompd, daemon_binary)
    env = dict(os.environ, XDG_STATE_HOME=str(temp / "state"),
               XDG_CONFIG_HOME=str(temp / "config"))
    env.pop("ROMP_DAEMON_ENVIRONMENT", None)
    state = temp / "state/romp"
    socket_path = state / "romp.sock"
    command([romp, "daemon", "install", "--dry-run", "--binary", rompd],
            cwd=root, env=env)
    process = subprocess.Popen([str(rompd)], env=env, stdout=subprocess.DEVNULL,
                               stderr=subprocess.DEVNULL)
    try:
        health = ready(env)
        assert health["protocol_version"] == 1 and health["restart_ready"]
        assert state.stat().st_mode & 0o777 == 0o700
        assert socket_path.stat().st_mode & 0o777 == 0o600
        second = subprocess.run([str(rompd)], env=env, text=True, capture_output=True)
        assert second.returncode != 0 and "already owns" in second.stderr
        assert json.loads(command([romp, "status", "--all", "--json"], env=env)) == []
        swift = temp / "swift-client"
        command(["swiftc", root / "docs/protocol/Types.generated.swift",
                 root / "scripts/protocol_smoke.swift", "-o", swift])
        assert "compatible" in command([swift, socket_path])
        prepare_shutdown(socket_path)
    finally:
        process.send_signal(signal.SIGTERM)
        process.wait(timeout=15)

    with sqlite3.connect(state / "romp.db") as db:
        db.execute("INSERT INTO jobs (id, repo, issue, branch, session_id) VALUES (?, ?, ?, ?, ?)",
                   ("crash-fixture", "fixture/repo", 7, "romp-7", "fixture-session"))

    with sqlite3.connect(state / "romp.db") as db:
        with tempfile.TemporaryDirectory(prefix="romp-watch-") as checkout:
            command(["git", "init", checkout])
            command(["git", "-C", checkout, "remote", "add", "origin", "https://github.com/fixture/missing.git"])
            db.execute("INSERT INTO repositories (origin, path) VALUES (?, ?)",
                       ("fixture/missing", checkout))

    process = None
    label = "com.romp.daemon.smoke." + str(os.getpid())
    target = "gui/" + str(os.getuid())
    plist = temp / "agent.plist"
    if args.launchd:
        import plistlib
        plist.write_bytes(plistlib.dumps({
            "Label": label, "Program": str(daemon_binary), "ProgramArguments": [str(daemon_binary)],
            "RunAtLoad": True, "KeepAlive": True,
            "EnvironmentVariables": {"PATH": env["PATH"],
                                     "XDG_STATE_HOME": env["XDG_STATE_HOME"],
                                     "XDG_CONFIG_HOME": env["XDG_CONFIG_HOME"]},
            "StandardErrorPath": str(temp / "daemon.log"),
        }))
        command(["/usr/bin/plutil", "-lint", plist])
        command(["/bin/launchctl", "bootstrap", target, plist])
    else:
        process = subprocess.Popen([str(rompd)], env=env, stdout=subprocess.DEVNULL,
                                   stderr=subprocess.DEVNULL)
    try:
        try:
            ready(env)
        except RuntimeError:
            if args.launchd:
                print(command(["/bin/launchctl", "print", target + "/" + label]))
                if (temp / "daemon.log").exists():
                    print((temp / "daemon.log").read_text())
            raise
        history = command([romp, "history", "--all"], env=env)
        assert "interrupted" in history and "fixture-session" in history, history
        assert json.loads(command([romp, "status", "--all", "--json"], env=env)) == []
        if args.launchd:
            before = command(["/bin/launchctl", "print", target + "/" + label])
            import re
            pid = int(re.search(r"pid = (\d+)", before).group(1))
            os.kill(pid, signal.SIGKILL)
            for _ in range(150):
                time.sleep(0.1)
                current = command(["/bin/launchctl", "print", target + "/" + label])
                match = re.search(r"pid = (\d+)", current)
                if match and int(match.group(1)) != pid:
                    break
            else:
                raise RuntimeError("launchd did not restart killed daemon")
            ready(env)
        prepare_shutdown(socket_path)
    finally:
        if args.launchd:
            command(["/bin/launchctl", "bootout", target, plist])
        elif process is not None:
            process.send_signal(signal.SIGTERM)
            process.wait(timeout=15)

print("daemon smoke passed: Go and Swift clients, singleton, permissions, interrupted recovery"
      + (", launchd KeepAlive and bootout" if args.launchd else ""))
