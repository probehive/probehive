import { createContext, useContext } from 'react'

import type { ProblemDetails, ValidationError } from '../api/http'
import { en, type MessageKey } from './en'
import type { Locale } from './locale'

export interface Translation {
  locale: Locale
  setLocale: (locale: Locale) => void
  t: (key: MessageKey, values?: Record<string, string | number>) => string
  /** Renders an instant in the viewer's time zone. Timestamps stay UTC on the wire. */
  formatDateTime: (value: string | Date) => string
  /** Localizes a coded failure, falling back to the server's English message. */
  translateError: (failure: ValidationError) => string
  /** Localizes a non-validation problem, falling back to its English detail or title. */
  translateProblem: (problem: ProblemDetails) => string
}

export const TranslationContext = createContext<Translation | null>(null)

// Built from the source catalog so an unknown code is detected without a cast.
const catalogKeys: Record<string, true> = Object.fromEntries(
  Object.keys(en).map((key) => [key, true as const]),
)

export function isMessageKey(candidate: string): candidate is MessageKey {
  return Object.hasOwn(catalogKeys, candidate)
}

export function useTranslation(): Translation {
  const value = useContext(TranslationContext)
  if (value === null) {
    // A component rendered outside the provider would silently lose localization.
    throw new Error('useTranslation requires a TranslationProvider ancestor')
  }
  return value
}
