"""One-command native OxiDNS HA preparation; every subprocess is isolated."""
import json
import os
from pathlib import Path
import shutil
import shlex
import subprocess
import sys

from flask import Flask
import pytest

from backend.agents.go_install_script import (
    AGENT_CORE_INSTALL_INTERFACE, AGENT_INSTALL_DEPENDENCIES,
    DNS_HA_PREPARATION, DNS_HA_PREPARATION_INTERFACE, generate_go_agent_install_script,
)
from backend.routes.agents import get_install_script
from backend import test_single_agent_core_install as single


def generated(**options):
    return generate_go_agent_install_script('https://config.test', 'ha-node', 'oxidns',
                                            prepare_dns_ha=True, **options)


def test_markers_and_auto_install_keep_normal_installer_unchanged(tmp_path):
    for core in (False, True):
        source = generated(install_core=core)
        assert source.splitlines()[2] == DNS_HA_PREPARATION_INTERFACE
        if core:
            assert source.splitlines()[1] == AGENT_CORE_INSTALL_INTERFACE
        assert 'exec sudo sh "$0" "$@"' in source
        assert 'read -r choice < /dev/tty' not in source
        assert 'choice=1' in source
        assert 'DNS_HA_EFFECTIVE_PORT="8080"' in source
        assert source.index('agent_prepare_dns_ha\n') < source.index('BINARY_BASE=')
        script = tmp_path / f'install-{core}.sh'
        script.write_text(source)
        checked = subprocess.run(['sh', '-n', str(script)], capture_output=True, text=True)
        assert checked.returncode == 0, checked.stderr
    ordinary = generate_go_agent_install_script('https://config.test', 'plain', 'oxidns')
    assert DNS_HA_PREPARATION_INTERFACE not in ordinary
    assert 'read -r choice < /dev/tty' in ordinary
    assert 'agent_prepare_dns_ha' not in ordinary
    assert ordinary == generate_go_agent_install_script('https://config.test', 'plain', 'oxidns', prepare_dns_ha=False)


@pytest.mark.parametrize('options', [
    {'type': 'mihomo'}, {'type': 'mosdns'}, {'type': 'aio'}, {'agent_type': 'shell'},
    {'prepare_dns_ha': '1'}, {'prepare_dns_ha': 'yes'}, {'prepare_dns_ha': ''},
    {'service_manager': 'command', 'stop_command': '/bin/true', 'start_command': '/bin/true', 'status_command': '/bin/true'},
])
def test_unsupported_ha_requests_are_rejected(options):
    app = Flask(__name__)
    app.add_url_rule('/install-script', view_func=get_install_script)
    response = app.test_client().get('/install-script', query_string={'type': 'oxidns', 'prepare_dns_ha': 'true', **options})
    assert response.status_code == 400


def preparation_harness(tmp_path, manager='apt-get', init='systemd'):
    root = tmp_path / 'root'
    commands = tmp_path / 'commands'
    commands.mkdir()
    for directory in ('etc/init.d', 'etc/keepalived', 'etc/systemd/system', 'run/systemd/system', 'state', 'opt/configflow-agent'):
        (root / directory).mkdir(parents=True, exist_ok=True)
    for tool in 'awk basename cat chmod cp dirname grep head mkdir mktemp mv readlink rm sed wc sh uname'.split():
        (commands / tool).symlink_to(shutil.which(tool))
    (commands / 'sleep').write_text('#!/bin/sh\nexit 0\n')
    (commands / 'sleep').chmod(0o755)
    jq = shutil.which('jq')
    assert jq, 'Run this test on the Linux test server with jq installed'
    (commands / 'jq').symlink_to(jq)
    (commands / 'wget').write_text('#!/bin/sh\nexit 0\n')
    (commands / 'wget').chmod(0o755)
    # Present BusyBox-like ip is insufficient: the actual JSON query must work.
    (commands / 'ip').write_text('#!/bin/sh\nexit 2\n')
    (commands / 'ip').chmod(0o755)
    controller = commands / 'systemctl'
    controller.write_text(f'''#!{sys.executable} -S
import json, os, sys
from pathlib import Path
root=Path(os.environ['HA_ROOT']); args=sys.argv[1:]
with open(os.environ['HA_EVENTS'],'a') as log: log.write(json.dumps(['systemctl',args])+'\\n')
mask=root/'run/systemd/system/keepalived.service'; state=root/'state'
if args[0]=='show':
 print('masked' if mask.is_symlink() else 'loaded' if (state/'unit').exists() else 'not-found')
elif args[0]=='is-active': raise SystemExit(0 if (state/'active').exists() else 3)
elif args[0]=='is-enabled': print('enabled' if (state/'enabled').exists() else 'disabled'); raise SystemExit(0 if (state/'enabled').exists() else 1)
elif args[0]=='mask':
 mask.symlink_to('/dev/null')
 if os.environ.get('MASK_PARTIAL_FAILURE'): raise SystemExit(8)
elif args[0]=='disable': (state/'enabled').unlink(missing_ok=True)
''')
    controller.chmod(0o755)
    package_manager = commands / manager
    package_manager.write_text(f'''#!{sys.executable} -S
import json, os, sys
from pathlib import Path
root=Path(os.environ['HA_ROOT']); commands=Path(os.environ['HA_COMMANDS']); args=sys.argv[1:]
with open(os.environ['HA_EVENTS'],'a') as log: log.write(json.dumps([Path(sys.argv[0]).name,args])+'\\n')
if args==['update']: raise SystemExit(0)
if os.environ.get('FAIL_PACKAGE')=='before': raise SystemExit(43)
if 'keepalived' in args:
 p=commands/'keepalived'; p.write_text('#!/bin/sh\\nexit 0\\n'); p.chmod(0o755)
 (root/'state/unit').touch()
 (root/'state/enabled').touch()
 (root/'etc/keepalived/keepalived.conf').write_text('vrrp_instance SAMPLE {{ virtual_router_id 51 }}\\n')
 if Path(sys.argv[0]).name!='apk' and not (root/'run/systemd/system/keepalived.service').is_symlink(): (root/'state/unexpected-start').touch()
 if os.environ.get('FAIL_PACKAGE')=='after-keepalived': raise SystemExit(44)
if 'keepalived-openrc' in args:
 p=root/'etc/init.d/keepalived'; p.write_text('#!/bin/sh\\n'); p.chmod(0o755)
if 'iproute' in args or 'iproute2' in args:
 p=commands/'ip'; p.write_text('#!/bin/sh\\nprintf "[]\\\\n"\\n'); p.chmod(0o755)
''')
    package_manager.chmod(0o755)
    source = 'set -eu\ncf_step() { :; }; cf_step_done() { :; }; cf_fact() { :; }; cf_ui() { printf "%s\\n" "$*"; }\n'
    source += AGENT_INSTALL_DEPENDENCIES + '\n' + DNS_HA_PREPARATION.replace('{agent_port}', '8082')
    source += f'\nINIT_SYSTEM={init}\nAGENT_DIR={shlex.quote(str(root / "opt/configflow-agent"))}\nCONFIG_FILE="$AGENT_DIR/config-oxidns.json"\nagent_prepare_dns_ha\nprintf "ready:%s\\n" "$DNS_HA_EFFECTIVE_PORT"\n'
    for old, new in (('/etc/', str(root/'etc')+'/'), ('/run/', str(root/'run')+'/'), ('/usr/sbin/', str(root/'usr/sbin')+'/')):
        source=source.replace(old,new)
    script=tmp_path/'prepare.sh'; script.write_text(source)
    events=tmp_path/'events'; events.touch()
    env={**os.environ, 'PATH': str(commands), 'HA_ROOT': str(root), 'HA_COMMANDS': str(commands), 'HA_EVENTS': str(events)}
    return script, env, root, events


def run_preparation(script, env):
    return subprocess.run(['/bin/sh', str(script)], env=env, capture_output=True, text=True, timeout=30)


@pytest.mark.parametrize('manager,init,ip_package', [
    ('apt-get','systemd','iproute2'), ('apk','openrc','iproute2'), ('dnf','systemd','iproute'),
    ('yum','systemd','iproute'), ('pacman','systemd','iproute2'), ('zypper','systemd','iproute2'),
])
def test_missing_ha_packages_are_installed_once_without_activating_default_vip(tmp_path, manager, init, ip_package):
    script, env, root, events=preparation_harness(tmp_path, manager, init)
    result=run_preparation(script,env)
    assert result.returncode==0, result.stdout+result.stderr
    transactions=[item for item in map(json.loads, events.read_text().splitlines()) if item[0]==manager]
    assert ip_package in transactions[-1][1]
    assert 'keepalived' in transactions[-1][1]
    assert ('keepalived-openrc' in transactions[-1][1]) is (init=='openrc')
    assert not (root/'state/unexpected-start').exists()
    assert not (root/'run/systemd/system/keepalived.service').is_symlink()
    if init=='systemd': assert not (root/'state/enabled').exists()
    assert (root/'etc/keepalived/keepalived.conf').read_text().startswith('# ConfigFlow:')
    assert next((root/'etc/keepalived').glob('keepalived.conf.package-default.*')).read_text().startswith('vrrp_instance SAMPLE')
    assert run_preparation(script,env).returncode==0
    repeated=[item for item in map(json.loads, events.read_text().splitlines()) if item[0]==manager]
    assert repeated==transactions


@pytest.mark.parametrize('failure', ['before', 'after-keepalived'])
def test_failed_package_transaction_cleans_mask_and_samples_and_allows_retry(tmp_path, failure):
    script,env,root,events=preparation_harness(tmp_path)
    failed=run_preparation(script,{**env,'FAIL_PACKAGE':failure})
    assert failed.returncode!=0
    assert not (root/'run/systemd/system/keepalived.service').is_symlink()
    assert not (root/'state/enabled').exists()
    assert not (root/'state/unexpected-start').exists()
    assert run_preparation(script,env).returncode==0
    assert (root/'etc/keepalived/keepalived.conf').read_text().startswith('# ConfigFlow:')


def test_partial_mask_failure_is_cleaned_before_exiting(tmp_path):
    script,env,root,events=preparation_harness(tmp_path)
    result=run_preparation(script,{**env,'MASK_PARTIAL_FAILURE':'yes'})
    assert result.returncode!=0
    assert not (root/'run/systemd/system/keepalived.service').is_symlink()
    assert all(item[0]=='systemctl' for item in map(json.loads,events.read_text().splitlines()))


def test_missing_daemon_with_existing_user_config_is_rejected_before_package_changes(tmp_path):
    script,env,root,events=preparation_harness(tmp_path)
    config=root/'etc/keepalived/keepalived.conf'; config.write_text('vrrp_instance USER {}\n')
    result=run_preparation(script,env)
    assert result.returncode!=0
    assert config.read_text()=='vrrp_instance USER {}\n'
    assert not any(item[0]=='apt-get' for item in map(json.loads,events.read_text().splitlines()))


def full_installer(tmp_path, monkeypatch):
    original=single.generated
    monkeypatch.setattr(single, 'generated', lambda service='oxidns', **options: original(service, prepare_dns_ha=True, **options))
    script,env,root,events=single.harness(tmp_path,monkeypatch,'oxidns',install_core=True)
    for name, body in {'keepalived':'exit 0', 'ip':'printf "[]\\n"'}.items():
        path=tmp_path/'commands'/name; path.write_text('#!/bin/sh\n'+body+'\n'); path.chmod(0o755)
    assert shutil.which('jq'), 'jq must be installed on the test server'
    return script,env,root,events


def test_complete_generated_installer_automatically_runs_and_preserves_existing_custom_config(tmp_path,monkeypatch):
    script,env,root,events=full_installer(tmp_path,monkeypatch)
    result=subprocess.run(['/bin/sh',str(script)],env=env,capture_output=True,text=True)
    assert result.returncode==0, result.stdout+result.stderr
    config=root/'opt/configflow-agent/config-oxidns.json'
    original=json.loads(config.read_text())
    original.update(agent_name='existing-user-name',agent_port=18082,agent_id='retained-id',token='retained-token',
                    config_path='/srv/custom/oxidns.yaml',service_unit='custom-oxidns',health_url='http://localhost:9199/health')
    config.write_text(json.dumps(original,indent=2)+'\n')
    before=config.read_bytes()
    result=subprocess.run(['/bin/sh',str(script)],env=env,capture_output=True,text=True)
    assert result.returncode==0, result.stdout+result.stderr
    assert config.read_bytes()==before
    assert events.read_text().count('core --core oxidns')==1
    assert '端口 18082' in result.stdout


@pytest.mark.parametrize('flag',['--cancel','--uninstall'])
def test_maintenance_does_not_prepare_ha_or_install_core(tmp_path,monkeypatch,flag):
    script,env,root,events=full_installer(tmp_path,monkeypatch)
    (tmp_path/'commands/keepalived').write_text('#!/bin/sh\nexit 88\n')
    result=subprocess.run(['/bin/sh',str(script),flag],env=env,capture_output=True,text=True)
    assert result.returncode==0,result.stdout+result.stderr
    assert 'core --core' not in events.read_text()
    assert 'agent-download' not in events.read_text()
    assert 'DNS 高可用依赖已准备' not in result.stdout


def test_unusable_keepalived_stops_before_core_or_agent_mutation(tmp_path,monkeypatch):
    script,env,root,events=full_installer(tmp_path,monkeypatch)
    (tmp_path/'commands/keepalived').write_text('#!/bin/sh\nexit 88\n')
    result=subprocess.run(['/bin/sh',str(script)],env=env,capture_output=True,text=True)
    assert result.returncode!=0
    assert 'core --core' not in events.read_text()
    assert 'agent-download' not in events.read_text()


def test_existing_binary_without_systemd_unit_still_installs_service_package(tmp_path):
    script, env, root, events = preparation_harness(tmp_path)
    binary = Path(env['HA_COMMANDS']) / 'keepalived'
    binary.write_text('#!/bin/sh\nexit 0\n')
    binary.chmod(0o755)
    result = run_preparation(script, env)
    assert result.returncode == 0, result.stdout + result.stderr
    transactions = [item for item in map(json.loads, events.read_text().splitlines()) if item[0] == 'apt-get']
    assert 'keepalived' in transactions[-1][1]
    assert (root / 'state/unit').exists()
    assert not (root / 'state/unexpected-start').exists()
    assert not (root / 'run/systemd/system/keepalived.service').is_symlink()
    assert not (root / 'state/enabled').exists()


def test_full_installer_handles_root_path_without_usr_local_bin(tmp_path, monkeypatch):
    script, env, root, events = full_installer(tmp_path, monkeypatch)
    env['PATH'] = f'{tmp_path}/commands:/usr/sbin:/usr/bin:/sbin:/bin'
    result = subprocess.run(['/bin/sh', str(script)], env=env, capture_output=True, text=True)
    assert result.returncode == 0, result.stdout + result.stderr
    assert events.read_text().count('core --core oxidns') == 1
    assert (root / 'opt/configflow-agent/config-oxidns.json').exists()
