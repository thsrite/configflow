"""Closed RouterOS console fixture grammar, not a general script interpreter.

Implements only the reviewed batch add/list/digest/name-query protocol. The
application's script text is retained verbatim in the HTTP transcript. Every
accepted add mutates the fake tables; malformed-output tests use fault injection.
"""
import hashlib
import re


QUOTED = r'"((?:[^"\\]|\\.)*)"'
FIELD = re.compile(r'([\w.-]+)=(?:' + QUOTED + r'|(yes|no))')
ADD = re.compile(r':if \(\$f=0\) do=\{:onerror e in=\{:put \("cf-id:(\d+):" \. '
                 r'\[(/ip/route|/ipv6/route|/ip/firewall/nat)/add (.*)\]\)\} '
                 r'do=\{:set f 1; :put \("cf-err:\1:" \. \$e\)\}\}')


class UnsupportedScript(ValueError):
    """A script outside the explicitly reviewed fake grammar."""


def unquote(value):
    return re.sub(r'\\(.)', lambda match: {'n': '\n', 'r': '\r', 't': '\t'}.get(match[1], match[1]), value)


def fields(text):
    result = {}
    cursor = 0
    for match in FIELD.finditer(text):
        if text[cursor:match.start()].strip() or match[1] in result:
            raise UnsupportedScript('unparsed or duplicate console add argument')
        result[match[1]] = {'yes': 'true', 'no': 'false'}[match[3]] if match[3] else unquote(match[2])
        cursor = match.end()
    if text[cursor:].strip():
        raise UnsupportedScript('trailing console add argument')
    return result


def encoded_row(row):
    keys = sorted(set(row) - {'creation-time', 'timeout'})
    return (str(len(keys)) + ':' + ';'.join(key + '=' + str(row[key]) for key in keys) + '\n').encode()


def execute(script, rows, add):
    if not isinstance(script, str):
        raise UnsupportedScript('script must be text')
    if script.startswith(':local f 0\n') and script.endswith('\n:put "cf-done"'):
        parsed = [ADD.fullmatch(line) for line in script.split('\n')[1:-1]]
        if not all(parsed) or [int(match[1]) for match in parsed] != list(range(len(parsed))):
            raise UnsupportedScript('invalid batch add script')
        # Parse every line first: fixture syntax rejection must not accidentally
        # perform a prefix of a script it never understood.
        operations = [(match[2], fields(match[3])) for match in parsed]
        output = []
        for index, (path, values) in enumerate(operations):
            try:
                ident = add(path, values)
            except ValueError as error:
                output.append(f'cf-err:{index}:{error}')
                break
            output.append(f'cf-id:{index}:{ident}')
        return {'ret': '\r\n'.join(output + ['cf-done'])}
    if script.startswith(':local n 0\n:local m ""\n:foreach a in={'):
        match = re.fullmatch(r':local n 0\n:local m ""\n:foreach a in=\{(.*)\} do=\{:onerror e in=\{'
            r'/ip/firewall/address-list/add (.*) address=\$a\} do=\{:set n \(\$n\+1\); '
            r':if \(\$n=1\) do=\{:set m \(\$a \. " " \. \$e\)\}\}\}\n:put \("cf-done:" \. \$n \. ":" \. \$m\)', script)
        if not match:
            raise UnsupportedScript('invalid address-list batch script')
        shared = fields(match[2])
        items = re.findall(QUOTED, match[1])
        # Delimiters outside quoted strings must be exactly semicolons.
        if re.sub(QUOTED, '', match[1]) != ';' * max(0, len(items) - 1):
            raise UnsupportedScript('invalid address-list member literals')
        failed, first = 0, ''
        for text in items:
            address = unquote(text)
            try:
                add('/ip/firewall/address-list', {**shared, 'address': address})
            except ValueError as error:
                failed += 1
                first = first or address + ' ' + str(error)
        return {'ret': f'cf-done:{failed}:{first}'}
    if script.startswith(':local groups ({})') and 'cf-digest:' in script:
        names = [unquote(value) for value in re.findall(r'\(\$groups->' + QUOTED + r'\)', script)]
        indices = re.findall(r':put \("cf-digest:(\d+):', script)
        if len(names) != len(indices) or indices != list(map(str, range(len(names)))):
            raise UnsupportedScript('invalid digest groups/indices')
        output = []
        for index, name in enumerate(names):
            members = sorted((row for row in rows if row.get('list') == name), key=lambda row: str(row['.id']))
            chunks = [hashlib.md5(b''.join(encoded_row(row) for row in members[start:start + 500])).hexdigest()
                      for start in range(0, len(members), 500)] or [hashlib.md5(b'').hexdigest()]
            digest = hashlib.md5(''.join(chunks).encode()).hexdigest()
            output.append(f'cf-digest:{index}:{len(members)}:{digest}')
        return {'ret': '\r\n'.join(output)}
    if script.startswith(':local i 0\n') and 'cf-rows:' in script:
        name = re.search(r'print as-value where list=' + QUOTED, script)
        bounds = re.search(r'\$i >= (\d+) && \$i < (\d+)', script)
        if not name or not bounds:
            raise UnsupportedScript('invalid address-list page script')
        members = [row for row in rows if row.get('list') == unquote(name[1])]
        first, last = map(int, bounds.groups())
        output = ['cf-row:' + encoded_row(row).decode().removesuffix('\n') for row in members[first:last]]
        return {'ret': '\r\n'.join(output + ['cf-rows:' + str(len(members))])}
    if script.startswith(':local names ({})\n') and 'cf-names:' in script:
        expression = re.search(r'find where list~' + QUOTED, script)
        if not expression:
            raise UnsupportedScript('invalid occupied-name script')
        pattern = re.compile(unquote(expression[1]))
        excluded = {unquote(value) for value in re.findall(r'list!=' + QUOTED, script)}
        names = sorted({row.get('list', '') for row in rows
                        if pattern.fullmatch(row.get('list', '')) and row.get('list') not in excluded})
        return {'ret': 'cf-names:\r\n' + '\r\n'.join(names)}
    raise UnsupportedScript('unsupported RouterOS script grammar')
