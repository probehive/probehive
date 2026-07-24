import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { Navigate, useNavigate } from 'react-router'

import { getSetupStatus, login, type SessionResponse } from '../api/auth'
import { ApiError } from '../api/http'
import { sessionQueryKey } from '../auth/useSession'

function loginErrorMessage(error: unknown): string {
  if (error instanceof ApiError && error.status === 401) {
    return 'Invalid email or password.'
  }
  if (error instanceof ApiError && error.status === 429) {
    return 'Too many attempts; wait a minute and try again.'
  }
  return 'Signing in failed. Try again.'
}

export default function LoginPage() {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const setupStatus = useQuery({ queryKey: ['setup-status'], queryFn: getSetupStatus })
  const mutation = useMutation<SessionResponse, unknown, void>({
    mutationFn: () => login(email, password),
    onSuccess: async (session) => {
      queryClient.setQueryData(sessionQueryKey, session)
      await navigate('/')
    },
  })

  if (setupStatus.data && !setupStatus.data.setupComplete) {
    return <Navigate to="/setup" replace />
  }

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    mutation.mutate()
  }

  return (
    <section>
      <h1>Sign in</h1>
      <form onSubmit={onSubmit} aria-label="Sign in">
        <label>
          Email
          <input
            name="email"
            type="email"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            autoComplete="username"
          />
        </label>
        <label>
          Password
          <input
            name="password"
            type="password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            autoComplete="current-password"
          />
        </label>
        <button type="submit" disabled={mutation.isPending}>
          {mutation.isPending ? 'Signing in…' : 'Sign in'}
        </button>
      </form>
      {mutation.isError && (
        <p className="error" role="alert">
          {loginErrorMessage(mutation.error)}
        </p>
      )}
    </section>
  )
}
