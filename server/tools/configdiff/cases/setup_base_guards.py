"""Wizard guards reached through retained user base YAML and real preparation."""
import yaml

from harness import Pair
from sources import Source
from subscriptions import Exchange
from cases.config_backup import message
from cases.setup import ACTION, PATH, configure, call, row, session
from cases.pull_routes import SourceStable
from cases.setup_prepare import PREPARE, options
from cases.subscriptions import require


def rejected(binary, recorder):
    vectors = [
        ('H1016', 'router', '[]\n', {}, 'Mihomo 基础配置必须是 YAML 对象'),
        ('H1016', 'router', 'dns: []\n', {}, 'Mihomo 基础配置及 dns 必须是 YAML 对象，请先修正基础配置'),
        ('H1016', 'gateway', 'dns: {nameserver: [127.0.0.1:53]}\n', {},
         'Mihomo DNS 上游不能指向本机 OxiDNS 或系统 DNS，否则会形成解析循环；请先修改基础配置'),
        ('H1017', 'router', '{}\n', {'controller_secret': '123456'}, '不能使用初始模板的默认密码 123456，请自己设置一个'),
        ('H1017', 'gateway', '{}\n', {'controller_secret': 'short'}, '控制面板密码需为 8–64 个字母、数字或符号，不含空格'),
        ('H1018', 'router', 'secret: "123456"\n', {'controller_secret': None},
         '请设置控制面板密码：Mihomo 控制面板（external-controller）需要用它防止他人访问'),
        ('H1019', 'router', 'tun: []\n', {}, 'Mihomo tun 必须是对象，请先修正基础配置'),
        ('H1020', 'router', yaml.safe_dump({'tun': {'exclude-interface': ['synthetic0']}}), {},
         '主路由分流的完整 TUN 与现有 tun.exclude-interface 过滤冲突：未进入 TUN 的连接会在主路由与 Linux 之间回环。请先在 Mihomo 基础 YAML 中移除该过滤'),
        ('H1021', 'gateway', yaml.safe_dump({'tun': {'include-uid': [1000]}}), {},
         'Linux 网关与现有 tun.include-uid 过滤冲突，请先在 Mihomo 基础 YAML 中移除该过滤，避免客户端绕过接管')]
    for port, label in [(1053, ' Mihomo DNS 127.0.0.1:1053 '), (53, ' OxiDNS 监听 0.0.0.0:53 '),
                        (9199, ' OxiDNS 管理 API 127.0.0.1:9199 ')]:
        vectors.append(('H1022', 'router', yaml.safe_dump({'mixed-port': port}), {},
            'Mihomo 基础配置的 mixed-port 使用端口 ' + str(port) + '，与' + label + '冲突；请先在 Mihomo 基础配置中改用其他端口'))
    for number, (case, goal, text, patch, error) in enumerate(vectors):
        with Source(b'base guard must not open a source') as source:
            fixture = options(goal, profile={'mihomo': {'custom_config': text}})
            fixture['system_config'] = {'rule_fetch_proxy': source.url}
            with Pair(binary, recorder, options=fixture) as pair:
                configure(pair)
                original_profile, original_session = row(pair), session(pair)
                def check(reply):
                    message(error)(reply)
                    require(row(pair) == original_profile and session(pair) == original_session,
                        'base rejection preserves the entire owned profile and wizard session')
                # resolve_domain_rules initializes the built-in shared library
                # before base validation. Compare that real first write too.
                call(pair, case, ACTION, PATH + '/prepare', body={**PREPARE, **patch}, expect=400,
                    check=check, interaction=Exchange(pair, source, expected=0),
                    variant=goal + '/' + str(number) + '/initial-catalog')
                call(pair, case, ACTION, PATH + '/prepare', body={**PREPARE, **patch}, expect=400,
                    check=check, interaction=SourceStable(pair, source),
                    variant=goal + '/' + str(number) + '/repeat-exact-storage')


GROUPS = {'setup-prepare-retained-base-guards': rejected}
