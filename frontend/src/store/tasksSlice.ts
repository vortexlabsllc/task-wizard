import { PayloadAction, createAsyncThunk, createSlice } from '@reduxjs/toolkit'
import {
  GetTasks,
  MarkTaskComplete,
  DeleteTask,
  SkipTask,
  CreateTask,
  SaveTask,
  UpdateDueDate,
} from '@/api/tasks'
import { newTask, Task } from '@/models/task'
import { RootState, store } from './store'
import { SyncState } from '@/models/sync'
import { WSEventPayloads } from '@/models/websocket'
import {
  getDefaultExpandedState,
  GROUP_BY,
  groupTaskBy,
  groupTasksBy,
  sortTasksByDueDate,
  TaskGroups,
} from '@/utils/grouping'
import { retrieveValue, storeValue } from '@/utils/storage'
import WebSocketManager from '@/utils/websocket'

// Timeout before an unacknowledged local write is reverted. If no success or
// failure signal (HTTP response or WS response) arrives within this window we
// assume the write did not land and roll back the optimistic change.
const WRITE_SIGNAL_TIMEOUT_MS = 10_000

type PendingMutation = {
  // Pre-change snapshot of the task so we can restore it on failure/timeout.
  // Null when the task did not exist locally (e.g. a create).
  snapshot: Task | null
  timer: ReturnType<typeof setTimeout>
}

// Tracks in-flight writes keyed by task id. While a task is pending we trust
// our own optimistic change and ignore server-echoed entities (HTTP response
// bodies and WS broadcasts) for that task, which may be stale because reads
// are served from the read replica.
const pendingMutations = new Map<number, PendingMutation>()

function isPending(taskId: number): boolean {
  return pendingMutations.has(taskId)
}

function clearPending(taskId: number) {
  const entry = pendingMutations.get(taskId)
  if (entry) {
    clearTimeout(entry.timer)
    pendingMutations.delete(taskId)
  }
}

// Marks a task as having an in-flight write. The timeout reverts the optimistic
// change if no signal arrives in time. Returns nothing; the caller is expected
// to have already applied the optimistic mutation to redux state.
function markPending(taskId: number, snapshot: Task | null, onTimeout: () => void) {
  // A task can only have one in-flight write at a time; if one is already
  // pending, keep its snapshot (the earliest unacknowledged state) but reset
  // the timer so the newest action gets a full window.
  const existing = pendingMutations.get(taskId)
  if (existing) {
    clearTimeout(existing.timer)
  }
  const timer = setTimeout(() => {
    if (!pendingMutations.has(taskId)) {
      return
    }
    clearPending(taskId)
    onTimeout()
  }, WRITE_SIGNAL_TIMEOUT_MS)
  pendingMutations.set(taskId, { snapshot, timer })
}

export interface TasksState {
  draft: Task
  items: Task[]

  searchQuery: string
  filteredItems: Task[]

  groupBy: GROUP_BY
  groupedItems: TaskGroups<Task>
  expandedGroups: Record<keyof TaskGroups<Task>, boolean>

  status: SyncState
  lastFetched: number | null
  error: string | null
}

const initialGroupBy = retrieveValue<GROUP_BY>('group_by', 'due_date')
const initialExpandedGroups = retrieveValue<Record<keyof TaskGroups<Task>, boolean>>(
  'expanded_groups',
  getDefaultExpandedState(initialGroupBy, []),
)

const initialState: TasksState = {
  draft: newTask(),
  items: [],

  searchQuery: '',
  filteredItems: [],

  groupBy: initialGroupBy,
  expandedGroups: initialExpandedGroups,
  groupedItems: {},

  status: 'loading',
  lastFetched: null,
  error: null,
}

export const fetchTasks = createAsyncThunk('tasks/fetchTasks', async () => {
  const data = await GetTasks()
  return data.tasks
})

export const completeTask = createAsyncThunk(
  'tasks/completeTask',
  async (req: { taskId: number, endRecurrence: boolean }) => {
    const response = await MarkTaskComplete(req.taskId, req.endRecurrence)
    return response.task
  },
)

export const skipTask = createAsyncThunk(
  'tasks/skipTask',
  async (taskId: number) => {
    const response = await SkipTask(taskId)
    return response.task
  },
)

export const deleteTask = createAsyncThunk(
  'tasks/deleteTask',
  async (taskId: number) => await DeleteTask(taskId),
)

export const createTask = createAsyncThunk(
  'tasks/createTask',
  async (task: Omit<Task, 'id'>) => await CreateTask(task),
)

export const saveTask = createAsyncThunk(
  'tasks/saveTask',
  async (task: Task) => await SaveTask(task),
)

export const updateDueDate = createAsyncThunk(
  'tasks/updateDueDate',
  async ({ taskId, dueDate }: { taskId: number; dueDate: string }) => {
    const response = await UpdateDueDate(taskId, dueDate)
    return response.task
  },
)

export const setGroupBy = createAsyncThunk(
  'tasks/setGroupBy',
  async (groupBy: GROUP_BY, thunkAPI) => {
      const state = thunkAPI.getState() as RootState

      const userLabels = state.labels.items
      const tasks = state.tasks.items
      const nextExpanded = getDefaultExpandedState(groupBy, userLabels)
      const groupedItems = groupTasksBy(tasks, userLabels, groupBy)

      storeValue('group_by', groupBy)
      storeValue('expanded_groups', nextExpanded)

      return {
        groupBy,
        groupedItems: groupedItems,
        expandedGroups: nextExpanded,
      }
  },
)

export const initGroups = createAsyncThunk(
  'tasks/initGroups',
  async (_, thunkAPI) => {
    const state = thunkAPI.getState() as RootState
    const userLabels = state.labels.items
    const tasks = state.tasks.items

    return groupTasksBy(tasks, userLabels, initialGroupBy)
  },
)

function taskMatchesQuery(task: Task, query: string): boolean {
  return task.title.toLowerCase().includes(query)
}

function filterItems(items: Task[], query: string): Task[] {
  if (query === '') {
    return items
  }

  const lowerQuery = query.toLowerCase()
  return items.filter(task => taskMatchesQuery(task, lowerQuery))
}

function deleteTaskFromGroups(taskId: number, groups: TaskGroups<Task>) {
  const keys = Object.keys(groups) as (keyof TaskGroups<Task>)[]

  keys.forEach(groupKey => {
    const group = groups[groupKey]
    group.content = group.content.filter(t => t.id !== taskId)
  })
}

// Applies the optimistic local effect of completing or skipping a task.
// Non-recurring tasks (or when ending recurrence) leave the active list
// immediately -- the client knows this outcome and does not need the server to
// confirm it. Recurring tasks keep their current row until the success signal
// supplies the server-computed next due date.
function applyCompleteOrSkipOptimistic(
  state: TasksState,
  taskId: number,
  endRecurrence: boolean,
): boolean {
  const task = state.items.find(t => t.id === taskId)
  if (!task) {
    return false
  }

  const isRecurring = task.frequency.type !== 'once' && !endRecurrence
  if (isRecurring) {
    // Leave the row in place; the success signal will update the due date.
    return true
  }

  tasksSlice.caseReducers.taskRemovedFromActive(state, { payload: taskId, type: 'tasks/taskRemovedFromActive' })
  return true
}

const tasksSlice = createSlice({
  name: 'tasks',
  initialState,
  reducers: {
    setDraft: (state, action: PayloadAction<Task>) => {
      state.draft = action.payload
    },
    filterTasks: (state, action: PayloadAction<string>) => {
      state.searchQuery = action.payload
      state.filteredItems = filterItems(state.items, action.payload)
    },
    toggleGroup: (state, action: PayloadAction<keyof TaskGroups<Task>>) => {
      const groupKey = action.payload
      const isExpanded = state.expandedGroups[groupKey]
      state.expandedGroups[groupKey] = !isExpanded

      storeValue('expanded_groups', state.expandedGroups)
    },
    taskUpserted: (state, action: PayloadAction<Task>) => {
      const task = action.payload

      const index = state.items.findIndex(t => t.id === task.id)
      if (index >= 0) {
        state.items[index] = task
      } else {
        state.items.push(task)
      }

      const filteredIndex = state.filteredItems.findIndex(t => t.id === task.id)
      const matchesQuery = state.searchQuery === '' || taskMatchesQuery(task, state.searchQuery.toLowerCase())

      if (filteredIndex >= 0) {
        if (matchesQuery) {
          state.filteredItems[filteredIndex] = task
        } else {
          state.filteredItems.splice(filteredIndex, 1)
        }
      } else if (matchesQuery) {
        state.filteredItems.push(task)
      }

      deleteTaskFromGroups(task.id, state.groupedItems)
      groupTaskBy(task, state.groupedItems, state.groupBy)
      sortTasksByDueDate(state.items)
      sortTasksByDueDate(state.filteredItems)
    },
    taskDeleted: (state, action: PayloadAction<number>) => {
      const taskId = action.payload
      state.items = state.items.filter(t => t.id !== taskId)
      state.filteredItems = state.filteredItems.filter(t => t.id !== taskId)

      deleteTaskFromGroups(taskId, state.groupedItems)
    },
    taskRemovedFromActive: (state, action: PayloadAction<number>) => {
      const taskId = action.payload
      state.items = state.items.filter(t => t.id !== taskId)
      state.filteredItems = state.filteredItems.filter(t => t.id !== taskId)
      deleteTaskFromGroups(taskId, state.groupedItems)
    },
    // Restores a task to its pre-write snapshot after a failed or timed-out
    // write. If the snapshot is null the task did not exist before (a create),
    // so reverting removes it.
    revertToSnapshot: (state, action: PayloadAction<number>) => {
      const taskId = action.payload
      const entry = pendingMutations.get(taskId)
      if (!entry) {
        return
      }
      const snapshot = entry.snapshot

      if (snapshot === null) {
        tasksSlice.caseReducers.taskDeleted(state, { payload: taskId, type: 'tasks/taskDeleted' })
        return
      }

      // Remove any optimistic change, then restore the exact prior task.
      tasksSlice.caseReducers.taskDeleted(state, { payload: taskId, type: 'tasks/taskDeleted' })
      tasksSlice.caseReducers.taskUpserted(state, { payload: snapshot, type: 'tasks/taskUpserted' })
    },
  },
  extraReducers: builder => {
    builder
      // Fetch tasks
      .addCase(fetchTasks.pending, state => {
        state.status = 'loading'
        state.error = null
      })
      .addCase(fetchTasks.fulfilled, (state, action) => {
        state.status = 'succeeded'

        // A full read comes from the read replica and may not yet reflect an
        // in-flight local write. Keep the optimistic local version for any task
        // that is still pending so a stale snapshot cannot undo it.
        const fetched = action.payload
        const pendingIds = new Set(pendingMutations.keys())
        const localByid = new Map(state.items.map(t => [t.id, t]))
        const merged = fetched.filter(t => !pendingIds.has(t.id))
        for (const id of pendingIds) {
          const local = localByid.get(id)
          // Only keep a local row that still exists (a pending non-recurring
          // completion or delete removes it, so there is nothing to keep).
          if (local) {
            merged.push(local)
          }
        }

        state.items = merged
        state.filteredItems = filterItems(merged, state.searchQuery)
        state.lastFetched = Date.now()
        state.error = null
      })
      .addCase(fetchTasks.rejected, (state, action) => {
        state.status = 'failed'
        state.error = action.error.message ?? null
      })
      // Initialize groups
      .addCase(initGroups.pending, state => {
        state.status = 'loading'
        state.error = null
      })
      .addCase(initGroups.fulfilled, (state, action) => {
        state.groupedItems = action.payload
        state.status = 'succeeded'
        state.error = null
      })
      .addCase(initGroups.rejected, (state, action) => {
        state.status = 'failed'
        state.error = action.error.message ?? null
      })
      // Change group by
      .addCase(setGroupBy.pending, state => {
        state.status = 'loading'
        state.error = null
      })
      .addCase(setGroupBy.fulfilled, (state, action) => {
        state.groupBy = action.payload.groupBy
        state.groupedItems = action.payload.groupedItems
        state.expandedGroups = action.payload.expandedGroups
        state.status = 'succeeded'
        state.error = null
      })
      .addCase(setGroupBy.rejected, (state, action) => {
        state.status = 'failed'
        state.error = action.error.message ?? null
      })
      // Create tasks
      .addCase(createTask.pending, state => {
        state.status = 'loading'
      })
      .addCase(createTask.fulfilled, (state, action) => {
        state.status = 'succeeded'

        const taskId = action.payload.task
        const task: Task = {
          ...action.meta.arg,
          id: taskId,
        }

        tasksSlice.caseReducers.taskUpserted(state, {
          payload: task,
          type: 'tasks/taskUpserted',
        })

        state.draft = newTask()

        state.error = null
      })
      .addCase(createTask.rejected, (state, action) => {
        state.status = 'failed'
        state.error = action.error.message ?? null
      })
      // Edit tasks
      .addCase(saveTask.pending, (state, action) => {
        state.status = 'loading'
        state.error = null

        // Apply the edit optimistically and track it so a failure/timeout can
        // roll it back. The server-echoed entity is not used for the write.
        const task = action.meta.arg
        const snapshot = state.items.find(t => t.id === task.id) ?? null
        tasksSlice.caseReducers.taskUpserted(state, { payload: task, type: 'tasks/taskUpserted' })
        markPending(task.id, snapshot, () => store.dispatch(revertToSnapshot(task.id)))
      })
      .addCase(saveTask.fulfilled, (state, action) => {
        state.status = 'succeeded'
        clearPending(action.meta.arg.id)
        state.draft = newTask()
        state.error = null
      })
      .addCase(saveTask.rejected, (state, action) => {
        state.status = 'failed'
        state.error = action.error.message ?? null
        const taskId = action.meta.arg.id
        clearPending(taskId)
        store.dispatch(revertToSnapshot(taskId))
      })
      // Mark tasks as complete
      .addCase(completeTask.pending, (state, action) => {
        state.status = 'loading'
        state.error = null

        const { taskId, endRecurrence } = action.meta.arg
        const snapshot = state.items.find(t => t.id === taskId) ?? null
        applyCompleteOrSkipOptimistic(state, taskId, endRecurrence)
        markPending(taskId, snapshot, () => store.dispatch(revertToSnapshot(taskId)))
      })
      .addCase(completeTask.fulfilled, (state, action) => {
        const { taskId } = action.meta.arg
        const echoed = action.payload

        // Only reconcile recurring reschedules from the (now authoritative,
        // in-memory) server value. Non-recurring completions were already
        // removed optimistically and must not be re-added by the echo.
        if (echoed.next_due_date) {
          tasksSlice.caseReducers.taskUpserted(state, { payload: echoed, type: 'tasks/taskUpserted' })
        }

        clearPending(taskId)
        state.status = 'succeeded'
        state.error = null
      })
      .addCase(completeTask.rejected, (state, action) => {
        state.status = 'failed'
        state.error = action.error.message ?? null
        const taskId = action.meta.arg.taskId
        clearPending(taskId)
        store.dispatch(revertToSnapshot(taskId))
      })
      // Skip tasks
      .addCase(skipTask.pending, (state, action) => {
        state.status = 'loading'
        state.error = null

        const taskId = action.meta.arg
        const snapshot = state.items.find(t => t.id === taskId) ?? null
        applyCompleteOrSkipOptimistic(state, taskId, false)
        markPending(taskId, snapshot, () => store.dispatch(revertToSnapshot(taskId)))
      })
      .addCase(skipTask.fulfilled, (state, action) => {
        const taskId = action.meta.arg
        const echoed = action.payload

        // A skip always reschedules; apply the server-computed next due date.
        if (echoed.next_due_date) {
          tasksSlice.caseReducers.taskUpserted(state, { payload: echoed, type: 'tasks/taskUpserted' })
        }

        clearPending(taskId)
        state.status = 'succeeded'
        state.error = null
      })
      .addCase(skipTask.rejected, (state, action) => {
        state.status = 'failed'
        state.error = action.error.message ?? null
        const taskId = action.meta.arg
        clearPending(taskId)
        store.dispatch(revertToSnapshot(taskId))
      })
      // Update due date
      .addCase(updateDueDate.pending, (state, action) => {
        state.status = 'loading'
        state.error = null

        const { taskId, dueDate } = action.meta.arg
        const existing = state.items.find(t => t.id === taskId)
        if (!existing) {
          return
        }

        const snapshot = existing
        const optimistic: Task = { ...existing, next_due_date: dueDate }
        tasksSlice.caseReducers.taskUpserted(state, { payload: optimistic, type: 'tasks/taskUpserted' })
        markPending(taskId, snapshot, () => store.dispatch(revertToSnapshot(taskId)))
      })
      .addCase(updateDueDate.fulfilled, (state, action) => {
        const { taskId } = action.meta.arg
        // The optimistic value already matches the requested due date; just
        // acknowledge. Do not overwrite with the (possibly stale) echo.
        clearPending(taskId)
        state.status = 'succeeded'
        state.error = null
      })
      .addCase(updateDueDate.rejected, (state, action) => {
        state.status = 'failed'
        state.error = action.error.message ?? null
        const taskId = action.meta.arg.taskId
        clearPending(taskId)
        store.dispatch(revertToSnapshot(taskId))
      })
      // Deleting tasks
      .addCase(deleteTask.pending, (state, action) => {
        state.status = 'loading'

        const taskId = action.meta.arg
        const snapshot = state.items.find(t => t.id === taskId) ?? null
        tasksSlice.caseReducers.taskDeleted(state, { payload: taskId, type: 'tasks/taskDeleted' })
        markPending(taskId, snapshot, () => store.dispatch(revertToSnapshot(taskId)))
      })
      .addCase(deleteTask.fulfilled, (state, action) => {
        state.status = 'succeeded'
        clearPending(action.meta.arg)
        state.error = null
      })
      .addCase(deleteTask.rejected, (state, action) => {
        state.status = 'failed'
        state.error = action.error.message ?? null
        const taskId = action.meta.arg
        clearPending(taskId)
        store.dispatch(revertToSnapshot(taskId))
      })
  },
})

export const { setDraft, filterTasks, toggleGroup, revertToSnapshot } = tasksSlice.actions

export const tasksReducer = tasksSlice.reducer

const { taskUpserted, taskDeleted } = tasksSlice.actions

// Server-echoed events for a task that has an in-flight local write are stale
// (reads come from the read replica) and would undo our optimistic change, so
// they are ignored. Events for other tasks are applied opportunistically.
const onTaskCreated = (data: WSEventPayloads['task_created']) => {
  if (isPending(data.id)) {
    return
  }
  store.dispatch(taskUpserted(data))
}

const onTaskUpdated = (data: WSEventPayloads['task_updated']) => {
  if (isPending(data.id)) {
    return
  }
  store.dispatch(taskUpserted(data))
}

const onTaskDeletedEvent = (data: WSEventPayloads['task_deleted']) => {
  if (isPending(data.id)) {
    return
  }
  store.dispatch(taskDeleted(data.id))
}

const onTaskCompleted = (data: WSEventPayloads['task_completed']) => {
  if (isPending(data.id)) {
    return
  }
  if (data.next_due_date) {
    store.dispatch(taskUpserted(data))
  } else {
    store.dispatch(tasksSlice.actions.taskRemovedFromActive(data.id))
  }
}

const onTaskUncompleted = (data: WSEventPayloads['task_uncompleted']) => {
  if (isPending(data.id)) {
    return
  }
  store.dispatch(taskUpserted(data))
}

const onTaskSkipped = (data: WSEventPayloads['task_skipped']) => {
  if (isPending(data.id)) {
    return
  }
  if (data.next_due_date) {
    store.dispatch(taskUpserted(data))
  } else {
    store.dispatch(taskDeleted(data.id))
  }
}

const onNotification = (data: WSEventPayloads['notification']) => {
  const enabled = retrieveValue<boolean>('desktop_notifications', false)
  if (!enabled || Notification.permission !== 'granted') {
    return
  }

  const state = store.getState()
  const task = state.tasks.items.find(t => t.id === data.task_id)
  if (!task) {
    return
  }

  let body = ''
  switch (data.type) {
    case 'pre_due':
      body = 'Task is due soon'
      break
    case 'overdue':
      body = 'Task is overdue'
      break
    case 'due_date':
    default:
      body = 'Task is due'
      break
  }

  try {
    // Sanitize task.title to prevent XSS
    const safeTitle = task.title.replace(/</g, "&lt;").replace(/>/g, "&gt;");
    new Notification(safeTitle, { body })
  } catch (e) {
    console.debug('Failed to show notification', e)
  }
}

export const registerWebSocketListeners = (ws: WebSocketManager) => {
  ws.on('task_created', onTaskCreated)
  ws.on('task_updated', onTaskUpdated)
  ws.on('task_deleted', onTaskDeletedEvent)
  ws.on('task_completed', onTaskCompleted)
  ws.on('task_uncompleted', onTaskUncompleted)
  ws.on('task_skipped', onTaskSkipped)
  ws.on('notification', onNotification)
}

export const unregisterWebSocketListeners = (ws: WebSocketManager) => {
  ws.off('task_created', onTaskCreated)
  ws.off('task_updated', onTaskUpdated)
  ws.off('task_deleted', onTaskDeletedEvent)
  ws.off('task_completed', onTaskCompleted)
  ws.off('task_uncompleted', onTaskUncompleted)
  ws.off('task_skipped', onTaskSkipped)
  ws.off('notification', onNotification)
}
