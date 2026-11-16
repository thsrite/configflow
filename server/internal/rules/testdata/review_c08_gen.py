#!/usr/bin/env python3
"""Review C08 references: fuzzed inputs and what CPython 3.11 makes of them.

Run from the repository root on the test server (synthetic values only):

    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/rules/testdata/review_c08_gen.py \
        server/internal/rules/testdata/review_c08.jsonl.gz

One JSON object per line (ASCII), deterministic seed. Kinds:

    rx      a pattern from a grammar fuzzer, IGNORECASE or not: the error
            (re.error text, or the class of another exception) or, for each
            subject, the span of re.search (null = no match)
    rxerr   rule_value_error('DOMAIN-REGEX', pattern) (_regex_error)
    syn     rule_value_error(kind, value) on fuzzed kinds and values
    split   split_rule_payload(line)
    line    parse_rule_line(line) on fuzzed lines of every dialect
    ip      ip_address / ip_network(strict=False) / cidr_error of fuzzed text
    dom     domain_value_error, is_valid_domain (both modes), normalize_match_query
    mq      match_query / match_domain on fuzzed (query, type, value)
    match   RuleConfigMatcher.match over fuzzed text/YAML rule sets
    upper   str.upper / str.lower of single characters (every code point)
"""
import copy
import gzip
import ipaddress
import json
import random
import re
import signal
import sys
import unicodedata

from backend.utils import rule_matcher, rule_syntax

assert sys.version_info[:2] == (3, 11), sys.version
R = random.Random(20261116)
OUT = gzip.open(sys.argv[1], 'wt', encoding='ascii', compresslevel=9)


def emit(**record):
    OUT.write(json.dumps(record, ensure_ascii=True) + '\n')


def exc_text(exc):
    return f'{type(exc).__name__}: {exc}'


# --------------------------------------------------------------------------
# Regex grammar fuzzer
# --------------------------------------------------------------------------

LIT = ['a', 'b', 'c', 'A', 'B', 'x', '0', '1', '_', '-', ' ', ',', ':', '=', '!', '<', '>', '/', '#', '@', '%', '&', '~', "'", '"', '`',
       'é', 'É', 'İ', 'ı', 'i', 'I', 'ß', 'ẞ', 's', 'S', 'ſ', 'K', 'k', 'K', 'Σ', 'σ', 'ς', 'ǅ', 'Ǆ', 'ǆ', 'ﬁ', 'Ω', 'ω', 'Ω',
       '例', '子', '😀', '٣', '３', 'Ⅻ', '²', '́', ' ', '　', ' ', '​', '\t', '\n', '\x00', '\x7f']
ESC = ['\\.', '\\-', '\\\\', '\\(', '\\)', '\\[', '\\]', '\\{', '\\}', '\\*', '\\+', '\\?', '\\|', '\\^', '\\$', '\\ ', '\\#', '\\/', '\\,', '\\é',
       '\\t', '\\n', '\\r', '\\f', '\\v', '\\a', '\\0', '\\07', '\\012', '\\177', '\\400', '\\x41', '\\x4', '\\xg', '\\u00e9', '\\u0130', '\\u00',
       '\\U0001F600', '\\U00110000', '\\N', '\\q', '\\e', '\\z', '\\Z', '\\A', '\\b', '\\B', '\\d', '\\D', '\\w', '\\W', '\\s', '\\S', '\\1', '\\2',
       '\\10', '\\g', '\\k', '\\p', '\\c', '\\E', '\\Q']
CLASS_ITEMS = ['a', 'z', 'a-z', 'A-Z', '0-9', 'a-c', 'c-a', '\\d', '\\D', '\\w', '\\W', '\\s', '\\S', '.', '-', '^', '\\]', ']', '[', '\\[', '\\\\',
               'é', 'É-Ö', 'İ', 'ı', 'K', 'k', 'ſ', 's', 'Σ', 'ς', 'ǅ', '\\x00-\\x1f', '\\u0100-\\u017f', '\\U0001F600', '😀-🙏', '\\b', '\\n',
               '\\t-\\r', '\\0', '\\777', '\\x', '\\q', '\\N{DIGIT ONE}', '[:alpha:]', 'a-\\d', '\\d-z', ' ', '#', '|', '(', ')', '{', '}', '*', '　']
NAMES = ['n', 'g1', 'name', '_x', '例', 'é', '1n', 'a-b', '', 'ℕ', '℘', 'x́', '·', 'a·', 'ª', 'ⅷ']


def gen_class():
    body = ''.join(R.choice(CLASS_ITEMS) for _ in range(R.randint(0, 4)))
    neg = '^' if R.random() < 0.3 else ''
    close = ']' if R.random() < 0.93 else ''
    return '[' + neg + body + close


def gen_quant():
    q = R.choice(['*', '+', '?', '{2}', '{1,3}', '{,2}', '{2,}', '{0}', '{0,0}', '{3,1}', '{1,2', '{,}', '{x}', '{}', '{1001}', '{0,1001}'])
    r = R.random()
    if r < 0.2:
        q += '?'
    elif r < 0.3:
        q += '+'
    elif r < 0.33:
        q += R.choice(['*', '+', '{2}'])
    return q


FLAG_SETS = ['i', 'm', 's', 'x', 'a', 'u', 'L', 'ai', 'au', 'im', 'is', 'ims', 'ix', '', 'z', 'iu', 'msx']


def gen_atom(depth, groups):
    r = R.random()
    if depth > 3:
        r *= 0.5
    if r < 0.30:
        return R.choice(LIT)
    if r < 0.42:
        return R.choice(ESC)
    if r < 0.52:
        return gen_class()
    if r < 0.57:
        return R.choice(['.', '^', '$', '\\A', '\\Z', '\\b', '\\B'])
    if r < 0.585:
        return R.choice(['(', ')', '[', ']', '{', '}', '|', '*', '+', '?', '\\'])
    body = gen_seq(depth + 1, groups)
    k = R.random()
    if k < 0.25:
        groups.append(None)
        return '(' + body + ')'
    if k < 0.35:
        return '(?:' + body + ')'
    if k < 0.42:
        name = R.choice(NAMES)
        groups.append(name)
        return f'(?P<{name}>' + body + ')'
    if k < 0.47:
        return f'(?P={R.choice(NAMES)})'
    if k < 0.53:
        return '(?=' + body + ')'
    if k < 0.58:
        return '(?!' + body + ')'
    if k < 0.64:
        return '(?<=' + body + ')'
    if k < 0.69:
        return '(?<!' + body + ')'
    if k < 0.73:
        return '(?>' + body + ')'
    if k < 0.78:
        ref = R.choice(['1', '2', '0', R.choice(NAMES)])
        alt = ('|' + gen_seq(depth + 1, groups)) if R.random() < 0.6 else ''
        return f'(?({ref})' + body + alt + ')'
    if k < 0.88:
        add = R.choice(FLAG_SETS)
        rem = R.choice(['', '', '-i', '-m', '-s', '-x', '-a', '-im', '-'])
        return f'(?{add}{rem}:' + body + ')'
    if k < 0.92:
        return '(?#' + R.choice(['c', ')', 'x(y', '']) + ')'
    if k < 0.96:
        return '\\' + str(R.randint(1, 3))
    return '(?' + R.choice(['P', 'P<', 'P<n', '<', '<n>', 'Px', '-', 'i', '=', '']) + body + ')'


def gen_seq(depth, groups):
    parts = []
    for _ in range(R.randint(0, 4 if depth < 3 else 2)):
        atom = gen_atom(depth, groups)
        if R.random() < 0.3:
            atom += gen_quant()
        parts.append(atom)
    seq = ''.join(parts)
    if R.random() < 0.15:
        seq += '|' + gen_seq(depth + 1, groups) if depth < 4 else '|'
    return seq


def gen_pattern():
    groups = []
    prefix = ''
    if R.random() < 0.15:
        prefix = '(?' + R.choice(FLAG_SETS + ['-i', 'i-s', 'aiLmsux']) + ')'
    pattern = prefix + gen_seq(0, groups)
    if R.random() < 0.05:
        pattern += '(?' + R.choice(['i', 'm', 's']) + ')'
    return pattern


SUBJECT_ALPHABET = ['a', 'b', 'c', 'A', 'B', 'x', 'X', '0', '1', '_', '-', ' ', '.', ',', '\n', '\t', 'é', 'É', 'İ', 'i', 'ı', 'I', 'ß', 'ẞ', 's', 'S',
                    'ſ', 'K', 'k', 'K', 'Σ', 'σ', 'ς', 'ǅ', 'Ǆ', 'ǆ', 'ﬁ', 'Ω', 'ω', 'Ω', '例', '😀', '٣', '３', 'Ⅻ', '²', '́', ' ', '　',
                    ' ', '\x00', '\x1c', '\x85', 'ǰ', 'ΐ', 'ᾈ', 'ᾀ', 'ﬅ', 'ﬆ', 'ŉ', 'ϐ', 'β', 'ϑ', 'θ', 'ẛ', 'ṡ']


def gen_subject(pattern):
    lits = [ch for ch in pattern if not ch.isspace() or R.random() < 0.3]
    out = []
    for _ in range(R.randint(0, 8)):
        if lits and R.random() < 0.45:
            out.append(R.choice(lits))
        else:
            out.append(R.choice(SUBJECT_ALPHABET))
    return ''.join(out)


def compile_outcome(pattern, flags):
    try:
        return re.compile(pattern, flags), None
    except re.error as exc:
        return None, {'error': str(exc)}
    except RecursionError:
        return None, {'exc': 'RecursionError'}
    except Exception as exc:  # OverflowError, ValueError, ...
        return None, {'exc': exc_text(exc)}


def regex_cases(count):
    seen = set()
    made = 0
    while made < count:
        pattern = gen_pattern()
        if pattern in seen or '\\N{' in pattern:
            continue
        seen.add(pattern)
        made += 1
        subjects = [gen_subject(pattern) for _ in range(8)]
        for flags in (0, re.IGNORECASE):
            compiled, error = compile_outcome(pattern, flags)
            record = {'pattern': pattern, 'ignore': bool(flags)}
            if error is not None:
                record.update(error)
            else:
                spans = []
                for subject in subjects:
                    m = compiled.search(subject)
                    spans.append(None if m is None else [m.start(), m.end()] + [list(m.span(g)) for g in range(1, compiled.groups + 1)])
                record['subjects'] = subjects
                record['spans'] = spans
            emit(kind='rx', **record)
        if '\n' not in pattern and '\r' not in pattern:
            try:
                result = rule_syntax.rule_value_error('DOMAIN-REGEX', pattern)
            except Exception as exc:
                result = '__exception__ ' + exc_text(exc)
            emit(kind='rxerr', pattern=pattern, result=result)


class _Slow(Exception):
    pass


def _alarm(*_):
    raise _Slow


signal.signal(signal.SIGALRM, _alarm)


def quick_enough(pattern, subjects):
    """Skip patterns whose search backtracks catastrophically in CPython."""
    try:
        signal.setitimer(signal.ITIMER_REAL, 0.2)
        for flags in (0, re.IGNORECASE):
            compiled = re.compile(pattern, flags)
            for subject in subjects:
                compiled.search(subject)
        return True
    except _Slow:
        return False
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)


def valid_regex_cases(count):
    """Patterns Python accepts (the grammar fuzzer rejects most), longer subjects."""
    seen = set()
    made = 0
    while made < count:
        pattern = gen_pattern()
        if pattern in seen or '\\N{' in pattern:
            continue
        seen.add(pattern)
        compiled, error = compile_outcome(pattern, 0)
        if error is not None:
            continue
        made += 1
        subjects = []
        for _ in range(10):
            subject = gen_subject(pattern)
            if R.random() < 0.3:
                subject += gen_subject(pattern)[:4]
            subjects.append(subject)
        if not quick_enough(pattern, subjects):
            continue
        for flags in (0, re.IGNORECASE):
            compiled, error = compile_outcome(pattern, flags)
            record = {'pattern': pattern, 'ignore': bool(flags)}
            if error is not None:
                record.update(error)
            else:
                spans = []
                for subject in subjects:
                    m = compiled.search(subject)
                    spans.append(None if m is None else [m.start(), m.end()] + [list(m.span(g)) for g in range(1, compiled.groups + 1)])
                record['subjects'] = subjects
                record['spans'] = spans
            emit(kind='rx', **record)


# --------------------------------------------------------------------------
# rule_value_error / split_rule_payload
# --------------------------------------------------------------------------

UNICODE_FUN = ['ı', 'ſ', 'ß', 'ﬁ', 'ﬀ', 'ﬃ', 'ﬆ', 'K', 'İ', 'ǰ', 'ΐ', 'և', 'ﬓ', 'ŉ', 'ᾳ', 'ﬄ', 'ẞ', 'Ꟁ', 'ꟁ', '\U0001e900', '\U0001e922',
               'Ⱟ', 'ⱟ', 'Ꟁ', 'Ꟑ', '\U00010570', '\U00010597', 'Ɤ', 'Ƛ', '\U00016e40', 'ϴ', 'ǅ']
SPACES = [' ', '\t', '　', ' ', ' ', '\x1c', '\x1f', '\x85', '​', '﻿', '᠎', ' ', ' ', '\x0b']


def mutate_kind(kind):
    r = R.random()
    if r < 0.3:
        return kind
    if r < 0.45:
        return kind.lower()
    if r < 0.6:
        i = R.randrange(len(kind))
        return kind[:i] + R.choice('ABCDEFGHIJKLMNOPQRSTUVWXYZ-') + kind[i + 1:]
    if r < 0.7:
        i = R.randrange(len(kind) + 1)
        return kind[:i] + R.choice('ABCDEFGHIJKLMNOPQRSTUVWXYZ-') + kind[i:]
    if r < 0.8:
        i = R.randrange(len(kind))
        return kind[:i] + kind[i + 1:]
    if r < 0.9:
        # Unicode letters whose upper() is ASCII.
        table = {'i': 'ı', 's': 'ſ', 'k': 'K'}
        return ''.join(table.get(c, c) if R.random() < 0.7 else c for c in kind.lower())
    return R.choice(UNICODE_FUN) + kind


DOMAIN_LABELS = ['example', 'com', 'a', 'b', 'x' * 63, 'x' * 64, 'xn--fsqu00a', 'xn--', 'xn--a', '例子', '测试', 'bücher', 'BÜCHER', 'ß', 'faß',
                 'Ａ', 'ǅ', 'a­', '​', 'a‍b', '1', '123', '0x1', '-a', 'a-', 'a_b', 'ＡＢＣ', '１２３', 'İ', 'ı', 'K', 'ﬁ', 'Ⅻ',
                 'مثال', 'עברית', 'ไทย', '́a', 'á', '😀', '\U0001d400', '\U000e0001', '�', 'a b', '', '.', '。', '．', '｡']


def gen_domain():
    labels = [R.choice(DOMAIN_LABELS) for _ in range(R.randint(1, 4))]
    sep = R.choice(['.', '.', '.', '。', '．', '｡'])
    text = sep.join(labels)
    if R.random() < 0.1:
        text += '.'
    if R.random() < 0.05:
        text = R.choice(SPACES) + text
    return text


def gen_ip_text():
    r = R.random()
    if r < 0.35:
        parts = [R.choice(['0', '1', '9', '10', '127', '192', '255', '256', '01', '001', '1000', '', 'a', '１', '٣', '+1', '-1', ' 1', '0x1'])
                 for _ in range(R.choice([3, 4, 4, 4, 5]))]
        text = '.'.join(parts)
    elif r < 0.75:
        groups = [R.choice(['', '', '0', '1', 'db8', '2001', 'ffff', 'FFFF', 'fe80', '0000', '00000', '12345', 'g', '1.2.3.4', '01.2.3.4', '::'])
                  for _ in range(R.randint(2, 9))]
        text = ':'.join(groups)
        if R.random() < 0.3:
            text += '%' + R.choice(['eth0', '', '1', 'a%b', 'en 0', ' ', 'x\ty', 'é', '网卡', 'a/b', '　', 'x\n', '%'])
    else:
        text = R.choice(['::', '::1', '1::', '::ffff:1.2.3.4', '::ffff:0102:0304', '1:2:3:4:5:6:7:8', '1:2:3:4:5:6:7::', '::2:3:4:5:6:7:8',
                         '1::2:3:4:5:6:7:8', '1:2:3:4:5:6:7:8::', '1:2:3:4:5:6:1.2.3.4', '1:2:3:4:5::1.2.3.4', '1.2.3.4', '0.0.0.0',
                         'fe80::1%eth0', 'fe80::1%eth 0', 'fe80::1% ', 'fe80::%1', '::%a'])
    if R.random() < 0.45:
        text += '/' + R.choice(['0', '8', '24', '32', '33', '64', '128', '129', '024', '00', '', ' 8', '8 ', '+8', '-1', '٨', '１', 'x',
                                '255.255.255.0', '0.0.0.255', '255.0.255.0', '255.255.255.255', '0.0.0.0', 'ffff::', '8/8', '99999999999999999999'])
    if R.random() < 0.05:
        text = R.choice(SPACES) + text
    if R.random() < 0.05:
        text += R.choice(SPACES)
    return text


PORT_VALUES = ['80', '0', '65535', '65536', '80/443', '80,443', '8000-9000', '9000-8000', '00080', '0000065535', '000065536', '1-', '-1', '1--2',
               '１', '٣', '80/', '/80', '', '*', '63', '64', '0/63', '12-18', '4294967295', '4294967296', '0-4294967295', '99999999999999999999',
               '/'.join(['1'] * 28), '/'.join(['1'] * 29), ','.join(['1'] * 29), '1 ', ' 1', '1\n', '1\r2', '+1', '1_000']
SYNTAX_KINDS = sorted(rule_syntax.RULE_TYPES)


def gen_value(kind):
    r = R.random()
    if r < 0.15:
        return gen_domain()
    if r < 0.30:
        return gen_ip_text()
    if r < 0.45:
        return R.choice(PORT_VALUES)
    if r < 0.55:
        return gen_pattern()
    if r < 0.70:
        children = []
        for _ in range(R.randint(0, 3)):
            child_kind = R.choice(SYNTAX_KINDS + ['FOO', 'domain', 'MATCH', 'SUB-RULE'])
            children.append('(' + child_kind + ',' + gen_value_simple() + ')')
        inner = ','.join(children)
        return R.choice(['(' + inner + ')', inner, '((' + inner + ')', '(' + inner + '))', '(' + inner + ')x'])
    if r < 0.8:
        return R.choice(['tcp', 'udp', 'TCP', 'Udp', 'tcp ', 'quic', 'ｔｃｐ', 'tcp,udp', ''])
    if r < 0.9:
        return R.choice(['12345', '0', '', '１２３', '-1', '1.5', 'AS123', ' 1', '1 '])
    return R.choice(SPACES) + R.choice(['x', '', 'a.test']) + R.choice(SPACES + [''])


def gen_value_simple():
    return R.choice(['example.com', 'tcp', '80', '1.2.3.0/24', '^a$', '', 'x,y', '(a)', 'bad..x', 'NETWORK'])


def syntax_cases(count):
    for _ in range(count):
        kind = mutate_kind(R.choice(SYNTAX_KINDS))
        value = gen_value(kind)
        try:
            result = rule_syntax.rule_value_error(kind, value)
        except Exception as exc:
            result = '__exception__ ' + exc_text(exc)
        emit(kind='syn', rule_kind=kind, value=value, result=result)
    # get_close_matches ties and near misses over every type.
    for base in SYNTAX_KINDS:
        for _ in range(6):
            kind = mutate_kind(base)
            emit(kind='syn', rule_kind=kind, value='x', result=rule_syntax.rule_value_error(kind, 'x'))
    for _ in range(count // 4):
        kind = mutate_kind(R.choice(SYNTAX_KINDS))
        pieces = [R.choice(SPACES + ['']) + kind + R.choice(SPACES + [''])]
        for _ in range(R.randint(0, 4)):
            pieces.append(R.choice(SPACES + ['']) + R.choice([gen_value_simple(), gen_domain(), 'DIRECT', 'no-resolve', '']) + R.choice(SPACES + ['']))
        line = ','.join(pieces)
        kind_out, payload, params = rule_syntax.split_rule_payload(line)
        emit(kind='split', text=line, result=[kind_out, payload, params])


# --------------------------------------------------------------------------
# parse_rule_line / ip / domain / match_query
# --------------------------------------------------------------------------

LINE_HEADS = ['DOMAIN', 'DOMAIN-SUFFIX', 'domain-suffix', 'DOMAIN-KEYWORD', 'DOMAIN-REGEX', 'IP-CIDR', 'IP-CIDR6', 'GEOIP', 'MATCH', 'ſrc-port',
              'domaın', 'ﬁle', 'ß', 'domain', 'full', 'keyword', 'regexp', 'regex', 'ip', 'payload', 'DOMAIN ', ' Full', 'FULL', 'İP', 'Domaın']


def gen_line():
    r = R.random()
    if r < 0.4:
        head = R.choice(LINE_HEADS)
        sep = R.choice([',', ',', ':', ' : ', ', '])
        value = R.choice([gen_domain(), gen_ip_text(), gen_pattern(), '', 'x'])
        line = head + sep + value
        if R.random() < 0.3:
            line += R.choice([',no-resolve', ', no-resolve', ',NO-RESOLVE', ',no-resolve ', ',No-Resolve,x', ',no-resolveİ', 'İ,no-resolve',
                              ',DIRECT', ',no-resolve　'])
    elif r < 0.6:
        line = R.choice(['+.', '*.', '.', '..', '', '-', '- ', '* ', '+']) + gen_domain()
    elif r < 0.8:
        line = gen_ip_text()
    else:
        line = gen_domain()
    if R.random() < 0.2:
        line = R.choice(['- ', '-', '"', "'", '# ', '!', '//', R.choice(SPACES)]) + line
    if R.random() < 0.2:
        line += R.choice(['"', "'", ' # c', '#', R.choice(SPACES), "'\""])
    return line


def ip_record(text):
    try:
        ip = ipaddress.ip_address(text)
        address = [ip.version, str(int(ip)), getattr(ip, 'scope_id', None)]
    except ValueError:
        address = None
    try:
        net = ipaddress.ip_network(text, strict=False)
        network = [net.version, str(int(net.network_address)), net.prefixlen, str(net)]
    except ValueError:
        network = None
    return address, network


def misc_cases(count):
    for _ in range(count):
        line = gen_line()
        parsed = rule_matcher.parse_rule_line(line)
        emit(kind='line', line=line, result=list(parsed) if parsed else None)
    for _ in range(count):
        text = gen_ip_text()
        address, network = ip_record(text)
        emit(kind='ip', text=text, address=address, network=network, cidr=rule_syntax.cidr_error(text))
    for _ in range(count):
        text = gen_domain() if R.random() < 0.7 else R.choice([gen_ip_text(), gen_ip_text() + '.', 'a_b.test', '-a.test', 'a-.test', '1.2.3', '1.2.3.4.'])
        emit(kind='dom', text=text, error=rule_syntax.domain_value_error(text), strict=rule_matcher.is_valid_domain(text),
             dns=rule_matcher.is_valid_domain(text, allow_dns_labels=True), normalized=rule_matcher.normalize_match_query(text))
    types = ['DOMAIN', 'DOMAIN-SUFFIX', 'DOMAIN-KEYWORD', 'DOMAIN-REGEX', 'REGEX', 'REGEXP', 'SUBDOMAIN-ONLY', 'DIRECT-SUBDOMAIN', 'IP', 'IP-CIDR',
             'IP-CIDR6', 'domain', '']
    for _ in range(count):
        query = R.choice([gen_domain(), gen_ip_text(), 'example.com', 'a.example.com', 'k.test', 'x.k.test', 'fe80::1%eth 0', 'é.test'])
        kind = R.choice(types)
        value = R.choice([gen_domain(), gen_ip_text(), gen_pattern(), 'example.com', 'K.test', 'EXAMPLE.COM', 'ſ', 'İ', 'k.test', ''])
        try:
            mq = rule_matcher.match_query(query, kind, value)
        except Exception as exc:
            mq = '__exception__ ' + exc_text(exc)
        try:
            md = rule_matcher.match_domain(query, kind, value)
        except Exception as exc:
            md = '__exception__ ' + exc_text(exc)
        emit(kind='mq', query=query, rule_type=kind, value=value, match_query=mq, match_domain=md)


# --------------------------------------------------------------------------
# RuleConfigMatcher on fuzzed rule-set bodies
# --------------------------------------------------------------------------

def yaml_scalar(text):
    style = R.random()
    if style < 0.3:
        return json.dumps(text, ensure_ascii=R.random() < 0.5)
    if style < 0.5:
        return "'" + text.replace("'", "''") + "'"
    return text


def gen_entry(behavior):
    if behavior == 'ipcidr' or (behavior == 'classical' and R.random() < 0.3):
        value = gen_ip_text()
        return ('IP-CIDR,' + value) if behavior == 'classical' else value
    if behavior == 'classical':
        head = R.choice(['DOMAIN', 'DOMAIN-SUFFIX', 'DOMAIN-KEYWORD', 'DOMAIN-REGEX', 'IP-CIDR6', 'domain-suffix', 'GEOIP', 'MATCH', 'AND', 'DST-PORT'])
        value = R.choice([gen_domain(), 'example.com', 'k.test', '^ex.*$', '(?i)^K', 'a{2', '80'])
        line = head + ',' + value
        if R.random() < 0.2:
            line += R.choice([',DIRECT', ',no-resolve', ',,'])
        return line
    pre = R.choice(['', '', '+.', '*.', '.', '*', 'a.*.', '*.*.', '+.*.'])
    return pre + R.choice([gen_domain(), 'example.com', 'k.test', 'Example.COM', 'xn--fsqu00a.com', 'a.b.k.test'])


def gen_yaml(entries):
    r = R.random()
    lines = []
    if R.random() < 0.1:
        lines.append(R.choice(['%YAML 1.1', '%YAML 1.2', '---', '# head', '%TAG ! tag:x,2000:']))
        if lines[-1].startswith('%'):
            lines.append('---')
    if r < 0.2:
        lines.append('payload: [' + ', '.join(yaml_scalar(e) for e in entries) + ']')
    else:
        indent = R.choice(['', '  ', '    ', '\t'])
        lines.append('payload:' + R.choice(['', ' ', ' # c', ' &a', ' !!seq']))
        for entry in entries:
            item = yaml_scalar(entry)
            if R.random() < 0.06:
                item = R.choice(['!!str ', '!!int ', '! ', '&x ', '*x', '? ', '- ']) + item
            if R.random() < 0.06:
                cut = len(item) // 2
                item = item[:cut] + '\n' + indent + '   ' + item[cut:]
            if R.random() < 0.05:
                item += ' # note'
            lines.append(indent + '- ' + item)
    if R.random() < 0.08:
        lines.append(R.choice(['payload: [x]', 'other: 1', '...', '---\npayload: [a.test]', 'a: *a', 'payload:', '  x: 1']))
    sep = R.choice(['\n', '\n', '\r\n', '\r'])
    text = sep.join(lines) + R.choice(['', '\n'])
    if R.random() < 0.05:
        text = '﻿' + text
    return text


def gen_text(entries):
    sep = R.choice(['\n', '\n', '\r\n', '\r', '\x85', ' ', '\x0b', '\x1c'])
    lines = list(entries)
    for _ in range(R.randint(0, 2)):
        lines.insert(R.randint(0, len(lines)), R.choice(['# c', '// c', '! c', '', '   ', 'payload:', '﻿']))
    text = sep.join(lines)
    if R.random() < 0.1:
        text = '﻿' + text
    return text


def match_cases(count):
    for _ in range(count):
        rules, contents = [], {}
        queries = []
        for n in range(R.randint(1, 3)):
            behavior = R.choice(['domain', 'domain', 'ipcidr', 'classical', 'classical'])
            fmt = R.choice(['text', 'yaml', 'yaml'])
            entries = [gen_entry(behavior) for _ in range(R.randint(1, 8))]
            rid = f's{n}'
            rules.append({'id': rid, 'itemType': 'ruleset', 'name': f'Set {n}', 'policy': 'P', 'behavior': behavior, 'format': fmt})
            contents[rid] = gen_yaml(entries) if fmt == 'yaml' else gen_text(entries)
            for entry in entries[:3]:
                token = re.split(r'[,]', entry)[-1].strip().lstrip('+*.')
                queries.append(token or 'x')
        queries += [R.choice(['example.com', 'a.example.com', 'k.test', 'x.k.test', 'a.b.k.test', '1.2.3.4', '::1', 'fe80::1%eth0', '10.0.0.1'])
                    for _ in range(3)]
        loader = lambda item, _: contents.get(item['id'], '')
        matcher = rule_matcher.RuleConfigMatcher(copy.deepcopy(rules), [], loader)
        results = []
        for query in queries:
            try:
                results.append(matcher.match(query))
            except Exception as exc:
                results.append({'__error__': exc_text(exc)})
        emit(kind='match', configs=rules, contents=contents, queries=queries, results=results)


# --------------------------------------------------------------------------
# _iter_ruleset_lines on token-soup YAML (libyaml corner cases)
# --------------------------------------------------------------------------

YAML_ITEM_TOKENS = ['a', 'b.test', 'x:y', 'x: y', 'k?v', '?', ':', ',', '-', '- ', '#', ' # c', '\t', '\t\t', ' ', '  ', '\ufeff', '&a ', '*a', '!!str ',
                    '!!int ', '! ', '!x ', '!!seq ', '"q\\tq"', '"\\ud83d\\ude00"', '"\\U0001F600"', '"\\x41"', '"\\/"', "'s''s'", '"a\nb"', '[', ']', '{', '}',
                    '|', '>', '%', '@', '`', '1', '0x1', 'yes', '~', 'null', '1.5', '2001-01-01', '<<', '=', 'é', 'İ', '例', '\x85', '\u2028', '...', '---']


def gen_yaml_soup():
    lines = []
    if R.random() < 0.1:
        lines.append(R.choice(['%YAML 1.1', '%YAML 1.2', '%YAML 2.0', '---', '--- !!map', '\ufeff---', '%TAG !e! tag:x,2000:']))
    flow = R.random() < 0.3
    head = R.choice(['payload:', 'payload:', 'payload:\t', 'payload :', 'payload\t:', '"payload":', "'payload':", '? payload\n:', 'payload: &a', 'payload: !!seq',
                     '\ufeffpayload:', '\tpayload:', 'Payload:', 'payload:\t# c'])
    items = []
    for _ in range(R.randint(0, 5)):
        if R.random() < 0.6:
            item = R.choice(['a', 'b.test', 'DOMAIN,x.test', "'q x'", '"d\\ty"', 'é例', 'x y', '+.k.test'])
            if R.random() < 0.4:
                noise = R.choice(YAML_ITEM_TOKENS)
                item = noise + item if R.random() < 0.5 else item + noise
        else:
            item = ''.join(R.choice(YAML_ITEM_TOKENS) for _ in range(R.randint(1, 4)))
        items.append(item)
    if flow:
        sep = R.choice([', ', ',', ',\t', ' ,', ',\n  ', ',\n\t'])
        lines.append(head + ' [' + sep.join(items) + R.choice([']', ']', '', '],', ']\t', '] # c']))
    else:
        lines.append(head)
        indent = R.choice(['', '  ', ' ', '\t', '  \t', '\ufeff'])
        for item in items:
            dash = R.choice(['- ', '- ', '-\t', '-', '-  '])
            lines.append(indent + dash + item)
            if R.random() < 0.1:
                lines.append(indent + R.choice(['  ', '   ', ' \t', '\t', '']) + R.choice(YAML_ITEM_TOKENS))
    if R.random() < 0.1:
        lines.append(R.choice(['other: 1', 'payload: [a]', '...', '---', '\ufeff', '# end', 'a: *a']))
    text = R.choice(['\n', '\n', '\r\n', '\r']).join(lines) + R.choice(['', '\n'])
    if R.random() < 0.05:
        text = '\ufeff' + text
    return text


def yaml_cases(count):
    for _ in range(count):
        text = gen_yaml_soup()
        try:
            result = [list(entry) for entry in rule_matcher._iter_ruleset_lines(text, 'yaml')]
        except Exception as exc:
            result = '__exception__ ' + exc_text(exc)
        emit(kind='yaml', text=text, result=result)


# --------------------------------------------------------------------------
# str.upper / str.lower of every code point (Unicode 14)
# --------------------------------------------------------------------------

def case_cases():
    upper, lower, start, cont = [], [], [], []
    for cp in range(0x110000):
        if 0xd800 <= cp <= 0xdfff:
            continue
        ch = chr(cp)
        if ch.upper() != ch:
            upper.append([cp, ch.upper()])
        if ch.lower() != ch:
            lower.append([cp, ch.lower()])
        if ch.isidentifier():
            start.append(cp)
        if ('a' + ch).isidentifier():
            cont.append(cp)
    emit(kind='upper', upper=upper, lower=lower, ident_start=start, ident_continue=cont)


regex_cases(int(sys.argv[2]) if len(sys.argv) > 2 else 6000)
valid_regex_cases(int(sys.argv[4]) if len(sys.argv) > 4 else 6000)
syntax_cases(4000)
misc_cases(3000)
match_cases(800)
yaml_cases(int(sys.argv[3]) if len(sys.argv) > 3 else 20000)
case_cases()
OUT.close()
