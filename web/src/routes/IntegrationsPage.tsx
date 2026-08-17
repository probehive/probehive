import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { Link, useParams } from 'react-router'

import { ApiError, getOrganization } from '../api/organizations'
import {
  activateWebhookSigningSecret,
  createWebhookIntegration,
  listWebhookIntegrations,
  prepareWebhookSigningSecret,
  retireWebhookSigningSecret,
  setWebhookIntegrationEnabled,
  type CreateWebhookIntegrationResponse,
  type WebhookIntegrationResponse,
  webhookIntegrationsQueryKey,
} from '../api/webhooks'
import { useTranslation } from '../i18n/context'
import type { MessageKey } from '../i18n/en'

type IntegrationActionKind = 'enable' | 'disable' | 'prepare' | 'activate' | 'retire'
const confirmationMessageKeys: Record<IntegrationActionKind, MessageKey> = {
  enable: 'integration.confirm.enable',
  disable: 'integration.confirm.disable',
  prepare: 'integration.confirm.prepare',
  activate: 'integration.confirm.activate',
  retire: 'integration.confirm.retire',
}

const confirmationActionKeys: Record<IntegrationActionKind, MessageKey> = {
  enable: 'integration.confirmAction.enable',
  disable: 'integration.confirmAction.disable',
  prepare: 'integration.confirmAction.prepare',
  activate: 'integration.confirmAction.activate',
  retire: 'integration.confirmAction.retire',
}

const successMessageKeys: Record<IntegrationActionKind, MessageKey> = {
  enable: 'integration.success.enable',
  disable: 'integration.success.disable',
  prepare: 'integration.success.prepare',
  activate: 'integration.success.activate',
  retire: 'integration.success.retire',
}


interface IntegrationAction {
  integrationId: string
  kind: IntegrationActionKind
}

interface OneTimeSecret {
  integrationId: string
  integrationName: string
  version: number
  value: string
}

interface RotationActionResult {
  integration: WebhookIntegrationResponse
  secretVersion?: number
  signingSecret?: string
}

interface CreateRequest {
  name: string
  destinationUrl: string
}

function validationMessages(
  error: unknown,
  field: string,
  translateError: ReturnType<typeof useTranslation>['translateError'],
): string[] {
  if (!(error instanceof ApiError) || error.status !== 400) {
    return []
  }
  return (error.problem.errors?.[field] ?? []).map(translateError)
}

function isAction(
  action: IntegrationAction | null,
  integrationId: string,
  kind: IntegrationActionKind,
): boolean {
  return action?.integrationId === integrationId && action.kind === kind
}

export default function IntegrationsPage() {
  const { organizationId } = useParams<'organizationId'>()
  const { t, formatDateTime, translateError, translateProblem } = useTranslation()
  const queryClient = useQueryClient()
  const integrationKey = webhookIntegrationsQueryKey(organizationId ?? '')
  const organizationQuery = useQuery({
    queryKey: ['organizations', organizationId],
    queryFn: () => getOrganization(organizationId ?? ''),
    enabled: organizationId !== undefined,
  })
  const integrationsQuery = useQuery({
    queryKey: integrationKey,
    queryFn: () => listWebhookIntegrations(organizationId ?? ''),
    enabled: organizationId !== undefined,
  })
  const [name, setName] = useState('')
  const [destinationUrl, setDestinationUrl] = useState('')
  const [action, setAction] = useState<IntegrationAction | null>(null)
  const [secret, setSecret] = useState<OneTimeSecret | null>(null)
  const [copied, setCopied] = useState(false)
  const [copyFailed, setCopyFailed] = useState(false)
  const [lastSuccess, setLastSuccess] = useState<{ kind: IntegrationActionKind; name: string } | null>(null)

  const createMutation = useMutation<CreateWebhookIntegrationResponse, unknown, CreateRequest>({
    mutationFn: (request) => createWebhookIntegration(
      organizationId ?? '',
      request.name,
      request.destinationUrl,
    ),
    onSuccess: (result) => {
      setName('')
      setDestinationUrl('')
      setSecret({
        integrationId: result.integration.id,
        integrationName: result.integration.name,
        version: result.integration.activeSecretVersion,
        value: result.signingSecret,
      })
      setCopied(false)
      setCopyFailed(false)
      queryClient.setQueryData<WebhookIntegrationResponse[]>(
        integrationKey,
        (current) => [...(current ?? []), result.integration],
      )
    },
    onSettled: async () => {
      await queryClient.invalidateQueries({ queryKey: integrationKey, exact: true })
    },
  })

  const stateMutation = useMutation<
    WebhookIntegrationResponse,
    unknown,
    { integrationId: string; enabled: boolean; version: number }
  >({
    mutationFn: (request) => setWebhookIntegrationEnabled(
      organizationId ?? '',
      request.integrationId,
      request.enabled,
      request.version,
    ),
    onSuccess: (updated, request) => {
      setAction(null)
      replaceIntegration(updated)
      setLastSuccess({ kind: request.enabled ? 'enable' : 'disable', name: updated.name })
    },
    onSettled: async () => {
      await queryClient.invalidateQueries({ queryKey: integrationKey, exact: true })
    },
  })

  const rotationMutation = useMutation<
    RotationActionResult,
    unknown,
    { integrationId: string; kind: 'prepare' | 'activate' | 'retire'; version: number }
  >({
    mutationFn: async (request) => {
      if (request.kind === 'prepare') {
        const result = await prepareWebhookSigningSecret(
          organizationId ?? '',
          request.integrationId,
          request.version,
        )
        return result
      }
      const operation = request.kind === 'activate'
        ? activateWebhookSigningSecret
        : retireWebhookSigningSecret
      return {
        integration: await operation(
          organizationId ?? '',
          request.integrationId,
          request.version,
        ),
      }
    },
    onSuccess: (result, request) => {
      setAction(null)
      replaceIntegration(result.integration)
      setLastSuccess({ kind: request.kind, name: result.integration.name })
      if (result.signingSecret && result.secretVersion !== undefined) {
        setSecret({
          integrationId: result.integration.id,
          integrationName: result.integration.name,
          version: result.secretVersion,
          value: result.signingSecret,
        })
        setCopied(false)
        setCopyFailed(false)
      }
    },
    onSettled: async () => {
      await queryClient.invalidateQueries({ queryKey: integrationKey, exact: true })
    },
  })

  function replaceIntegration(updated: WebhookIntegrationResponse) {
    queryClient.setQueryData<WebhookIntegrationResponse[]>(
      integrationKey,
      (current) => current?.map((value) => value.id === updated.id ? updated : value),
    )
  }

  function submitCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    createMutation.mutate({ name, destinationUrl })
  }

  function confirm(kind: IntegrationActionKind, integration: WebhookIntegrationResponse) {
    createMutation.reset()
    stateMutation.reset()
    rotationMutation.reset()
    setLastSuccess(null)
    setAction({ integrationId: integration.id, kind })
  }

  function cancelAction() {
    setAction(null)
    stateMutation.reset()
    rotationMutation.reset()
  }

  function runAction(integration: WebhookIntegrationResponse, kind: IntegrationActionKind) {
    if (kind === 'enable' || kind === 'disable') {
      stateMutation.mutate({
        integrationId: integration.id,
        enabled: kind === 'enable',
        version: integration.version,
      })
      return
    }
    rotationMutation.mutate({
      integrationId: integration.id,
      kind,
      version: integration.version,
    })
  }

  async function copySecret() {
    if (secret === null) {
      return
    }
    try {
      await navigator.clipboard.writeText(secret.value)
      setCopied(true)
      setCopyFailed(false)
    } catch {
      setCopied(false)
      setCopyFailed(true)
    }
  }

  function actionPending(integrationId: string, kind: IntegrationActionKind): boolean {
    return (
      isAction(action, integrationId, kind) &&
      (stateMutation.isPending || rotationMutation.isPending)
    )
  }

  function actionError(integrationId: string): string | null {
    const mutation = stateMutation.isError && stateMutation.variables?.integrationId === integrationId
      ? stateMutation
      : rotationMutation.isError && rotationMutation.variables?.integrationId === integrationId
        ? rotationMutation
        : null
    if (mutation === null) {
      return null
    }
    return mutation.error instanceof ApiError
      ? translateProblem(mutation.error.problem)
      : t('integration.action.failed')
  }

  if (organizationId === undefined) {
    return <p className="error" role="alert">{t('organization.notFound')}</p>
  }
  if (organizationQuery.isPending) {
    return <p role="status">{t('integration.loading')}</p>
  }
  if (organizationQuery.isError) {
    const notFound = organizationQuery.error instanceof ApiError && organizationQuery.error.status === 404
    return (
      <p className="error" role="alert">
        {notFound ? t('organization.notFound') : t('organization.loadFailed')}
      </p>
    )
  }

  const organization = organizationQuery.data
  const integrationsForbidden = integrationsQuery.error instanceof ApiError &&
    integrationsQuery.error.status === 403
  const integrationsNotFound = integrationsQuery.error instanceof ApiError &&
    integrationsQuery.error.status === 404

  return (
    <section className="integrations-page">
      <p className="breadcrumb">
        <Link to={'/organizations/' + organization.id}>{t('integration.back')}</Link>
      </p>
      <header className="page-heading integrations-heading">
        <div>
          <p className="eyebrow">{t('integration.eyebrow')}</p>
          <h1>{organization.displayName}</h1>
          <p className="muted">{t('integration.heading')}</p>
        </div>
      </header>
      <nav className="section-nav" aria-label={t('organization.defaultProject')}>
        <Link to={'/organizations/' + organization.id}>{t('integration.organization')}</Link>
        <a href="#integrations">{t('integration.heading')}</a>
      </nav>

      {integrationsQuery.isPending && <p role="status">{t('integration.loading')}</p>}
      {integrationsQuery.isError && (
        <div className="error" role="alert">
          <p>
            {integrationsForbidden
              ? t('integration.administratorOnly')
              : integrationsNotFound
                ? t('organization.notFound')
                : t('integration.loadFailed')}
          </p>
          {!integrationsForbidden && !integrationsNotFound && (
            <button
              type="button"
              className="button-secondary button-compact"
              onClick={() => integrationsQuery.refetch()}
            >
              {t('integration.retry')}
            </button>
          )}
        </div>
      )}

      {integrationsQuery.isSuccess && (
        <>
          {lastSuccess && (
            <p className="success" role="status">
              {t(successMessageKeys[lastSuccess.kind], { name: lastSuccess.name })}
            </p>
          )}
          {secret && (
            <section className="one-time-secret" aria-labelledby="one-time-secret-heading">
              <div>
                <p className="eyebrow">{t('integration.secret.eyebrow')}</p>
                <h2 id="one-time-secret-heading">{t('integration.secret.heading')}</h2>
                <p>
                  {t('integration.secret.once', {
                    name: secret.integrationName,
                    version: secret.version,
                  })}
                </p>
              </div>
              <code className="integration-secret-value" data-testid="one-time-secret">
                {secret.value}
              </code>
              <div className="form-actions">
                <button type="button" onClick={copySecret}>
                  {copied ? t('integration.secret.copied') : t('integration.secret.copy')}
                </button>
                <button
                  type="button"
                  className="button-secondary"
                  onClick={() => {
                    setSecret(null)
                    setCopied(false)
                    setCopyFailed(false)
                  }}
                >
                  {t('integration.secret.dismiss')}
                </button>
              </div>
              {copyFailed && <p className="error" role="alert">{t('integration.secret.copyFailed')}</p>}
            </section>
          )}

          <div className="integration-layout">
            <section className="integration-list-section" id="integrations" aria-labelledby="integrations-heading">
              <div className="section-heading">
                <div>
                  <p className="eyebrow">{organization.displayName}</p>
                  <h2 id="integrations-heading">{t('integration.list.heading')}</h2>
                </div>
                <span className="section-count" aria-label={t('integration.list.count')}>
                  {integrationsQuery.data.length}
                </span>
              </div>
              {integrationsQuery.data.length === 0 && (
                <p className="muted">{t('integration.empty')}</p>
              )}
              {integrationsQuery.data.length > 0 && (
                <div className="table-scroll">
                  <table className="integration-table">
                    <thead>
                      <tr>
                        <th scope="col">{t('integration.name')}</th>
                        <th scope="col">{t('integration.state')}</th>
                        <th scope="col">{t('integration.destination')}</th>
                        <th scope="col">{t('integration.version')}</th>
                        <th scope="col">{t('integration.updated')}</th>
                        <th scope="col">{t('integration.actions')}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {integrationsQuery.data.map((integration) => {
                        const selectedAction = action?.integrationId === integration.id ? action.kind : null
                        const error = actionError(integration.id)
                        const canPrepare = integration.pendingSecretVersion === null &&
                          integration.retiringSecretVersion === null
                        return (
                          <tr key={integration.id}>
                            <th scope="row">
                              <span className="integration-name">{integration.name}</span>
                            </th>
                            <td>
                              <span className="integration-state" data-state={integration.enabled ? 'enabled' : 'disabled'}>
                                {integration.enabled ? t('integration.state.enabled') : t('integration.state.disabled')}
                              </span>
                              {integration.pendingSecretVersion !== null && (
                                <span className="integration-rotation-state">
                                  {t('integration.rotation.pending', { version: integration.pendingSecretVersion })}
                                </span>
                              )}
                              {integration.retiringSecretVersion !== null && (
                                <span className="integration-rotation-state">
                                  {t('integration.rotation.retiring', { version: integration.retiringSecretVersion })}
                                </span>
                              )}
                            </td>
                            <td><code className="integration-destination">{integration.destinationUrl}</code></td>
                            <td>
                              <span>v{integration.version}</span>
                              <span className="integration-secret-version">
                                {t('integration.secret.activeVersion', { version: integration.activeSecretVersion })}
                              </span>
                            </td>
                            <td>{formatDateTime(integration.updatedAt)}</td>
                            <td>
                              <div className="integration-actions">
                                <button
                                  type="button"
                                  className="button-secondary button-compact"
                                  onClick={() => confirm(integration.enabled ? 'disable' : 'enable', integration)}
                                  disabled={stateMutation.isPending || rotationMutation.isPending}
                                >
                                  {integration.enabled ? t('integration.disable') : t('integration.enable')}
                                </button>
                                {canPrepare && (
                                  <button
                                    type="button"
                                    className="button-secondary button-compact"
                                    onClick={() => confirm('prepare', integration)}
                                    disabled={stateMutation.isPending || rotationMutation.isPending}
                                  >
                                    {t('integration.rotation.prepare')}
                                  </button>
                                )}
                                {integration.pendingSecretVersion !== null && (
                                  <button
                                    type="button"
                                    className="button-secondary button-compact"
                                    onClick={() => confirm('activate', integration)}
                                    disabled={stateMutation.isPending || rotationMutation.isPending}
                                  >
                                    {actionPending(integration.id, 'activate')
                                      ? t('integration.rotation.activating')
                                      : t('integration.rotation.activate')}
                                  </button>
                                )}
                                {integration.retiringSecretVersion !== null && (
                                  <button
                                    type="button"
                                    className="button-secondary button-compact"
                                    onClick={() => confirm('retire', integration)}
                                    disabled={stateMutation.isPending || rotationMutation.isPending}
                                  >
                                    {actionPending(integration.id, 'retire')
                                      ? t('integration.rotation.retiringWorking')
                                      : t('integration.rotation.retire')}
                                  </button>
                                )}
                              </div>
                              {selectedAction && (
                                <div className="integration-confirmation" role="group" aria-label={t('integration.confirmation')}>
                                  <p>{t(confirmationMessageKeys[selectedAction], {
                                    name: integration.name,
                                    version: integration.pendingSecretVersion ?? integration.retiringSecretVersion ?? integration.activeSecretVersion,
                                  })}</p>
                                  <div className="form-actions">
                                    <button
                                      type="button"
                                      className="button-secondary button-compact"
                                      onClick={cancelAction}
                                      disabled={stateMutation.isPending || rotationMutation.isPending}
                                    >
                                      {t('integration.cancel')}
                                    </button>
                                    <button
                                      type="button"
                                      className={selectedAction === 'disable' || selectedAction === 'retire' ? 'button-danger button-compact' : 'button-compact'}
                                      onClick={() => runAction(integration, selectedAction)}
                                      disabled={stateMutation.isPending || rotationMutation.isPending}
                                    >
                                      {stateMutation.isPending || rotationMutation.isPending
                                        ? t('integration.action.working')
                                        : t(confirmationActionKeys[selectedAction])}
                                    </button>
                                  </div>
                                  {error && <p className="error" role="alert">{error}</p>}
                                </div>
                              )}
                            </td>
                          </tr>
                        )
                      })}
                    </tbody>
                  </table>
                </div>
              )}
            </section>
            <aside className="integration-create-panel">
              <section aria-labelledby="integration-create-heading">
                <p className="eyebrow">{t('integration.eyebrow')}</p>
                <h2 id="integration-create-heading">{t('integration.create.heading')}</h2>
                <form className="integration-form" onSubmit={submitCreate} aria-label={t('integration.create.form')}>
                  <label>
                    {t('integration.name')}
                    <input
                      name="name"
                      value={name}
                      onChange={(event) => { setName(event.target.value); createMutation.reset() }}
                      maxLength={100}
                      required
                      autoComplete="off"
                    />
                  </label>
                  <ul className="field-errors" role="alert">
                    {validationMessages(createMutation.error, 'name', translateError).map((message) => (
                      <li key={message}>{message}</li>
                    ))}
                  </ul>
                  <label>
                    {t('integration.destination')}
                    <input
                      name="destinationUrl"
                      type="url"
                      value={destinationUrl}
                      onChange={(event) => { setDestinationUrl(event.target.value); createMutation.reset() }}
                      maxLength={2048}
                      placeholder="https://hooks.example.com/events"
                      required
                      autoComplete="url"
                    />
                  </label>
                  <ul className="field-errors" role="alert">
                    {validationMessages(createMutation.error, 'destinationUrl', translateError).map((message) => (
                      <li key={message}>{message}</li>
                    ))}
                  </ul>
                  <button type="submit" disabled={createMutation.isPending}>
                    {createMutation.isPending ? t('integration.create.submitting') : t('integration.create.submit')}
                  </button>
                </form>
                {createMutation.isError &&
                  validationMessages(createMutation.error, 'name', translateError).length === 0 &&
                  validationMessages(createMutation.error, 'destinationUrl', translateError).length === 0 && (
                    <p className="error" role="alert">
                      {createMutation.error instanceof ApiError
                        ? translateProblem(createMutation.error.problem)
                        : t('integration.create.failed')}
                    </p>
                  )}
              </section>
            </aside>
          </div>
        </>
      )}
    </section>
  )
}
