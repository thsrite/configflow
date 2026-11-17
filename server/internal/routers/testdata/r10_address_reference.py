"""Stable interface evidence uses real ipaddress, including host bits and scopes."""
import json
import sys
from backend.routers.ipv6_fakeip import stable
print(json.dumps([stable(row) for row in json.load(sys.stdin)], ensure_ascii=False))
