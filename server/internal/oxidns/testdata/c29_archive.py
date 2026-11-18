"""Real Python C29 ZIP oracle; only loopback rule sources are admitted."""
import base64
import io
import json
import sys
import zipfile
from urllib.parse import urlparse
from backend.utils.oxidns_archive import build_oxidns_zip
from backend.routes import mosdns
original_fetch = mosdns._fetch_remote_content
fetches = {}

def fetch(url, *args, **kwargs):
    path = urlparse(url).path
    fetches[path] = fetches.get(path, 0) + 1
    assert urlparse(url).hostname == '127.0.0.1', 'non-loopback source'
    return original_fetch(url, *args, **kwargs)
mosdns._fetch_remote_content = fetch
for line in sys.stdin:
    row = json.loads(line)
    fetches = {}
    for file in row['files']:
        file['content'] = base64.b64decode(file['content'])
    try:
        content = build_oxidns_zip(row['config'], row['yaml'], row['downloads'], row['files'], base_url=row['base_url'])
        with zipfile.ZipFile(content) as archive:
            answer = {'files': {f.filename: base64.b64encode(archive.read(f)).decode() for f in archive.infolist()},
                              'names': archive.namelist(), 'modes': {f.filename: (f.external_attr >> 16) & 0o777 for f in archive.infolist()}}
    except Exception as error:
        answer = {'error': str(error), 'class': type(error).__name__}
    print(json.dumps({'answer': answer, 'fetches': fetches}), flush=True)
