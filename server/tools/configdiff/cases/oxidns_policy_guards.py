"""Template device-policy and retained-resource refusals through editor APIs."""
from harness import Pair, json_body
from sources import Source
from cases.config_backup import message
from cases.oxidns_editor import text
from cases.oxidns_routes import BASE, SCOPES, api
from cases.pull_routes import SourceStable


def guards(binary, recorder):
    policy = {'ip': '192.0.2.8', 'name': '合成设备🧪', 'mode': 'direct'}
    rows = [
        ('H429', {'tag': 'shadow_dns', 'type': 'udp_server', 'args': {'listen': '0.0.0.0:53', 'entry': 'unmanaged'}},
         '监听入口 shadow_dns（0.0.0.0:53）监听客户端 DNS 端口 53，但 entry 是 unmanaged，没有进入主处理链 sequence_main'
         '（经这里查询的客户端不受设备策略控制），无法安全插入设备策略；请在 OxiDNS 完整参数中把它的 entry 改为 sequence_main，或让它改用其他端口。'),
        ('H432', {'tag': 'cf_device_policy', 'type': 'sequence', 'args': [{'exec': 'return'}]},
         '完整参数中存在同名设备策略插件，请先处理命名冲突。'),
        ('H433', {'tag': 'custom_policy_consumer', 'type': 'sequence', 'args': [{'exec': 'jump cf_device_policy'}]},
         '设备策略插件存在额外引用，请先在完整参数中处理。')]
    for template, prefix in SCOPES:
        with Source(b'template policy guards never fetch sources') as source, Pair(binary, recorder) as pair:
            for case, plugin, expected in rows:
                settings = {'device_policies': [policy], 'custom_config': text({'plugins': [plugin]})}
                pair.request(case, 'POST ' + template + '/editor', prefix + '/editor',
                    body=json_body({'settings': settings, 'base_url': BASE}), expect=400, writes=True,
                    interaction=SourceStable(pair, source), check=message(expected), variant='template-policy/' + case)
            start = api(pair, template, prefix, 'editor', 'S01', {'settings': {
                'custom_hosts': 'synthetic.example: 192.0.2.8\n', 'custom_config': text({'plugins': [
                    {'tag': 'independent_hosts_consumer', 'type': 'hosts', 'args': {'files': ['./rules/custom_hosts.txt']}}]})},
                'base_url': BASE}, variant='retained-Hosts-content-has-an-independent-consumer')['settings']
            pair.request('H434', 'POST ' + template + '/template-preview', prefix + '/template-preview',
                body=json_body({'settings': start, 'template_settings': {**start['template_settings'], 'custom_hosts': ''},
                    'resolutions': {'/resources/custom_hosts': 'template'}, 'base_url': BASE}), expect=400, writes=True,
                interaction=SourceStable(pair, source),
                check=message('Hosts 规则文件内容仍被完整配置引用，请先移除相关文件引用再清空内容'),
                variant='explicit-template-choice-cannot-delete-still-referenced-content')


GROUPS = {'oxidns-template-policy-and-retained-resource-guards': guards}
