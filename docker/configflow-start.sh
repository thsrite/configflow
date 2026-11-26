#!/bin/sh
# Container entrypoint (under tini): optionally update from source, validate startup, then run the server.
# The update step is opt-in (CONFIGFLOW_AUTO_UPDATE=1) and never blocks startup.
if [ "${CONFIGFLOW_AUTO_UPDATE:-0}" = "1" ]; then
    /usr/local/bin/configflow-autoupdate || echo "[auto-update] skipped after an unexpected error"
fi
/usr/local/bin/configflow -check || exit 1
# The server supervises the bundled Sub-Store itself and stops it on shutdown.
exec /usr/local/bin/configflow
