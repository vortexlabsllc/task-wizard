package app.taskwiz.repo

import app.taskwiz.api.TaskWizardApi
import app.taskwiz.auth.AuthManager
import app.taskwiz.data.LocalIdGenerator
import app.taskwiz.data.db.LocalState
import app.taskwiz.data.db.TaskWizardDatabase
import app.taskwiz.data.db.dao.OutboxDao
import app.taskwiz.data.db.dao.TaskDao
import app.taskwiz.data.db.entity.OutboxEntity
import app.taskwiz.data.db.entity.OutboxEntityType
import app.taskwiz.data.db.entity.OutboxOpType
import app.taskwiz.data.db.entity.TaskEntity
import app.taskwiz.data.db.toDomain
import app.taskwiz.data.db.toEntity
import app.taskwiz.data.network.NetworkMonitor
import app.taskwiz.data.sync.SyncCoordinator
import app.taskwiz.model.ActivityEntry
import app.taskwiz.model.CreateTaskReq
import app.taskwiz.model.Task
import app.taskwiz.model.TaskHistory
import app.taskwiz.model.UpdateTaskReq
import app.taskwiz.telemetry.TelemetryManager
import androidx.room.withTransaction
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import java.util.UUID
import javax.inject.Inject
import javax.inject.Singleton

@Singleton
class TaskRepository @Inject constructor(
    private val api: TaskWizardApi,
    private val db: TaskWizardDatabase,
    private val taskDao: TaskDao,
    private val outboxDao: OutboxDao,
    private val localIdGenerator: LocalIdGenerator,
    private val networkMonitor: NetworkMonitor,
    private val syncCoordinator: SyncCoordinator,
    private val telemetryManager: TelemetryManager,
    private val authManager: AuthManager,
) {
    private val scope = CoroutineScope(Dispatchers.IO + SupervisorJob())

    val tasks: StateFlow<List<Task>> = taskDao.observeTasks()
        .map { rows -> rows.map { it.toDomain() } }
        .stateIn(scope, SharingStarted.Eagerly, emptyList())

    suspend fun refreshTasks(): Result<List<Task>> {
        if (!networkMonitor.isOnline.value || !authManager.isSignedIn()) {
            return Result.success(tasks.value)
        }
        return try {
            val ok = syncCoordinator.syncOnceBlocking()
            if (ok) {
                Result.success(tasks.value)
            } else {
                Result.failure(Exception("Sync cycle failed"))
            }
        } catch (e: Exception) {
            telemetryManager.logError(TAG, "Failed to refresh tasks: ${e.message}", e)
            Result.failure(e)
        }
    }

    suspend fun forceRefreshTasks(): Result<List<Task>> {
        if (!authManager.isSignedIn()) return Result.success(tasks.value)
        return syncCoordinator.forceSyncOnceBlocking().map { tasks.value }
    }

    suspend fun getActivity(beforeId: Int = 0, limit: Int = 20): Result<List<ActivityEntry>> {
        if (!authManager.isSignedIn()) {
            return Result.success(emptyList())
        }
        return try {
            val response = api.getActivity(beforeId, limit)
            if (response.isSuccessful) {
                Result.success(response.body()?.activity ?: emptyList())
            } else {
                telemetryManager.logError(TAG, "Failed to fetch activity: ${response.code()}")
                Result.failure(Exception("Failed to fetch activity: ${response.code()}"))
            }
        } catch (e: Exception) {
            telemetryManager.logError(TAG, "Failed to fetch activity: ${e.message}", e)
            Result.failure(e)
        }
    }

    suspend fun revertActivity(taskId: Int, historyId: Int): Result<Unit> {
        if (!authManager.isSignedIn()) {
            return Result.success(Unit)
        }
        return try {
            val response = api.uncompleteTask(taskId, historyId)
            when {
                response.isSuccessful -> {
                    val returnedTask = response.body()?.task
                    if (returnedTask != null) {
                        db.withTransaction {
                            taskDao.upsert(returnedTask.toEntity())
                            taskDao.replaceLabels(taskId, returnedTask.labels.map { it.id })
                        }
                    }
                    Result.success(Unit)
                }
                response.code() == 409 -> Result.failure(RevertConflictException())
                else -> {
                    telemetryManager.logError(TAG, "Failed to revert activity: ${response.code()}")
                    Result.failure(Exception("Failed to revert activity: ${response.code()}"))
                }
            }
        } catch (e: Exception) {
            telemetryManager.logError(TAG, "Failed to revert activity: ${e.message}", e)
            Result.failure(e)
        }
    }

    suspend fun getTask(id: Int): Result<Task> {
        taskDao.getTaskById(id)?.let { return Result.success(it.toDomain()) }
        return try {
            val response = api.getTask(id)
            if (response.isSuccessful) {
                val task = response.body()?.task
                if (task != null) {
                    Result.success(task)
                } else {
                    telemetryManager.logError(TAG, "Failed to fetch task: empty body")
                    Result.failure(Exception("Failed to fetch task: empty body"))
                }
            } else {
                Result.failure(Exception("Failed to fetch task: ${response.code()}"))
            }
        } catch (e: Exception) {
            telemetryManager.logError(TAG, "Failed to fetch task: ${e.message}", e)
            Result.failure(e)
        }
    }

    suspend fun getTaskHistory(id: Int): Result<List<TaskHistory>> {
        if (!authManager.isSignedIn()) {
            return Result.success(emptyList())
        }
        return try {
            val response = api.getTaskHistory(id)
            if (response.isSuccessful) {
                Result.success(response.body()?.history ?: emptyList())
            } else {
                Result.failure(Exception("Failed to fetch task history: ${response.code()}"))
            }
        } catch (e: Exception) {
            telemetryManager.logError(TAG, "Failed to fetch task history: ${e.message}", e)
            Result.failure(e)
        }
    }

    suspend fun createTask(req: CreateTaskReq): Result<Int> {
        return try {
            val localId = UUID.randomUUID().toString()
            val placeholderId = localIdGenerator.nextId()
            val entity = TaskEntity(
                id = placeholderId,
                localId = localId,
                title = req.title,
                nextDueDate = req.nextDueDate,
                endDate = req.endDate,
                isRolling = req.isRolling,
                frequency = req.frequency,
                notification = req.notification,
                localState = LocalState.PENDING_CREATE,
            )
            db.withTransaction {
                taskDao.upsert(entity)
                taskDao.replaceLabels(placeholderId, req.labels)
                outboxDao.insert(
                    OutboxEntity(
                        entityType = OutboxEntityType.TASK,
                        opType = OutboxOpType.CREATE,
                        targetLocalId = localId,
                        targetServerId = placeholderId,
                    )
                )
            }
            syncCoordinator.flushPending()
            Result.success(placeholderId)
        } catch (e: Exception) {
            telemetryManager.logError(TAG, "Failed to create task locally: ${e.message}", e)
            Result.failure(e)
        }
    }

    suspend fun updateTask(req: UpdateTaskReq): Result<Unit> {
        return try {
            db.withTransaction {
                val existing = taskDao.getTaskById(req.id)?.task
                val nextState = when (existing?.localState) {
                    LocalState.PENDING_CREATE -> LocalState.PENDING_CREATE
                    else -> LocalState.PENDING_UPDATE
                }
                val entity = TaskEntity(
                    id = req.id,
                    localId = existing?.localId,
                    title = req.title,
                    nextDueDate = req.nextDueDate,
                    endDate = req.endDate,
                    isRolling = req.isRolling,
                    frequency = req.frequency,
                    notification = req.notification,
                    createdAt = existing?.createdAt,
                    updatedAt = existing?.updatedAt,
                    localState = nextState,
                )
                taskDao.upsert(entity)
                taskDao.replaceLabels(req.id, req.labels)

                if (nextState != LocalState.PENDING_CREATE) {
                    // For PENDING_CREATE, the outbox CREATE row reconstructs its payload from
                    // the latest DB state at send time, so no additional outbox row is needed.
                    outboxDao.insert(
                        OutboxEntity(
                            entityType = OutboxEntityType.TASK,
                            opType = OutboxOpType.UPDATE,
                            targetServerId = req.id,
                        )
                    )
                }
            }
            syncCoordinator.flushPending()
            Result.success(Unit)
        } catch (e: Exception) {
            telemetryManager.logError(TAG, "Failed to update task locally: ${e.message}", e)
            Result.failure(e)
        }
    }

    suspend fun deleteTask(id: Int): Result<Unit> {
        return try {
            var enqueued = false
            db.withTransaction {
                val existing = taskDao.getTaskById(id)?.task
                if (existing?.localState == LocalState.PENDING_CREATE) {
                    // Coalesce: drop the row + any pending ops for this entity, no network op needed.
                    existing.localId?.let { outboxDao.deleteByLocalId(OutboxEntityType.TASK, it) }
                    taskDao.deleteById(id)
                } else {
                    taskDao.setState(id, LocalState.PENDING_DELETE)
                    outboxDao.insert(
                        OutboxEntity(
                            entityType = OutboxEntityType.TASK,
                            opType = OutboxOpType.DELETE,
                            targetServerId = id,
                        )
                    )
                    enqueued = true
                }
            }
            if (enqueued) syncCoordinator.flushPending()
            Result.success(Unit)
        } catch (e: Exception) {
            telemetryManager.logError(TAG, "Failed to delete task locally: ${e.message}", e)
            Result.failure(e)
        }
    }

    suspend fun completeTask(id: Int, endRecurrence: Boolean = false): Result<Unit> =
        enqueueTaskOp(id, OutboxOpType.COMPLETE, endRecurrence.toString(), LocalState.PENDING_COMPLETE)

    suspend fun skipTask(id: Int): Result<Unit> =
        enqueueTaskOp(id, OutboxOpType.SKIP, null, LocalState.PENDING_SKIP)

    suspend fun updateDueDate(id: Int, dueDate: String): Result<Unit> {
        return try {
            db.withTransaction {
                taskDao.updateDueDate(id, dueDate)
                outboxDao.insert(
                    OutboxEntity(
                        entityType = OutboxEntityType.TASK,
                        opType = OutboxOpType.DUE_DATE,
                        targetServerId = id,
                        payloadJson = dueDate,
                    )
                )
            }
            syncCoordinator.flushPending()
            Result.success(Unit)
        } catch (e: Exception) {
            telemetryManager.logError(TAG, "Failed to update due date locally: ${e.message}", e)
            Result.failure(e)
        }
    }

    private suspend fun enqueueTaskOp(id: Int, opType: String, payload: String?, localState: String = LocalState.PENDING_UPDATE): Result<Unit> {
        return try {
            db.withTransaction {
                val existing = taskDao.getTaskById(id)?.task
                val resolvedState = if (existing?.localState == LocalState.PENDING_CREATE && localState == LocalState.PENDING_UPDATE) {
                    LocalState.PENDING_CREATE
                } else {
                    localState
                }
                taskDao.setState(id, resolvedState)
                outboxDao.insert(
                    OutboxEntity(
                        entityType = OutboxEntityType.TASK,
                        opType = opType,
                        targetServerId = id,
                        payloadJson = payload,
                    )
                )
            }
            syncCoordinator.flushPending()
            Result.success(Unit)
        } catch (e: Exception) {
            telemetryManager.logError(TAG, "Failed to enqueue $opType locally: ${e.message}", e)
            Result.failure(e)
        }
    }

    companion object {
        private const val TAG = "TaskRepository"
    }
}

class RevertConflictException : Exception("This action can no longer be reverted")
