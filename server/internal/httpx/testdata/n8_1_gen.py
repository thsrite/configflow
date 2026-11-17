"""Synthetic HTTP-boundary reference for N8-1; run only on a test server.

Uses Flask's real after_request hook, not a reimplementation of its policy.
No repository/device I/O: the authoritative token readers are replaced.
"""
import json
from urllib.parse import quote, quote_plus
from flask import Flask, Response, jsonify
from backend.common import config_export
from backend.common.profile_context import install_profile_context

RULE = "rule /+密钥"
RETIRED = "retired-secret"
AGENT = "agent /+secret"
config_export._repository_rule_proxy_tokens = lambda: [RULE, RETIRED]
config_export._repository_agent_tokens = lambda: [AGENT]


def emit(name, payload, endpoint="test.echo", status=200, method="GET", raw=False):
    app = Flask(__name__)
    app.testing = True
    install_profile_context(app)
    body = json.dumps(payload, ensure_ascii=False, separators=(",", ":"))
    def view():
        return (Response(body, mimetype="application/json") if raw else jsonify(payload)), status
    app.add_url_rule("/x", endpoint, view, methods=[method])
    response = app.test_client().open("/x", method=method)
    assert response.status_code == status
    print(json.dumps(dict(name=name, endpoint=endpoint, status=status, method=method,
                         raw=raw, body=body, expected=response.get_json()), ensure_ascii=False))


for secret in (RULE, RETIRED, AGENT):
    variants = [secret, quote(secret, safe=""), quote_plus(secret), quote(quote(secret, safe=""), safe=""),
                quote(quote_plus(secret), safe=""), quote(secret, safe="").lower() if secret == RULE else secret]
    for index, variant in enumerate(variants):
        # Lowercase percent escapes must be recognized too.
        for raw in (False, True):
            emit(f"variants-{secret[:3]}-{index}-{raw}", {f"prefix-{variant}-suffix": "visible", "value": f"before {variant} after"}, raw=raw)
for raw in (False, True):
    emit(f"safe-and-sensitive-{raw}", {"empty": "", "safe": "not a credential", "partial": "retired-secre",
        "retired_rule_proxy_tokens": [], "rule_proxy_token": RULE, "rule_fetch_proxy": "opaque",
        "nested": [{"rule_proxy_token": "opaque", "keep": "safe"}]}, raw=raw)
    emit(f"collisions-{raw}", {f"{RETIRED}-z": 1, f"{RETIRED}-a": 2, "[REDACTED]": 3,
         "nested": [{f"{AGENT}-z": 4, f"{AGENT}-a": 5}]}, raw=raw)
    emit(f"escaped-{raw}", {"value": "https://example.com/" + quote(AGENT, safe="")}, raw=raw)
for depth in (127, 128, 129):
    for leaf in ({"safe": "visible", "secret": RULE, RULE: RULE}, [], "safe", RULE):
        payload = leaf
        for _ in range(depth):
            payload = [payload]
        emit(f"depth-{depth}-{type(leaf).__name__}-{str(leaf)[:4]}", payload)

native_keys = ("native_config", "additional_files", "template_settings", "template_base_config",
               "legacy_native_config", "_common_disabled", "gateway_onboarding_native_backup")
settings_endpoints = ("oxidns.oxidns_settings", "oxidns.prepare_oxidns_editor", "oxidns.preview_oxidns_template",
                     "oxidns.apply_oxidns_common_settings", "oxidns.apply_oxidns_native_settings")
values_endpoints = ("oxidns.read_oxidns_common_settings", "oxidns.apply_oxidns_common_settings", "oxidns.apply_oxidns_native_settings")
all_endpoints = ["agents.register_agent", "agents.docker_oxidns_ha_enroll", "agents.get_agent_config",
                 "agents.generate_final_agent_config", "generate.generate_artifact", "profiles.profile_generate_artifact",
                 "settings.handle_rule_fetch_proxy", "oxidns.render_oxidns_draft", "profiles.profile_render_oxidns_draft"]
all_endpoints += list(settings_endpoints) + list(values_endpoints)
all_endpoints += ["profiles.profile_" + e.split(".", 1)[1] for e in settings_endpoints + values_endpoints]
all_endpoints = list(dict.fromkeys(all_endpoints))
for endpoint in all_endpoints:
    for method in ("GET", "POST"):
        for status in (200, 201, 299, 300, 400):
            payload = {"success": True, "token": AGENT, "content": AGENT, "config": RULE,
                       "rule_fetch_proxy": RULE, "values": {"secret": RULE}, "data": {"token": AGENT}}
            payload.update({key: {"secret": RULE, "rule_proxy_token": RULE} for key in native_keys})
            payload["settings"] = {key: payload[key] for key in native_keys}
            payload["settings"]["other"] = RULE
            for raw in (False, True):
                emit(f"exempt-{endpoint}-{method}-{status}-{raw}", payload, endpoint, status, method, raw=raw)
    for success in (False, 1, "true", None):
        emit(f"success-identity-{endpoint}-{success}", {"success": success, "token": AGENT, "content": RULE}, endpoint)
