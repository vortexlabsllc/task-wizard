package app.taskwiz.auth

import android.app.Activity
import android.content.Context
import android.content.pm.PackageManager
import android.util.Base64
import app.taskwiz.data.OfflineModeRepository
import app.taskwiz.model.AuthConfig
import com.microsoft.identity.client.AcquireTokenParameters
import com.microsoft.identity.client.AcquireTokenSilentParameters
import com.microsoft.identity.client.AuthenticationCallback
import com.microsoft.identity.client.IAccount
import com.microsoft.identity.client.IAuthenticationResult
import com.microsoft.identity.client.IPublicClientApplication
import com.microsoft.identity.client.ISingleAccountPublicClientApplication
import com.microsoft.identity.client.PublicClientApplication
import com.microsoft.identity.client.SilentAuthenticationCallback
import com.microsoft.identity.client.exception.MsalException
import com.microsoft.identity.client.exception.MsalUiRequiredException
import app.taskwiz.telemetry.TelemetryManager
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.security.MessageDigest
import javax.inject.Inject
import javax.inject.Singleton

@Singleton
class AuthManager @Inject constructor(
    private val telemetryManager: TelemetryManager,
    private val offlineModeRepository: OfflineModeRepository,
) : AuthTokenProvider {

    private var singleAccountApp: ISingleAccountPublicClientApplication? = null

    @Volatile
    private var currentAccount: IAccount? = null

    @Volatile
    private var cachedAccessToken: String? = null

    @Volatile
    private var cachedExpiryTimeMs: Long = 0

    @Volatile
    private var isAccountLoaded = false

    @Volatile
    private var activeAuthConfig: AuthConfig? = null

    private val _sessionExpired = MutableStateFlow(false)
    val sessionExpired: StateFlow<Boolean> = _sessionExpired

    private val userChangeListeners = mutableListOf<UserChangeListener>()

    fun interface UserChangeListener {
        fun onUserChanged(isSignedIn: Boolean)
    }

    fun addUserChangeListener(listener: UserChangeListener) {
        synchronized(userChangeListeners) {
            userChangeListeners.add(listener)
        }
    }

    fun removeUserChangeListener(listener: UserChangeListener) {
        synchronized(userChangeListeners) {
            userChangeListeners.remove(listener)
        }
    }

    private fun notifyUserChanged(isSignedIn: Boolean) {
        val snapshot = synchronized(userChangeListeners) {
            userChangeListeners.toList()
        }
        snapshot.forEach { it.onUserChanged(isSignedIn) }
    }

    fun registerSingleAccountApp(app: ISingleAccountPublicClientApplication) {
        singleAccountApp = app
    }

    fun getSingleAccountApp(): ISingleAccountPublicClientApplication? = singleAccountApp

    fun updateAccount(account: IAccount?) {
        currentAccount = account
        if (account == null) {
            cachedAccessToken = null
            cachedExpiryTimeMs = 0
            _sessionExpired.value = false
        }
        isAccountLoaded = true
        notifyUserChanged(account != null)
    }

    fun isSignedIn(): Boolean = currentAccount != null

    fun getAccountName(): String? = currentAccount?.username

    fun isLoaded(): Boolean = isAccountLoaded

    override fun getCachedAccessToken(): String? {
        val token = cachedAccessToken
        if (token != null && System.currentTimeMillis() < cachedExpiryTimeMs - TOKEN_SKEW_MS) {
            return token
        }
        return null
    }

    override suspend fun getAccessToken(forceRefresh: Boolean): String? {
        if (!forceRefresh) {
            val token = cachedAccessToken
            if (token != null && System.currentTimeMillis() < cachedExpiryTimeMs - TOKEN_SKEW_MS) {
                return token
            }
        }

        val account = currentAccount ?: return null
        val app = singleAccountApp ?: return null

        return try {
            val result = acquireTokenSilent(app, account)
            cacheToken(result)
            result.accessToken
        } catch (e: MsalException) {
            telemetryManager.logWarning(TAG, "Silent token acquire failed: ${e.message}", e)
            if (e is MsalUiRequiredException) {
                cachedAccessToken = null
                cachedExpiryTimeMs = 0
                _sessionExpired.value = true
            }
            null
        }
    }

    private fun acquireTokenSilent(
        app: ISingleAccountPublicClientApplication,
        account: IAccount
    ): IAuthenticationResult {
        val params = AcquireTokenSilentParameters.Builder()
            .forAccount(account)
            .fromAuthority(account.authority)
            .withScopes(getRequiredScopes())
            .build()

        return app.acquireTokenSilent(params)
    }

    fun initializeMsal(context: Context, authConfig: AuthConfig, callback: MsalInitCallback) {
        activeAuthConfig = authConfig
        offlineModeRepository.setCachedAuthConfig(authConfig)

        val configFile = writeMsalConfigFile(context, authConfig)
        PublicClientApplication.createSingleAccountPublicClientApplication(
            context,
            configFile,
            object : IPublicClientApplication.ISingleAccountApplicationCreatedListener {
                override fun onCreated(application: ISingleAccountPublicClientApplication) {
                    registerSingleAccountApp(application)
                    callback.onReady(application)
                }

                override fun onError(exception: MsalException) {
                    telemetryManager.logError(TAG, "Failed to initialize MSAL: ${exception.message}", exception)
                    callback.onError(exception)
                }
            }
        )
    }

    fun tryRestoreMsal(context: Context) {
        val cached = offlineModeRepository.getCachedAuthConfig()
        if (cached == null || !cached.enabled) {
            updateAccount(null)
            return
        }
        activeAuthConfig = cached
        val configFile = writeMsalConfigFile(context, cached)
        PublicClientApplication.createSingleAccountPublicClientApplication(
            context,
            configFile,
            object : IPublicClientApplication.ISingleAccountApplicationCreatedListener {
                override fun onCreated(application: ISingleAccountPublicClientApplication) {
                    registerSingleAccountApp(application)
                    loadCurrentAccount(application)
                }

                override fun onError(exception: MsalException) {
                    telemetryManager.logError(TAG, "Failed to restore MSAL: ${exception.message}", exception)
                    updateAccount(null)
                }
            }
        )
    }

    private fun loadCurrentAccount(app: ISingleAccountPublicClientApplication) {
        app.getCurrentAccountAsync(object : ISingleAccountPublicClientApplication.CurrentAccountCallback {
            override fun onAccountLoaded(activeAccount: IAccount?) {
                updateAccount(activeAccount)
            }

            override fun onAccountChanged(priorAccount: IAccount?, currentAccount: IAccount?) {
                updateAccount(currentAccount)
            }

            override fun onError(exception: MsalException) {
                telemetryManager.logError(TAG, "Failed to load current account", exception)
                isAccountLoaded = true
            }
        })
    }

    private fun writeMsalConfigFile(context: Context, authConfig: AuthConfig): File {
        val config = JSONObject().apply {
            put("client_id", authConfig.clientId)
            put("authorization_user_agent", "DEFAULT")
            put("account_mode", "SINGLE")
            put("redirect_uri", computeRedirectUri(context))
            put("authorities", JSONArray().apply {
                put(JSONObject().apply {
                    put("type", "AAD")
                    put("audience", JSONObject().apply {
                        if (authConfig.tenantId.isNullOrBlank()) {
                            put("type", "AzureADandPersonalMicrosoftAccount")
                        } else {
                            put("type", "AzureADMyOrg")
                            put("tenant_id", authConfig.tenantId)
                        }
                    })
                })
            })
        }

        val file = File(context.filesDir, "msal_config.json")
        file.writeText(config.toString())
        return file
    }

    fun signIn(activity: Activity, callback: AuthenticationCallback) {
        val app = singleAccountApp ?: run {
            telemetryManager.logError(TAG, "Sign-in failed: singleAccountApp not initialized")
            return
        }

        val builder = AcquireTokenParameters.Builder()
            .startAuthorizationFromActivity(activity)
            .withScopes(getRequiredScopes())
            .withCallback(object : AuthenticationCallback {
                override fun onSuccess(authenticationResult: IAuthenticationResult) {
                    cacheToken(authenticationResult)
                    updateAccount(authenticationResult.account)
                    offlineModeRepository.markSignedIn()
                    callback.onSuccess(authenticationResult)
                }

                override fun onError(exception: MsalException) {
                    callback.onError(exception)
                }

                override fun onCancel() {
                    callback.onCancel()
                }
            })

        currentAccount?.let { builder.forAccount(it) }

        app.acquireToken(builder.build())
    }

    fun signOut(callback: ISingleAccountPublicClientApplication.SignOutCallback) {
        val app = singleAccountApp ?: run {
            telemetryManager.logError(TAG, "Sign-out failed: singleAccountApp not initialized")
            return
        }
        app.signOut(object : ISingleAccountPublicClientApplication.SignOutCallback {
            override fun onSignOut() {
                updateAccount(null)
                callback.onSignOut()
            }

            override fun onError(exception: MsalException) {
                callback.onError(exception)
            }
        })
    }

    private fun cacheToken(result: IAuthenticationResult) {
        cachedAccessToken = result.accessToken
        cachedExpiryTimeMs = result.expiresOn.time
        _sessionExpired.value = false
    }

    fun getRequiredScopes(): List<String> {
        val audience = activeAuthConfig?.audience ?: DEFAULT_AUDIENCE
        return listOf(
            "$audience/User.Read",
            "$audience/User.Write",
            "$audience/Labels.Read",
            "$audience/Labels.Write",
            "$audience/Tasks.Read",
            "$audience/Tasks.Write"
        )
    }

    interface MsalInitCallback {
        fun onReady(app: ISingleAccountPublicClientApplication)
        fun onError(exception: Exception)
    }

    fun invalidateMsalApp() {
        singleAccountApp = null
        currentAccount = null
        cachedAccessToken = null
        cachedExpiryTimeMs = 0
        activeAuthConfig = null
        offlineModeRepository.clearCachedAuthConfig()
        isAccountLoaded = true
        notifyUserChanged(false)
    }

    companion object {
        private const val TAG = "AuthManager"
        private const val TOKEN_SKEW_MS = 120_000L
        private const val DEFAULT_AUDIENCE = "api://task-wizard"

        @Suppress("DEPRECATION")
        fun computeRedirectUri(context: Context): String {
            val packageName = context.packageName
            val packageInfo = context.packageManager.getPackageInfo(
                packageName, PackageManager.GET_SIGNATURES
            )
            val signature = packageInfo.signatures!!.first()
            val digest = MessageDigest.getInstance("SHA").digest(signature.toByteArray())
            val hash = Base64.encodeToString(digest, Base64.NO_WRAP)
            return "msauth://$packageName/${java.net.URLEncoder.encode(hash, "UTF-8")}"
        }
    }
}
