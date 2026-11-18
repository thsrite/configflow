"""Wire-protocol oracle, using unmodified itsdangerous serialization."""
import json
import sys
from unittest.mock import patch
from itsdangerous import URLSafeTimedSerializer, BadSignature
case = json.load(sys.stdin)
signer = URLSafeTimedSerializer(case['secret'], salt='client-fixed-ip-v1')
with patch('itsdangerous.timed.TimestampSigner.get_timestamp', lambda _: case['now']):
    token = signer.dumps(case['payload'])
    try:
        decoded = signer.loads(case['token'], max_age=600) if case.get('token') else None
        rejected = False
    except BadSignature:
        decoded, rejected = None, True
print(json.dumps({'token': token, 'decoded': decoded, 'rejected': rejected}, ensure_ascii=False))
