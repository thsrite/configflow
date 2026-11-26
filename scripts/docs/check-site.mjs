#!/usr/bin/env node
// Check files and fragments in built guide HTML.

import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';

function decodeHtmlEntities(text) {
  return text
    .replace(/&#x([0-9a-fA-F]+);/g, (_, hex) => String.fromCodePoint(parseInt(hex, 16)))
    .replace(/&#([0-9]+);/g, (_, dec) => String.fromCodePoint(parseInt(dec, 10)))
    .replace(/&quot;/g, '"')
    .replace(/&apos;/g, "'")
    .replace(/&#39;/g, "'")
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&amp;/g, '&');
}

function parseAttrs(attrText) {
  const attrs = Object.create(null);
  const re = /([a-zA-Z_:][-a-zA-Z0-9_:.]*)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'=<>`]+)))?/g;
  let match;
  while ((match = re.exec(attrText)) !== null) {
    const name = match[1].toLowerCase();
    const raw = match[2] ?? match[3] ?? match[4] ?? '';
    attrs[name] = decodeHtmlEntities(raw);
  }
  return attrs;
}

function parsePage(content) {
  const ids = new Set();
  const links = [];
  // Strip HTML comments and script/style blocks before scanning tags.
  const cleaned = content
    .replace(/<!--[\s\S]*?-->/g, '')
    .replace(/<(script|style)\b[^>]*>[\s\S]*?<\/\1>/gi, '');
  const tagRe = /<([a-zA-Z][a-zA-Z0-9:-]*)(\s[^>]*?)?\/?>/g;
  let match;
  while ((match = tagRe.exec(cleaned)) !== null) {
    const tag = match[1].toLowerCase();
    const attrText = match[2];
    if (!attrText) {
      continue;
    }
    const attrs = parseAttrs(attrText);
    if (attrs.id) {
      ids.add(attrs.id);
    }
    if (tag === 'a' && attrs.name) {
      ids.add(attrs.name);
    }
    const key = tag === 'a' ? 'href' : tag === 'img' ? 'src' : null;
    if (key && attrs[key]) {
      links.push(attrs[key]);
    }
  }
  return { ids, links };
}

function collectHtmlFiles(dir, out = []) {
  if (!fs.existsSync(dir)) {
    return out;
  }
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      collectHtmlFiles(full, out);
    } else if (entry.isFile() && entry.name.endsWith('.html')) {
      out.push(full);
    }
  }
  return out;
}

function isFile(filePath) {
  try {
    return fs.statSync(filePath).isFile();
  } catch {
    return false;
  }
}

function safeDecode(value) {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

let directory = 'doc/.vitepress/dist';
let rawBase = '/';
const argv = process.argv.slice(2);
for (let i = 0; i < argv.length; i += 1) {
  const arg = argv[i];
  if (arg === '--base') {
    rawBase = argv[++i] ?? '/';
  } else if (arg.startsWith('--base=')) {
    rawBase = arg.slice('--base='.length);
  } else if (!arg.startsWith('-')) {
    directory = arg;
  } else {
    console.error(`Unknown option: ${arg}`);
    process.exit(2);
  }
}

const root = path.resolve(directory);
const trimmedBase = rawBase.replace(/^\/+|\/+$/g, '');
const base = trimmedBase ? `/${trimmedBase}/` : '/';

const pages = new Map();
for (const filePath of collectHtmlFiles(root)) {
  pages.set(filePath, parsePage(fs.readFileSync(filePath, 'utf8')));
}
if (pages.size === 0) {
  console.error('No built HTML found; build the guide first.');
  process.exit(1);
}

const failures = [];
let checked = 0;
const origin = 'https://local.invalid';

for (const [source, page] of pages.entries()) {
  const relPosix = path.relative(root, source).split(path.sep).join('/');
  const sourceUrl = new URL(base + relPosix, origin);

  for (const href of page.links) {
    if (/^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(href) || href.startsWith('//')) {
      let parsed;
      try {
        parsed = new URL(href, origin);
      } catch {
        continue;
      }
      const decodedPath = safeDecode(parsed.pathname).toLowerCase();
      if (
        parsed.hostname === 'github.com' &&
        parsed.pathname.includes('/blob/') &&
        (decodedPath.endsWith('.md') || decodedPath.endsWith('.markdown'))
      ) {
        failures.push(`${relPosix}: documentation link leaves the site: ${href}`);
      }
      continue;
    }

    const resolved = new URL(href, sourceUrl);
    const resolvedPath = safeDecode(resolved.pathname);
    if (!resolvedPath.startsWith(base)) {
      failures.push(`${relPosix}: outside base: ${href}`);
      continue;
    }
    const relTarget = resolvedPath.slice(base.length);
    const targetBase = path.resolve(root, relTarget);
    const candidates = [targetBase, `${targetBase}.html`, path.join(targetBase, 'index.html')];
    const target = candidates.find((candidate) => isFile(candidate)) ?? null;
    checked += 1;
    if (target === null) {
      failures.push(`${relPosix}: missing file: ${href}`);
    } else if (resolved.hash && pages.has(target)) {
      const fragment = safeDecode(resolved.hash.slice(1));
      if (!pages.get(target).ids.has(fragment)) {
        failures.push(`${relPosix}: missing anchor: ${href}`);
      }
    }
  }
}

for (const failure of failures) {
  console.log(failure);
}
console.log(`Checked ${pages.size} HTML pages and ${checked} local references; ${failures.length} failures.`);
process.exit(failures.length > 0 ? 1 : 0);
