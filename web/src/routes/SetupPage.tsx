import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { Navigate, useNavigate } from 'react-router'

import { createFirstAdministrator, getSetupStatus, type UserResponse } from '../api/auth'
import { ApiError } from '../api/http'
import { sessionQueryKey } from '../auth/useSession'

function fieldErrors(error: unknown, field: string): string[] {
  if (error instanceof ApiError && error.status === 400) {
    return error.problem.errors?.[field] ?? []
  }
  return []
}

export default function SetupPage() {
  const [email, setEmail] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [password, setPassword] = useState('')
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const setupStatus = useQuery({ queryKey: ['setup-status'], queryFn: getSetupStatus })
  const mutation = useMutation<UserResponse, unknown, void>({
    mutationFn: () => createFirstAdministrator({ email, displayName, password }),
    onSuccess: async () => {
      // Setup signs the administrator in; drop cached state and land on the app.
      await queryClient.invalidateQueries({ queryKey: sessionQueryKey })
      await queryClient.invalidateQueries({ queryKey: ['setup-status'] })
      await navigate('/')
    },
  })

  if (setupStatus.data?.setupComplete && !mutation.isSuccess) {
    return <Navigate to="/login" replace />
  }

  const conflict = mutation.error instanceof ApiError && mutation.error.status === 409

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    mutation.mutate()
  }

  return (
    <section>
      <h1>Set up ProbeHive</h1>
      <p>Create the first administrator account for this installation.</p>
      <form onSubmit={onSubmit} aria-label="Create first administrator">
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
        <ul className="field-errors" role="alert">
          {fieldErrors(mutation.error, 'email').map((message) => (
            <li key={message}>{message}</li>
          ))}
        </ul>
        <label>
          Display name
          <input
            name="displayName"
            value={displayName}
            onChange={(event) => setDisplayName(event.target.value)}
            autoComplete="name"
          />
        </label>
        <ul className="field-errors" role="alert">
          {fieldErrors(mutation.error, 'displayName').map((message) => (
            <li key={message}>{message}</li>
          ))}
        </ul>
        <label>
          Password
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
          {mutation.isPending ? 'Creating…' : 'Create administrator'}
        </button>
      </form>
      {conflict && (
        <p className="error" role="alert">
          Setup is already completed on this installation. <a href="/login">Sign in instead.</a>
        </p>
      )}
    </section>
  )
}
