"""Request-level profile selection without mutable process-wide state."""

from collections.abc import Mapping
from typing import Optional
import urllib.parse

from flask import g, has_request_context, request


def append_url_query(url: str, params) -> str:
    """Append query parameters with standards-compliant percent encoding."""
    parts = urllib.parse.urlsplit(url)
    query_items = urllib.parse.parse_qsl(parts.query, keep_blank_values=True)
    query_items.extend(params.items() if isinstance(params, Mapping) else params)
    encoded_query = urllib.parse.urlencode(
        query_items,
        doseq=True,
        quote_via=urllib.parse.quote,
    )
    return urllib.parse.urlunsplit(parts._replace(query=encoded_query))

def profile_api_path(config_data, suffix: str) -> str:
    profile_id = config_data.get("profile_id") if isinstance(config_data, Mapping) else None
    return f"/api/profiles/{profile_id}{suffix}" if profile_id else suffix


def config_api_path(config_data, target: str) -> str:
    profile_id = config_data.get("profile_id") if isinstance(config_data, Mapping) else None
    return f"/api/config/{profile_id}/{target}" if profile_id else f"/api/config/{target}"


def _is_global_request() -> bool:
    if (request.view_args or {}).get("profile_id") is not None:
        return False
    endpoint = request.endpoint or ""
    if endpoint in {
        "subscriptions.get_all_subscription_proxies",
        "subscriptions.get_subscription_proxies",
        "subscription_aggregations.get_aggregation_provider",
    }:
        return False
    return (
        request.blueprint in {
            "auth", "settings", "mcp", "agents", "logs", "nodes",
            "subscriptions", "subscription_aggregations", "rule_library", "profiles",
        }
        or endpoint in {"config.export_config", "config.import_config", "config.reset_config"}
        or not request.path.startswith("/api/")
        or not endpoint
    )


def resolve_profile_id(explicit: Optional[str] = None, fallback: Optional[str] = None) -> str:
    """Resolve explicit/path, query, header, then the stable default profile."""
    from backend.common.config import get_repository

    repository = get_repository()
    if explicit is not None:
        candidate = explicit
    elif has_request_context() and (request.endpoint is None or not _is_global_request()):
        candidate = (request.view_args or {}).get("profile_id")
        if candidate is None:
            candidate = request.args.get("profile")
        if candidate is None:
            candidate = request.args.get("profile_id")
        if candidate is None:
            candidate = request.headers.get("X-ConfigFlow-Profile")
    else:
        candidate = None

    if candidate is None:
        candidate = fallback if fallback is not None else "default"
    repository.validate_profile_id(candidate)
    repository._profile_metadata(candidate)
    return candidate


def install_profile_context(app) -> None:
    """Install request validation and response observability on a Flask app."""

    @app.before_request
    def _set_profile_context():
        from backend.common.config import reset_config_context

        reset_config_context()
        if _is_global_request():
            g.configflow_profile_id = None
            return None
        g.configflow_profile_id = resolve_profile_id()

    @app.after_request
    def _sanitize_external_json(response):
        """Scrub ordinary JSON responses without touching internal return objects."""
        from backend.common.config_export import sanitize_external_payload

        endpoint = request.endpoint or ""
        if (
            response.is_json
            and not response.direct_passthrough
        ):
            try:
                payload = response.get_json(silent=True)
            except RecursionError:
                # The standard decoder can overflow before the iterative
                # sanitizer sees an adversarially deep raw JSON response.
                response.set_data(app.json.dumps("[REDACTED]"))
                return response
            if payload is not None:
                sanitized = sanitize_external_payload(payload)
                # The authenticated settings editor needs its proxy URL for edits.
                # Keep credentials hidden in other metadata and shared exports.
                if (
                    endpoint == "settings.handle_rule_fetch_proxy"
                    and response.status_code == 200
                    and isinstance(payload, dict)
                    and isinstance(payload.get("rule_fetch_proxy"), str)
                ):
                    sanitized["rule_fetch_proxy"] = payload["rule_fetch_proxy"]
                # A successful self-registration may disclose exactly one top-level
                # credential. Nested Agent records and every other API stay scrubbed.
                if (
                    endpoint == "agents.register_agent"
                    and 200 <= response.status_code < 300
                    and isinstance(payload, dict)
                    and payload.get("success") is True
                    and isinstance(payload.get("token"), str)
                ):
                    sanitized["token"] = payload["token"]
                # An authenticated Agent pull is a generated artifact, not metadata.
                # Keep its bytes and checksum consistent; other fields stay scrubbed.
                if (
                    endpoint == "agents.get_agent_config"
                    and response.status_code == 200
                    and isinstance(payload, dict)
                    and payload.get("success") is True
                    and isinstance(payload.get("content"), str)
                ):
                    sanitized["content"] = payload["content"]
                response.set_data(app.json.dumps(sanitized))
        scan = getattr(g, "rule_duplicate_scan", None)
        if scan is not None:
            import time
            from backend.utils.logger import get_logger

            scan_id, started = scan
            get_logger("backend.routes.rules").info(
                "Duplicate scan %s response ready: status=%d bytes=%d total_ms=%.1f",
                scan_id, response.status_code, response.calculate_content_length() or 0,
                (time.perf_counter() - started) * 1000,
            )
        return response

    @app.after_request
    def _add_profile_header(response):
        from backend.common.config import reset_config_context

        profile_id = getattr(g, "configflow_profile_id", None)
        if profile_id:
            response.headers.setdefault("X-ConfigFlow-Profile", profile_id)
        reset_config_context()
        return response

    @app.teardown_request
    def _clear_profile_context(_error=None):
        from backend.common.config import reset_config_context

        reset_config_context()
