"""libyaml for texts on which it is proven to agree with PyYAML.

libyaml parses several times faster, but on unusual YAML it differs from PyYAML
in what it accepts and reports. A text is therefore parsed by both loaders the
first time it is seen, and PyYAML's result (or error) is returned. Only a text
on which both produced the same value is parsed by libyaml alone afterwards.
Callers that parse the same document again and again, such as the rule sync
check of a saved OxiDNS configuration, get the speed; every caller keeps
PyYAML's values and diagnostics.
"""
from collections import OrderedDict
import hashlib
import math
import threading

import yaml

LIBYAML_SAFE_LOADER = getattr(yaml, 'CSafeLoader', None)
_VERIFIED_LIMIT = 512
_verified = OrderedDict()
_lock = threading.Lock()


def _same(left, right, _open=None):
    """Equal values of equal types, mappings in the same key order.

    A recursive alias is never the same: it stays with PyYAML.
    """
    if type(left) is not type(right):
        return False
    if isinstance(left, (dict, list, tuple)):
        _open = set() if _open is None else _open
        if len(left) != len(right) or id(left) in _open:
            return False
        _open.add(id(left))
        try:
            if isinstance(left, dict):
                return all(_same(a, b, _open) and _same(left[a], right[b], _open) for a, b in zip(left, right))
            return all(_same(a, b, _open) for a, b in zip(left, right))
        finally:
            _open.discard(id(left))
    if isinstance(left, float):
        # nan never equals itself, and 0.0 equals -0.0.
        return (math.isnan(left) and math.isnan(right)) or (left == right and math.copysign(1, left) == math.copysign(1, right))
    return left == right


def load(text, loader, fast_loader):
    """yaml.load(text, Loader=loader), through fast_loader once the text is verified.

    fast_loader is the libyaml counterpart of loader, or None without libyaml.
    """
    if fast_loader is None or not isinstance(text, str):
        return yaml.load(text, Loader=loader)
    key = (loader, hashlib.sha256(text.encode('utf-8', 'surrogatepass')).digest())
    with _lock:
        verified = key in _verified
        if verified:
            _verified.move_to_end(key)
    if verified:
        return yaml.load(text, Loader=fast_loader)
    value = yaml.load(text, Loader=loader)
    try:
        same = _same(yaml.load(text, Loader=fast_loader), value)
    except Exception:  # libyaml rejects or builds it differently; PyYAML keeps deciding
        same = False
    if same:
        with _lock:
            _verified[key] = None
            while len(_verified) > _VERIFIED_LIMIT:
                _verified.popitem(last=False)
    return value


def safe_load(text):
    """yaml.safe_load for text that is parsed repeatedly."""
    return load(text, yaml.SafeLoader, LIBYAML_SAFE_LOADER)
