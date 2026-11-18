"""Exact O9-H1/L5 HTTP expectation changes, shared by offline comparators."""


def expected_headers(python, go, content_type):
    """Absent/foreign Origins: no CORS; text responses: exact nosniff value.

    These fixtures do not send same-origin Origins. Only the three Flask-CORS
    headers and the Origin token in Vary migrate (O9-H1). All other Vary tokens,
    headers, statuses and bodies remain observable. O9-L5 permits only an absent
    X-Content-Type-Options becoming the literal nosniff on a text response.
    """
    expected = dict(python)
    for name in ('access-control-allow-origin', 'access-control-allow-headers', 'access-control-allow-methods'):
        expected.pop(name, None)
    vary = [token.strip() for token in expected.get('vary', '').split(',')
            if token.strip() and token.strip().lower() != 'origin']
    expected.pop('vary', None)
    if vary:
        expected['vary'] = ', '.join(vary)
    if not expected.get('x-content-type-options') and go.get('x-content-type-options') == 'nosniff' and content_type.startswith('text/'):
        expected['x-content-type-options'] = 'nosniff'
    return expected
