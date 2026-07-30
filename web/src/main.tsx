import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { createBrowserRouter, RouterProvider } from 'react-router'

import App from './App.tsx'
import RequireAuth from './auth/RequireAuth.tsx'
import { TranslationProvider } from './i18n/useTranslation.tsx'
import LoginPage from './routes/LoginPage.tsx'
import MonitorPage from './routes/MonitorPage.tsx'
import OrganizationPage from './routes/OrganizationPage.tsx'
import OrganizationsPage from './routes/OrganizationsPage.tsx'
import SetupPage from './routes/SetupPage.tsx'
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
      {
        element: <RequireAuth />,
        children: [
          { index: true, element: <OrganizationsPage /> },
          { path: 'organizations/:organizationId', element: <OrganizationPage /> },
          {
            path: 'organizations/:organizationId/projects/:projectId/monitors/:monitorId',
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
