"""Bounded observation of real content checks, only in the comparison launcher."""
import time

HEADER = 'x-configflow-proddiff-error'
WAIT_SECONDS = 120
REQUEST_SECONDS = WAIT_SECONDS + 30
ROUTES = {'/api/agents', '/api/agents/<agent_id>', '/api/agents/<agent_id>/push-diff'}
ENDPOINTS = {'agents.handle_agents', 'agents.handle_agent_item', 'agents.agent_push_diff'}


def checking(value):
    if isinstance(value, list):
        return any(checking(row) for row in value)
    return (isinstance(value, dict) and
            (value.get('status') == 'checking' or
             isinstance(value.get('content_sync'), dict) and value['content_sync'].get('status') == 'checking'))


def checked_reply(reply):
    if any(key.lower() == HEADER for key in reply[3]):
        raise RuntimeError('content sync observer timed out; no differences were classified')
    return reply


def wait_for_content(app, *, timeout=WAIT_SECONDS, clock=time.monotonic, sleep=time.sleep):
    """Re-read the same GET until its actual Checker settles; never fabricate data."""
    from flask import request

    @app.after_request
    def settled_content(response):
        if request.method != 'GET' or request.endpoint not in ENDPOINTS:
            return response
        deadline = clock() + timeout

        def failed():
            result = app.make_response(({'error': 'proddiff content sync did not settle'}, 503))
            result.headers[HEADER] = 'content-sync-timeout'
            result.headers['Cache-Control'] = 'no-store'
            return result

        while response.status_code == 200 and response.is_json and checking(response.get_json(silent=True)):
            remaining = deadline - clock()
            if remaining <= 0:
                return failed()
            sleep(min(remaining, .1))
            response = app.make_response(app.view_functions[request.endpoint](**(request.view_args or {})))
            if clock() >= deadline:
                return failed()
        return response
