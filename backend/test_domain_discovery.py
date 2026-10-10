"""域名发现：上报校验、存储、归类，以及默认规则集的创建与写入。"""
import copy
from datetime import datetime, timedelta

import pytest
from flask import Flask

from backend.agents import traffic_store
from backend.agents.traffic_store import TrafficStore, clean_report, route_key
from backend.common import config as config_module
from backend.common.agent_manager import init_agent_manager
from backend.common.config_repository import ProfileRepository
from backend.routes import register_blueprints
from backend.utils.domain_discovery import format_ruleset_line, registrable_domain, summarize


def _item(host, **overrides):
    item = {
        "host": host, "port": 443, "network": "tcp", "rule": "Match", "rule_payload": "",
        "policy": "漏网之鱼", "outlet": "direct", "conns": 1, "fails": 0, "fail_kinds": {},
        "zero_dl": 0, "up": 10, "down": 20,
    }
    item.update(overrides)
    return item


def _report(*items, status="running"):
    return {
        "schema": 1, "status": status, "mihomo_version": "v1.19.32",
        "window_start": "2026-10-10T10:00:00Z", "window_end": "2026-10-10T10:05:00Z",
        "ip_only_conns": 0, "dropped": 0, "items": list(items),
    }


# ---------- 上报校验 ----------

def test_clean_report_accepts_well_formed_report():
    report = _report(_item("www.example.com", fails=3, fail_kinds={"timeout": 3}))
    assert clean_report(report)["items"] == report["items"]


def test_clean_report_accepts_empty_rule():
    """全局 / 直连模式或指定了出口的入站，Mihomo 不经规则匹配，rule 为空。"""
    assert len(clean_report(_report(_item("www.example.com", rule="")))["items"]) == 1


@pytest.mark.parametrize("mutate", [
    lambda r: r.update(extra=1),
    lambda r: r.update(schema=2),
    lambda r: r.update(status="hacked"),
    lambda r: r.update(dropped=-1),
    lambda r: r.update(items="nope"),
    lambda r: r.update(items=[_item("a.com")] * (traffic_store.REPORT_MAX_ITEMS + 1)),
], ids=["extra-field", "schema", "status", "dropped", "items-type", "too-many"])
def test_clean_report_rejects_bad_top_level(mutate):
    report = _report(_item("www.example.com"))
    mutate(report)
    assert clean_report(report) is None


@pytest.mark.parametrize("mutate", [
    lambda i: i.update(host="1.2.3.4"),
    lambda i: i.update(host="Example.COM"),
    lambda i: i.update(host="bad host"),
    lambda i: i.update(outlet="tunnel"),
    lambda i: i.update(conns=-1),
    lambda i: i.update(conns=True),
    lambda i: i.update(policy="a\nb"),
    lambda i: i.update(policy="x" * 129),
    lambda i: i.update(rule_payload="x" * 257),
    lambda i: i.update(fail_kinds={"weird": 1}),
    lambda i: i.update(path="/secret"),
], ids=["ip-host", "upper-host", "space-host", "outlet", "negative", "bool-count", "control-char",
        "long-policy", "long-payload", "fail-kind", "item-field"])
def test_clean_report_drops_only_the_bad_item(mutate):
    bad = _item("bad.example.com")
    mutate(bad)
    cleaned = clean_report(_report(_item("good.example.com"), bad))
    assert [item["host"] for item in cleaned["items"]] == ["good.example.com"]
    assert cleaned["dropped"] == 1


# ---------- 存储 ----------

def test_store_merges_reports_and_expires_old_days(tmp_path):
    store = TrafficStore(tmp_path)
    old = datetime(2026, 10, 1, 12)
    store.add_report("a1", _report(_item("old.com")), now=old)
    now = datetime(2026, 10, 10, 12)
    store.add_report("a1", _report(_item("x.com", conns=2, fails=1, fail_kinds={"reset": 1})), now=now)
    store.add_report("a1", _report(_item("x.com", conns=3, fails=2, fail_kinds={"reset": 2})), now=now)

    data = store.load("a1", now=now)
    assert list(data["days"]) == ["2026-10-10"]
    stats = data["days"]["2026-10-10"]["hosts"]["x.com"]["by_route"][route_key("Match", "", "direct", "漏网之鱼")]
    assert stats["conns"] == 5 and stats["fails"] == 3 and stats["fail_kinds"] == {"reset": 3}
    assert data["status"]["value"] == "running"


def test_store_caps_hosts_per_day_keeping_heaviest(tmp_path, monkeypatch):
    monkeypatch.setattr(traffic_store, "MAX_HOSTS_PER_DAY", 2)
    store = TrafficStore(tmp_path)
    now = datetime(2026, 10, 10, 12)
    store.add_report("a1", _report(_item("a.com", conns=1), _item("b.com", conns=9), _item("c.com", conns=5)), now=now)
    day = store.load("a1", now=now)["days"]["2026-10-10"]
    assert set(day["hosts"]) == {"b.com", "c.com"}
    assert day["dropped"] == 1


# ---------- 归类 ----------

def _data(now, hosts, ip_only=0):
    return {"status": {"value": "running", "at": now.isoformat()}, "days": {
        now.date().isoformat(): {"ip_only_conns": ip_only, "dropped": 0, "hosts": hosts}}}


def _host(now, *routes):
    return {"first_seen": now.isoformat(), "last_seen": now.isoformat(), "by_route": {
        route_key(rule, "", outlet, policy): {"conns": conns, "fails": fails, "fail_kinds": {}, "zero_dl": 0,
                                               "up": 0, "down": 0}
        for rule, outlet, policy, conns, fails in routes}}


def test_registrable_domain_uses_public_suffix_list():
    assert registrable_domain("a.b.example.co.uk") == "example.co.uk"
    assert registrable_domain("www.google.com") == "google.com"


def test_summarize_groups_by_domain_and_tags():
    now = datetime(2026, 10, 10, 12)
    data = _data(now, {
        "www.blocked.com": _host(now, ("Match", "direct", "Final", 1, 9)),
        "cdn.blocked.com": _host(now, ("Match", "direct", "Final", 4, 0)),
        "ok.example.org": _host(now, ("Match", "direct", "Final", 10, 0)),
        "covered.net": _host(now, ("DomainSuffix", "proxy", "PROXY", 10, 0)),
        "pending.io": _host(now, ("Match", "direct", "Final", 2, 0)),
        "noise.ignored.dev": _host(now, ("Match", "direct", "Final", 50, 50)),
    }, ip_only=5)
    agent = {"id": "a1", "name": "gw"}

    def match_rule(host):
        return {"rule_id": "r1"} if host == "pending.io" else None

    result = summarize([(agent, data)], days=7, ignored=["ignored.dev"], match_rule=match_rule, now=now)
    by_domain = {item["domain"]: item for item in result["items"]}
    assert set(by_domain) == {"blocked.com", "example.org", "pending.io"}
    assert by_domain["blocked.com"]["tags"] == ["uncovered", "failing"]
    assert [host["host"] for host in by_domain["blocked.com"]["hosts"]] == ["www.blocked.com", "cdn.blocked.com"]
    assert by_domain["pending.io"]["tags"] == ["pending"]
    assert result["items"][0]["domain"] == "blocked.com"

    failing = summarize([(agent, data)], days=7, ignored=["ignored.dev"], view="failing", now=now)
    assert [item["domain"] for item in failing["items"]] == ["blocked.com"]
    ignored = summarize([(agent, data)], days=7, ignored=["ignored.dev"], view="ignored", now=now)
    assert [item["domain"] for item in ignored["items"]] == ["ignored.dev"]
    assert result["sniff_coverage"] == pytest.approx(round(77 / 82, 4))


def test_summarize_respects_day_range():
    now = datetime(2026, 10, 10, 12)
    yesterday = now - timedelta(days=1)
    data = _data(yesterday, {"old.com": _host(yesterday, ("Match", "direct", "Final", 1, 0))})
    assert summarize([({"id": "a"}, data)], days=1, now=now)["items"] == []
    assert len(summarize([({"id": "a"}, data)], days=7, now=now)["items"]) == 1


def test_format_ruleset_line():
    assert format_ruleset_line("classical", "DOMAIN-SUFFIX", "a.com") == "DOMAIN-SUFFIX,a.com"
    assert format_ruleset_line("domain", "DOMAIN-SUFFIX", "a.com") == "+.a.com"
    assert format_ruleset_line("domain", "DOMAIN", "a.com") == "a.com"


# ---------- 接口 ----------

@pytest.fixture
def env(tmp_path):
    repository = ProfileRepository(tmp_path)
    repository.save_shared({"rule_library": [
        {"id": "lib-url", "name": "remote", "source_type": "url", "url": "https://example.com/x.list",
         "behavior": "classical"},
        {"id": "lib-domain", "name": "my-domains", "source_type": "content", "content": "+.exists.com\n",
         "behavior": "domain"},
    ]})
    repository.save_profile("default", {
        "proxy_groups": [{"id": "g1", "name": "PROXY", "type": "select"}],
        "rule_configs": [
            {"id": "r1", "itemType": "rule", "rule_type": "DOMAIN-SUFFIX", "value": "pending.io",
             "policy": "PROXY", "enabled": True},
            {"id": "r-match", "itemType": "rule", "rule_type": "MATCH", "value": "", "policy": "DIRECT",
             "enabled": True},
        ],
    })
    config_module.set_repository(repository)
    manager = init_agent_manager()
    agent = manager.register_agent({"name": "gw", "host": "127.0.0.1", "service_type": "mihomo"})
    app = Flask(__name__)
    register_blueprints(app)
    return app.test_client(), repository, manager, agent


def _auth(agent):
    return {"Authorization": f"Bearer {agent['token']}"}


def test_report_requires_token_and_enabled_switch(env):
    client, _, manager, agent = env
    url = f"/api/agents/{agent['id']}/traffic-report"
    assert client.post(url, json=_report()).status_code == 401
    assert client.post(url, json=_report(), headers={"Authorization": "Bearer wrong"}).status_code == 401
    response = client.post(url, json=_report(), headers=_auth(agent))
    assert response.status_code == 409 and response.get_json()["enabled"] is False

    heartbeat = client.post(f"/api/agents/{agent['id']}/heartbeat", json={}, headers=_auth(agent))
    assert heartbeat.get_json()["domain_discovery_enabled"] is False
    assert client.put(f"/api/agents/{agent['id']}/domain-discovery", json={"enabled": True}).status_code == 200
    heartbeat = client.post(f"/api/agents/{agent['id']}/heartbeat", json={}, headers=_auth(agent))
    assert heartbeat.get_json()["domain_discovery_enabled"] is True

    assert client.post(url, json={**_report(), "schema": 9}, headers=_auth(agent)).status_code == 400
    ok = client.post(url, json=_report(_item("www.blocked.com", fails=5)), headers=_auth(agent))
    assert ok.status_code == 200 and ok.get_json()["rule_providers"] == {}


def test_switch_rejects_non_mihomo_agent(env):
    client, _, manager, _ = env
    other = manager.register_agent({"name": "dns", "host": "127.0.0.2", "service_type": "mosdns"})
    response = client.put(f"/api/agents/{other['id']}/domain-discovery", json={"enabled": True})
    assert response.status_code == 400


def test_domains_endpoint_lists_uncovered_and_pending(env):
    client, _, _, agent = env
    client.put(f"/api/agents/{agent['id']}/domain-discovery", json={"enabled": True})
    client.post(f"/api/agents/{agent['id']}/traffic-report", headers=_auth(agent), json=_report(
        _item("www.blocked.com", conns=0, fails=5, fail_kinds={"timeout": 5}),
        _item("api.pending.io"),
        _item("covered.net", rule="DomainSuffix", rule_payload="covered.net", outlet="proxy", policy="PROXY"),
    ))
    body = client.get("/api/domain-discovery/domains").get_json()
    by_domain = {item["domain"]: item for item in body["items"]}
    assert set(by_domain) == {"blocked.com", "pending.io"}
    assert by_domain["blocked.com"]["tags"] == ["uncovered", "failing"]
    assert by_domain["pending.io"]["tags"] == ["pending"]
    assert by_domain["pending.io"]["pending_rule"]["rule_id"] == "r1"
    assert body["agents"][0]["status"] == "running" and body["agents"][0]["enabled"] is True

    client.delete(f"/api/agents/{agent['id']}")
    assert client.get("/api/domain-discovery/domains").get_json()["items"] == []


def test_init_rulesets_creates_library_and_references_before_match(env):
    client, repository, _, _ = env
    assert client.post("/api/domain-discovery/rulesets/init", json={"proxy_policy": "NOPE"}).status_code == 400

    body = client.post("/api/domain-discovery/rulesets/init", json={"proxy_policy": "PROXY"}).get_json()
    assert body["direct"]["name"] == "域名发现-直连" and body["direct"]["active"] is True
    assert body["proxy"]["name"] == "域名发现-代理" and body["proxy"]["problem"] is None

    rules = repository.get_profile("default")["rule_configs"]
    assert [rule.get("policy") for rule in rules] == ["PROXY", "DIRECT", "PROXY", "DIRECT"]
    assert rules[-1]["rule_type"] == "MATCH"
    assert repository.get_profile("default")["domain_discovery"]["direct_ruleset"] == body["direct"]["id"]

    # 再次创建时名字不冲突
    again = client.post("/api/domain-discovery/rulesets/init",
                        json={"targets": ["direct"]}).get_json()
    assert again["direct"]["name"] == "域名发现-直连-2"


def test_apply_appends_deduplicates_and_reports_provider_revision(env):
    client, repository, _, agent = env
    assert client.post("/api/domain-discovery/apply", json={"items": [
        {"value": "a.com", "target": "proxy"}]}).status_code == 400

    client.post("/api/domain-discovery/rulesets/init", json={"proxy_policy": "PROXY"})
    response = client.post("/api/domain-discovery/apply", json={"items": [
        {"value": "Blocked.com", "rule_type": "DOMAIN-SUFFIX", "target": "proxy"},
        {"value": "www.blocked.com", "rule_type": "DOMAIN", "target": "proxy"},
        {"value": "local.cn", "rule_type": "DOMAIN", "target": "direct"},
    ]}).get_json()
    assert sorted(item["value"] for item in response["added"]) == ["blocked.com", "local.cn"]
    assert response["skipped"] == [{"value": "www.blocked.com", "target": "proxy", "reason": "exists"}]
    assert response["warnings"] == []

    library = {item["name"]: item for item in repository.get_shared()["rule_library"]}
    assert library["域名发现-代理"]["content"] == "DOMAIN-SUFFIX,blocked.com\n"
    assert library["域名发现-直连"]["content"] == "DOMAIN,local.cn\n"

    # 已写入的域名在列表里变成「待部署」
    client.put(f"/api/agents/{agent['id']}/domain-discovery", json={"enabled": True})
    report = client.post(f"/api/agents/{agent['id']}/traffic-report", headers=_auth(agent),
                         json=_report(_item("www.blocked.com", fails=4)))
    revisions = report.get_json()["rule_providers"]
    assert set(revisions) == {"域名发现-直连", "域名发现-代理"}
    item = client.get("/api/domain-discovery/domains").get_json()["items"][0]
    assert item["domain"] == "blocked.com" and item["tags"] == ["pending", "failing"]


def test_settings_accept_existing_domain_ruleset_and_reject_url_ruleset(env):
    client, repository, _, _ = env
    assert client.put("/api/domain-discovery/settings", json={"proxy_ruleset": "lib-url"}).status_code == 400
    body = client.put("/api/domain-discovery/settings", json={"proxy_ruleset": "lib-domain"}).get_json()
    assert body["proxy"]["active"] is False
    assert [item["id"] for item in body["candidates"]] == ["lib-domain"]

    response = client.post("/api/domain-discovery/apply", json={"items": [
        {"value": "exists.com", "target": "proxy"},
        {"value": "new.com", "rule_type": "DOMAIN", "target": "proxy"},
    ]}).get_json()
    assert [item["value"] for item in response["added"]] == ["new.com"]
    assert len(response["warnings"]) == 1
    library = {item["id"]: item for item in repository.get_shared()["rule_library"]}
    assert library["lib-domain"]["content"] == "+.exists.com\nnew.com\n"


def test_ignore_add_and_remove(env):
    client, repository, _, _ = env
    assert client.post("/api/domain-discovery/ignore", json={"values": ["bad host"]}).status_code == 400
    body = client.post("/api/domain-discovery/ignore", json={"values": ["Noise.dev", "noise.dev"]}).get_json()
    assert body["ignored"] == ["noise.dev"]
    body = client.delete("/api/domain-discovery/ignore", json={"values": ["noise.dev"]}).get_json()
    assert body["ignored"] == []
    assert repository.get_profile("default")["domain_discovery"]["ignored"] == []


def test_reregistration_keeps_domain_discovery_switch(env):
    client, _, manager, agent = env
    client.put(f"/api/agents/{agent['id']}/domain-discovery", json={"enabled": True})
    again = manager.register_agent({"name": "gw", "host": "127.0.0.1", "service_type": "mihomo"},
                                   existing_token=agent["token"])
    assert again["is_new"] is False
    assert manager.get_agent_by_id(agent["id"])["domain_discovery_enabled"] is True


def test_match_test_priority_counts_disabled_rules(env):
    client, repository, _, _ = env
    repository.save_profile("default", {"rule_configs": [
        {"id": "off", "itemType": "rule", "rule_type": "DOMAIN", "value": "x.com", "policy": "DIRECT", "enabled": False},
        {"id": "hit", "itemType": "rule", "rule_type": "DOMAIN-SUFFIX", "value": "pending.io", "policy": "PROXY",
         "enabled": True},
    ]})
    body = client.post("/api/rules/match-test", json={"query": "a.pending.io"}).get_json()
    assert body["rule_id"] == "hit" and body["priority"] == 2
