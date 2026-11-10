"""libyaml is only trusted on a text after it was seen to agree with PyYAML there."""
import math
import random
import threading

import pytest
import yaml

from backend.common import oxidns_native
from backend.common.oxidns_native import load_native_yaml
from backend.common.oxidns_settings import OxidnsSettingsError
from backend.utils import fast_yaml

libyaml = pytest.mark.skipif(fast_yaml.LIBYAML_SAFE_LOADER is None, reason='PyYAML built without libyaml')


@pytest.fixture(autouse=True)
def forget_verified_texts():
    fast_yaml._verified.clear()
    yield
    fast_yaml._verified.clear()


@pytest.fixture
def loaders(monkeypatch):
    """The loader class of every yaml.load call, in order."""
    used, real = [], yaml.load

    def load(stream, Loader):
        used.append(Loader)
        return real(stream, Loader=Loader)
    monkeypatch.setattr(fast_yaml.yaml, 'load', load)
    return used


def outcome(read):
    try:
        return 'value', read()
    except Exception as error:
        return 'error', type(error), str(error)


@libyaml
def test_a_text_is_checked_by_both_loaders_once_and_by_libyaml_afterwards(loaders):
    text = 'a: 1\nb: [x, {c: 2.5}]\n'
    assert fast_yaml.safe_load(text) == {'a': 1, 'b': ['x', {'c': 2.5}]}
    assert loaders == [yaml.SafeLoader, yaml.CSafeLoader]
    del loaders[:]
    for _ in range(3):
        assert fast_yaml.safe_load(text) == {'a': 1, 'b': ['x', {'c': 2.5}]}
    assert loaders == [yaml.CSafeLoader] * 3
    # Another text has not been checked yet, however similar.
    del loaders[:]
    fast_yaml.safe_load(text + '\n')
    assert loaders == [yaml.SafeLoader, yaml.CSafeLoader]


def test_a_text_the_fast_loader_reads_differently_stays_with_pyyaml(loaders):
    class Different(yaml.SafeLoader):
        pass
    Different.add_constructor('tag:yaml.org,2002:int', lambda loader, node: str(loader.construct_scalar(node)))
    for _ in range(3):
        assert fast_yaml.load('a: 1\n', yaml.SafeLoader, Different) == {'a': 1}
    assert loaders == [yaml.SafeLoader, Different] * 3
    assert not fast_yaml._verified


def test_a_text_the_fast_loader_rejects_stays_with_pyyaml(loaders):
    class Rejecting(yaml.SafeLoader):
        def get_single_data(self):
            raise yaml.YAMLError('not here')
    for _ in range(2):
        assert fast_yaml.load('a: 1\n', yaml.SafeLoader, Rejecting) == {'a': 1}
    assert loaders == [yaml.SafeLoader, Rejecting] * 2


def test_invalid_text_reports_pyyamls_error_every_time(loaders):
    text = 'a: [1, 2\nb: }\n'
    expected = outcome(lambda: yaml.safe_load(text))
    assert expected[0] == 'error'
    for _ in range(3):
        assert outcome(lambda: fast_yaml.safe_load(text)) == expected
    # PyYAML raised before libyaml was asked, and nothing was remembered.
    assert set(loaders) == {yaml.SafeLoader}
    assert not fast_yaml._verified


def test_without_libyaml_or_for_non_text_only_pyyaml_reads(loaders):
    assert fast_yaml.load('a: 1\n', yaml.SafeLoader, None) == {'a': 1}
    assert fast_yaml.load(b'a: 1\n', yaml.SafeLoader, yaml.SafeLoader) == {'a': 1}
    assert loaders == [yaml.SafeLoader] * 2
    assert not fast_yaml._verified


@pytest.mark.parametrize('left,right', [
    (1, True), (0, False), (1, 1.0), ('1', 1), (0.0, -0.0), ([1], (1,)), ({'a': 1}, [('a', 1)]),
    ({'a': 1, 'b': 2}, {'b': 2, 'a': 1}), ({'a': 1}, {'a': 1, 'b': 2}), ([1, 2], [1]), ({'a': [1, {'b': True}]}, {'a': [1, {'b': 1}]}),
    (None, False), ('a', b'a'), (float('nan'), 1.0), ({1: 'a'}, {True: 'a'}),
])
def test_values_that_only_compare_equal_are_not_the_same(left, right):
    assert not fast_yaml._same(left, right)
    assert not fast_yaml._same(right, left)


def test_recursive_and_very_deep_values_are_left_to_pyyaml(loaders):
    cyclic = yaml.safe_load('a: &x [1, *x]\n')
    assert not fast_yaml._same(cyclic, yaml.safe_load('a: &x [1, *x]\n'))
    for text in ('a: &x [1, *x]\n', 'a: ' + '[' * 400 + ']' * 400 + '\n'):
        expected = outcome(lambda: yaml.safe_load(text))
        del loaders[:]
        for _ in range(2):
            got = outcome(lambda: fast_yaml.safe_load(text))
            assert got[0] == expected[0] and (got[0] == 'value' or got == expected)
        assert yaml.SafeLoader in loaders and not fast_yaml._verified


def test_same_values_include_nan_and_nested_order():
    value = {'a': [1, 2.5, 'x', None, True, {'b': float('nan'), 'c': -0.0}], 'd': {}}
    other = {'a': [1, 2.5, 'x', None, True, {'b': float('nan'), 'c': -0.0}], 'd': {}}
    assert fast_yaml._same(value, other)
    assert math.isnan(other['a'][5]['b'])


@libyaml
def test_only_the_most_recent_texts_stay_verified(monkeypatch, loaders):
    monkeypatch.setattr(fast_yaml, '_VERIFIED_LIMIT', 2)
    texts = ['a: 1\n', 'a: 2\n', 'a: 3\n']
    for text in texts:
        fast_yaml.safe_load(text)
    assert len(fast_yaml._verified) == 2
    del loaders[:]
    fast_yaml.safe_load(texts[2])
    assert loaders == [yaml.CSafeLoader]
    del loaders[:]
    fast_yaml.safe_load(texts[0])  # forgotten: checked again
    assert loaders == [yaml.SafeLoader, yaml.CSafeLoader]
    # Reading a verified text keeps it ahead of older ones.
    fast_yaml.safe_load(texts[2])
    fast_yaml.safe_load('a: 4\n')
    del loaders[:]
    fast_yaml.safe_load(texts[2])
    assert loaders == [yaml.CSafeLoader]


def random_document(rng):
    scalars = ['a', 'on', 'off', 'yes', '~', 'null', '1', '0x1f', '0o17', '1_000', '1e3', '.inf', '.nan', '-0.0', '2026-11-10',
               '2026-11-10 08:00:00', '1:20', '"q"', "'s'", 'a b', 'a: b', '', '🇭🇰 香港', 'é', '&a x', '*a', '!!str 1', '!x y',
               '<<', '=', '[1, 2]', '{a: 1}', '|\n    text', '>\n    folded', 'a#b', 'a #c', '\ttab', 'x\ty', '@at', '`tick', '%p']
    lines = []
    for _ in range(rng.randrange(1, 9)):
        key = rng.choice(['k', 'on', '1', '1.5', 'a b', '? x', '"quoted"', '🙂', 'k'])
        shape = rng.randrange(6)
        if shape == 0:
            lines.append(f'{key}: {rng.choice(scalars)}')
        elif shape == 1:
            lines.append(f'{key}:')
            lines.extend(f'{rng.choice(["  ", "", "    ", chr(9)])}- {rng.choice(scalars)}' for _ in range(rng.randrange(1, 4)))
        elif shape == 2:
            lines.append(f'{key}:')
            lines.extend(f'  {rng.choice(["x", "y", "x"])}: {rng.choice(scalars)}' for _ in range(rng.randrange(1, 4)))
        elif shape == 3:
            lines.append(f'{key}: [{", ".join(rng.choice(scalars[:20]) for _ in range(rng.randrange(0, 4)))}]')
        elif shape == 4:
            lines.append(f'{key}: &anchor {rng.choice(scalars[:12])}')
            lines.append(f'{key}2: *anchor')
        else:
            lines.append(rng.choice(['---', '...', '# comment', '', '- top', '%YAML 1.1', '﻿bom: 1', 'k: "unterminated']))
    return rng.choice(['', '', '﻿']) + '\n'.join(lines) + rng.choice(['\n', '', '\n\n', '\r\n'])


@libyaml
def test_repeated_reads_always_match_pyyaml_on_random_documents():
    rng = random.Random(20261110)
    verified = errors = 0
    for _ in range(3000):
        document = random_document(rng)
        expected = outcome(lambda: yaml.safe_load(document))
        errors += expected[0] == 'error'
        for _ in range(3):
            got = outcome(lambda: fast_yaml.safe_load(document))
            assert got[0] == expected[0], document
            if got[0] == 'value':
                assert fast_yaml._same(got[1], expected[1]), document
            else:
                assert got == expected, document
        verified += (yaml.SafeLoader, fast_yaml.hashlib.sha256(document.encode('utf-8', 'surrogatepass')).digest()) in fast_yaml._verified
        if len(fast_yaml._verified) > 400:
            fast_yaml._verified.clear()
    # The generator must exercise the fast path, the rejections and the disagreements.
    assert verified > 500 and errors > 300


NATIVE_CASES = [
    'plugins: []\n',
    'on: off\nyes: no\ndate: 2026-10-16\ndecimal: 0123\noctal: 0o17\nclock: 1:20\nexponent: 1e3\nflag: false\nplugins: []',
    'plugins:\n  - tag: forward_local\n    type: forward\n    args:\n      upstreams:\n        - addr: udp://192.0.2.1\n      concurrent: 1_0\n      hex: 0x10\n',
    'api: {http: {listen: "127.0.0.1:9199"}}\nplugins: []\n',
    'a: &x [1, 2]\nb: *x\nplugins: []\n',
    'name: 🇭🇰 香港\nplugins: []\n',
    'ratio: 1.5\nneg: -3\nbool: TRUE\nnone: ~\nempty:\nplugins: []\n',
]
NATIVE_REJECTED = [
    'plugins: []\nplugins: []', 'plugins: []\nconstructor: bad', '__proto__: 1\nplugins: []', '1: a\nplugins: []',
    'plugins: !!python/object:dict {}', 'plugins: &cycle [*cycle]', 'plugins: [\n', 'x: &x {a: 1}\ny:\n  <<: *x\n',
    'plugins:\n\t- a\n', 'a: !!binary AAAA\n', '? [a, b]\n: 1\n',
]


@libyaml
@pytest.mark.parametrize('text', NATIVE_CASES)
def test_native_documents_keep_their_values_once_libyaml_reads_them(text, loaders):
    first = load_native_yaml(text)
    assert loaders == [oxidns_native.NativeLoader, oxidns_native._LibyamlNativeLoader]
    del loaders[:]
    again = load_native_yaml(text)
    assert loaders == [oxidns_native._LibyamlNativeLoader]
    assert fast_yaml._same(first, again)
    assert fast_yaml._same(first, yaml.load(text, Loader=oxidns_native.NativeLoader))
    # Callers edit the returned document; it must not be shared between reads.
    assert first is not again and all(first[key] is not again[key] for key in first if isinstance(first[key], (dict, list)))


@pytest.mark.parametrize('text', NATIVE_REJECTED)
def test_native_rejections_are_identical_on_every_read(text):
    def read():
        try:
            return 'value', load_native_yaml(text)
        except OxidnsSettingsError as error:
            return 'error', str(error)
    expected = read()
    assert expected[0] == 'error'
    # Whether the parser or the structure check after it refuses, every read says the same.
    assert all(read() == expected for _ in range(3))


@libyaml
def test_libyaml_native_loader_enforces_the_same_rules_on_its_own():
    # The class is only used on verified texts, but it must not be a weaker parser.
    for text in NATIVE_CASES + NATIVE_REJECTED:
        if '*cycle' in text:
            continue  # both build a recursive list, which is never called the same
        read = lambda loader: yaml.load(text, Loader=loader)
        expected = outcome(lambda: read(oxidns_native.NativeLoader))
        got = outcome(lambda: read(oxidns_native._LibyamlNativeLoader))
        assert got[0] == expected[0], text
        if got[0] == 'value':
            assert fast_yaml._same(got[1], expected[1]), text
        elif issubclass(expected[1], OxidnsSettingsError):
            assert got == expected, text


@libyaml
def test_generated_oxidns_configuration_is_read_by_libyaml_after_the_first_time(loaders):
    from backend.converters.oxidns import generate_oxidns_config
    text = generate_oxidns_config({'oxidns': {'local_dns': 'udp://192.0.2.1', 'remote_dns': 'udp://192.0.2.2'},
                                   'rule_configs': [], 'rule_library': [],
                                   'system_config': {'server_domain': 'https://config.test'}})
    del loaders[:]
    first = load_native_yaml(text)
    assert loaders[-1] is oxidns_native._LibyamlNativeLoader and len(loaders) == 2
    del loaders[:]
    assert fast_yaml._same(load_native_yaml(text), first)
    assert loaders == [oxidns_native._LibyamlNativeLoader]


@libyaml
def test_concurrent_reads_agree():
    texts = [f'plugins: []\nvalue: {index}\nlist: [1, 2, {index}]\n' for index in range(8)]
    failures = []

    def work():
        try:
            for _ in range(40):
                for index, text in enumerate(texts):
                    if load_native_yaml(text) != {'plugins': [], 'value': index, 'list': [1, 2, index]}:
                        failures.append(text)
        except Exception as error:  # pragma: no cover - reported below
            failures.append(repr(error))
    threads = [threading.Thread(target=work) for _ in range(8)]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()
    assert failures == []
    assert len(fast_yaml._verified) == len(texts)
