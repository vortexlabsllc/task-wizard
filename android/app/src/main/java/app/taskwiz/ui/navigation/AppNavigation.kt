package app.taskwiz.ui.navigation

import android.app.Activity
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Sync
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument
import app.taskwiz.R
import app.taskwiz.data.SwipeSettings
import app.taskwiz.data.TaskGrouping
import app.taskwiz.data.ThemeMode
import app.taskwiz.data.calendar.CalendarRepository
import app.taskwiz.model.CreateTaskReq
import app.taskwiz.model.Task
import app.taskwiz.model.UpdateTaskReq
import app.taskwiz.ui.screen.ActivityScreen
import app.taskwiz.ui.screen.LabelsScreen
import app.taskwiz.ui.screen.SettingsScreen
import app.taskwiz.ui.screen.SignInScreen
import app.taskwiz.ui.screen.SwipeActionsSettingsScreen
import app.taskwiz.ui.screen.TaskFormScreen
import app.taskwiz.ui.screen.TaskHistoryScreen
import app.taskwiz.ui.screen.TaskListScreen
import app.taskwiz.viewmodel.AuthViewModel
import app.taskwiz.viewmodel.ActivityViewModel
import app.taskwiz.viewmodel.LabelViewModel
import app.taskwiz.viewmodel.TaskFormViewModel
import app.taskwiz.viewmodel.TaskHistoryViewModel
import app.taskwiz.viewmodel.TaskListViewModel
import app.taskwiz.viewmodel.UserViewModel

@Composable
fun AppNavigation(
    themeMode: ThemeMode,
    onThemeModeChanged: (ThemeMode) -> Unit,
    taskGrouping: TaskGrouping,
    onTaskGroupingChanged: (TaskGrouping) -> Unit,
    calendarSyncEnabled: Boolean,
    onCalendarSyncChanged: (Boolean) -> Unit,
    calendarRepository: CalendarRepository,
    swipeSettings: SwipeSettings,
    onSwipeEnabledChanged: (Boolean) -> Unit,
    onSwipeStartToEndActionChanged: (app.taskwiz.data.SwipeAction) -> Unit,
    onSwipeEndToStartActionChanged: (app.taskwiz.data.SwipeAction) -> Unit,
    onSwipeDeleteConfirmationChanged: (Boolean) -> Unit,
    inlineCompleteEnabled: Boolean,
    onInlineCompleteEnabledChanged: (Boolean) -> Unit,
    telemetryEnabled: Boolean,
    onTelemetryEnabledChanged: (Boolean) -> Unit,
    debugLoggingEnabled: Boolean,
    onDebugLoggingEnabledChanged: (Boolean) -> Unit,
    initialTaskId: Int = -1,
    createTask: Boolean = false
){
    val navController = rememberNavController()
    val bottomScreens = listOf(Screen.Tasks, Screen.Activity, Screen.Labels, Screen.Settings)
    val navBackStackEntry by navController.currentBackStackEntryAsState()
    val currentRoute = navBackStackEntry?.destination?.route

    val showBottomBar = bottomScreens.any { it.route == currentRoute }

    LaunchedEffect(initialTaskId) {
        if (initialTaskId > 0) {
            navController.navigate(Routes.taskFormEdit(initialTaskId))
        }
    }

    LaunchedEffect(createTask) {
        if (createTask) {
            navController.navigate(Routes.TASK_FORM_CREATE)
        }
    }

    Scaffold(
        contentWindowInsets = WindowInsets(0, 0, 0, 0),
        bottomBar = {
            if (showBottomBar) {
                NavigationBar {
                    bottomScreens.forEach { screen ->
                        NavigationBarItem(
                            icon = { Icon(screen.icon, contentDescription = stringResource(screen.titleRes)) },
                            label = { Text(stringResource(screen.titleRes)) },
                            selected = currentRoute == screen.route,
                            onClick = {
                                navController.navigate(screen.route) {
                                    popUpTo(navController.graph.findStartDestination().id) {
                                        saveState = true
                                    }
                                    launchSingleTop = true
                                    restoreState = true
                                }
                            }
                        )
                    }
                }
            }
        }
    ) { innerPadding ->
        NavHost(
            navController = navController,
            startDestination = Screen.Tasks.route,
            modifier = Modifier.padding(innerPadding),
            enterTransition = { fadeIn() },
            exitTransition = { fadeOut() },
            popEnterTransition = { fadeIn() },
            popExitTransition = { fadeOut() }
        ) {
            composable(Screen.Tasks.route) {
                val viewModel: TaskListViewModel = hiltViewModel()
                val userViewModel: UserViewModel = hiltViewModel()
                val authViewModel: AuthViewModel = hiltViewModel()
                val isRefreshing by viewModel.isRefreshing.collectAsState()
                val taskGroups by viewModel.taskGroups.collectAsState()
                val expandedGroups by viewModel.expandedGroups.collectAsState()
                val deletionRequestedAt by userViewModel.deletionRequestedAt.collectAsState()
                val isOnline by viewModel.isOnline.collectAsState()
                val pendingSyncCount by viewModel.pendingSyncCount.collectAsState()
                val searchQuery by viewModel.searchQuery.collectAsState()
                val isSearchActive by viewModel.isSearchActive.collectAsState()
                val sessionExpired by authViewModel.sessionExpired.collectAsState()
                val isReauthenticating by authViewModel.isLoading.collectAsState()
                val isSignedIn by authViewModel.isSignedIn.collectAsState()
                val showSyncPrompt by viewModel.showSyncPrompt.collectAsState()
                val activity = LocalContext.current as Activity

                LaunchedEffect(taskGrouping) {
                    viewModel.setTaskGrouping(taskGrouping)
                }

                var wasSessionExpired by remember { mutableStateOf(sessionExpired) }
                LaunchedEffect(sessionExpired) {
                    if (wasSessionExpired && !sessionExpired) {
                        viewModel.refreshTasks()
                    }
                    wasSessionExpired = sessionExpired
                }

                if (showSyncPrompt && !isSignedIn) {
                    AlertDialog(
                        onDismissRequest = { viewModel.dismissSyncPrompt() },
                        icon = {
                            Icon(
                                Icons.Default.Sync,
                                contentDescription = null,
                                tint = MaterialTheme.colorScheme.primary
                            )
                        },
                        title = { Text(stringResource(R.string.sync_prompt_title)) },
                        text = { Text(stringResource(R.string.sync_prompt_message)) },
                        confirmButton = {
                            TextButton(onClick = {
                                viewModel.dismissSyncPrompt()
                                navController.navigate(Routes.SIGN_IN)
                            }) {
                                Text(stringResource(R.string.btn_sign_in_to_sync))
                            }
                        },
                        dismissButton = {
                            TextButton(onClick = { viewModel.dismissSyncPrompt() }) {
                                Text(stringResource(R.string.sync_prompt_later))
                            }
                        }
                    )
                }

                TaskListScreen(
                    taskGroups = taskGroups,
                    expandedGroups = expandedGroups,
                    isRefreshing = isRefreshing,
                    onRefresh = { viewModel.forceRefreshTasks() },
                    onCompleteTask = { viewModel.completeTask(it) },
                    onSkipTask = { viewModel.skipTask(it) },
                    onDeleteTask = { viewModel.deleteTask(it) },
                    onCompleteAndEndRecurrenceTask = { viewModel.completeTask(it, endRecurrence = true) },
                    onTaskClick = { navController.navigate(Routes.taskFormEdit(it)) },
                    onViewHistory = { navController.navigate(Routes.taskHistory(it)) },
                    onCreateTask = { navController.navigate(Routes.TASK_FORM_CREATE) },
                    onToggleGroup = { viewModel.toggleGroupExpanded(it) },
                    searchQuery = searchQuery,
                    isSearchActive = isSearchActive,
                    onSearchQueryChange = { viewModel.setSearchQuery(it) },
                    onSearchActiveChange = { viewModel.setSearchActive(it) },
                    swipeSettings = swipeSettings,
                    inlineCompleteEnabled = inlineCompleteEnabled,
                    isPendingDeletion = deletionRequestedAt != null,
                    isOnline = isOnline,
                    pendingSyncCount = pendingSyncCount,
                    isSignedIn = isSignedIn,
                    sessionExpired = sessionExpired,
                    isReauthenticating = isReauthenticating,
                    onReauthenticate = { authViewModel.signIn(activity) },
                )
            }

            composable(Screen.Activity.route) {
                val viewModel: ActivityViewModel = hiltViewModel()
                val items by viewModel.items.collectAsState()
                val isLoading by viewModel.isLoading.collectAsState()
                val isLoadingMore by viewModel.isLoadingMore.collectAsState()
                val hasMore by viewModel.hasMore.collectAsState()
                val isReverting by viewModel.isReverting.collectAsState()
                val message by viewModel.message.collectAsState()

                ActivityScreen(
                    items = items,
                    isLoading = isLoading,
                    isLoadingMore = isLoadingMore,
                    hasMore = hasMore,
                    isReverting = isReverting,
                    message = message,
                    onRevert = { taskId, historyId -> viewModel.revert(taskId, historyId) },
                    onLoadMore = { viewModel.loadMore() },
                    onMessageShown = { viewModel.clearMessage() },
                )
            }

            composable(Screen.Labels.route) {
                val viewModel: LabelViewModel = hiltViewModel()
                val labels by viewModel.labels.collectAsState()
                val isRefreshing by viewModel.isRefreshing.collectAsState()

                LabelsScreen(
                    labels = labels,
                    isRefreshing = isRefreshing,
                    onRefresh = { viewModel.forceRefreshLabels() },
                    onCreateLabel = { name, color -> viewModel.createLabel(name, color) },
                    onUpdateLabel = { id, name, color -> viewModel.updateLabel(id, name, color) },
                    onDeleteLabel = { viewModel.deleteLabel(it) }
                )
            }

            composable(Screen.Settings.route) {
                val authViewModel: AuthViewModel = hiltViewModel()
                val userViewModel: UserViewModel = hiltViewModel()
                SettingsScreen(
                    authViewModel = authViewModel,
                    userViewModel = userViewModel,
                    themeMode = themeMode,
                    onThemeModeChanged = onThemeModeChanged,
                    taskGrouping = taskGrouping,
                    onTaskGroupingChanged = onTaskGroupingChanged,
                    calendarSyncEnabled = calendarSyncEnabled,
                    onCalendarSyncChanged = onCalendarSyncChanged,
                    calendarRepository = calendarRepository,
                    swipeSettings = swipeSettings,
                    onSwipeEnabledChanged = onSwipeEnabledChanged,
                    onSwipeDeleteConfirmationChanged = onSwipeDeleteConfirmationChanged,
                    onNavigateToSwipeSettings = { navController.navigate(Routes.SWIPE_SETTINGS) },
                    onNavigateToSignIn = { navController.navigate(Routes.SIGN_IN) },
                    inlineCompleteEnabled = inlineCompleteEnabled,
                    onInlineCompleteEnabledChanged = onInlineCompleteEnabledChanged,
                    telemetryEnabled = telemetryEnabled,
                    onTelemetryEnabledChanged = onTelemetryEnabledChanged,
                    debugLoggingEnabled = debugLoggingEnabled,
                    onDebugLoggingEnabledChanged = onDebugLoggingEnabledChanged
                )
            }

            composable(
                route = Routes.SWIPE_SETTINGS,
                enterTransition = { slideInHorizontally(initialOffsetX = { it }) + fadeIn() },
                exitTransition = { fadeOut() },
                popEnterTransition = { fadeIn() },
                popExitTransition = { slideOutHorizontally(targetOffsetX = { it }) + fadeOut() }
            ) {
                SwipeActionsSettingsScreen(
                    swipeSettings = swipeSettings,
                    onStartToEndActionChanged = onSwipeStartToEndActionChanged,
                    onEndToStartActionChanged = onSwipeEndToStartActionChanged,
                    onBack = { navController.popBackStack() }
                )
            }

            composable(
                route = Routes.TASK_FORM,
                arguments = listOf(navArgument("taskId") {
                    type = NavType.IntType
                    defaultValue = -1
                }),
                enterTransition = { slideInHorizontally(initialOffsetX = { it }) + fadeIn() },
                exitTransition = { fadeOut() },
                popEnterTransition = { fadeIn() },
                popExitTransition = { slideOutHorizontally(targetOffsetX = { it }) + fadeOut() }
            ) { backStackEntry ->
                val taskId = backStackEntry.arguments?.getInt("taskId") ?: -1
                val viewModel: TaskFormViewModel = hiltViewModel()
                val availableLabels by viewModel.availableLabels.collectAsState()
                val isSaving by viewModel.isSaving.collectAsState()
                val saveResult by viewModel.saveResult.collectAsState()

                var existingTask by remember { mutableStateOf<Task?>(null) }

                LaunchedEffect(taskId) {
                    if (taskId > 0) {
                        viewModel.loadTask(taskId) { existingTask = it }
                    }
                }

                LaunchedEffect(saveResult) {
                    if (saveResult?.isSuccess == true) {
                        viewModel.clearSaveResult()
                        navController.popBackStack()
                    }
                }

                TaskFormScreen(
                    existingTask = existingTask,
                    availableLabels = availableLabels,
                    isSaving = isSaving,
                    onSave = { title, nextDueDate, endDate, frequency, notification, labelIds, isRolling ->
                        if (taskId > 0) {
                            viewModel.updateTask(UpdateTaskReq(
                                id = taskId,
                                title = title,
                                nextDueDate = nextDueDate,
                                endDate = endDate,
                                frequency = frequency,
                                notification = notification,
                                labels = labelIds,
                                isRolling = isRolling
                            ))
                        } else {
                            viewModel.createTask(CreateTaskReq(
                                title = title,
                                nextDueDate = nextDueDate,
                                endDate = endDate,
                                frequency = frequency,
                                notification = notification,
                                labels = labelIds,
                                isRolling = isRolling
                            ))
                        }
                    },
                    onBack = { navController.popBackStack() }
                )
            }

            composable(
                route = Routes.TASK_HISTORY,
                arguments = listOf(navArgument("taskId") {
                    type = NavType.IntType
                    defaultValue = -1
                }),
                enterTransition = { slideInHorizontally(initialOffsetX = { it }) + fadeIn() },
                exitTransition = { fadeOut() },
                popEnterTransition = { fadeIn() },
                popExitTransition = { slideOutHorizontally(targetOffsetX = { it }) + fadeOut() }
            ) { backStackEntry ->
                val taskId = backStackEntry.arguments?.getInt("taskId") ?: -1
                val viewModel: TaskHistoryViewModel = hiltViewModel()
                val history by viewModel.history.collectAsState()
                val isLoading by viewModel.isLoading.collectAsState()

                LaunchedEffect(taskId) {
                    if (taskId > 0) {
                        viewModel.loadHistory(taskId)
                    } else {
                        navController.popBackStack()
                    }
                }

                TaskHistoryScreen(
                    history = history,
                    isLoading = isLoading,
                    onBack = { navController.popBackStack() }
                )
            }

            composable(
                route = Routes.SIGN_IN,
                enterTransition = { slideInHorizontally(initialOffsetX = { it }) + fadeIn() },
                exitTransition = { fadeOut() },
                popEnterTransition = { fadeIn() },
                popExitTransition = { slideOutHorizontally(targetOffsetX = { it }) + fadeOut() }
            ) {
                val authViewModel: AuthViewModel = hiltViewModel()
                val isSignedIn by authViewModel.isSignedIn.collectAsState()
                val isLoading by authViewModel.isLoading.collectAsState()
                val errorMessage by authViewModel.errorMessage.collectAsState()
                val serverEndpoint by authViewModel.serverEndpoint.collectAsState()
                val activity = LocalContext.current as Activity

                LaunchedEffect(isSignedIn) {
                    if (isSignedIn) {
                        navController.popBackStack()
                    }
                }

                SignInScreen(
                    serverEndpoint = serverEndpoint,
                    isLoading = isLoading,
                    errorMessage = errorMessage,
                    onSignIn = { authViewModel.signIn(activity) },
                    onEndpointChanged = { authViewModel.updateServerEndpoint(it) },
                    onBack = { navController.popBackStack() }
                )
            }
        }
    }
}
