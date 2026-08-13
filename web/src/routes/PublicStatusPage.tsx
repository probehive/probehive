import { useQuery } from '@tanstack/react-query'
import { useParams } from 'react-router'

import { getPublicStatusPage } from '../api/statusPage'
import { useTranslation } from '../i18n/context'

export default function PublicStatusPage() {
  const { publicationToken = '' } = useParams()
  const { t, formatDateTime } = useTranslation()
  const query = useQuery({
    queryKey: ['public-status-page', publicationToken],
    queryFn: () => getPublicStatusPage(publicationToken),
    enabled: publicationToken.length > 0,
  })

  if (query.isPending) return <p>{t('publicStatus.loading')}</p>
  if (query.isError) return <p className="error" role="alert">{t('publicStatus.notFound')}</p>

  return (
    <article className="public-status-page">
      <header>
        <p className="public-status-brand">ProbeHive</p>
        <h1>{query.data.title}</h1>
        <p className="muted">{t('publicStatus.current')}</p>
      </header>
      <ul className="public-status-components">
        {query.data.components.map((component, position) => (
          <li key={`${position}:${component.label}`} aria-label={component.label}>
            <div>
              <h2>{component.label}</h2>
              <p className="muted">
                {t('publicStatus.updated', { time: formatDateTime(component.updatedAt) })}
              </p>
            </div>
            <div className="public-status-indicators">
              {component.maintenance && (
                <span className="public-maintenance">{t('publicStatus.maintenance')}</span>
              )}
              <span className="public-state" data-state={component.state}>
                {t(`publicStatus.state.${component.state}`)}
              </span>
            </div>
          </li>
        ))}
      </ul>
    </article>
  )
}
