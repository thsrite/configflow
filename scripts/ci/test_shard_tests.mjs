import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';

import { main, normalizePaths, selectValues, validatePartition } from './shard_tests.mjs';

test('partition rejects missing, extra, overlap, and duplicates', () => {
  const invalid = [
    [['a', 'b'], [['a']]],
    [['a'], [['a'], ['extra']]],
    [['a', 'b'], [['a', 'b'], ['b']]],
    [['a'], [['a', 'a']]],
    [['a', 'a'], [['a']]],
    [['a'], [['a'], []]],
    [[], []],
  ];
  for (const [expected, shards] of invalid) {
    assert.throws(() => validatePartition(expected, shards));
  }
});

test('vitest JSON selectors and validation CLI', () => {
  const expected = [{ file: '/tests/a.test.ts' }, { file: '/tests/b.test.ts' }];
  const reports = expected.map((item) => ({ testResults: [{ name: item.file }] }));
  assert.deepEqual(selectValues(expected, 'file'), ['/tests/a.test.ts', '/tests/b.test.ts']);

  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'shard-tests-'));
  try {
    const allPath = path.join(dir, 'all.json');
    const s1Path = path.join(dir, '1.json');
    const s2Path = path.join(dir, '2.json');
    fs.writeFileSync(allPath, JSON.stringify(expected));
    fs.writeFileSync(s1Path, JSON.stringify(reports[0]));
    fs.writeFileSync(s2Path, JSON.stringify(reports[1]));
    const code = main([
      'validate',
      '--expected',
      allPath,
      '--expected-key',
      'file',
      '--shard-key',
      'testResults.name',
      s1Path,
      s2Path,
    ]);
    assert.equal(code, 0);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }

  assert.throws(() => selectValues({ wrong: [] }, 'testResults.name'));
  assert.throws(() => selectValues([{ file: null }], 'file'));
});

test('validation rejects different full inventories even when selected union matches', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'shard-inv-'));
  try {
    const p1 = path.join(dir, '1.json');
    const p2 = path.join(dir, '2.json');
    fs.writeFileSync(p1, JSON.stringify({ all_nodeids: ['a', 'b'], selected_nodeids: ['a'] }));
    fs.writeFileSync(p2, JSON.stringify({ all_nodeids: ['a', 'b'], selected_nodeids: ['b'] }));
    const args = [
      'validate',
      '--expected',
      p1,
      '--expected-key',
      'all_nodeids',
      '--shard-key',
      'selected_nodeids',
      '--inventory-key',
      'all_nodeids',
      p1,
      p2,
    ];
    assert.equal(main(args), 0);

    fs.writeFileSync(p2, JSON.stringify({ all_nodeids: ['a', 'b', 'new'], selected_nodeids: ['b'] }));
    assert.equal(main(args), 1);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test('report paths normalize across checkout roots and reject unrelated paths', () => {
  const expected = normalizePaths(
    ['/local/configflow/frontend/tests/a.test.ts', '/local/configflow/frontend/tests/b.test.ts'],
    'frontend/tests',
  );
  const shards = ['a', 'b'].map((name, index) =>
    normalizePaths([`/runner/${index}/frontend/tests/${name}.test.ts`], 'frontend/tests'),
  );
  validatePartition(expected, shards);

  for (const bad of [
    '/runner/tests/a.test.ts',
    '/runner/frontend/tests/../secret',
    '/frontend/tests/copy/frontend/tests/a.test.ts',
  ]) {
    assert.throws(() => normalizePaths([bad], 'frontend/tests'));
  }
});
