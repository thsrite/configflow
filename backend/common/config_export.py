"""Safe copies of configuration data for external responses and exports."""

import urllib.parse
from collections import deque
from functools import lru_cache
from typing import Any, Dict, Iterable, Optional


SENSITIVE_SYSTEM_CONFIG_FIELDS = frozenset(
    {"rule_proxy_token", "retired_rule_proxy_tokens", "rule_fetch_proxy"}
)
_REDACTED = "[REDACTED]"
_STRING_CHECK_CACHE_SIZE = 4096
_STRING_CHECK_CACHE_MAX_LENGTH = 1024


def redact_oxidns_settings(settings: Any) -> Any:
    """Redact credentials in optional, human-shareable OxiDNS backups.

    Full administrator backups remain restorable. Upstream/native YAML strings
    can contain embedded credentials, so omit those opaque fields completely.
    Rules pinned to an upstream keep their shape; only the copied upstream's
    address and credentials are redacted.
    """
    opaque_fields = {'custom_config', 'native_config', 'template_base_config', 'legacy_native_config', 'local_dns', 'remote_dns', 'fallback_dns'}
    credential_fields = {'password', 'username', 'token', 'secret', 'authorization',
                         'private_key', 'private-key', 'key', 'socks5', 'proxy',
                         'url', 'urls', 'headers', 'bootstrap_rules'}
    pinned_fields = {'rule_actions', 'rule_dns_parked'}

    def redact(value, depth=0):
        if depth >= 64:
            return _REDACTED
        if isinstance(value, dict):
            return {key: ([] if key == 'additional_files' else '' if key in opaque_fields else
                          _REDACTED if str(key).lower() in credential_fields and item else
                          redact_pinned(item, depth + 1) if key in pinned_fields else
                          redact(item, depth + 1)) for key, item in value.items()}
        if isinstance(value, list):
            return [redact(item, depth + 1) for item in value]
        return value

    def redact_pinned(actions, depth):
        if not isinstance(actions, dict) or depth >= 62:
            return redact(actions, depth)
        result = {}
        for identity, action in actions.items():
            result[identity] = redact(action, depth + 1)
            upstream = result[identity].get('upstream') if isinstance(result[identity], dict) else None
            if isinstance(upstream, dict) and upstream.get('addr'):
                upstream['addr'] = _REDACTED
        return result

    return redact(settings)


def _repository_rule_proxy_tokens() -> Iterable[str]:
    """Read capability tokens from the authoritative repository, not a response."""
    from backend.common.config import get_repository

    yield from get_repository().rule_proxy_tokens_for_sanitization()


def _repository_agent_tokens() -> Iterable[str]:
    """Collect persisted Agent credentials without exposing manager return values."""
    from backend.common.config import get_repository

    yield from get_repository().agent_tokens_for_sanitization()


def _contains_encoded_secret(value: str, secrets: set[str]) -> bool:
    """Inspect bounded URL-decoding paths without enumerating encoding depth."""
    if not secrets:
        return False
    # Both URL decoders leave these strings unchanged. Ordinary response keys,
    # rule names and domains do not need a decoding queue or state tracking.
    if "%" not in value and "+" not in value:
        return any(secret in value for secret in secrets)

    pending = deque([value])
    seen = {value}
    max_states = max(1, 2 * len(value) + 1)
    max_steps = 2 * max_states
    states = 0
    steps = 0

    while pending and states < max_states and steps < max_steps:
        current = pending.popleft()
        states += 1
        if any(secret in current for secret in secrets):
            return True
        for decoder in (urllib.parse.unquote, urllib.parse.unquote_plus):
            if steps >= max_steps:
                break
            steps += 1
            decoded = decoder(current)
            if len(decoded) <= len(current) and decoded not in seen:
                seen.add(decoded)
                pending.append(decoded)
    return False


def contains_internal_rule_proxy_token(value: str) -> bool:
    """Detect current/retired internal rule tokens through arbitrary URL encoding."""
    if not isinstance(value, str):
        return False
    secrets = {token for token in _repository_rule_proxy_tokens() if token}
    return _contains_encoded_secret(value, secrets)


def _external_payload_secrets(system_config=None):
    """Read the current redaction context from the authoritative repository.

    令牌集合由仓库按 store head 记住（见 ProfileRepository._sanitization_tokens）。
    常驻的文档缓存里本来就有这些令牌，记住集合不扩大暴露面（文档超过缓存上限
    不进缓存时，也只多常驻这几个令牌字符串，不多常驻文档）；任何提交都会换
    head，令牌轮换在下一个响应就生效。不要在这里另建跨文档版本的缓存。
    """
    secrets = set(_repository_rule_proxy_tokens())
    secrets.update(_repository_agent_tokens())
    if isinstance(system_config, dict):
        secrets.update(
            system_config.get(field)
            for field in SENSITIVE_SYSTEM_CONFIG_FIELDS
            if isinstance(system_config.get(field), str) and system_config[field]
        )
    return frozenset(secret for secret in secrets if isinstance(secret, str) and secret)


def sanitize_external_payload(
    payload: Any, system_config: Optional[Dict[str, Any]] = None
) -> Any:
    """Remove the repository's internal token from arbitrary external payloads."""
    return _sanitize_with_secrets(payload, _external_payload_secrets(system_config))


def redact_secrets_in_text(text: str) -> str:
    """Hide credentials inside multi-line text such as Agent logs.

    The response hook replaces any string containing a credential as a whole,
    which blanks an entire log for one rule download URL. Replace only the
    credential and its URL-encoded forms; a line that still decodes to one is
    hidden on its own.
    """
    if not isinstance(text, str) or not text:
        return text
    secrets = _external_payload_secrets()
    if not _contains_encoded_secret(text, secrets):
        return text
    ordered = sorted(secrets, key=len, reverse=True)
    lines = []
    for line in text.splitlines(keepends=True):
        for secret in ordered:
            for variant in {secret, urllib.parse.quote(secret, safe=""), urllib.parse.quote_plus(secret)}:
                line = line.replace(variant, _REDACTED)
        if _contains_encoded_secret(line, secrets):
            line = _REDACTED + ("\n" if line.endswith("\n") else "")
        lines.append(line)
    return "".join(lines)


def stored_secret_finder():
    """``text -> [(start, end)]`` of the stored credentials, from one snapshot of them.

    For responses that mark credentials in display text instead of dropping
    it (see ``redact_secrets_in_text``): every credential and its URL-encoded
    forms is one range; a text that still decodes to one afterwards is
    returned as a single range.
    """
    secrets = _external_payload_secrets()
    variants = sorted({variant for secret in secrets
                       for variant in (secret, urllib.parse.quote(secret, safe=""), urllib.parse.quote_plus(secret))},
                      key=len, reverse=True)

    def find(text):
        if not secrets or not isinstance(text, str) or not text or not _contains_encoded_secret(text, secrets):
            return []
        ranges = []
        for variant in variants:
            start = text.find(variant)
            while start >= 0:
                ranges.append((start, start + len(variant)))
                start = text.find(variant, start + 1)
        rest = list(text)
        for start, end in ranges:
            rest[start:end] = "\0" * (end - start)
        if _contains_encoded_secret("".join(rest), secrets):
            return [(0, len(text))]
        return sorted(ranges)

    return find


def _sanitize_with_secrets(payload, secrets, *, sort_dict_keys=False):
    """Sanitize with one request-local credential snapshot."""

    # Repeated keys and rule metadata dominate large responses. Scope the cache
    # to this call so token changes cannot reuse earlier decisions, and bound
    # both entry count and string length rather than retaining arbitrary input.
    @lru_cache(maxsize=_STRING_CHECK_CACHE_SIZE)
    def contains_cached_secret(value: str) -> bool:
        return _contains_encoded_secret(value, secrets)

    # A response sanitizer is an availability boundary: it must not inherit
    # Python's recursion limit or follow hostile object cycles.  Depth counts
    # the root as zero; a container at the limit is replaced as one subtree.
    max_depth = 128
    root = [None]
    active_container_ids: set[int] = set()
    stack = [("visit", payload, 0, root, 0)]

    while stack:
        operation, *arguments = stack.pop()
        if operation == "leave":
            active_container_ids.remove(arguments[0])
            continue
        if operation == "finish_tuple":
            items, target, target_key = arguments
            target[target_key] = tuple(items)
            continue
        if operation == "finish_dict_item":
            output, item = arguments
            output[item[0]] = item[1]
            continue

        value, depth, target, target_key = arguments
        is_container = isinstance(value, (dict, list, tuple))
        if is_container and (
            depth >= max_depth or id(value) in active_container_ids
        ):
            target[target_key] = _REDACTED
            continue

        if isinstance(value, dict):
            output: Dict[Any, Any] = {}
            target[target_key] = output
            container_id = id(value)
            active_container_ids.add(container_id)
            stack.append(("leave", container_id))
            # A direct JSON response must visit keys in the same order as its
            # former encode/decode pass, including collisions after redaction.
            items = sorted(value.items()) if sort_dict_keys else tuple(value.items())
            for key, item_value in reversed(items):
                if key in SENSITIVE_SYSTEM_CONFIG_FIELDS:
                    continue
                item = [None, None]
                stack.append(("finish_dict_item", output, item))
                stack.append(("visit", item_value, depth + 1, item, 1))
                stack.append(("visit", key, depth + 1, item, 0))
            continue

        if isinstance(value, list):
            output_list = [None] * len(value)
            target[target_key] = output_list
            container_id = id(value)
            active_container_ids.add(container_id)
            stack.append(("leave", container_id))
            for index in range(len(value) - 1, -1, -1):
                stack.append(("visit", value[index], depth + 1, output_list, index))
            continue

        if isinstance(value, tuple):
            output_items = [None] * len(value)
            container_id = id(value)
            active_container_ids.add(container_id)
            stack.append(("leave", container_id))
            stack.append(("finish_tuple", output_items, target, target_key))
            for index in range(len(value) - 1, -1, -1):
                stack.append(("visit", value[index], depth + 1, output_items, index))
            continue

        if isinstance(value, str):
            contains_secret = (
                contains_cached_secret(value)
                if len(value) <= _STRING_CHECK_CACHE_MAX_LENGTH
                else _contains_encoded_secret(value, secrets)
            )
            target[target_key] = _REDACTED if contains_secret else value
        else:
            target[target_key] = value

    return root[0]


def sanitize_config_for_output(config: Dict[str, Any]) -> Dict[str, Any]:
    """Return an external-safe copy while leaving internal configuration untouched."""
    system_config = config.get("system_config")
    return sanitize_external_payload(
        config, system_config if isinstance(system_config, dict) else None
    )
