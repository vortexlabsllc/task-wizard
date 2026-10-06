import { acquireAccessToken, isAuthEnabled } from '@/utils/msal'
import { NavigationPaths } from '@/utils/navigation'

const API_URL = import.meta.env.VITE_APP_API_URL

type RequestMethod = 'GET' | 'POST' | 'PUT' | 'DELETE'

type FailureResponse = {
  error: string
}

let redirectingToLogin = false

export const isRedirectingToLogin = () => redirectingToLogin

/**
 * Guards against an endless app -> /login -> app -> /login bounce when the
 * server keeps returning 401 (e.g. a persistently bad token). Once we have
 * bounced to the login page within the last few seconds, further 401s should
 * surface as errors instead of forcing another full-page redirect.
 */
const LOGIN_REDIRECT_COOLDOWN_MS = 10_000
let lastLoginRedirectAt = 0

export const markLoginRedirect = (): void => {
  lastLoginRedirectAt = Date.now()
}

export const isWithinLoginRedirectCooldown = (): boolean =>
  Date.now() - lastLoginRedirectAt < LOGIN_REDIRECT_COOLDOWN_MS

export async function Request<SuccessfulResponse>(
  url: string,
  method: RequestMethod = 'GET',
  body: unknown = {},
  requiresAuth: boolean = true,
): Promise<SuccessfulResponse> {
  const fullURL = `${API_URL}/api/v1${url}`

  const headers: HeadersInit = {
    'Content-Type': 'application/json',
    'Cache-Control': 'no-store',
  }

  if (requiresAuth && isAuthEnabled()) {
    try {
      const token = await acquireAccessToken()
      if (token) {
        headers['Authorization'] = 'Bearer ' + token
      }
    } catch {
      // MSAL token not available; server session cookie will provide authentication
    }
  }

  const options: RequestInit = {
    method,
    headers,
    cache: 'no-store',
    credentials: 'include',
  }

  if (method !== 'GET') {
    options.body = JSON.stringify(body)
  }

  const response: Response = await fetch(fullURL, options)

  if (response.status === 401) {
    if (!redirectingToLogin && !isWithinLoginRedirectCooldown()) {
      redirectingToLogin = true
      markLoginRedirect()
      const returnTo = encodeURIComponent(window.location.pathname)
      window.location.href = `${NavigationPaths.Login}?return_to=${returnTo}`
    }
    throw new Error('Authentication required')
  }

  const HTTP_STATUS_NO_CONTENT = 204
  if (response.status === HTTP_STATUS_NO_CONTENT) {
    return {} as SuccessfulResponse
  }

  const data = await response.json()

  if (!response.ok) {
    throw new Error((data as FailureResponse).error)
  }

  return data as SuccessfulResponse
}
