import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { createBrowserRouter, RouterProvider } from 'react-router'

import App from './App.tsx'
import RequireAuth from './auth/RequireAuth.tsx'
import CreateOrganizationPage from './routes/CreateOrganizationPage.tsx'
import LoginPage from './routes/LoginPage.tsx'
import OrganizationPage from './routes/OrganizationPage.tsx'
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
          { index: true, element: <CreateOrganizationPage /> },
          { path: 'organizations/:organizationId', element: <OrganizationPage /> },
        ],
      },
    ],
  },
])

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
)
