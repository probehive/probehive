import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { Navigate, useNavigate } from 'react-router'

import { getSetupStatus, login, type SessionResponse } from '../api/auth'
import { ApiError } from '../api/http'
import { sessionQueryKey } from '../auth/useSession'
import { useTranslation } from '../i18n/context'
import type { Translation } from '../i18n/context'

function loginErrorMessage(t: Translation['t'], error: unknown): string {
  if (error instanceof ApiError && error.status === 401) {
    return t('login.invalid')
  }
  if (error instanceof ApiError && error.status === 429) {
    return t('login.rateLimited')
  }
  return t('login.failed')
}

export default function LoginPage() {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const { t } = useTranslation()
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
    <section className="auth-page">
      <h1>{t('login.heading')}</h1>
      <form onSubmit={onSubmit} aria-label={t('login.form')}>
        <label>
          {t('login.email')}
          <input
            name="email"
            type="email"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            autoComplete="username"
          />
        </label>
        <label>
          {t('login.password')}
          <input
            name="password"
            type="password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            autoComplete="current-password"
          />
        </label>
        <button type="submit" disabled={mutation.isPending}>
          {mutation.isPending ? t('login.submitting') : t('login.submit')}
        </button>
      </form>
      {mutation.isError && (
        <p className="error" role="alert">
          {loginErrorMessage(t, mutation.error)}
        </p>
      )}
    </section>
  )
}
