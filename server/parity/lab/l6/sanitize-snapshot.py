#!/usr/bin/env python3
"""L6 C.0: make a COPY of the production data directory safe for the lab.

Runs offline on the copy (no panel may be running on it), before the first
panel start, so nothing can publish, back up or fetch with production targets.
Python 3 standard library only; works on the test servers and the PVE host.

  sanitize-snapshot.py template <data-copy> <map.json> [--auto]
      Write a mapping file (mode 600) listing every Agent and router of the
      snapshot with its current address, all set to "disable". Edit it: point
      the ones that have a lab guest at that guest, fill "replace" with the
      production addresses that also appear elsewhere (panel URL, VIPs, DNS).
      --auto maps Agents onto the L6 guests by type (HA pairs first) and fills
      "replace" (Agent addresses, VIPs, panel URL); review it anyway.
      The map holds production values: keep it next to the copy, never commit.

  sanitize-snapshot.py apply <data-copy> <map.json> [--dry-run]
      Apply the map (see MAP below) and write a summary of what changed
      (counts and JSON paths only, no values) to stdout and <data-copy>/.l6-sanitize.json.

  sanitize-snapshot.py scan <data-copy> <map.json> [--verbose] [--values]
      Fail (exit 1) when any production target recorded by "template" in the
      map's _deny list (Agent/router/VIP/OxiDNS-ROS/WebDAV/subscription/panel
      hosts) still appears in the database or in a JSON/YAML state file.
      Private IPv4 outside the lab net is counted as information only (rule
      lists and client ranges contain them legitimately). --values prints the
      matched pattern (operator terminal only; never paste it into reports).

MAP (JSON):
  {
    "panel_url": "http://10.97.0.150",            # system_config.server_domain
    "lab_router": {"endpoint": "https://10.97.0.156", "username": "cf",
                   "password": "...", "verify_tls": false},
    "agents":  {"<agent id>": {"host": "10.97.0.152", "port": 8082} | "disable"},
    "routers": {"<router id>": "lab" | "disable"},
    "replace": {"<production string>": "<lab string>", ...},
    "subscription_url": "http://10.97.0.151:8090/sub/lab.yaml",
    "webdav": null | {"webdav_url": "...", "webdav_username": "...",
                      "webdav_password": "...", "webdav_path": "...", "auto_backup": false},
    "manual_nodes": "synthetic" | "keep",
    "easytier": "disable" | "keep"
  }

What apply does (C.0 of o14-switch-plan.md):
  - every Agent: mapped → host/port of the lab guest; otherwise disabled and its
    address moved to TEST-NET-1 (192.0.2.0/24, never routed), including the HA
    publication identities that carry member addresses;
  - every router: "lab" → endpoint/credentials of the lab CHR; otherwise
    endpoint on TEST-NET-1, automatic rule sync off;
  - OxiDNS ros_address_list / ros_route plugins (live and template): address
    and credentials of the lab CHR;
  - WebDAV: auto backup off and address/credentials emptied (or the lab WebDAV);
  - subscriptions (panel and bundled Sub-Store): URL → lab synthetic
    subscription; cached subscription contents, aggregated providers and the
    Sub-Store caches, sync artifacts and share tokens removed;
  - manual nodes → synthetic TEST-NET nodes (names kept); EasyTier outbound
    disabled and its network name/secret/private key replaced;
  - system_config: server_domain → panel_url; sub_store_url emptied when it
    points outside; rule_fetch_proxy emptied;
  - "replace" applied to every string in the database and to the JSON/YAML
    state files of the data directory (production addresses → lab addresses);
  - changed rows get the next store generation as revision, a new commit id is
    written (same rule as server/internal/store.Commit), the SQLite integrity
    check must pass afterwards.
"""
import argparse
import base64
import ipaddress
import json
import os
import re
import secrets
import shutil
import sqlite3
import sys
import uuid

TEST_NET = "192.0.2."
LAB_NET = ipaddress.ip_network("10.97.0.0/24")
PRIVATE = [ipaddress.ip_network(n) for n in ("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10")]
DB = "configflow.sqlite3"
STATE_SUFFIXES = (".json", ".yaml", ".yml")
SKIP_DIRS = {"rules", "dist", "metrics", "traffic", "migrations", "shared/rules"}
IPV4 = re.compile(r"(?<![\d.])(\d{1,3}(?:\.\d{1,3}){3})(?![\d.])")


def die(msg):
    print("sanitize: " + msg, file=sys.stderr)
    sys.exit(2)


def dumps(value):
    # the store's row encoding: compact, non-ASCII kept (round-trips the snapshot exactly)
    return json.dumps(value, ensure_ascii=False, separators=(",", ":"))


def guard_copy(data):
    data = os.path.realpath(data)
    if not os.path.isfile(os.path.join(data, DB)):
        die(f"{data} has no {DB}")
    snap = os.path.realpath(os.path.expanduser("~/cf-prod-snapshot"))
    if data == snap or data.startswith(snap + os.sep):
        die("refusing to touch the snapshot itself; work on a copy outside ~/cf-prod-snapshot")
    wal = os.path.join(data, DB + "-wal")
    if os.path.exists(wal) and os.path.getsize(wal) > 0:
        die("database has a non-empty -wal: stop the panel and checkpoint first (sqlite3 db 'pragma wal_checkpoint(truncate)')")
    for pid in filter(str.isdigit, os.listdir("/proc")):
        try:
            for fd in os.listdir(f"/proc/{pid}/fd"):
                if os.path.realpath(f"/proc/{pid}/fd/{fd}").startswith(data + os.sep):
                    die(f"process {pid} has files open under {data}; stop it first")
        except OSError:
            continue
    return data


def load_rows(conn):
    rows = {}
    for scope, owner, field, value, revision in conn.execute(
            "SELECT scope, owner, field, value, revision FROM fields"):
        rows[(scope, owner, field)] = (json.loads(value), value, revision)
    return rows


class Log:
    def __init__(self):
        self.items = {}

    def add(self, what, n=1):
        self.items[what] = self.items.get(what, 0) + n


def test_net(i):
    return f"{TEST_NET}{10 + i % 200}"


def walk_strings(value, fn, path="$"):
    """Return value with fn(path, s) applied to every string leaf."""
    if isinstance(value, str):
        return fn(path, value)
    if isinstance(value, list):
        return [walk_strings(v, fn, f"{path}[]") for v in value]
    if isinstance(value, dict):
        return {k: walk_strings(v, fn, f"{path}.{k}") for k, v in value.items()}
    return value


def compile_pairs(pairs):
    """Longest first; an IPv4 literal only matches as a whole address (10.0.0.1 never hits 10.0.0.11)."""
    out = []
    for old, new in sorted(pairs.items(), key=lambda kv: -len(kv[0])):
        if not old:
            continue
        if IPV4.fullmatch(old):
            out.append((re.compile(r"(?<![\d.])" + re.escape(old) + r"(?![\d]|\.\d)"), new))
        else:
            out.append((re.compile(re.escape(old)), new))
    return out


def apply_pairs(compiled, s):
    for rx, new in compiled:
        s = rx.sub(lambda _m, n=new: n, s)
    return s


def replacer(pairs, log, where):
    compiled = compile_pairs(pairs)

    def fn(path, s):
        out = apply_pairs(compiled, s)
        if out != s:
            log.add(f"replace {where}")
        return out
    return fn


def host_of(url_or_host):
    m = re.match(r"^[a-z]+://([^/:]+)", url_or_host or "")
    return m.group(1) if m else (url_or_host or "")


# ---------------------------------------------------------------- structured rules

def rule_agents(doc, m, log):
    agents = doc.get(("system", "", "agents"))
    if not isinstance(agents, list):
        return
    mapping = m.get("agents", {})
    moved = {}
    for i, a in enumerate(agents):
        aid = a.get("id", "")
        target = mapping.get(aid, "disable")
        if isinstance(target, dict):
            a["host"] = target["host"]
            if "port" in target:
                a["port"] = target["port"]
            a["enabled"] = True
            log.add("agent repointed to a lab guest")
        else:
            a["host"] = test_net(i)
            a["enabled"] = False
            log.add("agent disabled (address on TEST-NET-1)")
        moved[aid] = a["host"]
    for a in agents:  # HA publications keep member identities with addresses
        pub = a.get("oxidns_ha_publication")
        if isinstance(pub, dict) and isinstance(pub.get("identities"), dict):
            for member, ident in pub["identities"].items():
                if isinstance(ident, dict) and member in moved:
                    ident["host"] = moved[member]
                    log.add("HA identity address moved")


def rule_routers(doc, m, log):
    lab = m.get("lab_router") or {}
    mapping = m.get("routers", {})
    for key, value in doc.items():
        if key[0] != "profile" or key[2] != "routers" or not isinstance(value, dict):
            continue
        for i, r in enumerate(value.get("items") or []):
            if mapping.get(r.get("id", ""), "disable") == "lab" and lab:
                for k in ("endpoint", "username", "password", "verify_tls"):
                    if k in lab:
                        r[k] = lab[k]
                log.add("router repointed to the lab CHR")
            else:
                r["endpoint"] = f"https://{test_net(i)}"
                r["password"] = "l6-scrubbed"
                r["auto_rule_sync"] = False
                log.add("router disabled (endpoint on TEST-NET-1, auto sync off)")


def rule_oxidns_ros(doc, m, log):
    lab = m.get("lab_router") or {}
    host = host_of(lab.get("endpoint", "")) or test_net(0)
    for key, value in doc.items():
        if key[0] != "profile" or key[2] != "oxidns" or not isinstance(value, dict):
            continue
        for container in (value, value.get("template_settings") or {}):
            for plugin in ("ros_address_list", "ros_route"):
                args = (container.get(plugin) or {}).get("args")
                if isinstance(args, dict):
                    if "address" in args:
                        args["address"] = host
                    if "username" in args:
                        args["username"] = lab.get("username", "l6")
                    if "password" in args:
                        args["password"] = lab.get("password", "l6-scrubbed")
                    log.add(f"OxiDNS {plugin} → lab router")


def rule_backup(doc, m, log):
    key = ("system", "", "backup")
    if key not in doc:
        return
    dav = m.get("webdav")
    b = doc[key] if isinstance(doc[key], dict) else {}
    if dav:
        b.update(dav)
        b["auto_backup"] = bool(dav.get("auto_backup", False))
        log.add("WebDAV → lab WebDAV")
    else:
        b.update({"webdav_url": "", "webdav_username": "", "webdav_password": "", "auto_backup": False})
        log.add("WebDAV emptied, auto backup off")
    doc[key] = b


def rule_subscriptions(doc, m, log):
    url = m.get("subscription_url")
    subs = doc.get(("shared", "", "subscriptions"))
    if isinstance(subs, list):
        for s in subs:
            if url:
                s["url"] = url
                log.add("subscription → lab synthetic URL")
            else:
                s["enabled"] = False
                s["url"] = f"http://{test_net(0)}/disabled"
                log.add("subscription disabled")


def rule_nodes(doc, m, log):
    if m.get("manual_nodes", "synthetic") == "keep":
        return
    nodes = doc.get(("shared", "", "nodes"))
    if not isinstance(nodes, list):
        return
    for i, n in enumerate(nodes):
        if "proxy_string" in n:
            cred = base64.urlsafe_b64encode(b"aes-256-gcm:l6-synthetic").decode().rstrip("=")
            name = n.get("name") or f"l6-{i}"
            n["proxy_string"] = f"ss://{cred}@{test_net(100 + i)}:8388#{name}"
            log.add("manual node → synthetic TEST-NET node")


def rule_easytier(doc, m, log):
    for key, value in doc.items():
        if key[0] != "profile" or key[2] != "mihomo" or not isinstance(value, dict):
            continue
        et = value.get("easytier")
        if not isinstance(et, dict):
            continue
        if m.get("easytier", "disable") == "disable" and "enabled" in et:
            et["enabled"] = False
        opts = et.get("options") if isinstance(et.get("options"), dict) else {}
        if "network-name" in opts:
            opts["network-name"] = "l6-lab"
        if "network-secret" in opts:
            opts["network-secret"] = "l6-scrubbed-secret"
        if "local-private-key" in opts:
            opts["local-private-key"] = base64.b64encode(secrets.token_bytes(32)).decode()
        for k in ("peers", "peer", "listeners"):
            if k in opts:
                opts[k] = [f"tcp://{test_net(1)}:11010"] if isinstance(opts[k], list) else f"tcp://{test_net(1)}:11010"
        log.add("EasyTier scrubbed")


def rule_system_config(doc, m, log):
    sc = doc.get(("system", "", "system_config"))
    if not isinstance(sc, dict):
        return
    if m.get("panel_url"):
        sc["server_domain"] = m["panel_url"]
        log.add("server_domain → lab panel")
    sub = sc.get("sub_store_url") or ""
    if sub and not sub.startswith(("http://127.0.0.1", "http://localhost")):
        sc["sub_store_url"] = ""
        log.add("external sub_store_url emptied")
    if sc.get("rule_fetch_proxy"):
        sc["rule_fetch_proxy"] = ""
        log.add("rule_fetch_proxy emptied")


RULES = [rule_agents, rule_routers, rule_oxidns_ros, rule_backup, rule_subscriptions,
         rule_nodes, rule_easytier, rule_system_config]


# ---------------------------------------------------------------- files

def files_cleanup(data, m, log, dry):
    def rm_children(rel):
        d = os.path.join(data, rel)
        if os.path.isdir(d):
            for name in os.listdir(d):
                p = os.path.join(d, name)
                log.add(f"removed {rel}/*")
                if not dry:
                    shutil.rmtree(p) if os.path.isdir(p) else os.remove(p)
    rels = ["subscribes", "shared/subscribes", "shared/subscription-downloads", "providers"]
    pdir = os.path.join(data, "profiles")
    if os.path.isdir(pdir):
        for pid in sorted(os.listdir(pdir)):
            rels += [f"profiles/{pid}/subscribes", f"profiles/{pid}/providers"]
    for rel in rels:
        rm_children(rel)
    ss = os.path.join(data, "sub-store", "sub-store.json")
    if os.path.isfile(ss):
        d = json.load(open(ss, encoding="utf-8"))
        for s in d.get("subs") or []:
            if isinstance(s, dict) and s.get("url"):
                s["url"] = m.get("subscription_url") or f"http://{test_net(0)}/disabled"
                log.add("Sub-Store subscription URL → lab")
        for k, empty in (("artifacts", []), ("tokens", []), ("settings", {}), ("archives", [])):
            if d.get(k):
                d[k] = empty
                log.add(f"Sub-Store {k} cleared")
        if not dry:
            json.dump(d, open(ss, "w", encoding="utf-8"), ensure_ascii=False)
    root = os.path.join(data, "sub-store", "root.json")
    if os.path.isfile(root):
        d = json.load(open(root, encoding="utf-8"))
        for k in list(d):
            if k.startswith("sub-store-cached"):
                d[k] = "{}"
                log.add("Sub-Store caches cleared")
        if not dry:
            json.dump(d, open(root, "w", encoding="utf-8"), ensure_ascii=False)


def legacy_views(rel, d):
    """Map a legacy JSON file onto (scope, owner, field) keys so RULES can run on it in place."""
    v = {}
    if not isinstance(d, dict):
        return v
    if rel in ("config.json", "config.json.bak"):
        for scope in ("system", "shared"):
            for k, val in (d.get(scope) or {}).items():
                v[(scope, "", k)] = val
        for pid, prof in (d.get("profiles") or {}).items():
            if isinstance(prof, dict):
                for k, val in prof.items():
                    v[("profile", pid, k)] = val
    elif rel in ("system.json", "system.json.bak"):
        for k in ("agents", "system_config", "backup"):
            if k in d:
                v[("system", "", k)] = d[k]
    elif re.fullmatch(r"profiles/[^/]+/config\.json(\.bak)?", rel):
        pid = rel.split("/")[1]
        for k, val in d.items():
            scope = "shared" if k in ("subscriptions", "nodes", "subscription_aggregations", "rule_library") else "profile"
            v[(scope, "" if scope == "shared" else pid, k)] = val
    return v


def files_legacy(data, m, log, dry):
    """Legacy JSON copies (read only to rebuild a corrupt database, and only when their
    SHA256 matches the recorded one) get the same rules, so no production target survives."""
    names = ["config.json", "config.json.bak", "system.json", "system.json.bak"]
    pdir = os.path.join(data, "profiles")
    if os.path.isdir(pdir):
        for pid in sorted(os.listdir(pdir)):
            names += [f"profiles/{pid}/config.json", f"profiles/{pid}/config.json.bak"]
    for rel in names:
        path = os.path.join(data, rel)
        if not os.path.isfile(path):
            continue
        try:
            d = json.load(open(path, encoding="utf-8"))
        except (ValueError, OSError):
            log.add(f"legacy file unreadable, left as is: {rel}")
            continue
        sub = Log()
        views = legacy_views(rel, d)
        for rule in RULES:
            rule(views, m, sub)
        if sub.items:
            log.add(f"legacy rules applied: {rel}")
            if not dry:
                tmp = path + ".l6tmp"
                json.dump(d, open(tmp, "w", encoding="utf-8"), ensure_ascii=False, indent=2)
                os.replace(tmp, path)


def state_files(data):
    for top, dirs, files in os.walk(data):
        rel = os.path.relpath(top, data)
        dirs[:] = [d for d in dirs if os.path.normpath(os.path.join(rel, d)) not in SKIP_DIRS]
        for name in files:
            if name.endswith(STATE_SUFFIXES):
                yield os.path.join(top, name)


def files_replace(data, pairs, log, dry):
    if not pairs:
        return
    compiled = compile_pairs(pairs)
    for path in state_files(data):
        try:
            text = open(path, encoding="utf-8").read()
        except (UnicodeDecodeError, OSError):
            continue
        out = apply_pairs(compiled, text)
        if out != text:
            log.add(f"replace in file {os.path.relpath(path, data)}")
            if not dry:
                tmp = path + ".l6tmp"
                open(tmp, "w", encoding="utf-8").write(out)
                os.replace(tmp, path)


# ---------------------------------------------------------------- commands

LAB_SLOTS = {  # L6 guests by role (pve/lab.conf)
    ("mihomo", "shell"): ["10.97.0.152", "10.97.0.153"],
    ("oxidns", "shell"): ["10.97.0.154", "10.97.0.155"],
    ("oxidns", "docker"): ["10.97.0.158", "10.97.0.159"],
}
LAB_VIPS = {"shell": "10.97.0.249", "docker": "10.97.0.248"}


def auto_fill(doc, agents, routers, sc):
    """Map Agents onto the L6 guests by service type and install method, HA pairs first,
    and fill "replace" with every production address that moves (Agents, VIPs, panel URL)."""
    used = {}
    order = sorted(agents, key=lambda a: (not (a.get("oxidns_ha") or {}).get("enabled"),
                                          (a.get("oxidns_ha") or {}).get("role") != "master"))
    for i, a in enumerate(order):
        method = "docker" if (a.get("deployment_method") or "").startswith("docker") else "shell"
        slots = LAB_SLOTS.get((a.get("service_type"), method), [])
        k = (a.get("service_type"), method)
        n = used.get(k, 0)
        old = a.get("host") or ""
        if n < len(slots):
            used[k] = n + 1
            doc["agents"][a["id"]] = {"host": slots[n], "port": a.get("port") or 8082}
            new = slots[n]
        else:
            new = test_net(i)
        if old and old != new:
            doc["replace"][old] = new
        vip = (a.get("oxidns_ha") or {}).get("vip")
        if vip:
            doc["replace"][vip] = LAB_VIPS[method]
    for i, r in enumerate(routers):
        if i == 0 and (r.get("type") or "").lower() in ("routeros", "ros", "mikrotik"):
            doc["routers"][r["id"]] = "lab"
            old = host_of(r.get("endpoint") or "")
            if old:
                doc["replace"][old] = "10.97.0.156"
    server = (sc.get("server_domain") or "").rstrip("/")
    if server:
        doc["replace"][server] = doc["panel_url"]
        bare = host_of(server)
        if bare and bare != server:
            doc["replace"].setdefault(bare, host_of(doc["panel_url"]))
    # hosts that only need to disappear (they also sit in cached artifacts: rule-sync
    # receipts, previews, the HA payload cache): map them to never-resolving names
    for e in doc.get("_deny") or []:
        if e["kind"] in ("subscription host", "Sub-Store subscription host", "sub_store_url",
                         "WebDAV host", "rule_fetch_proxy") and e["value"] not in doc["replace"]:
            slug = re.sub(r"[^a-z]+", "-", e["kind"].lower()).strip("-")
            doc["replace"][e["value"]] = f"l6-{slug}-{len(doc['replace'])}.invalid"
    unknown = [a.get("id") for a in agents if doc["agents"].get(a.get("id")) == "disable"]
    doc["_auto"] = {"mapped": sum(1 for v in doc["agents"].values() if isinstance(v, dict)),
                    "disabled": len(unknown)}


def deny_targets(rows, data):
    """Production targets that must not survive: every address the panel or a device would connect to."""
    out = {}

    def add(v, kind):
        h = host_of(v) if "://" in (v or "") else (v or "")
        h = h.strip()
        if h and h not in ("127.0.0.1", "localhost", "0.0.0.0") and not h.startswith(TEST_NET):
            out.setdefault(h, kind)
    for a in rows.get(("system", "", "agents"), ([],))[0] or []:
        add(a.get("host"), "agent address")
        add((a.get("oxidns_ha") or {}).get("vip"), "HA VIP")
    for (scope, _, field), (value, _, _) in rows.items():
        if scope == "profile" and field == "routers" and isinstance(value, dict):
            for r in value.get("items") or []:
                add(r.get("endpoint"), "router endpoint")
        if scope == "profile" and field == "oxidns" and isinstance(value, dict):
            for c in (value, value.get("template_settings") or {}):
                for plugin in ("ros_address_list", "ros_route"):
                    add(((c.get(plugin) or {}).get("args") or {}).get("address"), "OxiDNS ROS plugin address")
    for s_ in rows.get(("shared", "", "subscriptions"), ([],))[0] or []:
        add(s_.get("url"), "subscription host")
    b = rows.get(("system", "", "backup"), ({},))[0] or {}
    add(b.get("webdav_url"), "WebDAV host")
    sc = rows.get(("system", "", "system_config"), ({},))[0] or {}
    for k in ("server_domain", "sub_store_url", "rule_fetch_proxy"):
        add(sc.get(k), k)
    ss = os.path.join(data, "sub-store", "sub-store.json")
    if os.path.isfile(ss):
        for s_ in json.load(open(ss, encoding="utf-8")).get("subs") or []:
            if isinstance(s_, dict):
                add(s_.get("url"), "Sub-Store subscription host")
    return out


def cmd_template(args):
    data = guard_copy(args.data)
    conn = sqlite3.connect(f"file:{os.path.join(data, DB)}?mode=ro", uri=True)
    rows = load_rows(conn)
    agents = rows.get(("system", "", "agents"), ([],))[0] or []
    routers = []
    for (scope, owner, field), (value, _, _) in rows.items():
        if scope == "profile" and field == "routers" and isinstance(value, dict):
            for r in value.get("items") or []:
                routers.append({"profile": owner, "id": r.get("id"), "name": r.get("name"),
                                "type": r.get("type"), "endpoint": r.get("endpoint")})
    sc = rows.get(("system", "", "system_config"), ({},))[0] or {}
    doc = {
        "_note": "PRIVATE: holds production addresses. Keep next to the copy; never commit or paste.",
        "panel_url": "http://10.97.0.150",
        "lab_router": {"endpoint": "https://10.97.0.156", "username": "cf", "password": "", "verify_tls": False},
        "agents": {a.get("id"): "disable" for a in agents},
        "_agents_info": [{"id": a.get("id"), "name": a.get("name"), "type": a.get("service_type"),
                          "method": a.get("deployment_method"), "host": a.get("host"), "port": a.get("port"),
                          "ha": (a.get("oxidns_ha") or {}).get("vip")} for a in agents],
        "routers": {r["id"]: "disable" for r in routers},
        "_routers_info": routers,
        "_panel_server_domain": sc.get("server_domain"),
        "replace": {},
        "subscription_url": "http://10.97.0.151:8090/sub/lab.yaml",
        "webdav": None,
        "manual_nodes": "synthetic",
        "easytier": "disable",
    }
    doc["_deny"] = [{"value": v, "kind": k} for v, k in sorted(deny_targets(rows, data).items())]
    if args.auto:
        auto_fill(doc, agents, routers, sc)
    fd = os.open(args.map, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as f:
        json.dump(doc, f, ensure_ascii=False, indent=1)
    print(f"wrote {args.map} (mode 600): {len(agents)} agents, {len(routers)} routers; edit, then run apply")


def cmd_apply(args):
    data = guard_copy(args.data)
    m = json.load(open(args.map, encoding="utf-8"))
    log = Log()
    db = os.path.join(data, DB)
    conn = sqlite3.connect(db, isolation_level=None)
    conn.execute("PRAGMA foreign_keys=ON")
    conn.execute("BEGIN IMMEDIATE")
    rows = load_rows(conn)
    doc = {k: json.loads(v[1]) for k, v in rows.items()}
    for rule in RULES:
        rule(doc, m, log)
    pairs = m.get("replace") or {}
    if pairs:
        doc = {k: walk_strings(v, replacer(pairs, log, f"{k[0]}.{k[2]}")) for k, v in doc.items()}
    changed = [k for k in doc if dumps(doc[k]) != rows[k][1]]
    gen = json.loads(conn.execute("SELECT value FROM meta WHERE key='generation'").fetchone()[0])
    if changed and not args.dry_run:
        new_gen = gen + 1
        for k in changed:
            conn.execute("UPDATE fields SET value=?, revision=? WHERE scope=? AND owner=? AND field=?",
                         (dumps(doc[k]), new_gen, *k))
        conn.execute("INSERT OR REPLACE INTO meta (key, value) VALUES ('generation', ?)", (dumps(new_gen),))
        conn.execute("INSERT OR REPLACE INTO meta (key, value) VALUES ('commit_id', ?)", (dumps(uuid.uuid4().hex),))
        conn.execute("COMMIT")
    else:
        conn.execute("ROLLBACK")
    ok = conn.execute("PRAGMA integrity_check").fetchone()[0]
    conn.close()
    files_cleanup(data, m, log, args.dry_run)
    files_legacy(data, m, log, args.dry_run)
    files_replace(data, pairs, log, args.dry_run)
    summary = {"dry_run": args.dry_run, "changed_fields": sorted(f"{s}.{f}" for s, _, f in changed),
               "generation": gen if args.dry_run or not changed else gen + 1,
               "integrity_check": ok, "actions": log.items}
    text = json.dumps(summary, ensure_ascii=False, indent=1)
    print(text)
    if not args.dry_run:
        open(os.path.join(data, ".l6-sanitize.json"), "w", encoding="utf-8").write(text + "\n")
    if ok != "ok":
        sys.exit(1)


def is_private_outside_lab(s):
    try:
        ip = ipaddress.ip_address(s)
    except ValueError:
        return False
    return ip not in LAB_NET and any(ip in n for n in PRIVATE)


def cmd_scan(args):
    data = os.path.realpath(args.data)
    m = json.load(open(args.map, encoding="utf-8"))
    entries = m.get("_deny") or []
    kinds = {e["value"]: e["kind"] for e in entries}
    deny = [(rx, kinds[v]) for v, (rx, _) in zip(sorted(kinds, key=lambda x: -len(x)),
                                                compile_pairs({v: "" for v in kinds}))]
    hits, private = {}, {}

    def note(table, where, path):
        key = f"{where}\t{re.sub(r'[0-9a-f]{12,}|agent_[0-9]+_[0-9a-f]+', '<id>', path)}"
        table[key] = table.get(key, 0) + 1

    def check(where, path, s):
        for rx, kind in deny:
            if rx.search(s):
                note(hits, f"{kind}\t{where}", path)
                if args.values:
                    print(f"VALUE\t{where}\t{path}\t{rx.pattern}")
        for ip in IPV4.findall(s):
            if is_private_outside_lab(ip):
                note(private, where, path)

    def walk(where, value, path="$"):
        if isinstance(value, dict):
            for k, v in value.items():
                walk(where, v, f"{path}.{k}")
        elif isinstance(value, list):
            for v in value:
                walk(where, v, f"{path}[]")
        elif isinstance(value, str):
            check(where, path, value)

    conn = sqlite3.connect(f"file:{os.path.join(data, DB)}?mode=ro", uri=True)
    for (scope, owner, field), (value, _, _) in load_rows(conn).items():
        walk(f"db:{scope}.{field}", value)
    for path in state_files(data):
        try:
            text = open(path, encoding="utf-8").read()
        except (UnicodeDecodeError, OSError):
            continue
        check(f"file:{os.path.relpath(path, data)}", "", text)
    print(f"# private IPv4 outside the lab net (info: LAN ranges in rules, client lists, old records): "
          f"{sum(private.values())} in {len(private)} places")
    if args.verbose:
        for key in sorted(private):
            print(f"{private[key]:5d}  info  {key}")
    print(f"# production targets from the map's _deny list ({len(deny)}) still present: {sum(hits.values())}")
    for key in sorted(hits):
        print(f"{hits[key]:5d}  FAIL  {key}")
    sys.exit(1 if hits else 0)


def main():
    p = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    sub = p.add_subparsers(dest="cmd", required=True)
    t = sub.add_parser("template"); t.add_argument("data"); t.add_argument("map")
    t.add_argument("--auto", action="store_true", help="map Agents onto the L6 guests by type and fill replace")
    a = sub.add_parser("apply"); a.add_argument("data"); a.add_argument("map"); a.add_argument("--dry-run", action="store_true")
    s = sub.add_parser("scan"); s.add_argument("data"); s.add_argument("map")
    s.add_argument("--verbose", action="store_true", help="list the private-address places too")
    s.add_argument("--values", action="store_true", help="print which target matched (operator terminal only)")
    args = p.parse_args()
    {"template": cmd_template, "apply": cmd_apply, "scan": cmd_scan}[args.cmd](args)


if __name__ == "__main__":
    main()
