import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, Outlet, useNavigate } from 'react-router'

import { logout } from './api/auth'
import { sessionQueryKey, useSession } from './auth/useSession'

export default function App() {
  const session = useSession()
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
          ProbeHive
        </Link>
        {session.data && (
          <span className="app-session">
            {session.data.email}{' '}
            <button type="button" onClick={() => signOut.mutate()} disabled={signOut.isPending}>
              {signOut.isPending ? 'Signing out…' : 'Sign out'}
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
