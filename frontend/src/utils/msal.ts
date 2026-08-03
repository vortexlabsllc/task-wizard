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

export const initializeMsal = () => {
  if (!initPromise) {
    initPromise = doInitializeMsal()
  }
  return initPromise
}

const doInitializeMsal = async () => {
  authConfig = await GetAuthConfig()
  if (!authConfig.enabled) return

  const tenantId = authConfig.tenant_id.trim()
  pcaPromise = createStandardPublicClientApplication({
    auth: {
      clientId: authConfig.client_id,
      authority: `https://login.microsoftonline.com/${tenantId || 'common'}`,
      redirectUri: `${window.location.origin}/login`,
    },
    cache: {
      cacheLocation: 'sessionStorage',
    },
  })

  const pca = await pcaPromise

  try {
    const response = await pca.handleRedirectPromise()
    if (response?.account) {
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

const ensureActiveAccount = (pca: IPublicClientApplication): AccountInfo => {
  if (!authConfig) {
    throw new Error('Authentication is not configured')
  }

  const activeAccount = pca.getActiveAccount()
  const tenantId = authConfig.tenant_id.trim()
  if (!tenantId) {
    if (activeAccount) {
      return activeAccount
    }
    const accounts = pca.getAllAccounts()
    if (accounts.length === 1) {
      pca.setActiveAccount(accounts[0])
      return accounts[0]
    }
    throw new Error('No active account selected')
  }

  if (activeAccount?.tenantId === tenantId) {
    return activeAccount
  }

  const tenantAccount = pca.getAllAccounts().find(a => a.tenantId === tenantId)
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

export const loginWithRedirect = async () => {
  if (!authConfig?.enabled || !pcaPromise) return
  const pca = await pcaPromise
  await pca.loginRedirect({ scopes: getScopes(), prompt: 'select_account' })
}

export const acquireAccessToken = async (): Promise<string> => {
  if (!authConfig?.enabled) return ''
  if (!pcaPromise) throw new Error('MSAL not initialized')
  if (cachedAuthResult?.accessToken && cachedAuthResult.expiresOn && cachedAuthResult.expiresOn.getTime() > Date.now()) {
    return cachedAuthResult.accessToken
  }
  const pca = await pcaPromise
  const account = ensureActiveAccount(pca)
  cachedAuthResult = await pca.acquireTokenSilent({ scopes: getScopes(), account })
  return cachedAuthResult.accessToken
}

export const logout = async () => {
  if (!authConfig?.enabled || !pcaPromise) {
    window.location.href = '/'
    return
  }
  const pca = await pcaPromise
  cachedAuthResult = null
  await pca.logoutRedirect({ postLogoutRedirectUri: `${window.location.origin}/login` })
}
