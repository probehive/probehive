import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'

import { ApiError } from '../api/http'
import {
  cancelMaintenanceWindow,
  createMaintenanceWindow,
  listMaintenanceWindows,
  type MaintenanceWindowResponse,
  type MaintenanceWindowStatus,
} from '../api/maintenance'
import { useTranslation } from '../i18n/context'

const minute = 60_000
const maximumDuration = 30 * 24 * 60 * minute

function defaultBounds(): { startsAt: string; endsAt: string } {
  const startsAt = new Date(Date.now() + 5 * minute)
  startsAt.setUTCSeconds(0, 0)
  const endsAt = new Date(startsAt.getTime() + 60 * minute)
  return {
    startsAt: startsAt.toISOString().slice(0, 16),
    endsAt: endsAt.toISOString().slice(0, 16),
  }
}

function utcInstant(value: string): string | null {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(value)) {
    return null
  }
  const instant = new Date(value + ':00.000Z')
  return Number.isNaN(instant.getTime()) ? null : instant.toISOString()
}

function maintenanceQueryKey(
  organizationId: string,
  projectId: string,
  monitorId: string,
) {
  return ['maintenance-windows', organizationId, projectId, monitorId] as const
}

function statusLabel(
  status: MaintenanceWindowStatus,
  t: ReturnType<typeof useTranslation>['t'],
): string {
  switch (status) {
    case 'upcoming':
      return t('maintenance.status.upcoming')
    case 'active':
      return t('maintenance.status.active')
    case 'ended':
      return t('maintenance.status.ended')
    case 'cancelled':
      return t('maintenance.status.cancelled')
  }
}

export default function MonitorMaintenanceSection({
  organizationId,
  projectId,
  monitorId,
}: {
  organizationId: string
  projectId: string
  monitorId: string
}) {
  const { t, formatDateTime, translateError, translateProblem } = useTranslation()
  const queryClient = useQueryClient()
  const [bounds, setBounds] = useState(defaultBounds)
  const queryKey = maintenanceQueryKey(organizationId, projectId, monitorId)
  const query = useQuery({
    queryKey,
    queryFn: () => listMaintenanceWindows(organizationId, projectId, monitorId),
  })
  const createMutation = useMutation<
    MaintenanceWindowResponse,
    unknown,
    { startsAt: string; endsAt: string }
  >({
    mutationFn: (value) => createMaintenanceWindow(
      organizationId,
      projectId,
      monitorId,
      value.startsAt,
      value.endsAt,
    ),
    onSuccess: (created) => {
      queryClient.setQueryData<MaintenanceWindowResponse[]>(queryKey, (current = []) =>
        [...current, created].sort((left, right) =>
          left.startsAt.localeCompare(right.startsAt) || left.id.localeCompare(right.id),
        ),
      )
      setBounds(defaultBounds())
    },
  })
  const cancelMutation = useMutation<MaintenanceWindowResponse, unknown, string>({
    mutationFn: (id) => cancelMaintenanceWindow(
      organizationId,
      projectId,
      monitorId,
      id,
    ),
    onSuccess: (cancelled) => {
      queryClient.setQueryData<MaintenanceWindowResponse[]>(queryKey, (current = []) =>
        current.map((value) => value.id === cancelled.id ? cancelled : value),
      )
    },
  })

  const startsAt = utcInstant(bounds.startsAt)
  const endsAt = utcInstant(bounds.endsAt)
  const duration = startsAt !== null && endsAt !== null
    ? Date.parse(endsAt) - Date.parse(startsAt)
    : 0
  const canCreate = startsAt !== null && endsAt !== null &&
    duration > 0 && duration <= maximumDuration

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!canCreate) {
      return
    }
    createMutation.mutate({ startsAt, endsAt })
  }

  function changeBound(field: 'startsAt' | 'endsAt', value: string) {
    setBounds((current) => ({ ...current, [field]: value }))
    createMutation.reset()
  }

  const startsAtErrors = createMutation.error instanceof ApiError &&
    createMutation.error.status === 400
    ? (createMutation.error.problem.errors?.startsAt ?? []).map(translateError)
    : []
  const endsAtErrors = createMutation.error instanceof ApiError &&
    createMutation.error.status === 400
    ? (createMutation.error.problem.errors?.endsAt ?? []).map(translateError)
    : []
  const createError = createMutation.error instanceof ApiError
    ? translateProblem(createMutation.error.problem)
    : t('maintenance.create.failed')
  const cancelError = cancelMutation.error instanceof ApiError
    ? translateProblem(cancelMutation.error.problem)
    : t('maintenance.cancel.failed')

  return (
    <section className="maintenance-section" aria-labelledby="maintenance-heading">
      <div className="section-heading">
        <div>
          <h2 id="maintenance-heading">{t('maintenance.heading')}</h2>
          <p className="muted">{t('maintenance.scope')}</p>
        </div>
      </div>

      <form className="monitor-form maintenance-form" onSubmit={submit} aria-label={t('maintenance.create.form')}>
        <div className="form-field">
          <label>
            {t('maintenance.startsAt')}
            <input
              name="startsAt"
              type="datetime-local"
              step="60"
              value={bounds.startsAt}
              onChange={(event) => changeBound('startsAt', event.target.value)}
            />
          </label>
        </div>
        <div className="form-field">
          <label>
            {t('maintenance.endsAt')}
            <input
              name="endsAt"
              type="datetime-local"
              step="60"
              value={bounds.endsAt}
              onChange={(event) => changeBound('endsAt', event.target.value)}
            />
          </label>
        </div>
        <div className="form-actions">
          <button type="submit" disabled={!canCreate || createMutation.isPending}>
            {createMutation.isPending
              ? t('maintenance.create.submitting')
              : t('maintenance.create.submit')}
          </button>
        </div>
        {(startsAtErrors.length > 0 || endsAtErrors.length > 0) && (
          <ul className="field-errors form-field-wide" role="alert">
            {[...startsAtErrors, ...endsAtErrors].map((message) => (
              <li key={message}>{message}</li>
            ))}
          </ul>
        )}
      </form>
      {createMutation.isSuccess && (
        <p className="success" role="status">{t('maintenance.create.done')}</p>
      )}
      {createMutation.isError && startsAtErrors.length === 0 && endsAtErrors.length === 0 && (
        <p className="error" role="alert">{createError}</p>
      )}

      {query.isPending && <p>{t('maintenance.loading')}</p>}
      {query.isError && <p className="error" role="alert">{t('maintenance.loadFailed')}</p>}
      {query.data?.length === 0 && <p className="muted">{t('maintenance.empty')}</p>}
      {query.data && query.data.length > 0 && (
        <div className="table-scroll">
          <table className="maintenance-table">
            <thead>
              <tr>
                <th scope="col">{t('maintenance.status')}</th>
                <th scope="col">{t('maintenance.starts')}</th>
                <th scope="col">{t('maintenance.ends')}</th>
                <th scope="col"><span className="visually-hidden">{t('maintenance.actions')}</span></th>
              </tr>
            </thead>
            <tbody>
              {query.data.map((window) => (
                <tr key={window.id}>
                  <td>
                    <span className="maintenance-status" data-status={window.status}>
                      {statusLabel(window.status, t)}
                    </span>
                  </td>
                  <td>{formatDateTime(window.startsAt)}</td>
                  <td>{formatDateTime(window.endsAt)}</td>
                  <td>
                    {window.status !== 'cancelled' && window.status !== 'ended' && (
                      <button
                        type="button"
                        className="button-secondary maintenance-cancel"
                        disabled={cancelMutation.isPending}
                        onClick={() => cancelMutation.mutate(window.id)}
                      >
                        {cancelMutation.isPending && cancelMutation.variables === window.id
                          ? t('maintenance.cancel.pending')
                          : t('maintenance.cancel.submit')}
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {cancelMutation.isSuccess && (
        <p className="success" role="status">{t('maintenance.cancel.done')}</p>
      )}
      {cancelMutation.isError && (
        <p className="error" role="alert">{cancelError}</p>
      )}
    </section>
  )
}
