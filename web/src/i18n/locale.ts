import { en, type Catalog, type MessageKey } from './en'
import { zhCN } from './zh-CN'

export const locales = ['en', 'zh-CN'] as const
export type Locale = (typeof locales)[number]

export const defaultLocale: Locale = 'en'

/** Adding a locale means adding a catalog here; no application code changes (ADR 0019). */
export const catalogs: Record<Locale, Catalog> = { en, 'zh-CN': zhCN }

export const localeNames: Record<Locale, string> = { en: 'English', 'zh-CN': '简体中文' }

/** A display preference, not a credential, so browser storage is appropriate. */
export const localeStorageKey = 'probehive.locale'

function match(candidate: string): Locale | null {
  const normalized = candidate.toLowerCase()
  for (const locale of locales) {
    if (locale.toLowerCase() === normalized) {
      return locale
    }
  }
  // Fall back to the language subtag so `zh`, `zh-Hans`, and `zh-SG` reach `zh-CN`.
  const language = normalized.split('-')[0]
  for (const locale of locales) {
    if (locale.toLowerCase().split('-')[0] === language) {
      return locale
    }
  }
  return null
}

/** Explicit preference, then the browser's ordered languages, then the source locale. */
export function resolveLocale(stored: string | null, preferred: readonly string[]): Locale {
  if (stored !== null) {
    const chosen = match(stored)
    if (chosen !== null) {
      return chosen
    }
  }
  for (const candidate of preferred) {
    const chosen = match(candidate)
    if (chosen !== null) {
      return chosen
    }
  }
  return defaultLocale
}

/** Substitutes {name} placeholders. Plural and value formatting use Intl, not the catalog. */
export function format(template: string, values?: Record<string, string | number>): string {
  if (values === undefined) {
    return template
  }
  return template.replace(/\{(\w+)\}/g, (whole, name: string) =>
    Object.hasOwn(values, name) ? String(values[name]) : whole,
  )
}

export function translate(
  locale: Locale,
  key: MessageKey,
  values?: Record<string, string | number>,
): string {
  const template = catalogs[locale][key] ?? en[key]
  return format(template, values)
}
