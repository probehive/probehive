import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { createBrowserRouter, RouterProvider } from 'react-router'

import App from './App.tsx'
import RequireAuth from './auth/RequireAuth.tsx'
import { TranslationProvider } from './i18n/useTranslation.tsx'
import IntegrationsPage from './routes/IntegrationsPage.tsx'
import LoginPage from './routes/LoginPage.tsx'
import MonitorPage from './routes/MonitorPage.tsx'
import OrganizationIncidentsPage from './routes/OrganizationIncidentsPage.tsx'
import OrganizationLayout from './routes/OrganizationLayout.tsx'
import OrganizationMonitorsPage from './routes/OrganizationMonitorsPage.tsx'
import OrganizationOverviewPage from './routes/OrganizationOverviewPage.tsx'
import OrganizationsPage from './routes/OrganizationsPage.tsx'
import OrganizationSettingsPage from './routes/OrganizationSettingsPage.tsx'
import OrganizationStatusPage from './routes/OrganizationStatusPage.tsx'
import PublicStatusPage from './routes/PublicStatusPage.tsx'
import SetupPage from './routes/SetupPage.tsx'
import UsersPage from './routes/UsersPage.tsx'
import './index.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: false,
    },
  },
})

const router = createBrowserRouter([
  {
    path: '/',
    element: <App />,
    children: [
      { path: 'login', element: <LoginPage /> },
      { path: 'setup', element: <SetupPage /> },
      { path: 'status/:publicationToken', element: <PublicStatusPage /> },
      {
        element: <RequireAuth />,
        children: [
          { index: true, element: <OrganizationsPage /> },
          { path: 'users', element: <UsersPage /> },
          {
            path: 'organizations/:organizationId',
            element: <OrganizationLayout />,
            children: [
              { index: true, element: <OrganizationOverviewPage /> },
              { path: 'monitors', element: <OrganizationMonitorsPage /> },
              { path: 'incidents', element: <OrganizationIncidentsPage /> },
              { path: 'integrations', element: <IntegrationsPage /> },
              { path: 'status', element: <OrganizationStatusPage /> },
              { path: 'settings', element: <OrganizationSettingsPage /> },
            ],
          },
          {
            path: 'organizations/:organizationId/projects/:projectId/monitors/:monitorId',
            element: <MonitorPage />,
          },
          {
            path:
              'organizations/:organizationId/projects/:projectId/monitors/:monitorId/incidents/:incidentId',
            element: <MonitorPage />,
          },
          {
            path:
              'organizations/:organizationId/projects/:projectId/monitors/:monitorId/runs/:runId',
            element: <MonitorPage />,
          },
        ],
      },
    ],
  },
])

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <TranslationProvider>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </TranslationProvider>
  </StrictMode>,
)
