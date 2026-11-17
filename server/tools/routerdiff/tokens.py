"""Inspect Python's documented signed preview payload with its actual library.

This is a reference-output/privacy assertion, not an implementation of the
application or a requirement on Go's opaque token encoding. Own-side public
apply/tamper/restart/real-age requests test acceptance on both sides.
"""
import json
import re

from itsdangerous import BadData, URLSafeTimedSerializer

from seed import DEVICE_PASSWORD


def inspect_python_token(token, profile, secret):
    if not isinstance(token, str) or not 0 < len(token) <= 1000000:
        raise ValueError('invalid reference preview token length/type')
    signer = URLSafeTimedSerializer(secret or 'synthetic-inspection-key', salt='client-fixed-ip-v1')
    try:
        # A random startup secret is deliberately not exposed by the server.
        # In that world only inspect the payload; public apply authenticates it.
        payload = signer.loads(token, max_age=600) if secret else signer.loads_unsafe(token)[1]
    except BadData:
        raise ValueError('reference preview signature/payload is invalid') from None
    if not isinstance(payload, dict) or set(payload) != {'profile_id', 'intent', 'operations'} or payload['profile_id'] != profile:
        raise ValueError('reference preview payload has invalid scope or members')
    if DEVICE_PASSWORD in json.dumps(payload, ensure_ascii=False):
        raise ValueError('reference preview contains the router password')
    intent, operations = payload['intent'], payload['operations']
    if not isinstance(intent, dict) or set(intent) != {'operation', 'devices'} or intent['operation'] not in ('add', 'delete'):
        raise ValueError('reference preview intent shape is invalid')
    devices = intent['devices']
    if not isinstance(devices, list) or not 1 <= len(devices) <= 64 or not isinstance(operations, list) or len(operations) != len(devices):
        raise ValueError('reference preview operation count is invalid')
    for device, operation in zip(devices, operations):
        if not isinstance(operation, dict) or set(operation) != {'router_id', 'connection', 'op'}:
            raise ValueError('reference preview operation shape is invalid')
        if not isinstance(operation['connection'], str) or not re.fullmatch('[0-9a-f]{64}', operation['connection']):
            raise ValueError('reference preview connection is not a digest')
        if operation['router_id'] != device['router_id'] or operation['op']['ip'] != device['ip']:
            raise ValueError('reference preview actions changed device order/identity')
