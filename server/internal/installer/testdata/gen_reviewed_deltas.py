"""Write reviewed_deltas.json: the reviewed installer delta tables of the Python tests.

backend/test_router_split_regression.py and backend/test_gateway_core_install.py
compare today's installers with the 0ef9e89 baselines after undoing a list of
reviewed changes. The Go tests apply the same lists; this script copies them
instead of retyping them. Run from the repository root:

    ~/cf-test/.venv/bin/python server/internal/installer/testdata/gen_reviewed_deltas.py \
        > server/internal/installer/testdata/reviewed_deltas.json
"""
import json
import sys

sys.path.insert(0, '.')
from backend import test_gateway_core_install as gateway  # noqa: E402
from backend import test_router_split_regression as split  # noqa: E402


def pairs(table):
    return [[new, old] for new, old in table]


print(json.dumps({
    'native_ha_dependency_cases': split._NATIVE_HA_DEPENDENCY_CASES,
    'native_ha_package_case': split._NATIVE_HA_PACKAGE_CASE,
    'gateway_dependency_cases': split._GATEWAY_DEPENDENCY_CASES,
    'manual_install_command': split._MANUAL_INSTALL_COMMAND,
    'gateway_dependency_rewords': pairs(split._GATEWAY_DEPENDENCY_REWORDS),
    'core_install_path': split._CORE_INSTALL_PATH,
    'core_agent_follows': split._CORE_AGENT_FOLLOWS,
    'openrc_stop_retry_deltas': pairs(split._OPENRC_STOP_RETRY_DELTAS),
    'resolved_stub_prompt_deltas': pairs(split._RESOLVED_STUB_PROMPT_DELTAS),
    'installer_hardening_deltas': pairs(split._INSTALLER_HARDENING_DELTAS),
    'unified_cleanup_pattern': split._UNIFIED_CLEANUP.pattern,
    'staged_agent': split._STAGED_AGENT,
    'previous_core_cleanup': split._PREVIOUS_CORE_CLEANUP,
    'single_service_script_sha256': split.baseline()['single_service_script_sha256'],
    # Python's own digests of today's scripts (same inputs as the baseline), as
    # a check of the Go port of without_reviewed_installer_deltas.
    'python_single_service_script_sha256': split.single_service_script_digests(),
    'gateway_old_need': gateway.GATEWAY_OLD_NEED,
    'gateway_dependency_call': gateway.GATEWAY_DEPENDENCY_CALL,
    'ordinary_aio_forwarding_deltas': pairs(gateway.ORDINARY_AIO_FORWARDING_DELTAS),
}, ensure_ascii=False, indent=1, sort_keys=True))
