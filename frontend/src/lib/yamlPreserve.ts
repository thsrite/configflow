import { parseDocument, type Document, type DocumentOptions, type ParseOptions, type SchemaOptions } from 'yaml'

/**
 * Write an edited YAML document back without reformatting the rest of the user's text.
 * Shared by the Mihomo base settings form and the IPv6 settings.
 */

export type YamlParseOptions = ParseOptions & DocumentOptions & SchemaOptions
/** The server reads the YAML with PyYAML, i.e. YAML 1.1 (`off`, `yes` are booleans, `0755` is octal). */
export const YAML_1_1 = { version: '1.1', merge: true, uniqueKeys: true } as const satisfies YamlParseOptions
export const YAML_PRINT_OPTIONS = { lineWidth: 0 } as const

/** Deep equality of parsed YAML values (plain objects, arrays, Map, Set, Date, scalars). */
export function sameYamlValue(left: unknown, right: unknown): boolean {
  if (left instanceof Date || right instanceof Date) {
    return left instanceof Date && right instanceof Date && Object.is(left.getTime(), right.getTime())
  }
  if (left instanceof Map || right instanceof Map || left instanceof Set || right instanceof Set) {
    const kind = left instanceof Map ? Map : Set
    return left instanceof kind && right instanceof kind && sameYamlValue([...left], [...right])
  }
  if (Array.isArray(left) || Array.isArray(right)) {
    return Array.isArray(left) && Array.isArray(right) && left.length === right.length && left.every((item, index) => sameYamlValue(item, right[index]))
  }
  if (left && right && typeof left === 'object' && typeof right === 'object') {
    const keys = Object.keys(left)
    return keys.length === Object.keys(right).length && keys.every(key => sameYamlValue((left as never)[key], (right as never)[key]))
  }
  return left === right || (typeof left === 'number' && typeof right === 'number' && Number.isNaN(left) && Number.isNaN(right))
}

/** Line pairs [a, b] of a longest common subsequence of two line lists. */
function commonLines(left: string[], right: string[]): Array<[number, number]> | null {
  let start = 0
  while (start < left.length && start < right.length && left[start] === right[start]) start++
  let leftEnd = left.length
  let rightEnd = right.length
  while (leftEnd > start && rightEnd > start && left[leftEnd - 1] === right[rightEnd - 1]) { leftEnd--; rightEnd-- }
  const rows = leftEnd - start
  const columns = rightEnd - start
  if (rows * columns > 4_000_000) return null
  const table = new Int32Array((rows + 1) * (columns + 1))
  for (let i = rows - 1; i >= 0; i--) {
    for (let j = columns - 1; j >= 0; j--) {
      table[i * (columns + 1) + j] = left[start + i] === right[start + j] ? table[(i + 1) * (columns + 1) + j + 1] + 1
        : Math.max(table[(i + 1) * (columns + 1) + j], table[i * (columns + 1) + j + 1])
    }
  }
  const pairs: Array<[number, number]> = []
  for (let i = 0; i < start; i++) pairs.push([i, i])
  let i = 0
  let j = 0
  while (i < rows && j < columns) {
    if (left[start + i] === right[start + j]) { pairs.push([start + i, start + j]); i++; j++ }
    else if (table[(i + 1) * (columns + 1) + j] >= table[i * (columns + 1) + j + 1]) i++
    else j++
  }
  for (let k = 0; k < left.length - leftEnd; k++) pairs.push([leftEnd + k, rightEnd + k])
  return pairs
}

/**
 * Follow the text's own layout so unchanged lines print identically:
 * list style `key:\n- item` (as PyYAML writes) or `key:\n  - item`, and the
 * most common mapping indentation (2 or 4 spaces, ...).
 */
function printOptions(source: string) {
  const lines = source.split('\n')
  let indentless = 0
  let indented = 0
  const steps = new Map<number, number>()
  for (let index = 0; index + 1 < lines.length; index++) {
    const key = /^(\s*)[^\s#-][^#]*:\s*(#.*)?$/.exec(lines[index])
    if (!key) continue
    let next = index + 1
    while (next < lines.length && /^\s*(#.*)?$/.test(lines[next])) next++
    if (next >= lines.length) continue
    const item = /^(\s*)- /.exec(lines[next])
    if (item) {
      if (next !== index + 1) continue
      if (item[1].length === key[1].length) indentless++
      else if (item[1].length > key[1].length) indented++
      continue
    }
    const step = (/^\s*/.exec(lines[next])?.[0].length ?? 0) - key[1].length
    if (step > 0 && step <= 8) steps.set(step, (steps.get(step) ?? 0) + 1)
  }
  let indent = 2
  for (const [step, count] of steps) if (count > (steps.get(indent) ?? 0)) indent = step
  return { ...YAML_PRINT_OPTIONS, indent, indentSeq: indentless <= indented }
}

const indentOf = (line: string) => line.length - line.trimStart().length

/**
 * Re-indent a printed line like the original text of this section (e.g. a
 * four-space `dns:` block in an otherwise two-space file), using the printed /
 * original pairs of lines that correspond one to one. Unknown cases are left as printed.
 */
function sectionIndent(printed: string[], original: string[]): (line: string) => string {
  const levels = new Map<number, number>([[0, 0]])
  for (let index = 0; index < printed.length; index++) {
    if (!printed[index].trim() || printed[index].trimStart().startsWith('#')) continue
    const from = indentOf(printed[index])
    const to = indentOf(original[index])
    if (levels.has(from) && levels.get(from) !== to) return line => line
    levels.set(from, to)
  }
  return line => {
    const from = indentOf(line)
    if (!line.trim() || levels.get(from) === from) return line
    const known = Math.max(...[...levels.keys()].filter(level => level <= from))
    return ' '.repeat(levels.get(known)! + from - known) + line.trimStart()
  }
}

function values(text: string, options: YamlParseOptions): { ok: boolean; value?: unknown } {
  const document = parseDocument(text, options)
  if (document.errors.length) return { ok: false }
  try {
    return { ok: true, value: document.toJS({ maxAliasCount: 100 }) }
  } catch {
    return { ok: false }
  }
}

/**
 * Print `after` (an edited copy of the document parsed from `original` with
 * `options`) changing only the lines the edit touched.
 *
 * The YAML printer normalizes comments, spacing, quotes and flow lists everywhere,
 * so the change is computed between two printouts (before / after the edit) and
 * only the changed lines replace the matching lines of the original text (a
 * line-level three-way merge). Untouched lines, comments and spacing stay exactly
 * as written. The merged text is parsed again with `options` and must mean
 * exactly the edited document; otherwise the full printout is returned.
 */
export function printYamlPreserving(original: string, after: Document, options: YamlParseOptions = YAML_1_1): string {
  const print = printOptions(original)
  const edited = after.toString(print)
  if (!original.trim()) return edited
  const before = parseDocument(original, options)
  if (before.errors.length) return edited
  // The printer always ends with a newline; compare as if the original did too.
  const newline = original.endsWith('\n')
  const O = (newline ? original : `${original}\n`).split('\n')
  const B = before.toString(print).split('\n')
  const T = edited.split('\n')
  // Trailing spaces never survive printing; ignore them when lining up (the original line is kept).
  const toOriginal = commonLines(B.map(line => line.trimEnd()), O.map(line => line.trimEnd()))
  const toEdited = commonLines(B, T)
  if (!toOriginal || !toEdited) return edited
  const inOriginal = new Map(toOriginal)
  // Base lines kept by the edit and also found in the original text.
  const anchors: Array<[number, number, number]> = [[-1, -1, -1]]
  for (const [b, t] of toEdited) {
    const o = inOriginal.get(b)
    if (o !== undefined && o > anchors[anchors.length - 1][1] && t > anchors[anchors.length - 1][2]) anchors.push([b, o, t])
  }
  anchors.push([B.length, O.length, T.length])
  const output: string[] = []
  for (let index = 1; index < anchors.length; index++) {
    const [b1, o1, t1] = anchors[index - 1]
    const [b2, o2, t2] = anchors[index]
    const baseGap = B.slice(b1 + 1, b2)
    const editedGap = T.slice(t1 + 1, t2)
    const originalGap = O.slice(o1 + 1, o2)
    const unchanged = baseGap.length === editedGap.length && baseGap.every((line, position) => line === editedGap[position])
    if (unchanged) output.push(...originalGap)
    else if (baseGap.length === originalGap.length) {
      // The original lines here correspond one to one with the base printout (only
      // spacing or quoting differs), so lines the edit kept are taken from the original
      // and the new ones follow the original's indentation of this section.
      const kept = new Map((commonLines(baseGap, editedGap) ?? []).map(([b, t]) => [t, b]))
      const indent = sectionIndent(baseGap, originalGap)
      output.push(...editedGap.map((line, position) => kept.has(position) ? originalGap[kept.get(position)!] : indent(line)))
    } else output.push(...editedGap)
    if (index < anchors.length - 1) output.push(O[o2])
  }
  const joined = output.join('\n')
  const merged = newline || !joined.endsWith('\n') ? joined : joined.slice(0, -1)
  if (merged === edited) return merged
  // Never trade correctness for formatting. Lines kept verbatim mean to the
  // server what they meant before; the replaced lines are the printer's own.
  const check = values(merged, options)
  let expected: unknown
  try {
    expected = after.toJS({ maxAliasCount: 100 })
  } catch {
    return edited
  }
  return check.ok && sameYamlValue(check.value, expected) ? merged : edited
}
