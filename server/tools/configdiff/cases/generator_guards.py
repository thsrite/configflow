"""Python converter guard branches through actual default/scoped previews."""
from harness import Pair
from cases.config_backup import message
from cases.setup import Stable
from cases.subscriptions import require


def oxidns_vectors():
    hosts = '自定义 Hosts 第 1 行格式无效；请使用“域名规则 IPv4/IPv6 [更多 IP]”，或“IP 域名 [别名]”，不能混合地址与别名'
    return [
        ('H152', {'custom_hosts': 'name.example not-an-address'}, hosts),
        ('H155', {'local_dns': '['}, 'DNS 上游 YAML 语法无效'),
        ('H156', {'local_dns': '[1]'}, 'DNS 上游 YAML 每项需要 addr 地址'),
        ('H156', {'local_dns': '[]'}, 'DNS 上游 YAML 每项需要 addr 地址'),
        ('H157', {'local_dns': '# no usable upstream'}, 'DNS 上游配置无效'),
        ('H158', {'fallback_dns': '['}, 'DNS 上游 YAML 语法无效'),
        ('H159', {'custom_config': '['}, 'OxiDNS 高级 YAML 语法无效'),
        ('H160', {'custom_config': '[]'}, 'OxiDNS 高级 YAML 顶层必须为对象'),
        ('H161', {'custom_config': 'include: [external.yaml]'}, '完整部署模板不支持外部 include；请将插件内联到高级 YAML'),
        ('H162', {'custom_config': 'plugins: {}'}, '高级 YAML 的 plugins 必须为数组'),
        ('H163', {'custom_config': 'runtime: []'}, '高级 YAML runtime 必须为对象'),
        ('H164', {'rule_update': {'enabled': True}, 'custom_config': 'plugins: [{tag: custom-download, type: download, args: {}}]'},
         '已有自定义下载或重载插件，请在高级 YAML 中配置规则定时更新，不能自动创建第二条维护链'),
        ('H165', {'rule_update': {'enabled': True}}, '定时更新需要至少一个规则订阅'),
        ('H167', {'custom_config': 'plugins: [1]'}, '插件必须为对象'),
        ('H168', {'custom_config': 'plugins: [{tag: bad!tag, type: cache, args: {size: 9}}]'}, 'OxiDNS 插件 tag 必须为有效的 1–64 位 ASCII 标识'),
        ('H168', {'custom_config': 'plugins: [{tag: qs.exec.reserved, type: cache, args: {size: 9}}]'}, 'OxiDNS 插件 tag 必须为有效的 1–64 位 ASCII 标识'),
        ('H169', {'custom_config': 'plugins: [{tag: server_udp, type: cache, args: {size: 9}}]'}, 'OxiDNS 插件 tag 重复：server_udp'),
    ]


def oxidns(binary, recorder):
    for index, (case, settings, text) in enumerate(oxidns_vectors()):
        with Pair(binary, recorder, options={'profile': {'oxidns': settings}}) as pair:
            for template, path in [('POST /api/generate/oxidns/preview', '/api/generate/oxidns/preview'),
                    ('POST /api/profiles/<profile_id>/generate/oxidns/preview', '/api/profiles/default/generate/oxidns/preview')]:
                pair.request(case, template, path, body='{}', expect=400, writes=True, interaction=Stable(pair),
                             check=message(text), variant='converter-guard/' + str(index))


def mihomo(binary, recorder):
    for case, source, prefix in [
            ('H171', 'ipv6: "false"\n', 'ipv6 必须为 true 或 false，不能是字符串或拼写错误的值'),
            ('H172', 'dns: {ipv6: "false"}\n', 'dns 下的 ipv6 必须为 true 或 false，不能是字符串或拼写错误的值'),
            ('H173', '[\n', 'mihomo.custom_config YAML 解析失败: '),
            ('H174', '[]\n', 'mihomo.custom_config 不是 mapping，得到 list')]:
        with Pair(binary, recorder, options={'profile': {'mihomo': {'custom_config': source}}}) as pair:
            if case == 'H173':
                pair.policy.internal_errors['body.message'] = prefix
            for template, path in [('POST /api/generate/mihomo/preview', '/api/generate/mihomo/preview'),
                    ('POST /api/profiles/<profile_id>/generate/mihomo/preview', '/api/profiles/default/generate/mihomo/preview')]:
                def check(reply):
                    value = reply.json()
                    require(value.get('success') is False and (value.get('message', '').startswith(prefix) if case == 'H173'
                            else value.get('message') == prefix), 'the actual converter guard keeps its precise Chinese context')
                pair.request(case, template, path, body='{}', expect=500, writes=True, interaction=Stable(pair),
                             check=check, variant='converter-guard')


GROUPS = {'generator-oxidns-guard-branches': oxidns, 'generator-mihomo-guard-branches': mihomo}
