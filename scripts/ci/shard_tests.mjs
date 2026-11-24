#!/usr/bin/env node
// Verify exact, disjoint partitions of CI test shard inventories.

import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';

function readJson(filePath) {
  return JSON.parse(fs.readFileSync(filePath, 'utf8'));
}

export function uniqueInventory(items, label) {
  if (!Array.isArray(items) || items.length === 0) {
    throw new Error(`${label} must contain a nonempty list`);
  }
  const counts = new Map();
  for (const item of items) {
    if (typeof item !== 'string' || item.length === 0) {
      throw new Error(`${label} must contain nonempty strings`);
    }
    counts.set(item, (counts.get(item) || 0) + 1);
  }
  const duplicates = [...counts.entries()]
    .filter(([, count]) => count > 1)
    .map(([item]) => item)
    .sort();
  if (duplicates.length > 0) {
    throw new Error(`${label} contains duplicates: ${JSON.stringify(duplicates.slice(0, 5))}`);
  }
  return new Set(items);
}

export function validatePartition(expected, shards) {
  const expectedSet = uniqueInventory(expected, 'Expected inventory');
  if (!Array.isArray(shards) || shards.length === 0) {
    throw new Error('At least one shard is required');
  }
  const actual = new Map();
  shards.forEach((shard, index) => {
    uniqueInventory(shard, `Shard ${index + 1}`);
    for (const item of shard) {
      actual.set(item, (actual.get(item) || 0) + 1);
    }
  });
  const overlaps = [...actual.entries()]
    .filter(([, count]) => count > 1)
    .map(([item]) => item)
    .sort();
  const missing = [...expectedSet].filter((item) => !actual.has(item)).sort();
  const extra = [...actual.keys()].filter((item) => !expectedSet.has(item)).sort();
  if (overlaps.length > 0 || missing.length > 0 || extra.length > 0) {
    throw new Error(
      `Invalid partition: overlapping=${JSON.stringify(overlaps.slice(0, 5))}, ` +
        `missing=${JSON.stringify(missing.slice(0, 5))}, extra=${JSON.stringify(extra.slice(0, 5))}`,
    );
  }
}

export function selectValues(data, selector) {
  const parts = selector ? selector.split('.') : [];
  function visit(value, rest) {
    if (Array.isArray(value)) {
      const out = [];
      for (const child of value) {
        out.push(...visit(child, rest));
      }
      return out;
    }
    if (rest.length > 0) {
      if (value === null || typeof value !== 'object' || !(rest[0] in value)) {
        throw new Error(`Missing JSON selector: ${selector}`);
      }
      return visit(value[rest[0]], rest.slice(1));
    }
    if (typeof value !== 'string' || value.length === 0) {
      throw new Error(`JSON selector ${JSON.stringify(selector)} must select nonempty strings`);
    }
    return [value];
  }
  return visit(data, parts);
}

function splitPathParts(rawPath) {
  return rawPath.replace(/\\/g, '/').split('/').filter((part) => part.length > 0 && part !== '.');
}

export function normalizePaths(items, prefix) {
  if (!prefix || path.isAbsolute(prefix) || prefix.startsWith('/')) {
    throw new Error('Path prefix must be a nonempty relative directory');
  }
  const anchor = splitPathParts(prefix);
  if (anchor.length === 0 || anchor.includes('..')) {
    throw new Error('Path prefix must be a nonempty relative directory');
  }
  const result = [];
  for (const item of items) {
    const parts = splitPathParts(item);
    if (parts.includes('..')) {
      throw new Error(`Report path does not uniquely match ${prefix}: ${item}`);
    }
    const matches = [];
    for (let i = 0; i <= parts.length - anchor.length; i += 1) {
      let ok = true;
      for (let j = 0; j < anchor.length; j += 1) {
        if (parts[i + j] !== anchor[j]) {
          ok = false;
          break;
        }
      }
      if (ok) {
        matches.push(i);
      }
    }
    if (matches.length !== 1) {
      throw new Error(`Report path does not uniquely match ${prefix}: ${item}`);
    }
    result.push(parts.slice(matches[0]).join('/'));
  }
  return result;
}

function parseValidateArgs(argv) {
  if (argv[0] !== 'validate') {
    throw new Error('Expected subcommand: validate');
  }
  const opts = {
    expected: '',
    expectedKey: '',
    shardKey: '',
    inventoryKey: '',
    pathPrefix: '',
    shards: [],
  };
  for (let i = 1; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === '--expected') {
      opts.expected = argv[++i] ?? '';
    } else if (arg === '--expected-key') {
      opts.expectedKey = argv[++i] ?? '';
    } else if (arg === '--shard-key') {
      opts.shardKey = argv[++i] ?? '';
    } else if (arg === '--inventory-key') {
      opts.inventoryKey = argv[++i] ?? '';
    } else if (arg === '--path-prefix') {
      opts.pathPrefix = argv[++i] ?? '';
    } else if (arg.startsWith('--')) {
      throw new Error(`Unknown option: ${arg}`);
    } else {
      opts.shards.push(arg);
    }
  }
  if (!opts.expected) {
    throw new Error('--expected is required');
  }
  if (opts.shards.length === 0) {
    throw new Error('At least one shard file is required');
  }
  return opts;
}

export function main(argv = process.argv.slice(2)) {
  try {
    const args = parseValidateArgs(argv);
    let expected = selectValues(readJson(args.expected), args.expectedKey);
    const documents = args.shards.map((filePath) => readJson(filePath));
    let shards = documents.map((doc) => selectValues(doc, args.shardKey));
    if (args.pathPrefix) {
      expected = normalizePaths(expected, args.pathPrefix);
      shards = shards.map((shard) => normalizePaths(shard, args.pathPrefix));
    }
    if (args.inventoryKey) {
      args.shards.forEach((filePath, index) => {
        let inventory = selectValues(documents[index], args.inventoryKey);
        if (args.pathPrefix) {
          inventory = normalizePaths(inventory, args.pathPrefix);
        }
        try {
          validatePartition(expected, [inventory]);
        } catch (err) {
          throw new Error(`Shard inventory differs in ${filePath}: ${err.message}`);
        }
      });
    }
    validatePartition(expected, shards);
    console.log(`Validated ${expected.length} unique entries across ${shards.length} disjoint shards`);
    return 0;
  } catch (err) {
    console.error(`CI shard error: ${err instanceof Error ? err.message : String(err)}`);
    return 1;
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  process.exit(main());
}
