import {
  createTask,
  saveTask,
  skipTask,
  deleteTask,
  setDraft,
} from '@/store/tasksSlice'
import { Label } from '@/models/label'
import { Frequency, INVALID_TASK_ID, Task } from '@/models/task'
import { getTextColorFromBackgroundColor } from '@/utils/colors'
import { Add } from '@mui/icons-material'
import {
  Typography,
  Container,
  Box,
  FormControl,
  Input,
  FormHelperText,
  Checkbox,
  RadioGroup,
  Radio,
  Select,
  Chip,
  MenuItem,
  Sheet,
  Button,
  Option,
} from '@mui/joy'
import React, { ChangeEvent } from 'react'
import { ConfirmationModal } from '@/views/Modals/Inputs/ConfirmationModal'
import { RepeatOption } from './RepeatOption'
import { SelectValue } from '@mui/base/useSelect/useSelect.types'
import { setTitle } from '@/utils/dom'
import { NavigationPaths, WithNavigate } from '@/utils/navigation'
import { NotificationTriggerOptions } from '@/models/notifications'
import { NotificationOptions } from '@/views/Notifications/NotificationOptions'
import { moveFocusToJoyInput } from '@/utils/joy'
import { addHours, format, set } from 'date-fns'
import { AppDispatch, RootState } from '@/store/store'
import { connect } from 'react-redux'
import { MakeDateUI, MarshallDate } from '@/utils/marshalling'
import { pushStatus } from '@/store/statusSlice'
import { Status } from '@/models/status'

export type TaskEditProps = {
  draft: Task

  userLabels: Label[]
  defaultNotificationTriggers: NotificationTriggerOptions

  setDraft: (task: Task) => void
  createTask: (task: Omit<Task, 'id'>) => Promise<any>
  saveTask: (task: Task) => Promise<any>
  skipTask: (id: number) => Promise<any>
  deleteTask: (id: number) => Promise<any>
  pushStatus: (status: Status) => void
} & WithNavigate

type Errors = { [key: string]: string }

export interface TaskEditState {
  errors: Errors
}

class TaskEditImpl extends React.Component<TaskEditProps, TaskEditState> {
  private titleInputRef = React.createRef<HTMLDivElement>()
  private confirmModalRef = React.createRef<ConfirmationModal>()

  constructor(props: TaskEditProps) {
    super(props)
    this.state = {
      errors: {},
    }
  }

  private navigateAway = () => {
    const { navigate } = this.props

    navigate(NavigationPaths.HomeView())
  }

  private HandleValidateTask = () => {
    const { draft, pushStatus } = this.props
    const { title, frequency, next_due_date: nextDueDate } = draft
    const errors: Errors = {}

    if (title.trim() === '') {
      errors.title = 'Title is required'
    }

    const frequencyType = frequency.type

    if (frequencyType !== 'once' && nextDueDate === null) {
      errors.dueDate = 'Start date is required'
    }

    if (frequencyType === 'custom') {
      if (frequency.on === 'interval' && frequency.every <= 0) {
        errors.frequency =
          'Invalid frequency, the value should be greater than 0'
      }

      if (frequency.on === 'days_of_the_week' && frequency.days.length === 0) {
        errors.frequency = 'At least 1 day is required'
      }

      if (
        frequency.on === 'day_of_the_months' &&
        frequency.months.length === 0
      ) {
        errors.frequency = 'At least 1 month is required'
      }
    }

    const newState: TaskEditState = {
      ...this.state,
      errors,
    }

    let isSuccessful = true
    if (Object.keys(errors).length > 0) {
      const errorMessages = Object.values(errors).join(', ')
      pushStatus({
        message: `Please resolve the following errors: ${errorMessages}`,
        severity: 'error',
        timeout: 4000,
      })
      isSuccessful = false
    }

    this.setState(newState)
    return isSuccessful
  }

  private onDueDateChanged = (e: ChangeEvent<HTMLInputElement>) => {
    if (e.target.value === '') {
      return
    }

    this.props.setDraft({
      ...this.props.draft,
      next_due_date: MarshallDate(MakeDateUI(e.target.value)),
    })
  }

  private onEndDateChanged = (e: ChangeEvent<HTMLInputElement>) => {
    if (e.target.value === '') {
      return
    }

    this.props.setDraft({
      ...this.props.draft,
      end_date: MarshallDate(MakeDateUI(e.target.value)),
    })
  }

  private onSaveClicked = async () => {
    if (!this.HandleValidateTask()) {
      return
    }

    const { draft, pushStatus } = this.props

    try {
      const promise =
        draft.id === INVALID_TASK_ID
          ? this.props.createTask(draft)
          : this.props.saveTask(draft)

      await promise
      this.navigateAway()

      pushStatus({
        message: 'Task saved',
        severity: 'success',
        timeout: 4000,
      })
    } catch {
      pushStatus({
        message: 'Failed to save task',
        severity: 'error',
        timeout: 4000,
      })
    }
  }

  private onDeleteClicked = (taskId: number) => {
    this.confirmModalRef.current?.open(async () => {
      try {
        await this.props.deleteTask(taskId)

        this.navigateAway()

        this.props.pushStatus({
          message: 'Task deleted',
          severity: 'success',
          timeout: 4000,
        })
      } catch {
        this.props.pushStatus({
          message: 'Failed to delete task',
          severity: 'error',
          timeout: 4000,
        })
      }
    })
  }

  private onSkipClicked = async (taskId: number) => {
    await this.props.skipTask(taskId)

    this.props.pushStatus({
      message: 'Task skipped',
      severity: 'success',
      timeout: 3000,
    })

    this.navigateAway()
  }

  private init = async () => {
    const { draft } = this.props
    setTitle(draft.id === INVALID_TASK_ID ? 'Create Task' : `Edit Task: ${draft.title}`)
  }

  componentDidMount(): void {
    this.init()

    moveFocusToJoyInput(this.titleInputRef)

    document.addEventListener('keydown', this.onEscapeKey)
  }

  componentWillUnmount(): void {
    document.removeEventListener('keydown', this.onEscapeKey)
  }

  private isOverlayOpen = (): boolean => {
    if (this.confirmModalRef.current?.isOpen) {
      return true
    }

    // MUI Joy renders Select/Menu popups in a portal only while they are open,
    // so their presence in the DOM means an overlay should consume the Escape.
    if (document.querySelector('[role="listbox"], [role="menu"]')) {
      return true
    }

    // Native date/time inputs open a browser picker whose Escape should close
    // the picker rather than the whole edit page.
    const active = document.activeElement as HTMLInputElement | null
    if (
      active?.tagName === 'INPUT' &&
      ['date', 'time', 'datetime-local'].includes(active.type)
    ) {
      return true
    }

    return false
  }

  private onEscapeKey = (e: KeyboardEvent) => {
    if (e.key !== 'Escape' || e.defaultPrevented) {
      return
    }

    if (this.isOverlayOpen()) {
      return
    }

    e.preventDefault()
    this.navigateAway()
  }

  private onTitleChanged = (e: ChangeEvent<HTMLInputElement>) => {
    this.props.setDraft({
      ...this.props.draft,
      title: e.target.value,
    })
  }

  private onTitleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      this.onSaveClicked()
      e.preventDefault()
      e.stopPropagation()
    }
  }

  private onHasDueDateChanged = (e: ChangeEvent<HTMLInputElement>) => {
    this.props.setDraft({
      ...this.props.draft,
      next_due_date: MarshallDate(e.target.checked ? set(addHours(new Date(), 1), { seconds: 0, milliseconds: 0 }) : null),
    })
  }

  private onHasEndDateChanged = (e: ChangeEvent<HTMLInputElement>) => {
    this.props.setDraft({
      ...this.props.draft,
      end_date: MarshallDate(e.target.checked ? new Date() : null),
    })
  }

  private onHasNotificationsChanged = (e: ChangeEvent<HTMLInputElement>) => {
    const { defaultNotificationTriggers: defaults, draft, setDraft } = this.props

    setDraft({
      ...draft,
      notification: e.target.checked
        ? {
            enabled: true,
            due_date: defaults.due_date,
            pre_due: defaults.pre_due,
            overdue: defaults.overdue,
          }
        : {
            enabled: false,
          },
    })
  }

  private onNotificationOptionsChanged = (
    notification: NotificationTriggerOptions,
  ) => {
    const { draft, setDraft } = this.props

    if (!draft.notification.enabled) {
      throw new Error('Notifications are disabled')
    }

    setDraft({
      ...draft,
      notification: {
        ...draft.notification,
        ...notification,
      },
    })
  }

  private onRescheduleFromDueDateSelected = () => {
    const { draft, setDraft } = this.props
    setDraft({
      ...draft,
      is_rolling: false,
    })
  }

  private onRescheduleFromCompletionDateSelected = () => {
    const { draft, setDraft } = this.props
    setDraft({
      ...draft,
      is_rolling: true,
    })
  }

  private onLabelsChanged = (
    event: React.MouseEvent | React.KeyboardEvent | React.FocusEvent | null,
    value: SelectValue<Label[], false>,
  ) => {
    if (!value) {
      return
    }

    const { userLabels, draft, setDraft } = this.props
    setDraft({
      ...draft,
      labels: userLabels.filter(
        userLabel =>
          value.some(selected => selected.id === userLabel.id),
      ),
    })
  }

  private onAddNewLabelClicked = () => {
    this.props.navigate(NavigationPaths.Labels)
  }

  private onCancelClicked = () => {
    this.navigateAway()
  }

  private onFrequencyChanged = (newFrequency: Frequency) => {
    const { draft, setDraft } = this.props
    setDraft({
      ...draft,
      frequency: newFrequency,
    })
  }

  render(): React.ReactNode {
    const { userLabels, draft } = this.props

    const {
      id: taskId,
      title,
      frequency,
      is_rolling,
      notification,
      labels,
    } = draft

    const nextDueDate = MakeDateUI(draft.next_due_date)
    const endDate = MakeDateUI(draft.end_date)

    const nextDueDateUI = nextDueDate ? format(nextDueDate, "yyyy-MM-dd'T'HH:mm") : ""
    const endDateUI = endDate ? format(endDate, "yyyy-MM-dd'T'HH:mm") : ""

    const taskLabels = userLabels.filter(label =>
      labels.some(taskLabel => taskLabel.id === label.id),
    )

    const { errors } = this.state
    const frequencyType = frequency.type
    const isRecurring = frequencyType !== 'once'
    const notificationsEnabled = notification.enabled

    return (
      <Container
        maxWidth='md'
        sx={{
          mb: '86px',
        }}
      >
        <Box>
          <FormControl error={Boolean(errors.title)}>
            <Typography level='h4'>Title :</Typography>
            <Typography>What is this task about?</Typography>
            <Input
              ref={this.titleInputRef}
              value={title}
              onChange={this.onTitleChanged}
              onKeyDown={this.onTitleKeyDown}
            />
          </FormControl>
        </Box>
        <Box mt={2}>
          <Typography level='h4'>Labels :</Typography>
          <Typography>How should this task be categorized?</Typography>
          <Select
            multiple
            onChange={this.onLabelsChanged}
            value={taskLabels}
            renderValue={() => (
              <Box
                sx={{
                  display: 'flex',
                  gap: '0.25rem',
                }}
              >
                {taskLabels.map(selectedOption => {
                  return (
                    <Chip
                      variant='soft'
                      color='primary'
                      key={`label-${selectedOption.id}`}
                      size='lg'
                      sx={{
                        background: selectedOption.color,
                        color: getTextColorFromBackgroundColor(
                          selectedOption.color,
                        ),
                      }}
                    >
                      {selectedOption.name}
                    </Chip>
                  )
                })}
              </Box>
            )}
            sx={{ minWidth: '15rem' }}
            slotProps={{
              listbox: {
                sx: {
                  width: '100%',
                },
              },
            }}
          >
            {userLabels &&
              userLabels.map(label => (
                <Option
                  key={`label-${label.id}`}
                  value={label}
                >
                  <div
                    style={{
                      width: '20 px',
                      height: '20 px',
                      borderRadius: '50%',
                      background: label.color,
                    }}
                  />
                  {label.name}
                </Option>
              ))}
            <MenuItem
              key={'addNewLabel'}
              onClick={this.onAddNewLabelClicked}
            >
              <Add />
              Add New Label
            </MenuItem>
          </Select>
        </Box>

        <Box mt={2}>
          <Typography level='h4'>
            {isRecurring ? 'Next due date :' : 'Due date :'}
          </Typography>

          <FormControl sx={{ mt: 1 }}>
            <Checkbox
              onChange={this.onHasDueDateChanged}
              checked={nextDueDate !== null}
              overlay
              label='Give this task a due date'
            />
          </FormControl>

          {nextDueDate && (
            <FormControl error={Boolean(errors.dueDate)}>
              <Typography>
              {isRecurring
                ? 'When is the next time this task is due?'
                : 'When is this task due?'}
              </Typography>
              <Input
              type='datetime-local'
              value={nextDueDateUI}
              onChange={this.onDueDateChanged}
              />
            </FormControl>
          )}
        </Box>

        {nextDueDate && (
          <RepeatOption
            nextDueDate={nextDueDate}
            frequency={frequency}
            onFrequencyUpdate={this.onFrequencyChanged}
            frequencyError={errors?.frequency}
          />
        )}

        {isRecurring && (
          <>
            <Box mt={2}>
              <Typography level='h4'>Scheduling Preferences :</Typography>
              <Typography>
                How should the next occurrence be calculated?
              </Typography>

              <RadioGroup
                name='tiers'
                sx={{
                  gap: 1,
                  '& > div': {
                    p: 1,
                  },
                }}
              >
                <FormControl>
                  <Radio
                    overlay
                    checked={!is_rolling}
                    onClick={this.onRescheduleFromDueDateSelected}
                    label='Reschedule from due date'
                  />
                  <FormHelperText>
                    the next task will be scheduled from the original due date,
                    even if the previous task was completed late
                  </FormHelperText>
                </FormControl>
                <FormControl>
                  <Radio
                    overlay
                    checked={is_rolling}
                    onClick={this.onRescheduleFromCompletionDateSelected}
                    label='Reschedule from completion date'
                  />
                  <FormHelperText>
                    the next task will be scheduled from the actual completion
                    date of the previous task
                  </FormHelperText>
                </FormControl>
              </RadioGroup>
            </Box>
            <Box mt={2}>
              <FormControl sx={{ mt: 1 }}>
                <Checkbox
                  onChange={this.onHasEndDateChanged}
                  checked={endDate !== null}
                  overlay
                  label='Give this task an end date'
                />
              </FormControl>

              { endDate && (
                <FormControl>
                  <FormHelperText>When should the recurrence end?</FormHelperText>
                  <Input
                    type='datetime-local'
                    value={endDateUI}
                    onChange={this.onEndDateChanged}
                    />
                </FormControl>
              )}
            </Box>
          </>
        )}

        {nextDueDate && (
          <Box mt={2}>
            <Typography level='h4'>Notifications :</Typography>

            <FormControl sx={{ mt: 1 }}>
              <Checkbox
                onChange={this.onHasNotificationsChanged}
                checked={notificationsEnabled}
                overlay
                label='Notify for this task'
              />
            </FormControl>
          </Box>
        )}

        {notificationsEnabled && (
          <NotificationOptions
            notification={notification}
            onChange={this.onNotificationOptionsChanged}
          />
        )}

        <Sheet
          variant='outlined'
          sx={{
            position: 'fixed',
            bottom: 0,
            left: 0,
            right: 0,
            padding: 2,
            display: 'flex',
            justifyContent: 'flex-end',
            gap: 2,
            'z-index': 1000,
            bgcolor: 'background.body',
            boxShadow: 'md',
          }}
        >
          <Button
            color='neutral'
            variant='outlined'
            onClick={this.onCancelClicked}
          >
            Cancel
          </Button>
          {taskId !== INVALID_TASK_ID && (
            <Button
              color='danger'
              variant='solid'
              onClick={() => this.onDeleteClicked(taskId)}
            >
              Delete
            </Button>
          )}
          {taskId !== INVALID_TASK_ID && frequency.type !== 'once' && (
            <Button
              color='warning'
              variant='solid'
              onClick={() => this.onSkipClicked(taskId)}
            >
              Skip
            </Button>
          )}
          <Button
            color='primary'
            variant='solid'
            onClick={this.onSaveClicked}
          >
            {taskId !== INVALID_TASK_ID ? 'Save' : 'Create'}
          </Button>
        </Sheet>
        <ConfirmationModal
          ref={this.confirmModalRef}
          title='Delete Task'
          confirmText='Delete'
          cancelText='Cancel'
          message='Are you sure you want to delete this task?'
        />
      </Container>
    )
  }
}

const mapStateToProps = (state: RootState) => {
  let defaultNotificationTriggers: NotificationTriggerOptions = {
    due_date: true,
    pre_due: false,
    overdue: false,
  }

  const userNotificationSettings = state.user.profile.notifications
  if (userNotificationSettings.provider.provider !== 'none') {
      defaultNotificationTriggers = userNotificationSettings.triggers
  }

  return {
    draft: state.tasks.draft,
    userLabels: state.labels.items,
    defaultNotificationTriggers,
  }
}

const mapDispatchToProps = (dispatch: AppDispatch) => ({
  setDraft: (task: Task) => dispatch(setDraft(task)),
  createTask: (task: Omit<Task, 'id'>) => dispatch(createTask(task)),
  saveTask: (task: Task) => dispatch(saveTask(task)),
  skipTask: (id: number) => dispatch(skipTask(id)),
  deleteTask: (id: number) => dispatch(deleteTask(id)),
  pushStatus: (status: Status) => dispatch(pushStatus(status)),
})

export const TaskEdit = connect(
  mapStateToProps,
  mapDispatchToProps,
)(TaskEditImpl)
