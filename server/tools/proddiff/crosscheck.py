"""Same-input crosscheck of run.py (Go-vs-Go mode) against the Go comparator.

Temporary parity tool for the comparator port; it is deleted together with
run.py when the switch removes the Python mode. Run from the repository root on
the test server; corpora hold real responses and stay in the private run dir.

  crosscheck.py mutate <corpus> <out>   originals + seeded mutations (right side)
  crosscheck.py diff <corpus> <out>     run.py response_diffs, one sorted list per entry
  crosscheck.py compare <a> <b> <corpus>  entry-by-entry equality; prints schema-only mismatches

The Go side is `comparator crosscheck <corpus> <out>` (same rules, one shared
Agent identity pairing, no artifact proofs).
"""
import base64
import copy
import json
import os
import random
import re
import sys
from pathlib import Path

os.environ['PRODDIFF_MODE'] = 'go'
sys.path.insert(0, str(Path(__file__).parent))
import run as pd  # noqa: E402
import yaml  # noqa: E402


def load(entry):
    def reply(side):
        status, content_type, body, headers = side
        body = base64.b64decode(body)
        return (status, content_type, body) if headers is None else (status, content_type, body, headers)
    return entry['route'], reply(entry['left']), reply(entry['right'])


def dump(route, left, right):
    def side(reply):
        return [reply[0], reply[1], base64.b64encode(reply[2]).decode(), reply[3] if len(reply) > 3 else None]
    return json.dumps({'route': route, 'left': side(left), 'right': side(right)}) + '\n'


def nodes(value, keys=()):
    yield keys, value
    children = value.items() if isinstance(value, dict) else enumerate(value) if isinstance(value, list) else ()
    for key, child in children:
        yield from nodes(child, keys + (key,))


def locate(value, keys):
    for key in keys:
        value = value[key]
    return value


def mutate_document(rng, document):
    """One structural edit; returns the edited copy or None."""
    document = copy.deepcopy(document)
    candidates = list(nodes(document))
    rng.shuffle(candidates)
    for keys, value in candidates:
        if not keys:
            continue
        parent = locate(document, keys[:-1])
        choice = rng.randrange(6)
        if choice == 0 and not isinstance(value, (dict, list)):
            parent[keys[-1]] = (not value if isinstance(value, bool) else value + 1 if isinstance(value, (int, float))
                                else 0 if value is None else str(value) + '-x')
        elif choice == 1 and isinstance(parent, dict):
            del parent[keys[-1]]
        elif choice == 2 and isinstance(value, dict):
            value['crosscheck_extra'] = 1
        elif choice == 3 and isinstance(value, list) and len(value) >= 2:
            value[0], value[1] = value[1], value[0]
        elif choice == 4 and isinstance(value, int) and not isinstance(value, bool):
            parent[keys[-1]] = float(value)
        elif choice == 5:
            parent[keys[-1]] = None if value is not None else ''
        else:
            continue
        return document
    return None


def mutate_body(rng, route, reply):
    body = reply[2]
    content_type = reply[1]
    try:
        if 'json' in content_type:
            changed = mutate_document(rng, json.loads(body))
            return None if changed is None else json.dumps(changed, ensure_ascii=False, indent=rng.choice([None, 1])).encode()
        text = body.decode('utf-8')
    except (ValueError, UnicodeError):
        return None
    if 'yaml' in content_type or re.search(r'/(mihomo|mosdns|oxidns)(/preview)?$', route):
        try:
            changed = mutate_document(rng, yaml.safe_load(text))
        except yaml.YAMLError:
            return None
        return None if changed is None else yaml.safe_dump(changed, allow_unicode=True, sort_keys=rng.choice([True, False])).encode()
    lines = text.split('\n')
    index = rng.randrange(len(lines))
    choice = rng.randrange(3)
    if choice == 0:
        lines[index] += ' # crosscheck'
    elif choice == 1:
        lines.insert(index, '# crosscheck')
    else:
        return text.replace('\n', '\r\n').encode()
    return '\n'.join(lines).encode()


def mutations(rng, route, left, right):
    out = [(left, right), (right, left)]
    body = mutate_body(rng, route, right)
    if body is not None:
        out.append((left, right[:2] + (body,) + right[3:]))
    if len(right) > 3:
        headers = dict(right[3])
        names = sorted(headers)
        if names and rng.random() < .5:
            name = rng.choice(names)
            if rng.random() < .5:
                headers[name] += '-x'
            else:
                del headers[name]
        else:
            headers['x-crosscheck'] = '1'
        out.append((left, right[:3] + (headers,)))
    if rng.random() < .2:
        out.append((left, (right[0] + 1,) + right[1:]))
    if rng.random() < .1:
        out.append((left, (right[0], 'text/plain') + right[2:]))
    return out


def main(argv):
    command = argv[1]
    if command == 'mutate':
        rng = random.Random(20261123)
        with open(argv[2]) as source, open(argv[3], 'w') as target:
            for line in source:
                route, left, right = load(json.loads(line))
                for a, b in mutations(rng, route, left, right):
                    target.write(dump(route, a, b))
        return 0
    if command == 'diff':
        rows = [line.split('\t') for line in (pd.PARITY / 'routes.tsv').read_text().splitlines() if line and not line.startswith('#')]
        rules = pd.load_rules(rows)
        identities = pd.AgentComparisonIdentities()
        with open(argv[2]) as source, open(argv[3], 'w') as target:
            for line in source:
                route, left, right = load(json.loads(line))
                try:
                    found = sorted(list(pair) for pair in pd.response_diffs(left, right, route, rules, identities, None))
                except Exception:  # noqa: BLE001  any crash is a mismatch the compare step reports
                    found = 'error'
                target.write(json.dumps(found) + '\n')
        return 0
    if command == 'compare':
        with open(argv[2]) as a, open(argv[3]) as b, open(argv[4]) as corpus:
            total = mismatches = 0
            for left, right, entry in zip(a, b, corpus):
                total += 1
                if json.loads(left) != json.loads(right):
                    mismatches += 1
                    route = json.loads(entry)['route']
                    print('MISMATCH', total, route, 'python=' + left.strip(), 'go=' + right.strip())
        print(f'crosscheck: entries={total} mismatches={mismatches}')
        return int(mismatches > 0)
    return 2


if __name__ == '__main__':
    sys.exit(main(sys.argv))
