"""Real Python manual editors against the same loopback device as Go."""
import json
import os
import sys
import uuid

from backend.routers import route_editor, routeros_routes, ikuai_routes
from backend.routers.routeros import RouterOSDriver
from backend.routers.ikuai import IKuaiDriver

case = json.load(sys.stdin)
endpoint = os.environ['R06_ENDPOINT']
# Deterministic UUID; timeout cases shorten only the real transport deadline.
# Every request and compensation still uses the real editor.
ikuai_routes.uuid = type('FixedUUID', (), {'uuid4': staticmethod(lambda: uuid.UUID('0123456789abcdef0123456789abcdef'))})
vendor = case['vendor']
driver = (RouterOSDriver(endpoint, 'synthetic-admin', 'synthetic-secret', verify_tls=False)
          if vendor == 'routeros' else IKuaiDriver(endpoint, 'synthetic-admin', 'synthetic-secret', verify_tls=False))
if case.get('timeout'):
    request = driver.session.request
    driver.session.request = lambda method, url, **kwargs: request(method, url, **{**kwargs, 'timeout': (0.2, 0.2)})
adapter = routeros_routes if vendor == 'routeros' else ikuai_routes
outcome = {'result': None, 'error': None, 'records': []}
try:
    if case['action'] == 'normalize':
        outcome['result'] = route_editor.normalize_request(case.get('payload'))
    else:
        rows = adapter.read_routes(driver)
        if case['action'] == 'read':
            outcome['result'] = rows
        elif case['action'] == 'checked_read':
            outcome['result'] = route_editor.read_routes(driver)
        elif case['action'] == 'plan':
            payload = dict(case['payload'])
            if payload['action'] != 'add':
                payload.update(id=rows[0]['id'], revision=route_editor.public_route(rows[0])['revision'])
            outcome['result'] = route_editor.build_route_plan(case['router'], rows, payload)
        else:
            expected = rows[0] if case['action'] != 'add' else None
            if expected and case.get('expected_change'):
                expected.update(case['expected_change'])
            result = adapter.mutate_route(driver, case['action'], expected, case.get('values'))
            result.pop('id', None)  # private iKuai-only return key is not used by execute_route_plan
            outcome['result'] = result
except Exception as error:
    outcome['error'] = {'class': type(error).__name__, 'message': str(error)}
finally:
    driver.close()
print(json.dumps(outcome, ensure_ascii=False, separators=(',', ':')))
