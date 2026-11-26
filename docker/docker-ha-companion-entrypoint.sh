#!/bin/sh
set -e

if [ "${CONFIGFLOW_DNS_HA:-}" != 1 ] || [ "${CONFIGFLOW_DNS_HA_NETWORK:-}" != host ]; then
    echo "The HA companion requires CONFIGFLOW_DNS_HA=1 and CONFIGFLOW_DNS_HA_NETWORK=host." >&2
    exit 1
fi

# The main Agent reads this evidence and verifies the live Unix socket peer.
# Identity/config/core data are read-only here; only the dedicated HA directory
# is writable. A journal never causes this entrypoint to start keepalived.
/usr/local/bin/configflow-agent ha-companion-check
# s6 runs the control socket; keepalived waits for the Agent's start command.
/usr/local/bin/configflow-agent docker-config companion

exec /init
