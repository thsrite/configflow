"""Incomplete stored editor settings; public read/refusal/note-only sequences."""
from dataclasses import replace

import policy_graph


def variants():
    result = [('mode', {'config_mode': 'invalid'}, 'OxiDNS 配置模式必须为 template 或 native')]
    for label, value in (('null', None), ('bool', False), ('number', 1), ('array', []), ('object', {})):
        result.append(('native-type-' + label, {'native_config': value}, 'OxiDNS 完整配置必须为 YAML 文本'))
    for label, value, message in (
        ('empty', '', 'OxiDNS 完整配置顶层必须为对象'), ('array', '[]', 'OxiDNS 完整配置顶层必须为对象'),
        ('null', 'null', 'OxiDNS 完整配置顶层必须为对象'), ('scalar', 'true', 'OxiDNS 完整配置顶层必须为对象'),
        ('syntax', '[', 'OxiDNS 完整配置 YAML 语法无效'),
        ('duplicate', 'plugins: []\nplugins: []', 'OxiDNS YAML 包含重复字段'),
        ('key-number', '1: value', 'OxiDNS YAML 字段名必须为安全文本'),
        ('key-forbidden', '__proto__: value', 'OxiDNS YAML 字段名必须为安全文本'),
        ('loop', 'loop: &loop {next: *loop}', 'OxiDNS YAML 不支持循环别名'),
        ('nonfinite', 'value: .nan', 'OxiDNS YAML 数字必须为有限值'),
        ('binary', 'value: !!binary YQ==', 'OxiDNS YAML 只支持普通对象、数组和标量')):
        result.append(('yaml-' + label, {'native_config': value}, message))
    for label, value in (('null', None), ('bool', False), ('number', 1), ('object', {}), ('string', 'file')):
        result.append(('files-shape-' + label, {'additional_files': value}, '附加文件必须为数组，最多 256 个文件'))
    result.append(('files-257', {'additional_files': [{'path': 'same', 'content': ''}] * 257},
                   '附加文件必须为数组，最多 256 个文件'))
    for label, value in (('null', None), ('array', []), ('number', 1), ('string', 'file')):
        result.append(('file-row-' + label, {'additional_files': [value]}, '附加文件条目必须为对象'))
    for label, value in (('null', None), ('empty', ''), ('number', 1), ('array', []), ('object', {}),
                         ('backslash', 'a\\b'), ('control', 'a\nb')):
        result.append(('file-path-' + label, {'additional_files': [{'path': value, 'content': ''}]}, '附加文件路径无效'))
    for label, value in (('absolute', '/absolute'), ('parent', '../escape'), ('empty-part', 'a//b'),
                         ('dot-part', 'a/./b'), ('parent-part', 'a/../b')):
        result.append(('file-path-' + label, {'additional_files': [{'path': value, 'content': ''}]},
                       '附加文件路径必须为安全的相对路径'))
    for label, value in (('config', 'config.yaml'), ('manifest', 'manifest.json'), ('state', 'state'),
                         ('state-child', 'state/runtime'), ('hidden', '.configflow'), ('hidden-child', '.configflow/cache')):
        result.append(('file-reserved-' + label, {'additional_files': [{'path': value, 'content': ''}]},
                       '附加文件不能覆盖主配置、部署元数据或 state/ 运行数据'))
    for label, paths, message in (('duplicate', ('same', 'same'), '附加文件路径重复'),
        ('parent-first', ('a', 'a/b'), '附加文件路径存在文件与目录冲突'),
        ('child-first', ('a/b', 'a'), '附加文件路径存在文件与目录冲突')):
        result.append(('file-' + label, {'additional_files': [{'path': path, 'content': ''} for path in paths]}, message))
    for label, content, encoding in (('null', None, 'text'), ('number', 1, 'text'), ('array', [], 'text'),
                                    ('object', {}, 'text'), ('encoding', '', 'utf-16')):
        result.append(('file-content-' + label, {'additional_files': [
            {'path': 'synthetic.txt', 'content': content, 'encoding': encoding}]},
            '附加文件内容必须为文本，编码为 text 或 base64'))
    result.append(('file-base64', {'additional_files': [{'path': 'synthetic.bin', 'content': '%%%INVALID', 'encoding': 'base64'}]},
                   '附加文件 Base64 内容无效'))
    return result


def worlds():
    result = []
    for label, patch, message in variants():
        options = policy_graph.options(policy_graph.document())
        options['alpha']['oxidns'].update(patch)
        items = tuple(replace(case, ident=case.ident.replace('native-policy', 'native-invalid'),
                              entries=tuple(ident for ident in case.entries if ident != 'H0086'))
                      for case in policy_graph.guard_cases(label, message))
        result.append(('native-invalid-' + label, options, (lambda items=items: items,)))
    return result
