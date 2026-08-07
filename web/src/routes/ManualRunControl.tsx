import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router'

import { ApiError } from '../api/http'
import type { MonitorResponse } from '../api/monitors'
import { triggerManualRun, type RunResponse } from '../api/runs'
import { useTranslation } from '../i18n/context'

export default function ManualRunControl({ monitor }: { monitor: MonitorResponse }) {
  const { t, translateProblem } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const runsKey = [
    'runs',
    monitor.organizationId,
    monitor.projectId,
    monitor.id,
  ] as const
  const mutation = useMutation<RunResponse>({
    mutationFn: () => triggerManualRun(
      monitor.organizationId,
      monitor.projectId,
      monitor.id,
    ),
    onSuccess: async (run) => {
      await queryClient.invalidateQueries({ queryKey: runsKey })
      void navigate(
        '/organizations/' + monitor.organizationId + '/projects/' + monitor.projectId +
        '/monitors/' + monitor.id + '/runs/' + run.id,
      )
    },
  })

  if (monitor.state === 'archived') {
    return null
  }

  const error = mutation.error instanceof ApiError
    ? translateProblem(mutation.error.problem)
    : t('run.manual.failed')

  return (
    <div className="manual-run-control">
      <button
        type="button"
        onClick={() => mutation.mutate()}
        disabled={mutation.isPending}
      >
        {mutation.isPending ? t('run.manual.running') : t('run.manual.action')}
      </button>
      {mutation.isError && (
        <p className="error" role="alert">{error}</p>
      )}
    </div>
  )
}
