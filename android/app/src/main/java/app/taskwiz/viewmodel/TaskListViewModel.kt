package app.taskwiz.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import app.taskwiz.data.GroupingRepository
import app.taskwiz.data.OfflineModeRepository
import app.taskwiz.data.TaskGroup
import app.taskwiz.data.TaskGrouper
import app.taskwiz.data.TaskGrouping
import app.taskwiz.auth.AuthManager
import app.taskwiz.data.db.dao.OutboxDao
import app.taskwiz.data.network.NetworkMonitor
import app.taskwiz.model.Label
import app.taskwiz.model.Task
import app.taskwiz.repo.LabelRepository
import app.taskwiz.repo.TaskRepository
import app.taskwiz.telemetry.TelemetryManager
import app.taskwiz.utils.SoundEffect
import app.taskwiz.utils.SoundManager
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import javax.inject.Inject

@HiltViewModel
class TaskListViewModel @Inject constructor(
    private val taskRepository: TaskRepository,
    private val labelRepository: LabelRepository,
    private val groupingRepository: GroupingRepository,
    private val offlineModeRepository: OfflineModeRepository,
    private val authManager: AuthManager,
    private val soundManager: SoundManager,
    private val telemetryManager: TelemetryManager,
    private val outboxDao: OutboxDao,
    networkMonitor: NetworkMonitor,
) : ViewModel() {

    val tasks: StateFlow<List<Task>> = taskRepository.tasks

    val isOnline: StateFlow<Boolean> = networkMonitor.isOnline

    val pendingSyncCount: StateFlow<Int> = outboxDao.observeCount()
        .stateIn(viewModelScope, SharingStarted.Eagerly, 0)

    private val _isRefreshing = MutableStateFlow(false)
    val isRefreshing: StateFlow<Boolean> = _isRefreshing

    private val _error = MutableStateFlow<String?>(null)
    val error: StateFlow<String?> = _error

    private val _taskGrouping = MutableStateFlow(groupingRepository.getTaskGrouping())
    val taskGrouping: StateFlow<TaskGrouping> = _taskGrouping

    private val _taskGroups = MutableStateFlow<List<TaskGroup>>(emptyList())
    val taskGroups: StateFlow<List<TaskGroup>> = _taskGroups

    private val _expandedGroups = MutableStateFlow(groupingRepository.getExpandedGroups())
    val expandedGroups: StateFlow<Set<String>> = _expandedGroups

    private val _searchQuery = MutableStateFlow("")
    val searchQuery: StateFlow<String> = _searchQuery

    private val _isSearchActive = MutableStateFlow(false)
    val isSearchActive: StateFlow<Boolean> = _isSearchActive

    private val _showSyncPrompt = MutableStateFlow(false)
    val showSyncPrompt: StateFlow<Boolean> = _showSyncPrompt

    init {
        refreshTasks()
        observeGrouping()
        observeSyncPrompt()
    }

    fun setTaskGrouping(grouping: TaskGrouping) {
        if (_taskGrouping.value == grouping) return
        _taskGrouping.value = grouping
        _expandedGroups.value = emptySet()
        groupingRepository.setExpandedGroups(emptySet())
    }

    fun setSearchQuery(query: String) {
        _searchQuery.value = query
    }

    fun setSearchActive(active: Boolean) {
        _isSearchActive.value = active
        if (!active) {
            _searchQuery.value = ""
        }
    }

    fun toggleGroupExpanded(groupKey: String) {
        val current = _expandedGroups.value.toMutableSet()
        if (current.contains(groupKey)) {
            current.remove(groupKey)
        } else {
            current.add(groupKey)
        }
        _expandedGroups.value = current
        groupingRepository.setExpandedGroups(current)
    }

    private fun observeGrouping() {
        viewModelScope.launch {
            combine(tasks, _taskGrouping, labelRepository.labels, _searchQuery) { tasks, grouping, labels, query ->
                val filtered = if (query.isBlank()) tasks else tasks.filter { it.title.contains(query, ignoreCase = true) }
                computeGroups(filtered, grouping, labels)
            }.flowOn(Dispatchers.Default).collect { groups ->
                _taskGroups.value = groups
            }
        }
    }

    private fun computeGroups(
        tasks: List<Task>,
        grouping: TaskGrouping,
        labels: List<Label>
    ): List<TaskGroup> {
        return when (grouping) {
            TaskGrouping.DUE_DATE -> TaskGrouper.groupByDueDate(tasks)
            TaskGrouping.LABEL -> TaskGrouper.groupByLabel(tasks, labels)
        }
    }

    fun refreshTasks() {
        viewModelScope.launch {
            _isRefreshing.value = true
            taskRepository.refreshTasks().onFailure {
                telemetryManager.logError(TAG, "Failed to refresh tasks: ${it.message}", it)
                _error.value = it.message
            }
            _isRefreshing.value = false
        }
    }

    fun forceRefreshTasks() {
        viewModelScope.launch {
            _isRefreshing.value = true
            taskRepository.forceRefreshTasks().onFailure {
                telemetryManager.logError(TAG, "Failed to force refresh tasks: ${it.message}", it)
                _error.value = it.message
            }
            _isRefreshing.value = false
        }
    }

    fun completeTask(id: Int, endRecurrence: Boolean = false) {
        viewModelScope.launch {
            taskRepository.completeTask(id, endRecurrence)
                .onSuccess {
                    soundManager.playSound(SoundEffect.TASK_COMPLETE)
                }
                .onFailure {
                    telemetryManager.logError(TAG, "Failed to complete task $id: ${it.message}", it)
                    _error.value = it.message
                }
        }
    }

    fun skipTask(id: Int) {
        viewModelScope.launch {
            taskRepository.skipTask(id)
                .onFailure {
                    telemetryManager.logError(TAG, "Failed to skip task $id: ${it.message}", it)
                    _error.value = it.message
                }
        }
    }

    fun deleteTask(id: Int) {
        viewModelScope.launch {
            taskRepository.deleteTask(id)
                .onFailure {
                    telemetryManager.logError(TAG, "Failed to delete task $id: ${it.message}", it)
                    _error.value = it.message
                }
        }
    }

    fun clearError() {
        _error.value = null
    }

    fun dismissSyncPrompt() {
        offlineModeRepository.markSyncPromptShown()
        _showSyncPrompt.value = false
    }

    private fun observeSyncPrompt() {
        viewModelScope.launch {
            tasks.collect { taskList ->
                if (taskList.size >= SYNC_PROMPT_TASK_THRESHOLD
                    && !authManager.isSignedIn()
                    && !offlineModeRepository.isSyncPromptShown()
                ) {
                    _showSyncPrompt.value = true
                }
            }
        }
    }

    override fun onCleared() {
        super.onCleared()
        soundManager.release()
    }

    companion object {
        private const val TAG = "TaskListViewModel"
        private const val SYNC_PROMPT_TASK_THRESHOLD = 5
    }
}
