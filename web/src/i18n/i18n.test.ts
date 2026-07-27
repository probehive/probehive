import { expect, test } from 'vitest'

import { en } from './en'
import { defaultLocale, locales, resolveLocale, translate } from './locale'
import { zhCN } from './zh-CN'

test('every locale defines every source key', () => {
  const sourceKeys = Object.keys(en).sort()
  expect(Object.keys(zhCN).sort()).toEqual(sourceKeys)
  // A locale with an empty string would render as a blank label rather than fall back.
  for (const [key, value] of Object.entries(zhCN)) {
    expect(value, `zh-CN[${key}]`).not.toBe('')
  }
})

test('an explicit preference wins over the browser languages', () => {
  expect(resolveLocale('zh-CN', ['en-GB'])).toBe('zh-CN')
  expect(resolveLocale('en', ['zh-CN'])).toBe('en')
})

test('negotiation falls back through the language subtag then the source locale', () => {
  expect(resolveLocale(null, ['zh-Hans-CN', 'en'])).toBe('zh-CN')
  expect(resolveLocale(null, ['zh'])).toBe('zh-CN')
  expect(resolveLocale(null, ['fr-FR', 'de'])).toBe(defaultLocale)
  expect(resolveLocale('klingon', ['fr'])).toBe(defaultLocale)
})

test('locales are distinct and include the source locale', () => {
  expect(new Set(locales).size).toBe(locales.length)
  expect(locales).toContain(defaultLocale)
})

test('translation substitutes named placeholders', () => {
  expect(translate('en', 'organization.create.view', { name: 'Acme' })).toBe('View Acme')
  expect(translate('zh-CN', 'organization.create.view', { name: 'Acme' })).toBe('查看 Acme')
  // An unsupplied placeholder is left visible rather than rendering "undefined".
  expect(translate('en', 'organization.create.view')).toBe('View {name}')
})
