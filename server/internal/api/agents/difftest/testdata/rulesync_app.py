"""A16 HTTP oracle: worker rounds are driven separately, never timed by HTTP."""
import runpy
from backend.agents import rule_sync
rule_sync.start_loop = lambda: None
runpy.run_module('backend.app',run_name='__main__')
