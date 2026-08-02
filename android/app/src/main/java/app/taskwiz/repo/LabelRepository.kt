package app.taskwiz.repo

import app.taskwiz.api.TaskWizardApi
import app.taskwiz.auth.AuthManager
import app.taskwiz.data.LocalIdGenerator
import app.taskwiz.data.db.LocalState
import app.taskwiz.data.db.TaskWizardDatabase
import app.taskwiz.data.db.dao.LabelDao
import app.taskwiz.data.db.dao.OutboxDao
import app.taskwiz.data.db.entity.LabelEntity
import app.taskwiz.data.db.entity.OutboxEntity
import app.taskwiz.data.db.entity.OutboxEntityType
import app.taskwiz.data.db.entity.OutboxOpType
import app.taskwiz.data.db.toDomain
import app.taskwiz.data.network.NetworkMonitor
import app.taskwiz.data.sync.SyncCoordinator
import app.taskwiz.model.CreateLabelReq
import app.taskwiz.model.Label
import app.taskwiz.model.UpdateLabelReq
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
class LabelRepository @Inject constructor(
    private val api: TaskWizardApi,
    private val db: TaskWizardDatabase,
    private val labelDao: LabelDao,
    private val outboxDao: OutboxDao,
    private val localIdGenerator: LocalIdGenerator,
    private val networkMonitor: NetworkMonitor,
    private val syncCoordinator: SyncCoordinator,
    private val telemetryManager: TelemetryManager,
    private val authManager: AuthManager,
) {
    private val scope = CoroutineScope(Dispatchers.IO + SupervisorJob())

    val labels: StateFlow<List<Label>> = labelDao.observeLabels()
        .map { rows -> rows.map { it.toDomain() } }
        .stateIn(scope, SharingStarted.Eagerly, emptyList())

    suspend fun refreshLabels(): Result<List<Label>> {
        if (!networkMonitor.isOnline.value || !authManager.isSignedIn()) return Result.success(labels.value)
        return try {
            syncCoordinator.syncOnceBlocking()
            Result.success(labels.value)
        } catch (e: Exception) {
            telemetryManager.logError(TAG, "Failed to refresh labels: ${e.message}", e)
            Result.failure(e)
        }
    }

    suspend fun forceRefreshLabels(): Result<List<Label>> {
        if (!authManager.isSignedIn()) return Result.success(labels.value)
        return syncCoordinator.forceSyncOnceBlocking().map { labels.value }
    }

    suspend fun createLabel(req: CreateLabelReq): Result<Int> {
        return try {
            val localId = UUID.randomUUID().toString()
            val placeholderId = localIdGenerator.nextId()
            db.withTransaction {
                labelDao.upsert(
                    LabelEntity(
                        id = placeholderId,
                        localId = localId,
                        name = req.name,
                        color = req.color,
                        localState = LocalState.PENDING_CREATE,
                    )
                )
                outboxDao.insert(
                    OutboxEntity(
                        entityType = OutboxEntityType.LABEL,
                        opType = OutboxOpType.CREATE,
                        targetLocalId = localId,
                        targetServerId = placeholderId,
                    )
                )
            }
            syncCoordinator.flushPending()
            Result.success(placeholderId)
        } catch (e: Exception) {
            telemetryManager.logError(TAG, "Failed to create label locally: ${e.message}", e)
            Result.failure(e)
        }
    }

    suspend fun updateLabel(req: UpdateLabelReq): Result<Unit> {
        return try {
            db.withTransaction {
                val existing = labelDao.getById(req.id)
                val nextState = when (existing?.localState) {
                    LocalState.PENDING_CREATE -> LocalState.PENDING_CREATE
                    else -> LocalState.PENDING_UPDATE
                }
                labelDao.upsert(
                    (existing ?: LabelEntity(id = req.id, name = req.name)).copy(
                        name = req.name,
                        color = req.color,
                        localState = nextState,
                    )
                )

                if (nextState != LocalState.PENDING_CREATE) {
                    outboxDao.insert(
                        OutboxEntity(
                            entityType = OutboxEntityType.LABEL,
                            opType = OutboxOpType.UPDATE,
                            targetServerId = req.id,
                        )
                    )
                }
            }
            syncCoordinator.flushPending()
            Result.success(Unit)
        } catch (e: Exception) {
            telemetryManager.logError(TAG, "Failed to update label locally: ${e.message}", e)
            Result.failure(e)
        }
    }

    suspend fun deleteLabel(id: Int): Result<Unit> {
        return try {
            var enqueued = false
            db.withTransaction {
                val existing = labelDao.getById(id)
                if (existing?.localState == LocalState.PENDING_CREATE) {
                    existing.localId?.let { outboxDao.deleteByLocalId(OutboxEntityType.LABEL, it) }
                    labelDao.deleteById(id)
                } else {
                    labelDao.upsert(
                        (existing ?: LabelEntity(id = id, name = "")).copy(localState = LocalState.PENDING_DELETE)
                    )
                    outboxDao.insert(
                        OutboxEntity(
                            entityType = OutboxEntityType.LABEL,
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
            telemetryManager.logError(TAG, "Failed to delete label locally: ${e.message}", e)
            Result.failure(e)
        }
    }

    companion object {
        private const val TAG = "LabelRepository"
    }
}
