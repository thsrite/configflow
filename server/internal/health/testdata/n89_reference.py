"""Record N8-9 typed-boundary expectations with the real Python business functions.

Only synthetic existing corpora are read. No Go output is read. Projection below
states the shared model R5/R8 text/integer/bool schema; it does not implement
verdicts, service classification, target choice or confidence calculations.
Run on a test server: PYTHONPATH=. ~/cf-test/.venv/bin/python
server/internal/health/testdata/n89_reference.py
"""
import copy
import gzip
import hashlib
import json
import math
import re
from pathlib import Path
from backend.utils import domain_probe as dp, health_check as hc, region_check as rc

HERE = Path(__file__).resolve().parent

class ContractError(Exception):
    def __init__(self, kind, message=''):
        self.kind, self.message = kind, message


def text(v):
    if v is None:
        return ''
    if isinstance(v, str):
        return v
    if type(v) in (int, float) and (type(v) is int or math.isfinite(v)):
        return json.dumps(v, ensure_ascii=False)
    raise ContractError('decode')


def integer(v):
    if isinstance(v, str):
        if not re.fullmatch(r'-?[0-9]+', v):
            return 0
        v = int(v)
    if type(v) not in (int,float) or not -(2**63) <= v < 2**63 or not math.isfinite(v) or int(v) != v:
        return 0
    return int(v)


def valid_integer(v):
    if isinstance(v,str):
        return re.fullmatch(r'-?[0-9]+',v) is not None and -(2**63) <= int(v) < 2**63
    return type(v) in (int,float) and -(2**63) <= v < 2**63 and math.isfinite(v) and int(v) == v


def finite_float(v):
    try:
        return v is None or type(v) in (int,float) and math.isfinite(float(v))
    except OverflowError:
        return False


def nullable_text(v):
    try:
        return None if v is None else text(v)
    except ContractError:
        return None


def required_text(obj, key, resource):
    if key not in obj:
        raise ContractError('check', f'{resource}缺少 {key}')
    if obj[key] is None or type(obj[key]) is bool or isinstance(obj[key], (dict, list)):
        value = json.dumps(obj[key], ensure_ascii=False, separators=(',', ':'))
        raise ContractError('check', f'{resource}的 {key} 必须是字符串，当前值：{value}')
    return text(obj[key])


def region(v, require_id=False):
    if not isinstance(v, dict):
        raise ContractError('decode')
    if not v:
        if require_id:
            raise ContractError('check', '区域检测结果缺少 id')
        return {}
    out = copy.deepcopy(v)
    if require_id or 'id' in v:
        out['id'] = required_text(v, 'id', '区域检测结果')
    status = v.get('status')
    out['status'] = integer(status) if valid_integer(status) else None
    try:
        out['final_url'] = text(v.get('final_url'))
    except ContractError:
        out['final_url'] = ''
    try:
        out['redirects'] = [text(x) for x in v['redirects']] if isinstance(v.get('redirects'), list) else []
    except ContractError:
        out['redirects'] = []
    out['markers'] = {k: nullable_text(x) for k, x in v['markers'].items()} if isinstance(v.get('markers'), dict) else {}
    out['error'] = v['error'] if isinstance(v.get('error'), str) else ''
    return out


def stats(v):
    if v is None:
        return None
    if not isinstance(v, dict):
        raise ContractError('decode')
    out = dict(v, ok=integer(v.get('ok')), total=integer(v.get('total')))
    ds = v.get('delays')
    out['delays'] = [0.0 if x is None else float(x) for x in ds] if isinstance(ds, list) and all(finite_float(x) for x in ds) else []
    out['errors'] = {k: integer(x) for k, x in v['errors'].items()} if isinstance(v.get('errors'), dict) else {}
    return out


def project(fn, args, kwargs):
    if fn in ('region_of_name', 'region_label'):
        return getattr(rc, fn)(text(args[0]))
    if fn.startswith('evaluate_') or fn == 'exit_info':
        if not isinstance(args[0], list):
            raise ContractError('decode')
        results = [region(x, True) for x in args[0]]
        if fn == 'evaluate_services':
            return rc.evaluate_services(results)
        if fn == 'exit_info':
            return rc.exit_info(results)
        # Match the existing pure-service corpus entry, without reimplementing it.
        return getattr(rc, '_' + fn)({x['id']: x for x in results})
    if fn == 'classify_domain_result':
        result = None if args[0] is None else region(args[0])
        return rc.classify_domain_result(result, args[1])
    if fn == 'pick_targets':
        if not isinstance(args[0], list):
            raise ContractError('decode')
        targets = []
        for item in args[0]:
            if not isinstance(item, dict):
                raise ContractError('decode')
            targets.append(dict(name=required_text(item, 'name', '区域检测目标'),
                                type=item.get('type') if isinstance(item.get('type'), str) else '',
                                alive=item.get('alive') if type(item.get('alive')) is bool else True))
        return rc.pick_targets(targets, text(args[1]))
    if fn == 'summarize_domain':
        if args[0] is None:
            return rc.summarize_domain({})
        if not isinstance(args[0], dict):
            raise ContractError('decode')
        return rc.summarize_domain({k: {'state': nullable_text(v.get('state')) or ''} if isinstance(v, dict) else {'state':''} for k,v in args[0].items()})
    if fn == 'path_healthy':
        return dp.path_healthy(stats(args[0]))
    if fn == 'judge':
        return dp.judge(stats(args[0]), stats(args[1]), **kwargs)
    if fn == 'probe_url':
        p = args[1]
        if p is not None and not valid_integer(p):
            raise ContractError('health', 'port must be integer')
        return dp.probe_url(args[0], integer(p))
    if fn == 'domain_suggestion':
        if args[0] is None:
            return None
        if not isinstance(args[0], list) or any(not isinstance(x,dict) for x in args[0]):
            raise ContractError('decode')
        hosts, originals = [], {}
        for i, h in enumerate(args[0]):
            p = h.get('probe')
            if not isinstance(p, dict) or not p:
                continue
            normalized = {k: nullable_text(p.get(k)) for k in ('host','verdict','target','checked_at')}
            normalized['checked_at'] = normalized['checked_at'] or ''
            confidence = p.get('confidence')
            valid = valid_integer(confidence)
            normalized['confidence'] = integer(confidence)
            normalized['reasons'] = p.get('reasons')
            originals[str(i)] = (normalized['host'], integer(confidence) if valid else None, nullable_text(p.get('checked_at')))
            normalized['host'] = str(i)
            hosts.append({'probe': normalized})
        result = dp.domain_suggestion(hosts)
        if result is not None:
            result['host'], result['confidence'], result['checked_at'] = originals[result['host']]
        return result
    if fn == 'initialize_provider_health':
        source = copy.deepcopy(args[0])
        if not isinstance(source, dict):
            raise ContractError('decode')
        old = source.get('health_check_url')
        if 'health_check' not in source:
            try:
                source['health_check_url'] = text(old)
            except ContractError:
                source['health_check_url'] = ''
        hc.initialize_provider_health(source)
        if 'health_check_url' in args[0]:
            source['health_check_url'] = old
        else:
            source.pop('health_check_url', None)
        return source
    raise KeyError(fn)


POSITIONAL = {
    'initialize_provider_health': ['source'], 'region_of_name':['name'], 'region_label':['code'],
    'evaluate_services':['results'], 'exit_info':['results'], 'classify_domain_result':['result','original_url'],
    'pick_targets':['targets','preferred_group'], 'summarize_domain':['per_target'], 'path_healthy':['stats'],
    'judge':['direct','proxy'], 'probe_url':['host','port'], 'domain_suggestion':['hosts'],
}

CONTRACTS = {
    'region_of_name': 'model.String: numeric text and invalid root rejection',
    'region_label': 'model.String: numeric text and invalid root rejection',
    'initialize_provider_health': 'model.String consumes legacy URL; original source fields retained',
    'pick_targets': 'AvailableTarget: required textual identity, R5 type/alive defaults',
    'exit_info': 'RegionResult: required identity, nullable marker text, R5 auxiliary fields',
    'classify_domain_result': 'RegionResult: shared Int status and model.String URL/markers',
    'summarize_domain': 'ordered TargetState records: absent/wrong state reads as zero',
    'path_healthy': 'PathStats: shared Int counts with R5 fallback; Python computes path health',
    'judge': 'PathStats: shared Int counts, float64 delays, R5 fields; Python computes full verdict',
    'domain_suggestion': 'Suggestion: nullable text/shared Int confidence; Python selects best typed probe',
    'probe_url': 'typed integer port: shared Int accepted forms, native validation otherwise',
    'settings': 'strconv environment timeout grammar; original setting error envelope',
    '_run_probe': 'native JSON refuses nonfinite worker output; explicit unavailability message',
    'run_probe': 'native JSON refuses nonfinite worker output; explicit unavailability message',
    'check_settings': 'native JSON refuses nonfinite worker output; explicit DNS error message',
    'ProbeStore.save': 'write validation: object entry and explicitly textual checked_at',
    'RegionStore.update': 'write validation: object sections/entries and explicitly textual checked_at',
}

def main():
    sources, expected = {}, []
    for filename in ('corpus.json', 'review_c16.jsonl.gz', 'c16_recorded.jsonl.gz'):
        path = HERE / filename
        sources[filename] = hashlib.sha256(path.read_bytes()).hexdigest()
        rows = json.loads(path.read_text())['rows'] if filename.endswith('.json') else [json.loads(x) for x in gzip.open(path, 'rt')]
        for i, row in enumerate(rows):
            fn = row['fn']
            if fn not in POSITIONAL and not fn.startswith('evaluate_') and fn not in ('ProbeStore.save', 'RegionStore.update', 'settings', '_run_probe', 'check_settings', 'run_probe'):
                continue
            named = isinstance(row.get('args'), dict)
            args = [row['args'].get(k) for k in POSITIONAL.get(fn,['results'])] if named else row.get('args',[])
            kwargs = {k: row['args'][k] for k in ('passive_failing','direct_healthy','proxy_healthy') if k in row['args']} if named else row.get('kwargs') or {}
            item = {'file':filename, 'row':i, 'fn':fn, 'contract':CONTRACTS.get(fn, 'RegionResult: required identity and shared scalar/R5 fields; real Python service verdict')}
            try:
                if fn == 'settings':
                    value = row.get('env',{}).get('CONFIGFLOW_NODE_TEST_TIMEOUT_MS','5000').strip()
                    if '_' in value or not value.lstrip('+-').isascii():
                        raise ContractError('probe-unavailable', '节点测速设置无效，请检查测试地址及超时（500–10000 毫秒）')
                    continue
                if fn in ('_run_probe', 'check_settings', 'run_probe'):
                    payload = row.get('worker',{}).get('stdout', row.get('stdout',''))
                    if any(word in payload for word in ('NaN', 'Infinity')):
                        message = 'DNS 检测组件返回了无效结果，请检查组件版本' if fn == 'check_settings' else '测速组件返回了无效结果，请检查组件版本'
                        raise ContractError('probe-unavailable', message)
                    continue
                if fn in ('ProbeStore.save', 'RegionStore.update'):
                    records = [row['args'].get('results')] if fn == 'ProbeStore.save' else [row.get('mutated',{}).get(k) for k in ('domains','websites')]
                    if any(not isinstance(items,dict) or any(not isinstance(x,dict) or 'checked_at' in x and not isinstance(x['checked_at'],str) for x in items.values()) for items in records):
                        message = '探测记录必须为对象且 checked_at 为字符串' if fn == 'ProbeStore.save' else '区域检测存储的 domains 与 websites 必须为对象且 checked_at 为字符串'
                        raise ContractError('check',message)
                    continue
                try:
                    json.dumps(args,allow_nan=False)
                except ValueError:
                    raise ContractError('decode')
                out = project(fn, args, kwargs)
                # Store writes need explicit validation, not Python comparison.
                original = row.get('mutated', row.get('ok')) if named else row.get('out')
                if row.get('error') or row.get('err') or json.dumps(out,ensure_ascii=False,sort_keys=True) != json.dumps(original,ensure_ascii=False,sort_keys=True):
                    item['out'] = out
                else:
                    continue
            except ContractError as e:
                item.update(error_kind=e.kind, message=e.message)
            except Exception:
                # Preserve original comparison for native URL rules and cases not projected here.
                continue
            expected.append(item)
    output = {'sources':sources, 'expectations':expected}
    (HERE / 'n89_expected.json').write_text(json.dumps(output, ensure_ascii=False, indent=2, allow_nan=False)+'\n')
    print('typed reference expectations:', len(expected))

if __name__ == '__main__':
    main()
