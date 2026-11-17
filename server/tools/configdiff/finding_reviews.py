"""Preserve actual finding recheck outcomes without treating skips as closure."""
import copy
from config_finding_resolutions import annotate


def key(result):
    return result['case'], result['variant'], result['route']


def reviews(previous, current, saved=(), baseline=''):
    by_key = {key(item): copy.deepcopy(item) for item in saved}
    for item in list(previous) + list(current):
        if item['state'] not in ('different', 'O12-separate') or key(item) in by_key:
            continue
        by_key[key(item)] = {name: copy.deepcopy(item[name]) for name in ('case', 'variant', 'route', 'group', 'differences')}
        by_key[key(item)].update(status='pending', checked_baseline='', previous_state=item['state'])
    for item in current:
        identity = key(item)
        if identity not in by_key:
            continue
        record = by_key[identity]
        record['status'] = {'compared': 'closed', 'different': 'open', 'O12-separate': 'separate'}.get(item['state'], 'blocked')
        record['checked_baseline'] = baseline or 'unlabelled-run'
        record['python_status'], record['go_status'] = item.get('python_status'), item.get('go_status')
        record['remaining_differences'] = copy.deepcopy(item.get('differences', []))
    return annotate(sorted(by_key.values(), key=key))


def markdown(records):
    if not records:
        return []
    lines = ['## 既有差异复验状态', '',
             '仅实际相比通过标为closed；accepted要求全部原始差异精确符合链接中的批准决定，仍保留严格比较证据。未实现/依赖跳过标blocked，未重跑标pending。基线表示该条最后一次复验；未重跑条目不随本轮更新。', '',
             '| 用例 / 变体 | 路由 | 状态 | 修复提交 / 接受依据 | 最后复验Go基线 | Python / Go HTTP |',
             '|---|---|---|---|---|---|']
    for item in records:
        def safe(value):
            return str(value).replace('|', '\\|').replace('\n', ' ')
        lines.append('| ' + safe(item['case'] + ' / ' + item['variant']) + ' | `' + safe(item['route']) + '` | '
                     + item['status'] + ' | ' + (', '.join(item.get('fix_commits', [])) or item.get('accepted_basis', '') or '—')
                     + ('；' + safe(item['closure_note']) if item.get('closure_note') else '')
                     + ' | ' + (item['checked_baseline'] or '尚未复验') + ' | '
                     + str(item.get('python_status', '—')) + ' / ' + str(item.get('go_status', '—')) + ' |')
    return lines + ['']
