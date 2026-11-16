"""Public download header checks; opaque validators are never blindly blanked."""
from email.utils import parsedate_to_datetime
import re

from compare import Difference
from harness import DesignError


def validate_headers(reply, start, end):
    errors = []
    etag = reply.headers.get('etag')
    if etag and not re.fullmatch(r'(?:W/)?"[\x21\x23-\x7e\x80-\xff]*"', etag):
        errors.append('ETag format')
    modified = reply.headers.get('last-modified')
    if modified:
        try:
            stamp = parsedate_to_datetime(modified)
            if stamp.tzinfo is None or not start - 2 <= stamp.timestamp() <= end + 2:
                errors.append('Last-Modified window')
        except (ValueError, TypeError):
            errors.append('Last-Modified format')
    length = reply.headers.get('content-length')
    if length is not None and (not length.isdecimal() or int(length) != len(reply.body)):
        errors.append('Content-Length does not describe actual body')
    return errors


def capture_headers(replies, policy):
    out = []
    for index, reply in enumerate(replies):
        errors = validate_headers(reply, policy.start, policy.end)
        if errors:
            if index == 0:
                raise DesignError('Python download header fixture: ' + '; '.join(errors))
            out.extend(Difference('headers', 'invalid-Go-download', '<valid>', error) for error in errors)
    if out:
        return out
    for field, pattern in [('etag', r'(?:W/)?"[\x21\x23-\x7e\x80-\xff]*"'),
                           ('last-modified', r'[A-Za-z]{3}, [0-9]{2} [A-Za-z]{3} [0-9]{4} [0-9]{2}:[0-9]{2}:[0-9]{2} GMT')]:
        values = [r.headers.get(field) for r in replies]
        if all(values):
            policy.bind(*values, pattern, 'download-' + field)
    return out
