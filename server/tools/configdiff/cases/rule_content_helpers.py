"""Rule content inspection, metadata and append guards through public HTTP."""
from harness import Pair, json_body
from sources import Source
from cases.config_backup import message
from cases.pull_routes import SourceStable
from cases.setup import Stable
from cases.subscriptions import require

CHECK = 'POST /api/rule-library/check-content'
DOMAIN = 'Domain 不接受 Classical 规则行；精确域名请填写 example.com，域名及其子域名请填写 +.example.com。'
AMBIGUOUS = '无法确定此行的 Classical 语义，已保留原文；请改为明确的规则类型和值。'
YAML_ITEM = 'payload 每项应为一行文本；请先展开 YAML 锚点、别名或多行内容。'


def inspection(binary, recorder):
    vectors = [('H124', 'domain', 'text', 'DOMAIN,example.com\n', 1, DOMAIN, 'errors'),
        ('H125', 'domain', 'text', 'not a domain\n', 1, '请填写域名或通配符，例如 example.com、+.example.com。', 'errors'),
        ('H126', 'classical', 'text', '192.0.2.0/99\n', 1, '不是有效 IP 地址或 CIDR；请检查地址与前缀长度。', 'errors'),
        ('H127', 'classical', 'text', 'domain:example.com\n', 1, AMBIGUOUS, 'warnings'),
        ('H135', 'domain', 'yaml', '- example.com\n', 0, 'YAML 规则需要 payload 列表。', 'errors')]
    for value in ('other: []\n', 'payload: example.com\n', 'payload: [example.com]\npayload: [example.net]\n'):
        vectors.append(('H136', 'domain', 'yaml', value, 0, 'YAML 规则需要唯一的 payload 列表。', 'errors'))
    vectors.append(('H137', 'domain', 'yaml', 'base: &rules [example.com]\npayload: *rules\n', 0,
        '请将 payload 的 YAML 别名展开后再编辑。', 'errors'))
    for value in ('payload: [1]\n', 'payload: [false]\n', 'payload: [null]\n', 'payload: [[example.com]]\n',
                  'payload: [{domain: example.com}]\n', 'payload:\n  - |\n    example.com\n',
                  'payload:\n  - >\n    example.com\n', 'payload:\n  - &host example.com\n  - *host\n',
                  'base: &host example.com\npayload: [*host]\n', 'payload:\n  - !!str &host example.com\n'):
        vectors.append(('H138', 'domain', 'yaml', value, 0, YAML_ITEM, 'errors'))
    with Pair(binary, recorder) as pair:
        for case, behavior, form, content, line, text, field in vectors:
            def check(reply):
                expected = {'content': content, 'changes': [], 'errors': []}
                if behavior == 'classical':
                    expected['warnings'] = []
                expected[field] = [{'line': line, 'message': text, **({'before': content.strip()} if behavior == 'classical' else {})}]
                require(reply.json() == expected, 'inspection retains exact input and reports only the intended structured diagnostic')
            pair.request(case, CHECK, body=json_body({'content': content, 'behavior': behavior, 'format': form}),
                expect=200, writes=True, interaction=Stable(pair), check=check, variant=form + '/' + repr(content))


def metadata(binary, recorder):
    with Source(b'bad declarations must never fetch') as source, Pair(binary, recorder) as pair:
        vectors = [('H139', {'behavior': 'unknown'}, '规则集类型必须是 domain、ipcidr 或 classical。'),
            ('H139', {'format': 'binary'}, '规则集格式必须是 text、yaml 或 mrs。'),
            ('H139', {'source_type': 'manual'}, '规则来源必须是 content 或 url。'),
            ('H139', {'format': 'mrs'}, 'MRS 是二进制规则格式，不能作为手写文本保存；请使用 text/yaml 或订阅 MRS 文件。'),
            ('H139', {'format': 'mrs', 'source_type': 'url', 'behavior': 'classical'}, 'MRS 只支持 domain 或 ipcidr，不支持 classical。'),
            ('H140', {'content': 'DOMAIN,example.com\n'}, '规则内容第 1 行：' + DOMAIN),
            ('H140', {'format': 'yaml', 'content': '- example.com\n'}, 'YAML 规则需要 payload 列表。')]
        vectors += [('H134', {'content': value}, '规则内容必须是文本。') for value in (None, False, 1, [], {})]
        for index, (case, patch, text) in enumerate(vectors):
            data = {'id': 'helper-invalid-' + str(index), 'name': '内容校验🧪', 'source_type': 'content',
                    'behavior': 'domain', 'format': 'text', 'content': 'example.com\n', 'url': source.url, **patch}
            pair.request(case, 'POST /api/rule-library', body=json_body(data), expect=400, writes=True,
                interaction=SourceStable(pair, source), check=message(text), variant='source-validation/' + str(index))


def append(binary, recorder):
    vectors = [('H146', '{other: []}\n'), ('H146', 'payload: wrong\n'),
               ('H147', 'base: &rules [example.com]\npayload: *rules\n'),
               ('H148', 'payload: [1]\n'), ('H148', 'payload: [false]\n'), ('H148', 'payload: [null]\n'),
               ('H149', 'payload: [unterminated\n')]
    with Pair(binary, recorder) as pair:
        for case, content in vectors:
            pair.request(case, CHECK, body=json_body({'content': content, 'behavior': 'domain', 'format': 'yaml',
                'append_lines': ['new.example']}), expect=400, writes=True, interaction=Stable(pair),
                check=message('规则集的 payload 格式无法读取，请先在规则仓库检查内容。'), variant='append/' + repr(content))


GROUPS = {'rule-content-inspection-helpers': inspection, 'rule-content-metadata-helpers': metadata,
          'rule-content-append-helpers': append}
