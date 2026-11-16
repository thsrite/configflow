"""Run the public Python RouterOS driver against the Go loopback REST fake."""
import json
import os
import sys
from dataclasses import fields, is_dataclass

import requests

from backend.routers.routeros import RouterOSDriver


endpoint = os.environ["R04_ENDPOINT"]
case = json.load(sys.stdin)
state = case.get("state", {})
requests.post(endpoint + "/__r04/reset", json=state, timeout=5).raise_for_status()
driver = RouterOSDriver(endpoint, "synthetic-user", "synthetic-password", verify_tls=False)
records = []
outcome = {"result": None, "records": records, "error": None}


def jsonable(value):
    if is_dataclass(value):
        return {field.name: jsonable(getattr(value, field.name)) for field in fields(value)
                if not callable(getattr(value, field.name))}
    if isinstance(value, dict):
        return {key: jsonable(item) for key, item in value.items() if not callable(item)}
    if isinstance(value, (list, tuple)):
        return [jsonable(item) for item in value]
    return value


def run():
    action = case["action"]
    if action == "probe":
        return jsonable(driver.probe())
    if action == "read_state":
        return jsonable(driver.read_state())
    if action == "read_dhcp_dns":
        return jsonable(driver.read_dhcp_dns())
    if action == "ping":
        return driver.ping(case.get("address", "192.0.2.44"), case.get("interface", ""), timeout=0.5)
    if action == "apply":
        return driver.apply(case["op"])
    if action == "roundtrip":
        undo = driver.apply(case["op"])
        restored = driver.apply(undo)
        return {"undo": undo, "restored": restored}
    if action == "apply_many":
        driver.apply_many(case["ops"], lambda op, undo: records.append({"op": op, "undo": undo}))
        return {"records": records}
    if action == "batch_roundtrip":
        driver.apply_many(case["ops"], lambda op, undo: records.append({"op": op, "undo": undo}))
        errors = driver.restore_many([row["undo"] for row in reversed(records)], case.get("stop", False))
        return {"records": records, "restore_errors": errors}
    if action == "restore_many":
        return driver.restore_many(case["undos"], case.get("stop", False))
    raise ValueError("unknown action: " + action)


try:
    outcome["result"] = run()
except Exception as error:  # boundary comparison records user-visible driver failures
    outcome["error"] = {"class": error.__class__.__name__, "message": str(error)}
finally:
    driver.close()
print(json.dumps(outcome, ensure_ascii=False, separators=(",", ":")))
