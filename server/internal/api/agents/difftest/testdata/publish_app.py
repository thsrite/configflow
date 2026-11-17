"""A14 route oracle with the designed unregistered A23 hook (out of scope)."""
import runpy
from backend.agents import ipv6_verification
ipv6_verification.enroll=lambda *args,**kwargs: None
runpy.run_module('backend.app',run_name='__main__')
