import type {
  AccountInfo,
  AuthenticationResult,
  IPublicClientApplication,
} from '@azure/msal-browser'
import { createStandardPublicClientApplication } from '@azure/msal-browser'
import { GetAuthConfig } from '@/api/auth'
import type { AuthConfig } from '@/api/auth'

let authConfig: AuthConfig | null = null
let cachedAuthResult: AuthenticationResult | null = null
let pcaPromise: Promise<IPublicClientApplication> | null = null
let initPromise: Promise<void> | null = null

/**
 * Remember the last account the user signed in with (survives browser
 * restarts). Used as a loginHint so that if a full interactive login is ever
 * needed, Microsoft pre-selects their account instead of showing an empty
 * picker.
 */
const LAST_ACCOUNT_STORAGE_KEY = 'tw.lastAccount'

export const initializeMsal = () => {
  if (!initPromise) {
    initPromise = doInitializeMsal().catch((err) => {
      initPromise = null
      throw err
    })
  }
  return initPromise
}

const doInitializeMsal = async () => {
  authConfig = await GetAuthConfig()
  if (!authConfig.enabled) return

  pcaPromise = createStandardPublicClientApplication({
    auth: {
      clientId: authConfig.client_id,
      authority: `https://login.microsoftonline.com/${authConfig.tenant_id}`,
      redirectUri: `${window.location.origin}/login`,
    },
    cache: {
      // localStorage (not sessionStorage) so the refresh token survives a
      // browser restart and the user is not forced to sign in again.
      cacheLocation: 'localStorage',
    },
  })

  const pca = await pcaPromise

  try {
    const response = await pca.handleRedirectPromise()
    if (response?.account) {
      rememberAccount(response.account)
      pca.setActiveAccount(response.account)
      cachedAuthResult = response
    }
  } catch {
    // Allow the app to continue in unauthenticated state
  }
}

const getScopes = (): string[] => {
  if (!authConfig) return []
  return [
    `${authConfig.audience}/Tasks.Read`,
    `${authConfig.audience}/Tasks.Write`,
  ]
}

const rememberAccount = (account: AccountInfo): void => {
  try {
    localStorage.setItem(LAST_ACCOUNT_STORAGE_KEY, account.username ?? '')
  } catch {
    // localStorage may be unavailable (private mode etc.) — non-fatal.
  }
}

const getLastLoginHint = (): string | undefined => {
  try {
    const hint = localStorage.getItem(LAST_ACCOUNT_STORAGE_KEY)
    return hint && hint.trim().length > 0 ? hint : undefined
  } catch {
    return undefined
  }
}

const ensureActiveAccount = (pca: IPublicClientApplication): AccountInfo => {
  if (!authConfig) {
    throw new Error('Authentication is not configured')
  }

  const activeAccount = pca.getActiveAccount()
  if (activeAccount?.tenantId === authConfig.tenant_id) {
    return activeAccount
  }

  const tenantAccount = pca.getAllAccounts().find(a => a.tenantId === authConfig!.tenant_id)
  if (!tenantAccount) {
    if (activeAccount) {
      pca.setActiveAccount(null)
    }
    throw new Error('No accounts found for configured tenant')
  }
  pca.setActiveAccount(tenantAccount)
  return tenantAccount
}

export const isAuthEnabled = (): boolean => {
  return authConfig?.enabled ?? false
}

/**
 * Start an interactive sign-in.
 *
 * No explicit prompt is used: when the user has a live Microsoft SSO cookie
 * this completes without any visible interaction, and the remembered
 * loginHint pre-selects their account so there is never an empty picker.
 */
export const loginWithRedirect = async () => {
  if (!authConfig?.enabled || !pcaPromise) return
  const pca = await pcaPromise

  // Prefer the cached account so we can resume silently after a full-page
  // redirect instead of forcing a fresh interactive login.
  let loginHint = getLastLoginHint()
  try {
    const account = ensureActiveAccount(pca)
    rememberAccount(account)
    loginHint = account.username || loginHint
  } catch {
    // No cached account — fall back to the remembered hint.
  }

  await pca.loginRedirect({
    scopes: getScopes(),
    ...(loginHint ? { loginHint } : {}),
  })
}

const isTokenValid = (): boolean =>
  !!(cachedAuthResult?.accessToken &&
    cachedAuthResult.expiresOn &&
    cachedAuthResult.expiresOn.getTime() > Date.now())

/**
 * Acquire an access token for the API, preferring the cheapest path:
 *
 *   1. In-memory cached token (same page session)
 *   2. acquireTokenSilent against the cached MSAL account (refresh token in
 *      localStorage — works across browser restarts)
 *   3. ssoSilent (uses the Microsoft SSO cookie — usually completes without
 *      any user interaction)
 *
 * Only if all of these fail does the caller need to show the interactive
 * login screen.
 */
export const acquireAccessToken = async (): Promise<string> => {
  if (!authConfig?.enabled) return ''
  if (isTokenValid()) return cachedAuthResult!.accessToken
  if (!pcaPromise) throw new Error('MSAL not initialized')

  const pca = await pcaPromise

  try {
    const account = ensureActiveAccount(pca)
    rememberAccount(account)
    cachedAuthResult = await pca.acquireTokenSilent({ scopes: getScopes(), account })
  } catch (silentError) {
    console.warn('acquireTokenSilent failed, falling back to ssoSilent:', silentError)
    cachedAuthResult = await pca.ssoSilent({ scopes: getScopes() })
  }

  return cachedAuthResult.accessToken
}

export const logout = async () => {
  if (!authConfig?.enabled || !pcaPromise) {
    window.location.href = '/'
    return
  }
  const pca = await pcaPromise
  cachedAuthResult = null
  try {
    localStorage.removeItem(LAST_ACCOUNT_STORAGE_KEY)
  } catch {
    // non-fatal
  }
  await pca.logoutRedirect({ postLogoutRedirectUri: `${window.location.origin}/login` })
}
