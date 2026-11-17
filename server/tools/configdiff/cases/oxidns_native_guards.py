"""Native YAML and attachment limits through transient public editor requests."""
import base64

from harness import Pair, json_body
from cases.config_backup import message
from cases.setup import Stable
from cases.subscriptions import require


def attachment(path='rules/input.txt', content='', encoding='text'):
    return {'path': path, 'content': content, 'encoding': encoding}


def vectors():
    def native(text, files=()):
        return {'config_mode': 'native', 'native_config': text, 'additional_files': list(files)}
    for key in ('__proto__', 'prototype', 'constructor', 'true', '!!int 1'):
        yield 'H469', native(key + ': safe\nplugins: []\n'), 'OxiDNS YAML 字段名必须为安全文本', key
    yield 'H470', native('plugins: []\nplugins: []\n'), 'OxiDNS YAML 包含重复字段', 'duplicate-key'
    yield 'H471', native('#' + 'x' * (2 * 1024 * 1024)), 'OxiDNS 完整配置必须为不超过 2 MiB 的 YAML 文本', 'real-byte-limit'
    yield 'H472', native('plugins: [\n'), 'OxiDNS 完整配置 YAML 语法无效', 'parse-error'
    yield 'H473', native('value: ' + '[' * 34 + 'null' + ']' * 34), 'OxiDNS YAML 超出结构大小限制', 'depth-34'
    yield 'H473', native('value: [' + ','.join(['null'] * 50001) + ']'), 'OxiDNS YAML 超出结构大小限制', 'real-50001-nodes'
    yield 'H474', native('value: &self [*self]\nplugins: []\n'), 'OxiDNS YAML 不支持循环别名', 'recursive-alias'
    yield 'H475', native('value: !!binary YQ==\nplugins: []\n'), 'OxiDNS YAML 只支持普通对象、数组和标量', 'binary-scalar'
    for value in ('.nan', '.inf', '-.inf'):
        yield 'H476', native('value: ' + value + '\nplugins: []\n'), 'OxiDNS YAML 数字必须为有限值', value
    yield 'H477', native('[]'), 'OxiDNS 完整配置顶层必须为对象', 'array-root'
    for files in ({}, [attachment('rules/' + str(i)) for i in range(257)]):
        yield 'H478', native('plugins: []', files) if isinstance(files, list) else {
            'config_mode': 'native', 'native_config': 'plugins: []', 'additional_files': files}, '附加文件必须为数组，最多 256 个文件', str(type(files).__name__)
    yield 'H479', native('plugins: []', [1]), '附加文件条目必须为对象', 'nonobject-item'
    for path in ('config.yaml', 'manifest.json', 'state', 'state/file', '.configflow-private'):
        yield 'H480', native('plugins: []', [attachment(path)]), '附加文件不能覆盖主配置、部署元数据或 state/ 运行数据', path
    yield 'H481', native('plugins: []', [attachment(), attachment()]), '附加文件路径重复', 'duplicate'
    for a, b in [('a', 'a/b'), ('a/b', 'a')]:
        yield 'H482', native('plugins: []', [attachment(a), attachment(b)]), '附加文件路径存在文件与目录冲突', a
    for content, encoding in [(1, 'text'), ('x', 'invalid')]:
        yield 'H483', native('plugins: []', [attachment(content=content, encoding=encoding)]), '附加文件内容必须为文本，编码为 text 或 base64', encoding
    yield 'H484', native('plugins: []', [attachment(content='x' * (32 * 1024 * 1024 + 1))]), '单个附加文件不能超过 16 MiB', 'real-character-limit'
    yield 'H485', native('plugins: []', [attachment(content='%%%not-base64', encoding='base64')]), '附加文件 Base64 内容无效', 'invalid-base64'
    yield 'H486', native('plugins: []', [attachment(content='x' * (16 * 1024 * 1024 + 1))]), '单个附加文件不能超过 16 MiB', 'real-decoded-byte-limit'
    yield 'H487', native('plugins: []', [attachment('file-' + str(i), 'x' * (13 * 1024 * 1024)) for i in range(5)]), '附加文件总大小不能超过 64 MiB', 'real-65MiB-total'
    # Repeated includes are legal (not a cycle); one managed leaf suffices to
    # reach the actual 512 expansion count without changing any constant.
    yield 'H488', native('include: [' + ','.join(['leaf.yaml'] * 513) + ']\nplugins: []',
                        [attachment('leaf.yaml', 'plugins: []')]), 'OxiDNS include 超过 8 层或文件数量限制', 'real-513-expansions'
    yield 'H489', native('include: not-an-array\nplugins: []'), 'OxiDNS include 必须为文件路径数组', 'include-type'
    yield 'H490', native('plugins: {}'), 'OxiDNS plugins 必须为数组', 'plugins-type'
    for reference in ('', '/absolute.yaml', '${EXTERNAL}/file.yaml', 'a\\b', 'a\nb'):
        import json
        yield 'H491', native('include: [' + json.dumps(reference) + ']\nplugins: []'), 'include 必须引用附加文件中的相对路径，不能使用绝对路径或环境变量', repr(reference)
    yield 'H492', native('include: [leaf.yaml]', [attachment('leaf.yaml', 'include: [leaf.yaml]')]), 'OxiDNS include 存在循环引用', 'self-include'
    yield 'H493', native('include: [absent.yaml]'), 'OxiDNS include 文件未在附加文件中提供', 'missing-include'
    yield 'H494', native('include: [leaf.yaml]', [attachment('leaf.yaml', base64.b64encode(b'\xff').decode(), 'base64')]), 'OxiDNS include 文件必须为 UTF-8 YAML', 'invalid-UTF8-include'
    yield 'H495', native('include: [leaf.yaml]', [attachment('leaf.yaml', '[]')]), 'OxiDNS include 文件顶层必须为对象', 'include-root-type'
    yield 'H496', native('plugins: [{tag: invalid_plugin, type: cache, args: {size: wrong-type}}]'), None, 'actual-expanded-schema-error'


def guards(binary, recorder):
    for template, prefix in [('POST /api/oxidns/render', '/api/oxidns'),
            ('POST /api/profiles/<profile_id>/oxidns/render', '/api/profiles/default/oxidns')]:
        with Pair(binary, recorder) as pair:
            for case, settings, text, label in vectors():
                def check(reply):
                    if text is None:
                        require(reply.json().get('success') is False and reply.json().get('message', '').startswith('OxiDNS 配置校验未通过：'),
                                'expanded native schema validation actually produced its Chinese contextual error')
                    else:
                        message(text)(reply)
                pair.request(case, template, prefix + '/render', body=json_body({'settings': settings}), expect=400,
                             writes=True, interaction=Stable(pair), check=check, variant='native-normalization/' + label, timeout=90)


def package_references(binary, recorder):
    import yaml
    # The template permits hand-written plugin args; a native schema check
    # would reject them before resource_references, so use the actual complete
    # template package path to reach these materializer guards.
    for case, files, text in [('H497', [''], 'OxiDNS 文件引用必须为非空文本'),
                             ('H498', 'not-a-list', 'OxiDNS 文件引用必须为列表')]:
        custom = yaml.safe_dump({'plugins': [{'tag': 'custom_resource', 'type': 'cache', 'args': {'size': 9, 'files': files}}]})
        with Pair(binary, recorder, options={'profile': {'oxidns': {'custom_config': custom}}}) as pair:
            for route, path in [('POST /api/generate/oxidns', '/api/generate/oxidns'),
                    ('POST /api/profiles/<profile_id>/generate/oxidns', '/api/profiles/default/generate/oxidns')]:
                pair.request(case, route, path, body='{}', expect=400, writes=True, interaction=Stable(pair),
                             check=message(text), variant='template-package-reaches-resource-materializer')


GROUPS = {'oxidns-native-yaml-attachment-real-limits': guards, 'oxidns-template-package-resource-guards': package_references}
