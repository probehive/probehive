import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'

import { TranslationContext, isMessageKey, type Translation } from './context'
import type { MessageKey } from './en'
import { localeStorageKey, resolveLocale, translate, type Locale } from './locale'

function readStoredLocale(): string | null {
  try {
    return globalThis.localStorage?.getItem(localeStorageKey) ?? null
  } catch {
    // Storage can be unavailable or blocked; fall back to negotiation.
    return null
  }
}

export function TranslationProvider({ children }: { children: ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(() =>
    resolveLocale(readStoredLocale(), globalThis.navigator?.languages ?? []),
  )

  useEffect(() => {
    document.documentElement.lang = locale
  }, [locale])

  const setLocale = useCallback((next: Locale) => {
    setLocaleState(next)
    try {
      globalThis.localStorage?.setItem(localeStorageKey, next)
    } catch {
      // A rejected write only costs the preference on the next visit.
    }
  }, [])

  const value = useMemo<Translation>(() => {
    const t = (key: MessageKey, values?: Record<string, string | number>) =>
      translate(locale, key, values)
    return {
      locale,
      setLocale,
      t,
      formatDateTime: (input) =>
        new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'medium' }).format(
          typeof input === 'string' ? new Date(input) : input,
        ),
      translateError: (failure) => {
        const key = `error.${failure.code}`
        return isMessageKey(key) ? t(key) : failure.message
      },
      translateProblem: (problem) => {
        const key = `error.${problem.code ?? ''}`
        if (isMessageKey(key)) {
          return t(key)
        }
        return problem.detail ?? problem.title ?? t('error.server.internalError')
      },
    }
  }, [locale, setLocale])

  return <TranslationContext.Provider value={value}>{children}</TranslationContext.Provider>
}
