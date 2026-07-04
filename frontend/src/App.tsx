import { NavBar } from './views/Navigation/NavBar'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { initializeMsal, isAuthEnabled, acquireAccessToken } from './utils/msal'
import { CreateSession } from './api/auth'
import { NavigationPaths } from './utils/navigation'
import React from 'react'
import { CssBaseline, CssVarsProvider } from '@mui/joy'
import { preloadSounds } from './utils/sound'
import WebSocketManager from './utils/websocket'
import { fetchLabels } from './store/labelsSlice'
import { AppDispatch, store } from './store/store'
import { connect } from 'react-redux'
import { fetchUser } from './store/userSlice'
import { StatusList } from './components/StatusList'
import { DeletionBanner } from './components/DeletionBanner'
import { fetchTasks, initGroups } from './store/tasksSlice'
import { FIVE_MINUTES_MS } from '@/constants/time'
import { isRedirectingToLogin } from './utils/api'

type AppProps = {
  pathname: string
  navigate: (path: string) => void

  fetchLabels: () => Promise<any>
  fetchUser: () => Promise<any>
  fetchTasks: () => Promise<any>
  initGroups: () => void
}

type AppState = {
  ready: boolean
}

class AppImpl extends React.Component<AppProps, AppState> {
  private initializedAuthenticated = false
  private initializingAuthenticated = false

  constructor(props: AppProps) {
    super(props)
    this.state = { ready: false }
  }

  private isPublicRoute = (): boolean => {
    return this.props.pathname === NavigationPaths.Privacy || this.props.pathname === NavigationPaths.Login
  }

  private onVisibilityChange = () => {
    if (!document.hidden && this.initializedAuthenticated) {
      this.refreshStaleData()
    }
  }

  private initializeAuthenticated = async () => {
    if (this.initializedAuthenticated || this.initializingAuthenticated) {
      return
    }

    this.initializingAuthenticated = true
    try {
      preloadSounds()
      const ws = WebSocketManager.getInstance()
      await ws.connect()

      await this.props.fetchUser()
      await this.props.fetchLabels()
      await this.props.fetchTasks()
      await this.props.initGroups()

      this.initializedAuthenticated = true
    } finally {
      this.initializingAuthenticated = false
    }
  }

  private refreshStaleData = async () => {
    const state = store.getState()
    const now = Date.now()

    let groupsOutdated = false

    if (!state.user.lastFetched || now - state.user.lastFetched > FIVE_MINUTES_MS) {
      await this.props.fetchUser()
    }

    if (!state.labels.lastFetched || now - state.labels.lastFetched > FIVE_MINUTES_MS) {
      await this.props.fetchLabels()
      groupsOutdated = true
    }

    if (!state.tasks.lastFetched || now - state.tasks.lastFetched > FIVE_MINUTES_MS) {
      await this.props.fetchTasks()
      groupsOutdated = true
    }

    if (groupsOutdated) {
      await this.props.initGroups()
    }
  }

  async componentDidMount(): Promise<void> {
    await initializeMsal()

    if (!this.isPublicRoute()) {
      if (isAuthEnabled()) {
        try {
          const token = await acquireAccessToken()
          if (token) {
            try {
              await CreateSession()
            } catch {
              // Session creation is best-effort
            }
          }
        } catch {
          // No MSAL tokens; will rely on session cookie
        }
      }

      await this.initializeAuthenticated()
    }

    if (!isRedirectingToLogin()) {
      this.setState({ ready: true })
    }
    document.addEventListener('visibilitychange', this.onVisibilityChange)
  }

  async componentDidUpdate(prevProps: AppProps): Promise<void> {
    if (this.isPublicRoute()) {
      return
    }

    if (prevProps.pathname !== this.props.pathname) {
      await this.initializeAuthenticated()
      return
    }

    await this.initializeAuthenticated()
  }

  componentWillUnmount(): void {
    document.removeEventListener('visibilitychange', this.onVisibilityChange)
  }

  render() {
    const { pathname } = this.props
    const { ready } = this.state

    if (!ready && !this.isPublicRoute()) {
      return null
    }

    return (
      <div style={{ minHeight: '100vh' }}>
        <CssBaseline />
        <CssVarsProvider
          modeStorageKey='themeMode'
          attribute='data-theme'
          defaultMode='system'
          colorSchemeNode={document.body}
        >
          <NavBar
            pathname={pathname}
          />
          <DeletionBanner />
          <Outlet />
          <StatusList />
        </CssVarsProvider>
      </div>
    )
  }
}

const mapDispatchToProps = (dispatch: AppDispatch) => ({
  fetchUser: () => dispatch(fetchUser()),
  fetchLabels: () => dispatch(fetchLabels()),
  fetchTasks: () => dispatch(fetchTasks()),
  initGroups: () => dispatch(initGroups()),
})

const ConnectedApp = connect(
  null,
  mapDispatchToProps,
)(AppImpl)

export const App = () => {
  const location = useLocation()
  const navigate = useNavigate()
  return (
    <ConnectedApp
      pathname={location.pathname}
      navigate={navigate}
    />
  )
}
