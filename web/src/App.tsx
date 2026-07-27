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
        <Link to="/" className="app-title">
          {t('app.title')}
        </Link>
        <label className="app-language">
          {t('app.language')}
          <select value={locale} onChange={(event) => setLocale(event.target.value as typeof locale)}>
            {locales.map((available) => (
              <option key={available} value={available}>
                {localeNames[available]}
              </option>
            ))}
          </select>
        </label>
        {session.data && (
          <span className="app-session">
            {session.data.email}{' '}
            <button type="button" onClick={() => signOut.mutate()} disabled={signOut.isPending}>
              {signOut.isPending ? t('app.signingOut') : t('app.signOut')}
            </button>
          </span>
        )}
      </header>
      <main className="app-main">
        <Outlet />
      </main>
    </>
  )
}
