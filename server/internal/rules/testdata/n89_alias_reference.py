"""Real Python provider copy with an external anchor; only D2 hash is projected."""
import ast
import hashlib
import json
from pathlib import Path
from types import SimpleNamespace
import yaml
from backend.utils import group_candidates as gc
from server.internal.rules.testdata.d2_http_cache import d2_json
source='''shared: &health # keep health
  enable: true
  url: https://example.com/check
  interval: 300
proxy-providers:
  Synthetic:
    type: http
    url: https://example.com/provider
    health-check: *health
    extension: *health
  Other: {type: http, url: https://example.com/other}
proxy-groups: [{name: Group, use: [Synthetic, Other]}]
'''
config={'proxy_groups':[{'id':'g','name':'Group','subscriptions':['s'],'exclude_regex':'Ads'}],
        'subscriptions':[{'id':'s','name':'Synthetic'}],'subscription_aggregations':[],'nodes':[]}
main=yaml.safe_load(source)
real_sha256=hashlib.sha256
previous=gc.hashlib
gc.hashlib=SimpleNamespace(sha256=lambda raw:real_sha256(d2_json(ast.literal_eval(raw.decode()))))
try:
    gc.lower_group_provider_filters(config,main)
finally:
    gc.hashlib=previous
Path(__file__).with_name('n89_alias_expected.json').write_text(json.dumps({'source':source,'config':config,'native':main},ensure_ascii=False,indent=2)+'\n')
print('external alias provider reference: 1')
