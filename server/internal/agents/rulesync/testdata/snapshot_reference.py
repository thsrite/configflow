"""Real Python snapshot archive refusals; all transport is an in-memory fake."""
import network_guard  # noqa: F401
import base64
import copy
import json
import tarfile
import pytest
from backend.test_rule_sync_snapshot import snapshot, _archive, _serve, _fetch
from backend.agents.rule_sync_projection import read_bundle_files
cases=[]
for kind in ('valid','identity','digest','missing-file','missing-entry','duplicate-entry','size-type','duplicate-file','symlink','foreign-path','bad-main','gzip'):
    f=snapshot.__wrapped__()
    f['agent']['host']='127.0.0.1'
    if kind=='identity': f['manifest']['profile_id']='other'
    if kind=='digest': f['manifest']['files'][1]['sha256']='0'*64
    if kind=='missing-entry': f['manifest']['files']=f['manifest']['files'][:-1]
    if kind=='duplicate-entry': f['manifest']['files'][1]=f['manifest']['files'][0]
    if kind=='size-type': f['manifest']['files'][1]['size']=True
    if kind=='bad-main': f['agent']['applied_content']['config_version']='0'*64
    extra=[]
    if kind=='duplicate-file': extra=[('manifest.json',b'{}')]
    if kind=='foreign-path': extra=[('files/../other',b'')]
    if kind=='symlink':
        member=tarfile.TarInfo('files/link');member.type=tarfile.SYMTYPE;member.linkname='/outside';extra=[(member,b'')]
    raw=b'bad gzip' if kind=='gzip' else _archive(f['manifest'],f['files'],omit=['files/rules/direct.txt'] if kind=='missing-file' else [],extra=extra)
    case={'name':kind,'archive':base64.b64encode(raw).decode(),'expected':f['baseline']['bundle'].manifest,'files':{path:{'data':base64.b64encode(content).decode(),'role':role} for path,(content,role) in read_bundle_files(f['baseline']['bundle']).items()},'version':f['agent']['applied_content']['config_version']}
    with pytest.MonkeyPatch.context() as patch:
        _serve(patch,raw)
        try:
            result=_fetch(f)
            case['result']={path:{'data':base64.b64encode(content).decode(),'role':role} for path,(content,role) in read_bundle_files(result).items()}
        except ValueError as error: case['error']=str(error)
    cases.append(case)
print(json.dumps(cases,ensure_ascii=False))
