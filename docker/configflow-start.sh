#!/bin/sh
# Container entrypoint: optionally update from source, validate startup, then start services.
# The update step is opt-in (CONFIGFLOW_AUTO_UPDATE=1) and never blocks startup.
if [ "${CONFIGFLOW_AUTO_UPDATE:-0}" = "1" ]; then
    python3 /usr/local/bin/configflow-auto-update.py || echo "[auto-update] skipped after an unexpected error"
fi
/usr/local/bin/configflow -check || exit 1
exec /usr/bin/supervisord -c /etc/supervisor/conf.d/supervisord.conf
