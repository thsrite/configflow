"""Narrow DAV/CONNECT equivalence; original wire observations stay untouched."""
import copy
import ipaddress
import re
import xml.etree.ElementTree as ET

from compare import Difference, differences


def allprop(body):
    """Recognize only empty or pure DAV:propfind/DAV:allprop request bodies."""
    if not isinstance(body, str) or len(body) > 4096:
        return False
    if body == '':
        return True
    # No DTD, entities, comments or processing instructions. An XML declaration
    # is allowed, but no hidden instructions may disappear during DOM parsing.
    text = re.sub(r'^<\?xml\s+version=["\']1\.0["\'](?:\s+encoding=["\']UTF-8["\'])?\s*\?>', '', body)
    if '<!' in text or '<?' in text:
        return False
    try:
        root = ET.fromstring(text)
    except (ET.ParseError, ValueError):
        return False
    return (root.tag == '{DAV:}propfind' and not root.attrib
        and not (root.text or '').strip() and len(root) == 1
        and root[0].tag == '{DAV:}allprop' and not root[0].attrib
        and not list(root[0]) and not (root[0].text or '').strip()
        and not (root[0].tail or '').strip())


def dav_differences(left, right, policy=None):
    """Compare complete snapshots, normalizing only two proven allprop bodies."""
    values = [copy.deepcopy(left), copy.deepcopy(right)]
    if all(isinstance(value, dict) and isinstance(value.get('requests'), list) for value in values):
        for a, b in zip(values[0]['requests'], values[1]['requests']):
            if (isinstance(a, dict) and isinstance(b, dict)
                    and a.get('method') == b.get('method') == 'PROPFIND'
                    and allprop(a.get('body')) and allprop(b.get('body'))):
                a['body'] = b['body'] = ''
    return differences(*values, 'dav', policy)


def authority(value):
    """Validate a CONNECT authority with an explicit port; preserve target bytes."""
    if not isinstance(value, str):
        return None
    match = re.fullmatch(r'(\[[0-9a-fA-F:.]+\]|[a-zA-Z0-9.-]+):([0-9]+)', value)
    if not match or len(match[2]) > 5 or not 1 <= int(match[2]) <= 65535:
        return None
    host = match[1]
    if host.startswith('['):
        try:
            host = str(ipaddress.IPv6Address(host[1:-1]))
        except ValueError:
            return None
    return host.lower(), int(match[2])


def connect_error(row):
    """Validate native HTTP/1 CONNECT metadata before ignoring representation."""
    target = row.get('path')
    if authority(target) is None:
        return 'invalid CONNECT authority'
    line = row.get('request_line')
    match = re.fullmatch(r'CONNECT ' + re.escape(target) + r' HTTP/(1\.[01])', line) if isinstance(line, str) else None
    if not match:
        return 'request line must name the exact recorded target and HTTP/1.0 or 1.1'
    host = row.get('host')
    if not isinstance(host, str) or (host == '' and match[1] != '1.0'):
        return 'HTTP/1.1 CONNECT requires Host'
    if host and authority(host) != authority(target):
        return 'Host must match CONNECT target authority'
    return ''


def source_differences(left, right, policy=None):
    """Compare all requests, permitting only native CONNECT wire differences."""
    values = [copy.deepcopy(left), copy.deepcopy(right)]
    extra = []
    if all(isinstance(value, list) for value in values):
        for index, (a, b) in enumerate(zip(*values)):
            if not (isinstance(a, dict) and isinstance(b, dict)
                    and a.get('method') == b.get('method') == 'CONNECT'):
                continue
            errors = [connect_error(row) for row in (a, b)]
            if any(errors):
                extra.append(Difference(f'subscription.source.requests[{index}].connect',
                    'invalid-CONNECT-metadata', errors[0], errors[1]))
                continue
            # Targets remain byte-for-byte strict, as do authorization,
            # proxy_authorization, bodies and every other observed member.
            for row in (a, b):
                row['request_line'] = 'CONNECT ' + row['path'] + ' HTTP/1.x'
                row['host'] = row['path']
            if a.get('user_agent') == '' and b.get('user_agent') == 'Go-http-client/1.1':
                b['user_agent'] = ''  # Only the actually observed native default.
    return extra + differences(*values, 'subscription.source.requests', policy)
