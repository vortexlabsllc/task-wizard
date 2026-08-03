import type { AccountInfo } from '@azure/msal-browser'

const TENANT_ID = 'tenant-123'
const OTHER_TENANT = 'other-tenant'

const makeAccount = (tenantId: string, username: string): AccountInfo =>
  ({
    homeAccountId: `${username}.${tenantId}`,
    environment: 'login.microsoftonline.com',
    tenantId,
    username,
    localAccountId: username,
  }) as AccountInfo

const tenantAccount = makeAccount(TENANT_ID, 'user@example.com')
const otherAccount = makeAccount(OTHER_TENANT, 'other@example.com')
const authResult = {
  accessToken: 'access-token',
  account: tenantAccount,
  expiresOn: new Date(Date.now() + 3_600_000),
}

describe('msal utils', () => {
  let mockPca: {
    handleRedirectPromise: jest.Mock
    getActiveAccount: jest.Mock
    getAllAccounts: jest.Mock
    setActiveAccount: jest.Mock
    acquireTokenSilent: jest.Mock
    loginRedirect: jest.Mock
    logoutRedirect: jest.Mock
  }

  let initializeMsal: () => Promise<void>
  let acquireAccessToken: () => Promise<string>

  beforeEach(async () => {
    jest.resetModules()

    mockPca = {
      handleRedirectPromise: jest.fn().mockResolvedValue(null),
      getActiveAccount: jest.fn().mockReturnValue(null),
      getAllAccounts: jest.fn().mockReturnValue([]),
      setActiveAccount: jest.fn(),
      acquireTokenSilent: jest.fn(),
      loginRedirect: jest.fn(),
      logoutRedirect: jest.fn(),
    }

    jest.doMock('@azure/msal-browser', () => ({
      createStandardPublicClientApplication: jest.fn().mockResolvedValue(mockPca),
    }))

    jest.doMock('@/api/auth', () => ({
      GetAuthConfig: jest.fn().mockResolvedValue({
        enabled: true,
        client_id: 'client-id',
        tenant_id: TENANT_ID,
        audience: 'https://audience',
      }),
    }))

    const module = await import('@/utils/msal')
    initializeMsal = module.initializeMsal
    acquireAccessToken = module.acquireAccessToken
  })

  describe('acquireAccessToken', () => {
    it('returns token when acquireTokenSilent succeeds', async () => {
      await initializeMsal()
      mockPca.getAllAccounts.mockReturnValue([tenantAccount])
      mockPca.acquireTokenSilent.mockResolvedValue(authResult)

      const token = await acquireAccessToken()
      expect(token).toBe('access-token')
    })

    it('selects the tenant-matching account when multiple accounts are cached', async () => {
      await initializeMsal()
      mockPca.getAllAccounts.mockReturnValue([otherAccount, tenantAccount])
      mockPca.acquireTokenSilent.mockResolvedValue(authResult)

      await acquireAccessToken()

      expect(mockPca.setActiveAccount).toHaveBeenCalledWith(tenantAccount)
      expect(mockPca.setActiveAccount).not.toHaveBeenCalledWith(otherAccount)
    })

    it('throws when no tenant account exists', async () => {
      await initializeMsal()
      mockPca.getActiveAccount.mockReturnValue(otherAccount)
      mockPca.getAllAccounts.mockReturnValue([otherAccount])

      await expect(acquireAccessToken()).rejects.toThrow()
      expect(mockPca.setActiveAccount).toHaveBeenCalledWith(null)
    })

    it('uses the selected account for an optional tenant', async () => {
      jest.doMock('@/api/auth', () => ({
        GetAuthConfig: jest.fn().mockResolvedValue({
          enabled: true,
          client_id: 'client-id',
          tenant_id: '',
          audience: 'https://audience',
        }),
      }))
      jest.resetModules()

      const module = await import('@/utils/msal')
      await module.initializeMsal()
      mockPca.getActiveAccount.mockReturnValue(otherAccount)
      mockPca.acquireTokenSilent.mockResolvedValue({
        ...authResult,
        account: otherAccount,
      })

      await expect(module.acquireAccessToken()).resolves.toBe('access-token')
      expect(mockPca.setActiveAccount).not.toHaveBeenCalled()
    })
  })
})
