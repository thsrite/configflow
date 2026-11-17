"""Named reference defects; safe Go fixes require review, never a false pass."""
from dataclasses import dataclass


@dataclass(frozen=True)
class ReferenceBug:
    owner: str
    summary: str
    followups: tuple = ()
    severity: str = 'medium'


BUGS = {
    'IKUAI_NETMASK_SUFFIX_ROLLBACK': ReferenceBug('R05（iKuai）',
        '默认 fake 保留真实 static_rt netmask 显示后缀。Python 发布返回 502，新增静态路由的回读和补偿均因后缀不匹配失败，'
        '设备留下配置。未修改 fake 来掩盖此问题。',
        ('普通 netmask 的独立变体覆盖 R011-S02 成功路径；默认后缀变体记录这个 Python bug。',
         'L5 待核：真实固件 static_rt 的 netmask 后缀形态，以及失败后设备补偿是否干净。')),
    'AGENT_FORWARD_FILTER_UNHASHABLE': ReferenceBug('A20（Linux gateway）及 R15（forwarding HTTP）',
        '一个 Agent 的 forward_filter.state/source.kind/remedy.source 返回数组或对象时，Python 用不可哈希值查 frozenset，'
        '抛出未捕获 TypeError，整个 GET 返回 HTML 500，另一台健康网关的信息也丢失。应隔离这份不可信报告并保留健康条目。'),
    'HEAD_CREATES_ROUTER': ReferenceBug('R15（router HTTP）',
        'GET/POST 共用的路由器集合处理函数只按 method==GET 分支；HEAD 落入创建分支。无 body 返回400，'
        '合法 body 会新增路由器并递增配置 revision，响应 body 被 HEAD 协议隐藏。Go HEAD 必须只读且按 GET 返回200。'),
    'HEAD_PERSISTS_FORWARDING': ReferenceBug('R15（forwarding HTTP）及 A20（Linux gateway）',
        'GET/POST 共用的 forwarding 处理函数只按 method==GET 分支；HEAD 落入持久化分支。无 body 返回400且不读 Agent，'
        '合法 agent_id 会向设备 POST 并开启持久化转发。Go HEAD 必须执行只读 GET，不得写设备。'),
}
