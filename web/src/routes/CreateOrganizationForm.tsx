import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { Link } from 'react-router'

import { ApiError, createOrganization, type CreateOrganizationOutcome } from '../api/organizations'
import { useTranslation } from '../i18n/context'
import { organizationsQueryKey } from './useOrganizations'

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
 * Creates an additional Organization. Setup already provisions the first one
 * (ADR 0018), so this is never on the path to a first Monitor.
 */
export default function CreateOrganizationForm() {
  const [slug, setSlug] = useState('')
  const [displayName, setDisplayName] = useState('')
  const { t } = useTranslation()
  const fieldErrors = useFieldErrors()
  const queryClient = useQueryClient()
  const mutation = useMutation<CreateOrganizationOutcome, unknown, void>({
    mutationFn: () => createOrganization({ slug, displayName }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: organizationsQueryKey })
    },
  })

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    mutation.mutate()
  }

  const conflict = mutation.error instanceof ApiError && mutation.error.status === 409
  const outcome = mutation.data

  return (
    <section>
      <h2>{t('organization.create.heading')}</h2>
      <form onSubmit={onSubmit} aria-label={t('organization.create.form')}>
        <label>
          {t('organization.create.slug')}
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
          {t('organization.create.displayName')}
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
          {mutation.isPending ? t('organization.create.submitting') : t('organization.create.submit')}
        </button>
      </form>
      {conflict && (
        <p className="error" role="alert">
          {t('organization.create.conflict')}
        </p>
      )}
      {outcome && (
        <p className="success" role="status">
          {outcome.created ? t('organization.create.created') : t('organization.create.replayed')}{' '}
          <Link to={`/organizations/${outcome.organization.id}`}>
            {t('organization.create.view', { name: outcome.organization.displayName })}
          </Link>
        </p>
      )}
    </section>
  )
}
