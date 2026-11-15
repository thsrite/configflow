#!/usr/bin/env python3
"""Generate review.json (review C02): safe_url_for_log on hosts whose lower-casing
depends on CPython 3.11's Unicode tables (Final_Sigma context, Other_Uppercase,
letters added after Unicode 14) and on ipaddress's scope-id rules; plus
requests' URL preparation used by local_http. Synthetic values only.

    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/urlx/testdata/gen_review.py
"""
import json
import sys
from pathlib import Path

import requests

assert sys.version_info[:2] == (3, 11)
from backend.utils.url_utils import safe_url_for_log  # noqa: E402

HOSTS = ['ⒶΣ.test', 'Ⓐ.test', 'AΣ.test', 'AΣ́.test', 'xΣͅ.test', 'ʰΣ.test', 'Ɤ.test', 'Ꟛ.test', '\U00016E40.test',
         '\U0001E922x.test', 'ǅΣ.test', 'İ.test', 'Σ.test', 'K.test', 'Ⱟ.test', 'Ᲊ.test']
BRACKETED = ['[fe80::1%25eth0]', '[fe80::1%eth0]', '[fe80::1%]', '[fe80::1%a%b]', '[fe80::1%%]', '[::ffff:1.2.3.4]', '[::1.2.3.4]',
             '[1:2:3:4:5:6:7:8]', '[1:2:3:4:5:6:7::]', '[::01.2.3.4]', '[v1.x]', '[V1.x]', '[v1.]', '[vg.x]', '[::1%x y]', '[0::0]',
             '[1::2::3]', '[:::]', '[::ffff:256.1.1.1]']
URLS = [f'https://user:pw@{h}/p?token=1&a=b' for h in HOSTS + BRACKETED]
URLS += ['https://x.test/p?%C4%B0token=1', 'https://x.test/p?Key=1', 'https://x.test/p?k%C3%89y=1', 'https://x.test/p?ﬁle=1']

PREPARE = ['http://config.test/api/rules', 'http://Config.TEST:080/api/x', 'http://config.test/api/a%41%zz', 'http://config.test/api/%zz',
           'http://config.test/api/x?a=b c', 'http://config.test/api/x?a=%zz&b=%41', 'http://config.test/api/x#frag', 'http://config.test/api/x?q#f',
           'http://config.test/api/%7euser', 'http://config.test/api/a b', 'http://config.test/api/é', 'http://config.test/api/x?é=ü',
           'http://config.test/api/../x', 'http://config.test/api/./x', 'http://config.test', 'http://config.test:0/api/x',
           'http://user:p w@config.test/api/x', 'HTTP://CONFIG.TEST/api/X', 'http://config.test/api/x?a=1&a=2', 'http://config.test/api/%2e%2e/x',
           'http://[::1]:8080/api/x', 'http://config.test/api/x?%7e=%7e', 'http://config.test/api/x|y', 'http://config.test/api/x?a=|']


def prepared(url):
    try:
        return requests.Request('GET', url).prepare().url
    except Exception as exc:
        return {'error': type(exc).__name__}


out = {'urls': [[url, safe_url_for_log(url)] for url in URLS],
       'prepare': [[url, prepared(url)] for url in PREPARE]}
Path(__file__).with_name('review.json').write_text(json.dumps(out, ensure_ascii=True, indent=0), encoding='ascii')
