import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'

import { createOrganization } from '../api/organizations'
import { ApiError } from '../api/http'
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
 * so this is never on the path to a first Monitor.
 */
export default function CreateOrganizationForm() {
  const [displayName, setDisplayName] = useState('')
  const { t } = useTranslation()
  const fieldErrors = useFieldErrors()
  const queryClient = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => createOrganization({ displayName }),
    onSuccess: async () => {
      setDisplayName('')
      await queryClient.invalidateQueries({ queryKey: organizationsQueryKey })
    },
  })

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    mutation.mutate()
  }

  return (
    <section>
      <h2>{t('organization.create.heading')}</h2>
      <form onSubmit={onSubmit} aria-label={t('organization.create.form')}>
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
      {mutation.isSuccess && (
        <p className="success" role="status">
          {t('organization.create.created')}
        </p>
      )}
    </section>
  )
}
