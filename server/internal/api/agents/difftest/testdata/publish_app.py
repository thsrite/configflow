"""A14/A23 publication oracle with real IPv6 enrollment and Flask routes."""
import os
import runpy

if os.environ.get('CONFIGFLOW_DIFF_HOLD_IPV6_SCHEDULER') == '1':
    # The Go side holds its scheduler alike (see holdIPv6Scheduler): a 5 s
    # automatic round landing between activate and rollback on one side only
    # made the stored ipv6_verifications differ by timing.
    import backend.agents.ipv6_verification as ipv6_verification
    ipv6_verification.start_loop = lambda _manager: None

runpy.run_module('backend.app', run_name='__main__')
