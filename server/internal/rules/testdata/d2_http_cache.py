"""Exact cache-path projection for live Flask/Go differentials only.

The real Python owner supplies the old filename. D2's declared compact JSON
format supplies the native filename independently from Go product code. All
other response/storage fields and unrecognized files remain untouched.
"""
import hashlib
import json
from pathlib import Path
from backend.utils.rule_utils import rule_cache_filename


def native_filename(rule):
    identity=rule.get('library_rule_id') or rule.get('id')
    identity=str(identity if identity is not None else rule.get('name',''))
    behavior=rule.get('behavior') if rule.get('behavior') is not None else 'domain'
    fields=[identity,rule['url'],behavior,rule.get('format') or '']
    return 'by-source/'+hashlib.sha256(d2_json(fields)).hexdigest()+'.list'


def d2_json(fields):
    text=json.dumps(fields,ensure_ascii=False,separators=(',',':'))
    for char,escape in [('&','\\u0026'),('<','\\u003c'),('>','\\u003e'),('\u2028','\\u2028'),('\u2029','\\u2029')]:
        text=text.replace(char,escape)
    return text.encode()



class CachePaths:
    def __init__(self, repository, root):
        self.repository=repository
        self.root=Path(root)
        self.aliases={}
        self.update()
        # Worlds are seeded by Go then copied. Translate only known native
        # fixture files on the Python copy, preserving their physical mtime.
        for old,native in self.aliases.items():
            for suffix in ('','.bak','.invalid'):
                source=self.root/'shared/rules'/(native+suffix)
                target=self.root/'shared/rules'/(old+suffix)
                if source.exists():
                    assert not target.exists(), 'ambiguous differential seed cache'
                    source.rename(target)

    def update(self):
        for rule in self.repository.get_shared().get('rule_library',[]) or []:
            if not isinstance(rule,dict) or rule.get('source_type','url')!='url' or not isinstance(rule.get('url'),str) or not rule['url']:
                continue
            # Do not grant path exemptions to malformed declarations. Their
            # R5/validation behavior must be asserted by their own references.
            if any(key in rule and rule[key] is not None and not isinstance(rule[key],str)
                   for key in ('id','library_rule_id','behavior','format')):
                continue
            self.aliases[rule_cache_filename(rule)]=native_filename(rule)

    def path(self, value):
        for suffix in ('','.bak','.invalid'):
            original=value[:-len(suffix)] if suffix else value
            if original in self.aliases:
                return self.aliases[original]+suffix
        return value

    def files(self, files):
        result={}
        for key,value in files.items():
            mapped=key
            prefix='shared/rules/'
            if key.startswith(prefix):
                mapped=prefix+self.path(key[len(prefix):])
            assert mapped not in result, 'D2 path collision would hide an extra file'
            result[mapped]=value
        return result

    def cache_reply(self, body):
        if isinstance(body,dict) and isinstance(body.get('results'),list):
            for row in body['results']:
                if isinstance(row,dict) and isinstance(row.get('local_path'),str):
                    row['local_path']=self.path(row['local_path'])
        return body
