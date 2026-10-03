import { createAsyncThunk, createSlice } from '@reduxjs/toolkit'
import { GetActivity, UncompleteTask } from '@/api/tasks'
import { ActivityEntry } from '@/models/activity'
import { SyncState } from '@/models/sync'
import WebSocketManager from '@/utils/websocket'
import { store } from './store'

export const ACTIVITY_PAGE_SIZE = 20

export interface ActivityState {
  items: ActivityEntry[]
  loaded: boolean
  hasMore: boolean
  status: SyncState
  error: string | null
}

const initialState: ActivityState = {
  items: [],
  loaded: false,
  hasMore: false,
  status: 'loading',
  error: null,
}

export const fetchActivity = createAsyncThunk(
  'activity/fetchActivity',
  async () => {
    const data = await GetActivity(0, ACTIVITY_PAGE_SIZE)
    return data.activity
  },
)

export const loadMoreActivity = createAsyncThunk(
  'activity/loadMoreActivity',
  async (_, thunkAPI) => {
    const state = thunkAPI.getState() as { activity: ActivityState }
    const items = state.activity.items
    const cursor = items.length > 0 ? items[items.length - 1].id : 0
    const data = await GetActivity(cursor, ACTIVITY_PAGE_SIZE)
    return { cursor, entries: data.activity }
  },
)

export const revertAction = createAsyncThunk(
  'activity/revertAction',
  async (
    { taskId, historyId }: { taskId: number; historyId: number },
  ) => {
    // Trust the write signal only. Over WebSocket the server broadcasts
    // task_uncompleted and the feed refreshes via the listener; over plain HTTP
    // the tasks slice already reflects the change optimistically, so there is no
    // need to re-read the (read-replica-backed) activity feed here.
    await UncompleteTask(taskId, historyId)
  },
)

const activitySlice = createSlice({
  name: 'activity',
  initialState,
  reducers: {},
  extraReducers: builder => {
    builder
      .addCase(fetchActivity.pending, state => {
        state.status = 'loading'
        state.error = null
      })
      .addCase(fetchActivity.fulfilled, (state, action) => {
        state.status = 'succeeded'
        state.items = action.payload
        state.hasMore = action.payload.length === ACTIVITY_PAGE_SIZE
        state.loaded = true
        state.error = null
      })
      .addCase(fetchActivity.rejected, (state, action) => {
        state.status = 'failed'
        state.error = action.error.message ?? null
      })
      .addCase(loadMoreActivity.pending, state => {
        state.status = 'loading'
        state.error = null
      })
      .addCase(loadMoreActivity.fulfilled, (state, action) => {
        state.status = 'succeeded'
        state.error = null

        // Guard against a race with fetchActivity (e.g. triggered by a WS event)
        // that replaced the list while this page was in flight. If the list no
        // longer ends with the cursor this page was based on, the response is
        // stale and appending it would create gaps or misordering, so drop it.
        const currentTail = state.items.length > 0 ? state.items[state.items.length - 1].id : 0
        if (currentTail !== action.payload.cursor) {
          return
        }

        const existingIds = new Set(state.items.map(e => e.id))
        const newEntries = action.payload.entries.filter(e => !existingIds.has(e.id))
        state.items.push(...newEntries)
        state.hasMore = action.payload.entries.length === ACTIVITY_PAGE_SIZE
      })
      .addCase(loadMoreActivity.rejected, (state, action) => {
        state.status = 'failed'
        state.error = action.error.message ?? null
      })
  },
})

export const activityReducer = activitySlice.reducer

const onActivityChanged = () => {
  if (!store.getState().activity.loaded) {
    return
  }
  store.dispatch(fetchActivity())
}

export const registerActivityWebSocketListeners = (ws: WebSocketManager) => {
  ws.on('task_completed', onActivityChanged)
  ws.on('task_uncompleted', onActivityChanged)
  ws.on('task_skipped', onActivityChanged)
  ws.on('task_deleted', onActivityChanged)
}

export const unregisterActivityWebSocketListeners = (ws: WebSocketManager) => {
  ws.off('task_completed', onActivityChanged)
  ws.off('task_uncompleted', onActivityChanged)
  ws.off('task_skipped', onActivityChanged)
  ws.off('task_deleted', onActivityChanged)
}
