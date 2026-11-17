"""PyYAML oracle for the native facade's synthetic boundary tests.

Input and output are JSON arrays. Only synthetic documents are supplied; no
application imports or stored/production data are read.
"""

import datetime
import json
import sys

import yaml


def encode(value):
    if isinstance(value, (datetime.date, datetime.datetime)):
        return value.isoformat()
    raise TypeError(type(value).__name__)


results = []
for source in json.load(sys.stdin):
    try:
        value = yaml.safe_load(source)
        # Round-trip through JSON to give dates a documented, explicit form.
        results.append({"value": json.loads(json.dumps(value, default=encode))})
    except yaml.YAMLError:
        results.append({"rejected": True})
json.dump(results, sys.stdout, ensure_ascii=False, allow_nan=False)
