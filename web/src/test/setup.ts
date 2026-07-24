import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach, beforeEach } from 'vitest'

import { setAntiforgeryForTests } from '../api/http'

beforeEach(() => {
  // Unsafe requests need a token; pre-seed it so tests only mock the calls they assert.
  setAntiforgeryForTests({ headerName: 'X-ProbeHive-Antiforgery', requestToken: 'test-token' })
})

afterEach(() => {
  cleanup()
})
