import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, Outlet, useNavigate } from 'react-router'

import { logout } from './api/auth'
import { sessionQueryKey, useSession } from './auth/useSession'
import { useTranslation } from './i18n/context'
import { locales, localeNames } from './i18n/locale'

export default function App() {
  const session = useSession()
  const { t, locale, setLocale } = useTranslation()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const signOut = useMutation({
    mutationFn: logout,
    onSuccess: async () => {
      queryClient.setQueryData(sessionQueryKey, null)
      await navigate('/login')
    },
  })

  return (
    <>
      <header className="app-header">
        <div className="app-header-inner">
          <Link to="/" className="app-title">
            <img src="/favicon.svg" alt="" aria-hidden="true" />
            <span>{t('app.title')}</span>
          </Link>
          {session.data && (
            <nav className="app-nav" aria-label={t('organizations.heading')}>
              <Link to="/">{t('organizations.heading')}</Link>
            </nav>
          )}
          <div className="app-actions">
            <label className="app-language">
              <span>{t('app.language')}</span>
              <select
                aria-label={t('app.language')}
                value={locale}
                onChange={(event) => setLocale(event.target.value as typeof locale)}
              >
                {locales.map((available) => (
                  <option key={available} value={available}>
                    {localeNames[available]}
                  </option>
                ))}
              </select>
            </label>
            {session.data && (
              <div className="app-session">
                <span className="app-session-email">{session.data.email}</span>
                <button
                  type="button"
                  className="button-secondary button-compact"
                  onClick={() => signOut.mutate()}
                  disabled={signOut.isPending}
                >
                  {signOut.isPending ? t('app.signingOut') : t('app.signOut')}
                </button>
              </div>
            )}
          </div>
        </div>
      </header>
      <main className="app-main">
        <Outlet />
      </main>
    </>
  )
}
