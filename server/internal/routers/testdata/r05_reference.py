"""Run the public Python iKuai driver against the Go loopback iKuai fake (R05 boundary corpus).

Input (stdin): {"action", "state", ...}; output (stdout): {"result", "error", "records", "rollback"}.
Undo records are serialised when the driver returns them, as the Go driver returns them; the
rollback then runs on the live objects, as backend/routers/release.py does.
"""
import copy
import json
import os
import sys
from contextlib import nullcontext
from dataclasses import fields, is_dataclass
from unittest import mock

import requests

from backend.routers import ikuai as ikuai_module
from backend.routers.base import RouterError
from backend.routers.ikuai import IKuaiDriver, decode_value, oversized_requests, request_bytes

endpoint = os.environ["R05_ENDPOINT"]
case = json.load(sys.stdin)
if "state" in case:
    requests.post(endpoint + "/__r05/reset", json=case["state"], timeout=5).raise_for_status()
driver = IKuaiDriver(endpoint + case.get("path", ""), case.get("username", "synthetic-admin"),
                     case.get("password", "synthetic-secret"), verify_tls=False)
outcome = {"result": None, "error": None, "records": [], "rollback": []}


def jsonable(value):
    if is_dataclass(value):
        return {field.name: jsonable(getattr(value, field.name)) for field in fields(value)
                if not callable(getattr(value, field.name))}
    if isinstance(value, dict):
        return {key: jsonable(item) for key, item in value.items() if not callable(item)}
    if isinstance(value, (list, tuple)):
        return [jsonable(item) for item in value]
    return value


def group_view(group):
    return {'id': group['id'], 'gid': group['gid'], 'name': group['name'], 'comment': group['comment'],
            'values': group['values'], 'entries': group['entries'], 'refs': group['refs'],
            'role': group['role'] or '', 'index': group['index'] or 0, 'users': group['users'],
            'dup': bool(group.get('dup')), 'adopted': bool(group.get('adopted'))}


def state_view(state):
    groups = state.ikuai_groups
    state.ikuai_groups = None
    view = {'state': jsonable(state), 'groups': None}
    if groups is not None:
        view['groups'] = {
            'groups': [group_view(group) for group in groups['groups']],
            'rules': [{'key': key, 'func': rule['func'], 'id': rule['id'], 'comment': rule['comment'],
                       'enabled': rule['enabled'], 'action': rule['action'] if isinstance(rule['action'], str) else '',
                       'custom': rule['custom'], 'objects': rule['objects'], 'src_objects': rule['src_objects']}
                      for key, rule in groups['rules'].items()]}
    return view


def error_of(error):
    return {"class": error.__class__.__name__, "message": str(error),
            "undo": copy.deepcopy(getattr(error, "undo", None))}


def release():
    """Apply ops (in one batch unless "batch" is false) like release._apply_stage; on failure
    roll back every recorded undo in reverse inside a rollback batch, like release._rollback."""
    live = []
    failed = None
    try:
        with driver.batch() if case.get("batch", True) else nullcontext():
            for op in case["ops"]:
                try:
                    undo = driver.apply(op)
                except RouterError as error:
                    if getattr(error, "undo", None):
                        live.append(error.undo)
                        outcome["records"].append({"undo": copy.deepcopy(error.undo), "ok": False})
                    raise
                live.append(undo)
                outcome["records"].append({"undo": copy.deepcopy(undo), "ok": True})
    except RouterError as error:
        failed = error
        outcome["error"] = error_of(error)
    if failed is None and not case.get("always_rollback"):
        return None
    try:
        with driver.batch(rollback=True):
            for undo in reversed([undo for undo in live if undo]):
                try:
                    outcome["rollback"].append({"ok": jsonable(driver.apply(undo))})
                except RouterError as error:
                    outcome["rollback"].append({"error": error_of(error)})
    except RouterError as error:
        outcome["rollback"].append({"batch_error": error_of(error)})
    return None


def transport():
    """The message of each requests failure class (the Go side injects the same classes)."""
    messages = {}
    for name in case["classes"]:
        exception = {"SSLError": requests.exceptions.SSLError, "ConnectTimeout": requests.ConnectTimeout,
                     "ReadTimeout": requests.ReadTimeout, "ConnectionError": requests.ConnectionError,
                     "ChunkedEncodingError": requests.exceptions.ChunkedEncodingError,
                     "InvalidURL": requests.exceptions.InvalidURL}[name]
        local = IKuaiDriver("http://192.0.2.1", "synthetic-admin", "synthetic-secret")
        local._logged_in = True
        with mock.patch.object(local.session, "post", side_effect=exception("synthetic-secret-detail")):
            try:
                local._write("nat_rule", "edit", {"id": 1}, case.get("timeout"))
            except RouterError as error:
                messages[name] = error_of(error)
    return messages


def run():
    action = case["action"]
    if action == "probe":
        return jsonable(driver.probe())
    if action == "read_state":
        return state_view(driver.read_state())
    if action == "read_dhcp_dns":
        return jsonable(driver.read_dhcp_dns())
    if action == "apply":
        return driver.apply(case["op"])
    if action == "roundtrip":
        undo = driver.apply(case["op"])
        return {"undo": copy.deepcopy(undo), "restored": driver.apply(undo)}
    if action == "release":
        return release()
    if action == "call":
        return driver.call(case["func"], case["param"])
    if action == "list_all":
        return driver._list_all(case["func"], case.get("prefix", ""), case.get("ids", True))
    if action == "write":
        return driver._write(case["func"], case["write_action"], case["param"])
    if action == "write_twice":
        first = driver._write(case["func"], case["write_action"], case["param"])
        return [first, driver._write(case["func"], case["write_action"], case["param2"])]
    if action == "transport":
        return transport()
    if action == "values":
        return {"decoded": [decode_value(value, key) for key, value in case["values"]],
                "tagnames": [ikuai_module._tagname(kind, key) for kind, key in case.get("tags", [])]}
    if action == "oversized":
        return oversized_requests(case["ops"])
    if action == "request_bytes":
        return [request_bytes(item["func"], item["action"], item["param"]) for item in case["items"]]
    raise ValueError("unknown action: " + action)


try:
    outcome["result"] = run()
except RouterError as error:
    outcome["error"] = error_of(error)
except Exception as error:  # the comparison records Python crashes on malformed device data
    outcome["error"] = {"class": error.__class__.__name__, "message": str(error), "undo": None}
finally:
    driver.close()
print(json.dumps(outcome, ensure_ascii=False, separators=(",", ":")))
