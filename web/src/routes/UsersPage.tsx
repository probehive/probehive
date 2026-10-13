import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'

import { ApiError, createUser } from '../api/users'
import { useTranslation } from '../i18n/context'
import { usersQueryKey, useUsers } from './useUsers'

function useFieldErrors() {
  const { translateError } = useTranslation()
  return (error: unknown, field: string): string[] => {
    if (error instanceof ApiError && error.status === 400) {
      return (error.problem.errors?.[field] ?? []).map(translateError)
    }
    return []
  }
}

/**
 * Instance user administration. An instance is one team: creating a user makes
 * it an Administrator of every Organization, so teammates share the instance
 * without per-Organization member management.
 */
export default function UsersPage() {
  const [email, setEmail] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [password, setPassword] = useState('')
  const { t, formatDateTime } = useTranslation()
  const fieldErrors = useFieldErrors()
  const queryClient = useQueryClient()
  const users = useUsers()
  const mutation = useMutation({
    mutationFn: () => createUser({ email, displayName, password }),
    onSuccess: async () => {
      setEmail('')
      setDisplayName('')
      setPassword('')
      await queryClient.invalidateQueries({ queryKey: usersQueryKey })
    },
  })

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    mutation.mutate()
  }

  const conflict = mutation.error instanceof ApiError && mutation.error.status === 409

  return (
    <section className="users-page">
      <header className="page-heading">
        <div>
          <p className="eyebrow">{t('app.title')}</p>
          <h1>{t('users.heading')}</h1>
        </div>
      </header>
      <div className="organizations-layout">
        <section className="organization-list-section" aria-labelledby="users-list-heading">
          <div className="section-heading">
            <div>
              <p className="eyebrow">{t('users.heading')}</p>
              <h2 id="users-list-heading">{t('users.available')}</h2>
            </div>
            <span className="section-count" aria-label={t('users.available')}>
              {users.data ? users.data.length : '-'}
            </span>
          </div>
          <p className="muted">{t('users.modelHint')}</p>
          {users.isPending && <p role="status">{t('users.loading')}</p>}
          {users.isError && (
            <p className="error" role="alert">
              {t('users.loadFailed')}
            </p>
          )}
          {users.data && users.data.length === 0 && <p className="muted">{t('users.empty')}</p>}
          {users.data && users.data.length > 0 && (
            <ul className="organization-list">
              {users.data.map((user) => (
                <li key={user.id} className="organization-card">
                  <span className="organization-card-name">{user.displayName}</span>
                  <div className="organization-card-meta">
                    <span>{user.email}</span>
                    <span>{user.role}</span>
                    <span>{formatDateTime(user.createdAt)}</span>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </section>
        <aside className="organization-create-panel">
          <section>
            <h2>{t('users.create.heading')}</h2>
            <form onSubmit={onSubmit} aria-label={t('users.create.form')}>
              <label>
                {t('users.create.email')}
                <input
                  name="email"
                  type="email"
                  value={email}
                  onChange={(event) => setEmail(event.target.value)}
                  autoComplete="off"
                />
              </label>
              <ul className="field-errors" role="alert">
                {fieldErrors(mutation.error, 'email').map((message) => (
                  <li key={message}>{message}</li>
                ))}
              </ul>
              <label>
                {t('users.create.displayName')}
                <input
                  name="displayName"
                  value={displayName}
                  onChange={(event) => setDisplayName(event.target.value)}
                  autoComplete="off"
                />
              </label>
              <ul className="field-errors" role="alert">
                {fieldErrors(mutation.error, 'displayName').map((message) => (
                  <li key={message}>{message}</li>
                ))}
              </ul>
              <label>
                {t('users.create.password')}
                <input
                  name="password"
                  type="password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  autoComplete="new-password"
                />
              </label>
              <ul className="field-errors" role="alert">
                {fieldErrors(mutation.error, 'password').map((message) => (
                  <li key={message}>{message}</li>
                ))}
              </ul>
              <button type="submit" disabled={mutation.isPending}>
                {mutation.isPending ? t('users.create.submitting') : t('users.create.submit')}
              </button>
            </form>
            {conflict && (
              <p className="error" role="alert">
                {t('users.create.conflict')}
              </p>
            )}
            {mutation.isSuccess && (
              <p className="success" role="status">
                {t('users.create.created')}
              </p>
            )}
          </section>
        </aside>
      </div>
    </section>
  )
}
