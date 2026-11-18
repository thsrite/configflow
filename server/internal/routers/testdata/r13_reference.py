"""R13 normal and error-class summaries from the real Python owner."""
import json
import sys
from backend.routers.pending import summarize
cases=json.load(sys.stdin)
print(json.dumps([summarize(c['report'],c['plan']) for c in cases],ensure_ascii=False))
