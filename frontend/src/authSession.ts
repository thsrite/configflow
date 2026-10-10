import type { AxiosInstance } from 'axios'

// Navigation only needs a recent successful check. Protected API requests still
// authenticate on the server and invalidate this hint immediately on a 401.
const AUTH_CACHE_MS = 60_000
let generation = 0
let observedToken: string | null | undefined
let cached: { token: string | null; allowed: boolean; until: number } | undefined
let pending: { token: string | null; generation: number; promise: Promise<boolean> } | undefined

export const invalidateAuthSession = () => {
  generation++
  cached = undefined
  pending = undefined
}

export const getAuthSessionGeneration = () => generation

export const getAuthToken = (): string | null => {
  const token = localStorage.getItem('token')
  if (token !== observedToken) {
    invalidateAuthSession()
    observedToken = token
  }
  return token
}

const cacheDeadline = (token: string | null): number => {
  const deadline = Date.now() + AUTH_CACHE_MS
  if (!token) return deadline
  try {
    const payload = token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/')
    const { exp } = JSON.parse(atob(payload))
    // This is only a cache lifetime hint, never client-side JWT verification.
    if (typeof exp === 'number' && Number.isFinite(exp)) return Math.min(deadline, exp * 1000)
  } catch {
    // Non-JWT or malformed payloads still require server verification.
  }
  return deadline
}

export const checkNavigationAuth = async (client: Pick<AxiosInstance, 'get'>): Promise<boolean> => {
  const token = getAuthToken()
  const currentGeneration = generation
  const isCurrent = () => generation === currentGeneration && localStorage.getItem('token') === token
  if (cached?.token === token && cached.until > Date.now()) return cached.allowed

  let check = pending
  if (!check || check.token !== token || check.generation !== currentGeneration) {
    const promise = (async () => {
      const { data } = await client.get('/auth/status')
      if (!data.authEnabled) return true
      if (!token || !isCurrent()) return false
      await client.get('/auth/verify')
      return true
    })().then(allowed => {
      if (isCurrent()) cached = { token, allowed, until: cacheDeadline(token) }
      return allowed
    })
    check = { token, generation: currentGeneration, promise }
    pending = check
  }

  try {
    const allowed = await check.promise
    if (isCurrent()) return allowed
    // Logout must not be undone by a slow check. A replaced token needs its own
    // check, and an explicitly invalidated result must not repopulate the cache.
    if (token && !getAuthToken()) return false
    return checkNavigationAuth(client)
  } catch (error) {
    if (!isCurrent() && getAuthToken()) return checkNavigationAuth(client)
    throw error
  } finally {
    if (pending === check) pending = undefined
  }
}
