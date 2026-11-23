#!/usr/bin/env python3
"""chrcon.py <vmid> [file-with-lines|-] -- drive the CHR serial console; prints output.

Lines "#wait N" sleep N seconds, "^C" sends Ctrl-C, "q!" sends a bare q (license pager).
From the L2-L5 labs; the first login shows a license pager and a forced password change.
"""
import socket, sys, time, select
vmid = sys.argv[1]
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM); s.connect(f"/var/run/qemu-server/{vmid}.serial0")
def drain(t=1.0):
    out = b""; end = time.time() + t
    while time.time() < end:
        r, _, _ = select.select([s], [], [], 0.2)
        if r:
            d = s.recv(65536)
            if not d: break
            out += d
    return out.decode("utf-8", "replace")
def send(line, wait=1.0):
    s.send(line.encode() + b"\r"); return drain(wait)
lines = open(sys.argv[2]).read().splitlines() if len(sys.argv) > 2 and sys.argv[2] != "-" else sys.stdin.read().splitlines()
buf = drain(1.0)
for ln in lines:
    if ln.startswith("#wait"): time.sleep(float(ln.split()[1])); buf += drain(0.5); continue
    if ln == "^C": s.send(b"\x03"); buf += drain(1); continue
    if ln == "q!": s.send(b"q"); buf += drain(1); continue
    buf += send(ln, 1.5)
import re
print(re.sub(r"\x1b\[[0-9;?]*[A-Za-z]", "", buf)[-6000:])
