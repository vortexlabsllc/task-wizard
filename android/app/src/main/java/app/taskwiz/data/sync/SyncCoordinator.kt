package app.taskwiz.data.sync

import androidx.room.withTransaction
import app.taskwiz.api.TaskWizardApi
import app.taskwiz.auth.AuthManager
import app.taskwiz.data.db.LocalState
import app.taskwiz.data.db.TaskWizardDatabase
import app.taskwiz.data.db.dao.LabelDao
import app.taskwiz.data.db.dao.OutboxDao
import app.taskwiz.data.db.dao.TaskDao
import app.taskwiz.data.db.entity.LabelEntity
import app.taskwiz.data.db.entity.OutboxEntity
import app.taskwiz.data.db.entity.OutboxEntityType
import app.taskwiz.data.db.entity.OutboxOpType
import app.taskwiz.data.db.entity.TaskEntity
import app.taskwiz.data.db.entity.TaskLabelCrossRef
import app.taskwiz.data.db.toEntity
import app.taskwiz.data.network.NetworkMonitor
import app.taskwiz.model.CreateLabelReq
import app.taskwiz.model.CreateTaskReq
import app.taskwiz.model.UpdateDueDateReq
import app.taskwiz.model.UpdateLabelReq
import app.taskwiz.model.UpdateTaskReq
import app.taskwiz.telemetry.TelemetryManager
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Drains the outbox of pending mutations against the server, remaps local placeholder ids to
 * server-assigned ids, then refreshes tasks/labels caches so the local DB matches the server.
 *
 * Ordering guarantees: processes outbox rows strictly in insertion order (by id). After a
 * successful CREATE, any later outbox rows and DB rows still referencing the local placeholder
 * id are rewritten to the new server id before the next op runs.
 */
@Singleton
class SyncCoordinator @Inject constructor(
    private val api: TaskWizardApi,
    private val db: TaskWizardDatabase,
    private val taskDao: TaskDao,
    private val labelDao: LabelDao,
    private val outboxDao: OutboxDao,
    private val networkMonitor: NetworkMonitor,
    private val telemetryManager: TelemetryManager,
    private val authManager: AuthManager,
) {
    private val scope = CoroutineScope(Dispatchers.IO + SupervisorJob())
    private val mutex = Mutex()

    @Volatile
    private var activeFullJob: Job? = null

    private fun canSync(): Boolean = networkMonitor.isOnline.value && authManager.isSignedIn()

    fun syncOnce() {
        if (!canSync()) return
        if (activeFullJob?.isActive == true) return
        activeFullJob = scope.launch {
            mutex.withLock {
                try {
                    flushOutbox()
                    refreshAll()
                } catch (e: Exception) {
                    telemetryManager.logError(TAG, "Sync cycle failed: ${e.message}", e)
                }
            }
        }
    }

    suspend fun syncOnceBlocking(): Boolean {
        if (!canSync()) return false
        activeFullJob?.takeIf { it.isActive }?.let {
            it.join()
            return true
        }
        var success = false
        val job = scope.launch {
            mutex.withLock {
                try {
                    flushOutbox()
                    refreshAll()
                    success = true
                } catch (e: Exception) {
                    telemetryManager.logError(TAG, "Sync cycle failed: ${e.message}", e)
                }
            }
        }
        activeFullJob = job
        job.join()
        return success
    }

    suspend fun forceSyncOnceBlocking(): Result<Unit> {
        if (!authManager.isSignedIn()) {
            return Result.failure(IllegalStateException("Cannot refresh while signed out"))
        }
        return mutex.withLock {
            try {
                flushOutbox()
                refreshAll(requireSuccessfulFetch = true)
                Result.success(Unit)
            } catch (e: Exception) {
                telemetryManager.logError(TAG, "Forced refresh failed: ${e.message}", e)
                Result.failure(e)
            }
        }
    }

    /**
     * Flush-only path for locally-initiated mutations. No server refresh — the server's
     * WebSocket echo (handled by [WebSocketSyncBridge]) triggers reconciliation. Always
     * launches a coroutine so newly-enqueued rows are never stranded; concurrent flushes
     * serialize through [mutex] and each pass drains the full outbox, so redundant calls
     * collapse into cheap no-ops rather than missed work.
     */
    fun flushPending() {
        if (!canSync()) return
        scope.launch {
            mutex.withLock {
                try {
                    flushOutbox()
                } catch (e: Exception) {
                    telemetryManager.logError(TAG, "Outbox flush failed: ${e.message}", e)
                }
            }
        }
    }

    private suspend fun flushOutbox() {
        while (true) {
            val op = outboxDao.peekNext() ?: return
            val ok = try {
                processOp(op)
            } catch (e: Exception) {
                telemetryManager.logError(TAG, "Outbox op ${op.opType} ${op.entityType} failed: ${e.message}", e)
                outboxDao.update(op.copy(attempts = op.attempts + 1, lastError = e.message))
                return
            }
            if (!ok) {
                // Non-retriable failure already recorded; stop to avoid blocking forever.
                return
            }
        }
    }

    private suspend fun processOp(op: OutboxEntity): Boolean {
        return when (op.entityType) {
            OutboxEntityType.TASK -> processTaskOp(op)
            OutboxEntityType.LABEL -> processLabelOp(op)
            else -> {
                outboxDao.deleteById(op.id)
                true
            }
        }
    }

    private suspend fun processTaskOp(op: OutboxEntity): Boolean {
        when (op.opType) {
            OutboxOpType.CREATE -> {
                val oldId = op.targetServerId ?: return consume(op)
                val current = taskDao.getTaskById(oldId) ?: run {
                    // Row was deleted before sync — drop this op.
                    outboxDao.deleteById(op.id)
                    return true
                }
                if (current.labels.any { it.id < 0 }) {
                    // Referenced label still pending creation; wait for it to sync first.
                    recordFailure(op, "Waiting for pending label creates")
                    return false
                }
                val payload = CreateTaskReq(
                    title = current.task.title,
                    nextDueDate = current.task.nextDueDate,
                    endDate = current.task.endDate,
                    isRolling = current.task.isRolling,
                    frequency = current.task.frequency,
                    notification = current.task.notification,
                    labels = current.labels.map { it.id },
                )
                val response = api.createTask(payload)
                if (!response.isSuccessful) {
                    recordFailure(op, "HTTP ${response.code()}")
                    return false
                }
                val newId = response.body()?.task ?: run {
                    recordFailure(op, "Empty create response")
                    return false
                }
                taskDao.remapId(oldId, newId)
                op.targetLocalId?.let { local ->
                    outboxDao.remapLocalIdToServer(OutboxEntityType.TASK, local, newId)
                }
                outboxDao.remapServerId(OutboxEntityType.TASK, oldId, newId)
                outboxDao.deleteById(op.id)
            }
            OutboxOpType.UPDATE -> {
                val id = op.targetServerId ?: return consume(op)
                if (id < 0) {
                    // Still a placeholder — preceding CREATE must run first; skip this cycle.
                    recordFailure(op, "Awaiting preceding CREATE")
                    return false
                }
                val current = taskDao.getTaskById(id) ?: run {
                    outboxDao.deleteById(op.id)
                    return true
                }
                if (current.labels.any { it.id < 0 }) {
                    recordFailure(op, "Waiting for pending label creates")
                    return false
                }
                val payload = UpdateTaskReq(
                    id = id,
                    title = current.task.title,
                    nextDueDate = current.task.nextDueDate,
                    endDate = current.task.endDate,
                    isRolling = current.task.isRolling,
                    frequency = current.task.frequency,
                    notification = current.task.notification,
                    labels = current.labels.map { it.id },
                )
                val response = api.updateTask(payload)
                if (!response.isSuccessful) {
                    recordFailure(op, "HTTP ${response.code()}")
                    return false
                }
                taskDao.setState(id, LocalState.SYNCED)
                outboxDao.deleteById(op.id)
            }
            OutboxOpType.DELETE -> {
                val id = op.targetServerId ?: return consume(op)
                if (id < 0) return consume(op)
                val response = api.deleteTask(id)
                if (!response.isSuccessful && response.code() != 404) {
                    recordFailure(op, "HTTP ${response.code()}")
                    return false
                }
                taskDao.deleteById(id)
                outboxDao.deleteById(op.id)
            }
            OutboxOpType.COMPLETE -> {
                val id = op.targetServerId ?: return consume(op)
                if (id < 0) {
                    recordFailure(op, "Awaiting preceding CREATE")
                    return false
                }
                val endRecurrence = op.payloadJson?.toBooleanStrictOrNull() ?: false
                val response = api.completeTask(id, endRecurrence)
                if (!response.isSuccessful) {
                    taskDao.setState(id, LocalState.PENDING_UPDATE)
                    recordFailure(op, "HTTP ${response.code()}")
                    return false
                }
                val returnedTask = response.body()?.task
                if (returnedTask?.nextDueDate != null) {
                    taskDao.upsert(returnedTask.toEntity())
                    taskDao.replaceLabels(id, returnedTask.labels.map { it.id })
                } else {
                    taskDao.deleteById(id)
                }
                outboxDao.deleteById(op.id)
            }
            OutboxOpType.SKIP -> {
                val id = op.targetServerId ?: return consume(op)
                if (id < 0) {
                    recordFailure(op, "Awaiting preceding CREATE")
                    return false
                }
                val response = api.skipTask(id)
                if (!response.isSuccessful) {
                    taskDao.setState(id, LocalState.PENDING_UPDATE)
                    recordFailure(op, "HTTP ${response.code()}")
                    return false
                }
                val returnedTask = response.body()?.task
                if (returnedTask?.nextDueDate != null) {
                    taskDao.upsert(returnedTask.toEntity())
                    taskDao.replaceLabels(id, returnedTask.labels.map { it.id })
                } else {
                    taskDao.deleteById(id)
                }
                outboxDao.deleteById(op.id)
            }
            OutboxOpType.DUE_DATE -> {
                val id = op.targetServerId ?: return consume(op)
                if (id < 0) {
                    recordFailure(op, "Awaiting preceding CREATE")
                    return false
                }
                val dueDate = op.payloadJson ?: return consume(op)
                val response = api.updateDueDate(id, UpdateDueDateReq(dueDate))
                if (!response.isSuccessful) {
                    recordFailure(op, "HTTP ${response.code()}")
                    return false
                }
                taskDao.setState(id, LocalState.SYNCED)
                outboxDao.deleteById(op.id)
            }
            else -> return consume(op)
        }
        return true
    }

    private suspend fun processLabelOp(op: OutboxEntity): Boolean {
        when (op.opType) {
            OutboxOpType.CREATE -> {
                val oldId = op.targetServerId ?: return consume(op)
                val current = labelDao.getById(oldId) ?: run {
                    outboxDao.deleteById(op.id)
                    return true
                }
                val payload = CreateLabelReq(current.name, current.color)
                val response = api.createLabel(payload)
                if (!response.isSuccessful) {
                    recordFailure(op, "HTTP ${response.code()}")
                    return false
                }
                val newId = response.body()?.label ?: run {
                    recordFailure(op, "Empty create response")
                    return false
                }
                labelDao.remapId(oldId, newId)
                op.targetLocalId?.let { local ->
                    outboxDao.remapLocalIdToServer(OutboxEntityType.LABEL, local, newId)
                }
                outboxDao.remapServerId(OutboxEntityType.LABEL, oldId, newId)
                outboxDao.deleteById(op.id)
            }
            OutboxOpType.UPDATE -> {
                val id = op.targetServerId ?: return consume(op)
                if (id < 0) {
                    recordFailure(op, "Awaiting preceding CREATE")
                    return false
                }
                val current = labelDao.getById(id) ?: run {
                    outboxDao.deleteById(op.id)
                    return true
                }
                val response = api.updateLabel(UpdateLabelReq(id, current.name, current.color))
                if (!response.isSuccessful) {
                    recordFailure(op, "HTTP ${response.code()}")
                    return false
                }
                labelDao.upsert(current.copy(localState = LocalState.SYNCED))
                outboxDao.deleteById(op.id)
            }
            OutboxOpType.DELETE -> {
                val id = op.targetServerId ?: return consume(op)
                if (id < 0) return consume(op)
                val response = api.deleteLabel(id)
                if (!response.isSuccessful && response.code() != 404) {
                    recordFailure(op, "HTTP ${response.code()}")
                    return false
                }
                labelDao.deleteById(id)
                outboxDao.deleteById(op.id)
            }
            else -> {
                outboxDao.deleteById(op.id)
            }
        }
        return true
    }

    private suspend fun consume(op: OutboxEntity): Boolean {
        outboxDao.deleteById(op.id)
        return true
    }

    private suspend fun recordFailure(op: OutboxEntity, message: String) {
        outboxDao.update(op.copy(attempts = op.attempts + 1, lastError = message))
        telemetryManager.logWarning(TAG, "Outbox op ${op.opType} ${op.entityType} id=${op.id}: $message")
    }

    private suspend fun refreshAll(requireSuccessfulFetch: Boolean = false) {
        val labels = fetchLabelsForRefresh(requireSuccessfulFetch)
        val tasks = fetchTasksForRefresh(requireSuccessfulFetch)
        if (labels == null && tasks == null) return
        db.withTransaction {
            labels?.let { applyLabelsToDb(it) }
            tasks?.let { applyTasksToDb(it) }
        }
    }

    private suspend fun fetchTasksForRefresh(requireSuccessfulFetch: Boolean = false): List<TaskRefreshPayload>? {
        val response = api.getTasks()
        if (!response.isSuccessful) {
            val message = "Refresh tasks failed: HTTP ${response.code()}"
            telemetryManager.logWarning(TAG, message)
            if (requireSuccessfulFetch) throw IllegalStateException(message)
            return null
        }
        return response.body()?.tasks?.map { task ->
            TaskRefreshPayload(
                entity = task.toEntity(localState = LocalState.SYNCED),
                labelIds = task.labels.map { it.id },
            )
        } ?: emptyList()
    }

    private suspend fun applyTasksToDb(tasks: List<TaskRefreshPayload>) {
        val dirtyIds = taskDao.dirtyIds().toSet()
        val serverIds = tasks.map { it.entity.id }.toSet()

        taskDao.allIds()
            .filter { it !in serverIds && it !in dirtyIds }
            .forEach { taskDao.deleteById(it) }

        for (task in tasks) {
            if (task.entity.id in dirtyIds) continue
            taskDao.upsert(task.entity)
            taskDao.replaceLabels(task.entity.id, task.labelIds)
        }
    }

    private suspend fun fetchLabelsForRefresh(requireSuccessfulFetch: Boolean = false): List<LabelEntity>? {
        val response = api.getLabels()
        if (!response.isSuccessful) {
            val message = "Refresh labels failed: HTTP ${response.code()}"
            telemetryManager.logWarning(TAG, message)
            if (requireSuccessfulFetch) throw IllegalStateException(message)
            return null
        }
        return response.body()?.labels?.map { label ->
            LabelEntity(
                id = label.id,
                localId = null,
                name = label.name,
                color = label.color,
                createdAt = label.createdAt,
                updatedAt = label.updatedAt,
                localState = LocalState.SYNCED,
            )
        } ?: emptyList()
    }

    private suspend fun applyLabelsToDb(labels: List<LabelEntity>) {
        val dirtyIds = labelDao.dirtyIds().toSet()
        val serverIds = labels.map { it.id }.toSet()
        labelDao.allIds()
            .filter { it !in serverIds && it !in dirtyIds }
            .forEach { labelDao.deleteById(it) }
        for (label in labels) {
            if (label.id in dirtyIds) continue
            labelDao.upsert(label)
        }
    }

    private data class TaskRefreshPayload(
        val entity: TaskEntity,
        val labelIds: List<Int>,
    )


    companion object {
        private const val TAG = "SyncCoordinator"
    }
}
