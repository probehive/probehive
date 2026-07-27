import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'

import { ApiError, renameOrganization, type OrganizationResponse } from '../api/organizations'
import { useTranslation } from '../i18n/context'
import { organizationsQueryKey } from './useOrganizations'

/**
 * Changes an Organization's display name. The slug stays fixed because it is the
 * idempotency key for provisioning (ADR 0022).
 */
export default function RenameOrganizationForm({ organization }: { organization: OrganizationResponse }) {
  const [displayName, setDisplayName] = useState(organization.displayName)
  const { t, translateError } = useTranslation()
  const queryClient = useQueryClient()
  const mutation = useMutation<OrganizationResponse, unknown, void>({
    mutationFn: () => renameOrganization(organization.id, displayName),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: organizationsQueryKey })
    },
  })

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    mutation.mutate()
  }

  const fieldErrors =
    mutation.error instanceof ApiError && mutation.error.status === 400
      ? (mutation.error.problem.errors?.displayName ?? []).map(translateError)
      : []

  return (
    <section>
      <h2>{t('organization.rename.heading')}</h2>
      <form onSubmit={onSubmit} aria-label={t('organization.rename.form')}>
        <label>
          {t('organization.rename.displayName')}
          <input
            name="displayName"
            value={displayName}
            onChange={(event) => setDisplayName(event.target.value)}
            autoComplete="off"
          />
        </label>
        <ul className="field-errors" role="alert">
          {fieldErrors.map((message) => (
            <li key={message}>{message}</li>
          ))}
        </ul>
        <button type="submit" disabled={mutation.isPending}>
          {mutation.isPending ? t('organization.rename.submitting') : t('organization.rename.submit')}
        </button>
      </form>
      <p className="hint">{t('organization.rename.slugNote')}</p>
      {mutation.isSuccess && (
        <p className="success" role="status">
          {t('organization.rename.done')}
        </p>
      )}
    </section>
  )
}
