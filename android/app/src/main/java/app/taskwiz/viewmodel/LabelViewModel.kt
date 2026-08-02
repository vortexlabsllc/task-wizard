package app.taskwiz.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import app.taskwiz.model.*
import app.taskwiz.repo.LabelRepository
import app.taskwiz.telemetry.TelemetryManager
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import javax.inject.Inject

@HiltViewModel
class LabelViewModel @Inject constructor(
    private val labelRepository: LabelRepository,
    private val telemetryManager: TelemetryManager
) : ViewModel() {

    val labels: StateFlow<List<Label>> = labelRepository.labels

    private val _isRefreshing = MutableStateFlow(false)
    val isRefreshing: StateFlow<Boolean> = _isRefreshing

    private val _error = MutableStateFlow<String?>(null)
    val error: StateFlow<String?> = _error

    init {
        refreshLabels()
    }

    fun refreshLabels() {
        viewModelScope.launch {
            _isRefreshing.value = true
            labelRepository.refreshLabels()
                .onFailure {
                    telemetryManager.logError(TAG, "Failed to refresh labels: ${it.message}", it)
                    _error.value = it.message
                }
            _isRefreshing.value = false
        }
    }

    fun forceRefreshLabels() {
        viewModelScope.launch {
            _isRefreshing.value = true
            labelRepository.forceRefreshLabels()
                .onFailure {
                    telemetryManager.logError(TAG, "Failed to force refresh labels: ${it.message}", it)
                    _error.value = it.message
                }
            _isRefreshing.value = false
        }
    }

    fun createLabel(name: String, color: String) {
        viewModelScope.launch {
            labelRepository.createLabel(CreateLabelReq(name, color))
                .onFailure {
                    telemetryManager.logError(TAG, "Failed to create label: ${it.message}", it)
                    _error.value = it.message
                }
        }
    }

    fun updateLabel(id: Int, name: String, color: String) {
        viewModelScope.launch {
            labelRepository.updateLabel(UpdateLabelReq(id, name, color))
                .onFailure {
                    telemetryManager.logError(TAG, "Failed to update label $id: ${it.message}", it)
                    _error.value = it.message
                }
        }
    }

    fun deleteLabel(id: Int) {
        viewModelScope.launch {
            labelRepository.deleteLabel(id)
                .onFailure {
                    telemetryManager.logError(TAG, "Failed to delete label $id: ${it.message}", it)
                    _error.value = it.message
                }
        }
    }

    fun clearError() {
        _error.value = null
    }

    companion object {
        private const val TAG = "LabelViewModel"
    }
}
