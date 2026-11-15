"""Golden for _registration_log_fields: run from the repository root with the
test venv (rt -- bash -lc '~/cf-test/.venv/bin/python
server/internal/api/agents/testdata/gen_registration_log_fields.py')."""
import json, os, sys
sys.path.insert(0, os.getcwd())
from backend.routes.agents import _registration_log_fields

values = [
    "plain", "", " leading space", "a nbsp", "zero​width", "bom﻿x", "line sep", "para x",
    "tab\tx", "nl\nx", "cr\rx", "del\x7fx", "c1\x85x", "ideo　space", "emoji😀🔐", "cjk 名称",
    "ext-h \U00031350", "kawi \U00011f00", "unassigned \U000e0080", "private ", "soft­hyphen",
    "combining é", "rtl ‮", "x" * 200, "名" * 200, "😀" * 130,
    1, -5, 0, 2 ** 70, 1.5, 1e100, -0.0, 1.0, 3.14159265358979, 1e-7, 123456789012345678.0,
    True, False, None, [1], {"a": 1},
]
cases = []
for value in values:
    data = {"name": value, "host": value, "port": value, "service_type": value, "version": value, "other": value}
    cases.append({"input": data, "output": _registration_log_fields(data)})
with open(os.path.join(os.path.dirname(__file__), "registration_log_fields.json"), "w", encoding="utf-8") as f:
    json.dump(cases, f, ensure_ascii=True, indent=1)
    f.write("\n")
