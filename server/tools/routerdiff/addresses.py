"""Runtime-only global-classification fixtures; never network destinations.

Real values are compared before report-only aliases are applied. No global
address constant or generated address is committed or printed in a report.
"""
import ipaddress
import secrets
import base64
import binascii

_values = {}


def network(label, version=4, prefix=None):
    if version not in (4, 6):
        raise ValueError('fixture address family must be 4 or 6')
    key = (label, version, prefix)
    if key in _values:
        return _values[key]
    prefix = prefix if prefix is not None else 24 if version == 4 else 48
    if type(prefix) is not int or not (24 <= prefix <= 32 if version == 4 else 32 <= prefix <= 128):
        raise ValueError('unsupported fixture prefix')
    while True:
        value = ipaddress.ip_network((secrets.randbits(32 if version == 4 else 128), prefix), strict=False)
        address = value.network_address
        if value.version == version and value.is_global and not address.is_multicast and not address.is_reserved \
                and str(value) not in _values.values():
            break
    _values[key] = str(value)
    return str(value)


def report_aliases(value):
    aliases = {}
    for index, ((label, version, _), cidr) in enumerate(_values.items(), 1):
        aliases[cidr] = f'<synthetic-global-{version}-{index}>'
        aliases[str(ipaddress.ip_network(cidr).network_address)] = f'<synthetic-global-{version}-{index}-address>'
    def redact(item):
        if isinstance(item, str):
            try:
                decoded = base64.b64decode(item, validate=True).decode('utf-8')
            except (binascii.Error, UnicodeError, ValueError):
                decoded = ''
            if any(literal in decoded for literal in aliases):
                return '<base64 text: ' + redact(decoded) + '>'
            for literal in sorted(aliases, key=len, reverse=True):
                item = item.replace(literal, aliases[literal])
            return item
        if isinstance(item, dict):
            return {redact(key): redact(child) for key, child in item.items()}
        if isinstance(item, (tuple, list)):
            return [redact(child) for child in item]
        return item
    return redact(value)
