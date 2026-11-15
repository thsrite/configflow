import sys
from datetime import datetime, timedelta
for h in (10**10, 10**12, 2**31*24, 24*999999999, 24*1000000000, 10**30, -10**10, 24*740000, 24*739000):
    try:
        timedelta(hours=h); d = datetime.now() - timedelta(hours=h); print(h, 'ok', d)
    except Exception as e: print(h, type(e).__name__, e)
for s in ('9'*4300, '9'*4301, '0'*4301+'1', '1_'*2200+'1', '-'+'9'*4300, ' '+'9'*4301):
    try: int(s); print(len(s), 'ok')
    except Exception as e: print(len(s), type(e).__name__, str(e)[:80])
for c in ('abc', ['x'], None, 5, 1.5, True):
    try: 'usage_percent' in c; print(repr(c), 'in ok')
    except Exception as e: print(repr(c), e)
for c in ('xusage_percentx', ['usage_percent']):
    try: c['usage_percent']
    except Exception as e: print(repr(c), e)
for v in ([1,'a'], [1.5,'a'], [None], [{}], [[1]], [10**400, 1.5], [1.5, 10**400]):
    try: print(sum(v))
    except Exception as e: print(v if len(str(v))<50 else 'big', type(e).__name__, e)
try: print((10**400)/3)
except Exception as e: print(e)
for v in ([True], [1, 1.0], [1.0, 1], [True, 1], [float('nan'), 1, 2], [1, float('nan'), 2]):
    print(v, max(v), min(v), sum(v)/len(v))
import os
print(sys.version)
