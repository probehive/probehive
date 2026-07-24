import { useMutation } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { Link } from 'react-router'

import { ApiError, createOrganization, type CreateOrganizationOutcome } from '../api/organizations'

function fieldErrors(error: unknown, field: string): string[] {
  if (error instanceof ApiError && error.status === 400) {
    return error.problem.errors?.[field] ?? []
  }
  return []
}

export default function CreateOrganizationPage() {
  const [slug, setSlug] = useState('')
  const [displayName, setDisplayName] = useState('')
  const mutation = useMutation<CreateOrganizationOutcome, unknown, void>({
    mutationFn: () => createOrganization({ slug, displayName }),
  })

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    mutation.mutate()
  }

  const conflict = mutation.error instanceof ApiError && mutation.error.status === 409
  const outcome = mutation.data

  return (
    <section>
      <h1>Create an Organization</h1>
      <form onSubmit={onSubmit} aria-label="Create organization">
        <label>
          Slug
          <input
            name="slug"
            value={slug}
            onChange={(event) => setSlug(event.target.value)}
            placeholder="acme"
            autoComplete="off"
          />
        </label>
        <ul className="field-errors" role="alert">
          {fieldErrors(mutation.error, 'slug').map((message) => (
            <li key={message}>{message}</li>
          ))}
        </ul>
        <label>
          Display name
          <input
            name="displayName"
            value={displayName}
            onChange={(event) => setDisplayName(event.target.value)}
            placeholder="Acme Monitoring"
            autoComplete="off"
          />
        </label>
        <ul className="field-errors" role="alert">
          {fieldErrors(mutation.error, 'displayName').map((message) => (
            <li key={message}>{message}</li>
          ))}
        </ul>
        <button type="submit" disabled={mutation.isPending}>
          {mutation.isPending ? 'Creating…' : 'Create'}
        </button>
      </form>
      {conflict && (
        <p className="error" role="alert">
          That slug is already in use by an Organization with a different display name.
        </p>
      )}
      {outcome && (
        <p className="success" role="status">
          {outcome.created ? 'Organization created.' : 'Organization already existed; returning it.'}{' '}
          <Link to={`/organizations/${outcome.organization.id}`}>View {outcome.organization.displayName}</Link>
        </p>
      )}
    </section>
  )
}
