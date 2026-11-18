"""A18 oracle: actual Flask routes, Python HA owner, SQLite and loopback devices."""
import contextlib
import io
import json
import os
from pathlib import Path
import sys
with contextlib.redirect_stdout(io.StringIO()):
    import requests
    from flask import Flask
    from urllib.parse import urlsplit
    from backend.common import config
    from backend.common.config_repository import ProfileRepository
    from backend.common.agent_manager import get_agent_manager
    from backend.agents import oxidns_ha as ha, oxidns_ha_publish as pub, dns_follow
    from backend.routes import register_blueprints
    from backend.routes.auth import setup_before_request
    data=json.load(sys.stdin)
    pub.start_loop=lambda _manager:None
    dns_follow.start_loop=lambda _manager:None
    repo=ProfileRepository(Path(os.environ['DATA_DIR']))
    config.set_repository(repo)
    repo.save_system({'agents':data['agents']})
    repo.save_profile('default',{'oxidns':{'config_mode':'native','native_config':data['draft']}})
    original=requests.sessions.Session.request
    def local_only(self,method,url,**kwargs):
        parsed=urlsplit(url)
        assert parsed.hostname in data['urls'], 'unexpected network destination'
        url=data['urls'][parsed.hostname]+parsed.path+('?' + parsed.query if parsed.query else '')
        assert urlsplit(url).hostname=='127.0.0.1'
        return original(self,method,url,**kwargs)
    requests.sessions.Session.request=local_only
    manager=get_agent_manager()
    for member in data['agents']:
        manager.update_heartbeat(member['id'],{'service_status':'running'})
    app=Flask(__name__)
    register_blueprints(app)
    setup_before_request(app)
    client=app.test_client()
    from backend.common.auth import generate_token
    admin={'Authorization':'Bearer '+generate_token('synthetic-a18-admin')}
    replies=[]
    path='/api/agents/primary/oxidns-ha'
    def request(method,path,body=None,auth=True):
        r=client.open(path,method=method,data=body,content_type='application/json',headers=admin if auth else {})
        replies.append({'status':r.status_code,'body':'' if method=='HEAD' else r.get_json()})
    request('GET',path,auth=False)
    request('GET',path+'-environment',auth=False)
    request('GET',path)
    request('HEAD',path)
    request('GET',path+'-environment')
    request('PUT',path,data['body'])
    if replies[-1]['status']==202:
        for _ in range(14): pub.advance(manager,'primary')
        request('GET',path)
        request('GET','/api/agents/secondary/oxidns-ha')
        request('GET','/api/agents/primary/oxidns/rules/update')
        request('POST','/api/agents/primary/generate-config',json.dumps({'expected_profile_id':'default','expected_service_type':'oxidns','base_url':'https://panel.example'}))
        request('DELETE',path)
    result={'replies':replies,'stored':[]}
    for ident in ['primary','secondary']:
        a=manager.get_agent_by_id(ident)
        result['stored'].append({key:a[key] for key in ['oxidns_ha','oxidns_ha_state','oxidns_ha_publication'] if key in a})
print(json.dumps(result,ensure_ascii=False))
