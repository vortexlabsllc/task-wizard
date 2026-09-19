package app.taskwiz.data.network

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import app.taskwiz.api.ApiEndpointProvider
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import okhttp3.OkHttpClient
import okhttp3.Request
import java.util.concurrent.CopyOnWriteArrayList
import javax.inject.Inject
import javax.inject.Singleton

@Singleton
class NetworkMonitor @Inject constructor(
    @ApplicationContext context: Context,
    private val httpClient: OkHttpClient,
    private val endpointProvider: ApiEndpointProvider,
) {
    private val cm = context.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager

    // Assume connected unless proven otherwise: either the system reports no network, or the
    // system reports a connected-but-unvalidated network AND our ping probe fails.
    private val _isOnline = MutableStateFlow(true)
    val isOnline: StateFlow<Boolean> = _isOnline.asStateFlow()

    private val listeners = CopyOnWriteArrayList<() -> Unit>()
    private val scope = CoroutineScope(Dispatchers.IO + SupervisorJob())

    init {
        val request = NetworkRequest.Builder()
            .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
            .build()
        cm.registerNetworkCallback(request, object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) = refresh()
            override fun onLost(network: Network) = refresh()
            override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) = refresh()
        })
        refresh()
    }

    fun addOnAvailableListener(block: () -> Unit) {
        listeners.add(block)
    }

    /** Recomputes online state; pings the server only when connected but not validated. */
    private fun refresh() {
        val state = connectivityState()
        if (state == ConnectivityState.CONNECTED) {
            _isOnline.value = true
            notifyIfReconnected()
            return
        }
        if (state == ConnectivityState.DISCONNECTED) {
            _isOnline.value = false
            return
        }
        // CONNECTED_NOT_VALIDATED: assume online until the ping proves otherwise.
        scope.launch {
            val online = pingSucceeds()
            if (_isOnline.value != online) {
                _isOnline.value = online
                notifyIfReconnected()
            }
        }
    }

    private fun notifyIfReconnected() {
        if (_isOnline.value) listeners.forEach { runCatching { it() } }
    }

    private fun connectivityState(): ConnectivityState {
        val active = cm.activeNetwork ?: return ConnectivityState.DISCONNECTED
        val caps = cm.getNetworkCapabilities(active) ?: return ConnectivityState.DISCONNECTED
        if (!caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)) {
            return ConnectivityState.DISCONNECTED
        }
        return if (caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)) {
            ConnectivityState.CONNECTED
        } else {
            ConnectivityState.CONNECTED_NOT_VALIDATED
        }
    }

    private fun pingSucceeds(): Boolean {
        return try {
            val request = Request.Builder().url("${endpointProvider.getBaseUrl()}/ping").build()
            httpClient.newCall(request).execute().use { it.isSuccessful }
        } catch (e: Exception) {
            false
        }
    }

    private enum class ConnectivityState { DISCONNECTED, CONNECTED_NOT_VALIDATED, CONNECTED }
}
