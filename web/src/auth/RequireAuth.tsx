import { Navigate, Outlet } from 'react-router'

import { useTranslation } from '../i18n/context'
import { useSession } from './useSession'

/** Layout route that gates its children behind an authenticated session. */
export default function RequireAuth() {
  const session = useSession()
  const { t } = useTranslation()

  if (session.isPending) {
    return <p role="status">{t('auth.checkingSession')}</p>
  }
  if (!session.data) {
    return <Navigate to="/login" replace />
  }
  return <Outlet />
}
