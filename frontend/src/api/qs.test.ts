import { describe, expect, it } from 'vitest'

import { qs } from './qs'

describe('qs', () => {
  it('returns an empty string when every value is absent', () => {
    expect(qs({})).toBe('')
    expect(qs({ a: undefined, b: undefined })).toBe('')
  })

  it('drops undefined and nothing else', () => {
    expect(qs({ a: 'x', b: undefined })).toBe('?a=x')
  })

  // ⚠ THE ONE BEHAVIOUR THAT CHANGED. garden dropped '' (and null), electricity
  // dropped every falsy value, chat dropped ''. An empty string is now a real
  // parameter, so a caller that means "absent" must say `undefined`.
  it('keeps an empty string, because absent and empty are different questions', () => {
    expect(qs({ q: '' })).toBe('?q=')
  })

  // electricity's `if (v)` would have dropped both of these. Neither is absent.
  it('keeps a zero and a false', () => {
    expect(qs({ limit: 0 })).toBe('?limit=0')
    expect(qs({ hard: false })).toBe('?hard=false')
  })

  it('expands an array into repeated keys', () => {
    expect(qs({ label: ['a', 'b'] })).toBe('?label=a&label=b')
  })

  it('drops an empty array, which contributes no keys at all', () => {
    expect(qs({ label: [] })).toBe('')
  })

  it('percent-encodes keys and values', () => {
    expect(qs({ q: 'a b&c=d' })).toBe('?q=a+b%26c%3Dd')
    expect(qs({ q: 'jarní řez' })).toBe('?q=jarn%C3%AD+%C5%99ez')
  })

  it('preserves declaration order, so a URL is stable across calls', () => {
    expect(qs({ b: '2', a: '1' })).toBe('?b=2&a=1')
  })
})
