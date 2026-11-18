"""Use real Python lifecycle business on the approved model consumption view.

The original review archive is immutable. _write still runs the original Python
implementation on the original file so unowned members are not normalized by
our consumption projection. No Go result is read by this generator.
"""
import contextlib
import hashlib
import io
import json
import math
import pathlib
import tempfile
from unittest import mock

with contextlib.redirect_stdout(io.StringIO()):
    from backend.utils import sub_store_runtime as runtime

root = pathlib.Path(__file__).parent
source = (root / "review_reference.json").read_bytes()
rows = json.loads(source)["runtime"]


class Number(str):
    pass


def project(original, tokens):
    result = dict(original)
    if "state" in result:
        token = tokens["state"]
        result["state"] = str(token) if isinstance(token, Number) else token if isinstance(token, str) else None
    if "error" in result:
        token = tokens["error"]
        result["error"] = str(token) if isinstance(token, Number) else token if isinstance(token, str) else ""
    return result


def finite_timestamp(value):
    if not isinstance(value, (int, float)) or isinstance(value, bool):
        return False
    try:
        return math.isfinite(float(value))
    except (ValueError, OverflowError):
        return False


def record(row):
    content = row["state"].encode("latin-1")
    try:
        original = json.loads(content)
    except (ValueError, UnicodeError):
        original = {}
    if not isinstance(original, dict):
        return {"error": "*substore.RuntimeStateError", "message": "Sub-Store 运行状态必须是 JSON 对象", "commands": [], "after": row["state"]}
    tokens = json.loads(content, parse_int=Number, parse_float=Number) if original else {}
    view = project(original, tokens)
    if row["action"] == "tick" and view.get("state") != "sleeping" and "last_used" in original and not finite_timestamp(original["last_used"]):
        return {"error": "*substore.RuntimeStateError", "message": "Sub-Store 运行状态的 last_used 必须是有限数字", "commands": [], "after": row["state"]}
    with tempfile.TemporaryDirectory() as directory:
        path = pathlib.Path(directory) / "state.json"
        path.write_bytes(content)
        owner = runtime.SubStoreRuntime(directory=directory, idle_seconds=300)
        original_read, original_write = owner._read, owner._write
        commands = []

        def read():
            value = original_read()
            try:
                tokens = json.loads(path.read_bytes(), parse_int=Number, parse_float=Number)
            except (ValueError, UnicodeError):
                tokens = {}
            return project(value, tokens)

        def write(**changes):
            # Reference the real writer over the real original file, as R2
            # requires when changing only owned members.
            with mock.patch.object(owner, "_read", original_read):
                original_write(**changes)

        def command(action):
            commands.append(action)
            return "RUNNING" if action == "status" else ""

        with mock.patch.object(owner, "_read", read), mock.patch.object(owner, "_write", write), mock.patch.object(owner, "_command", command), mock.patch.object(owner, "_ready", lambda: row["running"]), mock.patch.object(runtime, "get_mode", lambda: "on_demand"), mock.patch.object(runtime.time, "monotonic", lambda: 1000.0), mock.patch("backend.utils.dependencies.is_builtin_available", lambda: True):
            if row["action"] == "tick":
                output = owner.tick()
            elif row["action"] == "status":
                output = owner.status(row["running"])
            else:
                with owner.use():
                    pass
                output = None
        return {"output": output, "commands": commands, "after": path.read_bytes().decode("latin-1")}

expectations = [record(row) for row in rows]
# These are literal bytes produced by the original Python writer, not a
# re-created Go formatting golden. Unknown field and number text are included.
with tempfile.TemporaryDirectory() as directory:
    owner = runtime.SubStoreRuntime(directory=directory)
    with mock.patch.object(owner, "_read", lambda: {"state": "failed", "error": "旧状态", "last_used": 123.5, "future": {"number": 9007199254740993123, "text": "保留"}}):
        owner._write()
    old_bytes = (pathlib.Path(directory) / "state.json").read_text()
result = {"source_sha256": hashlib.sha256(source).hexdigest(), "expectations": expectations, "old_python_bytes": old_bytes}
(root / "n89_runtime_reference.json").write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
print("native runtime reference:", len(expectations), "exact real-Python cases")
