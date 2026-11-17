"""Signed reference-token privacy checks must fail on leaked credentials."""
import copy
import unittest

from itsdangerous import URLSafeTimedSerializer

from seed import DEVICE_PASSWORD, JWT_SECRET
from tokens import inspect_python_token


class TokenTests(unittest.TestCase):
    def test_real_serializer_validity_shape_and_privacy(self):
        payload = {'profile_id': 'alpha', 'intent': {'operation': 'add', 'devices': [{'ip': '10.44.0.21', 'router_id': 'r-ros'}]},
                   'operations': [{'router_id': 'r-ros', 'connection': 'f' * 64, 'op': {'ip': '10.44.0.21'}}]}
        signer = URLSafeTimedSerializer(JWT_SECRET, salt='client-fixed-ip-v1')
        inspect_python_token(signer.dumps(payload), 'alpha', JWT_SECRET)
        inspect_python_token(signer.dumps(payload), 'alpha', None)
        for mutate, reason in [
            (lambda item: item.update(profile_id='beta'), 'invalid scope'),
            (lambda item: item.update(password=DEVICE_PASSWORD), 'invalid scope'),
            (lambda item: item['operations'][0]['op'].update(leaked=DEVICE_PASSWORD), 'router password'),
            (lambda item: item['operations'][0].update(connection=DEVICE_PASSWORD), 'router password'),
            (lambda item: item['operations'][0].update(connection='not-a-digest'), 'not a digest'),
            (lambda item: item['operations'][0]['op'].update(ip='10.44.0.22'), 'changed device order')]:
            value = copy.deepcopy(payload)
            mutate(value)
            with self.subTest(reason=reason), self.assertRaisesRegex(ValueError, reason):
                inspect_python_token(signer.dumps(value), 'alpha', JWT_SECRET)
        with self.assertRaisesRegex(ValueError, 'signature/payload'):
            inspect_python_token('x' + signer.dumps(payload), 'alpha', JWT_SECRET)


if __name__ == '__main__':
    unittest.main()
