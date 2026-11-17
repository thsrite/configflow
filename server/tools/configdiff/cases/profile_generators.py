"""Public generator P3–P6 selection with distinct per-profile output markers."""
import copy
import io
import re
import zipfile
from urllib.parse import urlencode

import yaml

from compare import database, differences, files, structure
from downloads import capture_headers
from harness import Pair, json_body
from inventory import selector_rows
from sources import Source
from subscriptions import Exchange
from cases.generate import FILENAMES
from cases.generator_shapes import project
from cases.helpers import profile_policy
from cases.profile_reads import selected
from cases.subscriptions import require


def profile(owner):
    marker = 'selector-' + owner
    custom = yaml.safe_dump({'plugins': [{'tag': marker, 'type': 'cache', 'args': {'size': 17}}]})
    return {'mihomo': {'custom_config': 'mixed-port: ' + ('19281' if owner == 'default' else '19282') + '\n'},
            'surge': {'custom_config': '[General]\ndns-server = ' + marker + '.example\n'},
            'loon': {'custom_config': '[General]\ndns-server = ' + marker + '.example\n'},
            'mosdns': {'log_enabled': True, 'log_file': './' + marker + '.log'},
            'oxidns': {'config_mode': 'native', 'native_config': custom}}


def generated_errors(before, after, owner, name, body):
    out = differences(before['database'], after['database'], 'generator.readonly-database')
    left, right = copy.deepcopy(before['files']), copy.deepcopy(after['files'])
    if name:
        path = 'profiles/' + owner + '/generated/' + name
        saved = right.pop(path, None)
        previous = left.pop(path, None)
        out += differences(structure(body, filename=path), saved.get('content') if isinstance(saved, dict) else None,
                           'generator.selected-file-content')
        # A repeated generated write may replace exactly its own recovery .bak.
        # Its contents must be the actual previous file, never arbitrary data.
        if path + '.bak' in right and previous is not None:
            backup = right.pop(path + '.bak')
            left.pop(path + '.bak', None)
            out += differences(previous.get('content'), backup.get('content'), 'generator.previous-file-backup')
    return out + differences(left, right, 'generator.unselected-files')


class GeneratedOnly(Exchange):
    def __init__(self, pair, source, owner, name):
        super().__init__(pair, source, expected=0)
        self.pair, self.owner, self.name = pair, owner, name
        self.violations = [[], []]

    def snapshot(self, index):
        value = {'database': database(self.pair.dirs[index]), 'files': files(self.pair.dirs[index])}
        if self.name:
            backup = 'profiles/' + self.owner + '/generated/' + self.name + '.bak'
            if backup in value['files']:
                # A backup has the original format despite its .bak suffix.
                # Only this intra-server predecessor assertion uses the
                # original extension; global file_diffs still compares all
                # stored backup bytes/structure through its normal policy.
                value['files'][backup]['content'] = structure((self.pair.dirs[index] / backup).read_bytes(), filename=backup[:-4])
        return value

    def before(self, index):
        super().before(index)
        self.previous = self.snapshot(index)

    def after(self, index, reply):
        super().after(index, reply)
        self.violations[index] = generated_errors(self.previous, self.snapshot(index), self.owner, self.name, reply.body)
        if index == 0:
            require(not self.violations[0], 'generator leaves SQL/foreign files exact and writes only selected output matching the response: '
                    + repr(self.violations[0][:5]))

    def compare(self, policy):
        return super().compare(policy) + self.violations[1]


def selectors(binary, recorder):
    for rid, route, recipes in selector_rows():
        match = re.fullmatch(r'POST /api/(?:profiles/<profile_id>/)?generate/(mihomo|surge|loon|mosdns|oxidns)(/preview)?', route)
        if not match:
            continue
        service, preview = match[1], bool(match[2])
        with Source(b'generator must use local profile text') as source, Pair(binary, recorder,
                options={'profile': profile('default'), 'other_profile': profile('other')}) as pair:
            pair.policy = profile_policy(pair)
            for suffix, number in recipes:
                query, header, owner = selected(route, number, pair.default_profile)
                path = route.split(' ', 1)[1].replace('<profile_id>', 'default')
                if query:
                    path += '?' + urlencode(query)
                def check(reply):
                    require(reply.headers.get('x-configflow-profile') == owner, 'generator names exact selected URL/query/header/default profile')
                    if preview:
                        text = reply.json()['content']
                    elif service in ('mosdns', 'oxidns'):
                        with zipfile.ZipFile(io.BytesIO(reply.body)) as archive:
                            text = archive.read('config.yaml').decode()
                    else:
                        text = reply.body.decode()
                    marker = str(19281 if owner == 'default' else 19282) if service == 'mihomo' else 'selector-' + owner
                    other = str(19282 if owner == 'default' else 19281) if service == 'mihomo' else 'selector-' + ('other' if owner == 'default' else 'default')
                    require(marker in text and other not in text, 'actual generated configuration contains only selected profile marker')
                saved = {'mihomo': 'config.yaml', 'surge': 'config.conf', 'loon': 'loon.lcf'}.get(service, '') if not preview else ''
                pair.request(f'{rid}-B{suffix:02}', route, path, body=json_body({'base_url': 'http://panel.example'}),
                    headers={'Host': 'panel.example', 'X-ConfigFlow-Profile': header}, expect=200, writes=True,
                    check=check, projection=project(service) if preview else None,
                    filename=FILENAMES[service] if not preview else '', capture=None if preview else capture_headers,
                    interaction=GeneratedOnly(pair, source, owner, saved), variant='P' + str(number) + '/distinct-generated-profile')


GROUPS = {'profile-generator-selectors': selectors}
