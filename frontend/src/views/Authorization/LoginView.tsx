import { Logo } from '@/Logo'
import { Sheet } from '@mui/joy'
import {
  Container,
  Box,
  Typography,
  Button,
} from '@mui/joy'
import React from 'react'
import { Link } from 'react-router-dom'
import { initializeMsal, isAuthEnabled, loginWithRedirect, acquireAccessToken } from '@/utils/msal'
import { CreateSession } from '@/api/auth'
import { setTitle } from '@/utils/dom'
import { getQuery, NavigationPaths, WithNavigate } from '@/utils/navigation'
import { markLoginRedirect } from '@/utils/api'
import { connect } from 'react-redux'
import { AppDispatch } from '@/store/store'
import { pushStatus } from '@/store/statusSlice'
import { StatusSeverity } from '@/models/status'

type LoginViewProps = WithNavigate & {
  pushStatus: (message: string, severity: StatusSeverity, timeout?: number) => void
}

type LoginViewState = {
  msalReady: boolean
}

class LoginViewImpl extends React.Component<LoginViewProps, LoginViewState> {
  constructor(props: LoginViewProps) {
    super(props)
    this.state = { msalReady: false }
  }

  private getReturnPath = (): string => {
    const returnTo = getQuery('return_to')
    return returnTo || NavigationPaths.HomeView()
  }

  async componentDidMount(): Promise<void> {
    setTitle('Login')

    await initializeMsal()
    this.setState({ msalReady: true })

    if (isAuthEnabled()) {
      try {
        const token = await acquireAccessToken()
        if (token) {
          try {
            await CreateSession()
          } catch {
            // Session creation is best-effort
          }
          this.props.navigate(this.getReturnPath())
          return
        }
      } catch (error) {
        // No cached MSAL tokens; show sign-in button.
        console.error('Login: could not acquire token without interaction', error)
      }
    }
  }

  private handleLogin = async () => {
    try {
      // Remember this bounce so a rapid follow-up 401 doesn't force another
      // full-page redirect into a loop.
      markLoginRedirect()
      await loginWithRedirect()
    } catch (error) {
      this.props.pushStatus((error as Error).message, 'error', 5000)
    }
  }

  render(): React.ReactNode {
    const { msalReady } = this.state

    return (
      <Container
        component='main'
        maxWidth='xs'
      >
        <Box
          sx={{
            marginTop: 4,
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
          }}
        >
          <Sheet
            sx={{
              mt: 1,
              width: '100%',
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              padding: 2,
              borderRadius: '8px',
              boxShadow: 'md',
            }}
          >
            <Logo />

            <Typography sx={{ mt: 2 }}>Sign in to your account to continue</Typography>

            <Button
              fullWidth
              size='lg'
              variant='solid'
              disabled={!msalReady}
              loading={!msalReady}
              sx={{
                width: '100%',
                mt: 3,
                borderRadius: '8px',
              }}
              onClick={this.handleLogin}
            >
              Sign in
            </Button>
          </Sheet>
          <Typography
            level='body-xs'
            sx={{ mt: 2, textAlign: 'center' }}
          >
            <Link
              to={NavigationPaths.Privacy}
              style={{ color: 'inherit' }}
            >
              Privacy Policy
            </Link>
          </Typography>
        </Box>
      </Container>
    )
  }
}

const mapDispatchToProps = (dispatch: AppDispatch) => ({
  pushStatus: (message: string, severity: StatusSeverity, timeout?: number) =>
    dispatch(pushStatus({ message, severity, timeout })),
})

export const LoginView = connect(null, mapDispatchToProps)(LoginViewImpl)
