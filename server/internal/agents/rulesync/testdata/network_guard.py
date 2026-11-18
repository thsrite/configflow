"""Reference processes may use in-memory transports, never an external network."""
import requests

def _deny(*_args, **_kwargs):
    raise AssertionError('unexpected network request in Python reference')

requests.sessions.Session.request = _deny
