import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const tokens = readFileSync(resolve(__dirname, '../src/styles/tokens.css'), 'utf8')

describe('iOS input zoom guard', () => {
  it('keeps text inputs at 16px or more on iOS only, without disabling pinch zoom', () => {
    const block = tokens.slice(tokens.indexOf('@supports (-webkit-touch-callout: none)'))
    expect(block).toContain('font-size: max(16px, 1em)')
    expect(block).toMatch(/input:not\(\[type='checkbox'\]\)/)
    expect(block).toContain('textarea')
    const html = readFileSync(resolve(__dirname, '../index.html'), 'utf8')
    expect(html).not.toMatch(/maximum-scale|user-scalable\s*=\s*no/)
  })
})
