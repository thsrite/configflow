"""Serve production requests on a reusable, bounded pool of worker threads."""
import os


DEFAULT_HTTP_THREADS = 8


def server_options():
    """Keep transport behavior compatible with the existing nginx frontend."""
    raw_threads = os.environ.get('CONFIGFLOW_HTTP_THREADS', str(DEFAULT_HTTP_THREADS))
    try:
        threads = int(raw_threads)
    except ValueError:
        raise ValueError('CONFIGFLOW_HTTP_THREADS must be an integer between 1 and 64') from None
    if not 1 <= threads <= 64:
        raise ValueError('CONFIGFLOW_HTTP_THREADS must be an integer between 1 and 64')
    return {
        'host': '0.0.0.0',
        'port': 5001,
        'threads': threads,
        'channel_timeout': 300,
        # Routes already interpret these headers (including deployments behind
        # an external proxy). Do not silently remove or reinterpret them here.
        'clear_untrusted_proxy_headers': False,
    }


STOP_FORWARDING_RELOADER = 'configflow'


def stop_forwarding_reloader():
    """Register a debug reloader whose parent passes SIGTERM on to the server.

    With LOG_LEVEL=DEBUG supervisord (and docker stop) signal the reloader
    parent. Werkzeug's parent turns SIGTERM into sys.exit(0), and the
    subprocess.call it waits in then SIGKILLs the serving child, so the child
    never saves Agent monitoring history. This parent forwards SIGTERM and waits
    for the child to exit; a second SIGTERM kills the child. In the child, the
    SIGTERM handler app.py installed is put back over Werkzeug's sys.exit(0),
    so the stop behaves as it does without debug mode.
    """
    import signal
    import subprocess
    from werkzeug import _reloader

    service_handler = signal.getsignal(signal.SIGTERM)

    class StopForwardingReloader(_reloader.reloader_loops['auto']):
        def __init__(self, *args, **kwargs):
            # Werkzeug replaces SIGTERM just before building the reloader.
            super().__init__(*args, **kwargs)
            if os.environ.get('WERKZEUG_RUN_MAIN') == 'true' and callable(service_handler):
                signal.signal(signal.SIGTERM, service_handler)

        def restart_with_reloader(self):
            while True:
                _reloader._log('info', f' * Restarting with {self.name}')
                child = subprocess.Popen(_reloader._get_args_for_reloading(), close_fds=False,
                                         env={**os.environ, 'WERKZEUG_RUN_MAIN': 'true'})
                received = []

                def forward(signum, frame, child=child, received=received):
                    received.append(signum)
                    if len(received) == 1:
                        child.send_signal(signal.SIGTERM)
                    else:
                        child.kill()

                signal.signal(signal.SIGTERM, forward)
                try:
                    exit_code = child.wait()
                except KeyboardInterrupt:
                    # The terminal sent Ctrl+C to the child as well; let it save
                    # before returning, and kill it if it does not stop.
                    try:
                        child.wait(timeout=10)
                    except (subprocess.TimeoutExpired, KeyboardInterrupt):
                        child.kill()
                    raise
                if exit_code != 3:
                    return exit_code

    _reloader.reloader_loops[STOP_FORWARDING_RELOADER] = StopForwardingReloader
    return STOP_FORWARDING_RELOADER


def run_http_server(app, *, debug=False):
    from backend.utils.local_http import register_application
    register_application(app)
    if debug:
        # Keep the local debugger/reloader and its existing background-loop
        # guard in app.py. Production never uses Werkzeug's per-request threads.
        app.run(host='0.0.0.0', port=5001, debug=True, reloader_type=stop_forwarding_reloader())
        return

    from waitress import serve
    from backend.http_queue import QueueWaitDispatcher

    options = server_options()
    dispatcher = QueueWaitDispatcher()
    try:
        dispatcher.set_thread_count(options['threads'])
        serve(app, _dispatcher=dispatcher, **options)
    finally:
        dispatcher.shutdown()
