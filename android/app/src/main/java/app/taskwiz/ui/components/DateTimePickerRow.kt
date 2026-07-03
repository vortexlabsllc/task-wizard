package app.taskwiz.ui.components

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.width
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CalendarMonth
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TimePicker
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.material3.rememberTimePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import app.taskwiz.R
import app.taskwiz.ui.utils.toDisplayString
import java.time.Instant
import java.time.ZoneId
import java.time.ZoneOffset
import java.time.ZonedDateTime

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DateTimePickerRow(
    label: String,
    value: ZonedDateTime?,
    onValueSelected: (ZonedDateTime) -> Unit
) {
    var showDatePicker by remember { mutableStateOf(false) }
    var showTimePicker by remember { mutableStateOf(false) }
    var pendingDate by remember { mutableStateOf<ZonedDateTime?>(null) }

    val datePickerState = rememberDatePickerState(
        // Material3's DatePicker treats selectedDateMillis as UTC midnight of a calendar
        // day, so seed it with the local calendar date's UTC midnight. Passing the raw
        // instant would roll the displayed day forward in negative-offset zones (e.g. an
        // evening in Seattle is already the next day in UTC).
        initialSelectedDateMillis = (value ?: ZonedDateTime.now(ZoneId.systemDefault()))
            .toLocalDate()
            .atStartOfDay(ZoneOffset.UTC)
            .toInstant()
            .toEpochMilli()
    )
    val timePickerState = rememberTimePickerState(
        initialHour = value?.hour ?: ZonedDateTime.now(ZoneId.systemDefault()).hour,
        initialMinute = value?.minute ?: 0,
        is24Hour = false
    )

    OutlinedButton(
        onClick = { showDatePicker = true },
        modifier = Modifier.fillMaxWidth(),
        contentPadding = PaddingValues(horizontal = 16.dp, vertical = 12.dp)
    ) {
        Icon(Icons.Default.CalendarMonth, contentDescription = null)
        Spacer(Modifier.width(8.dp))
        Text(
            text = value?.toDisplayString() ?: label,
            modifier = Modifier.weight(1f)
        )
    }

    if (showDatePicker) {
        DatePickerDialog(
            onDismissRequest = { showDatePicker = false },
            confirmButton = {
                TextButton(onClick = {
                    showDatePicker = false
                    val millis = datePickerState.selectedDateMillis
                    if (millis != null) {
                        // selectedDateMillis is UTC midnight for the picked day.
                        // Extract the LocalDate in UTC, then build a ZonedDateTime
                        // at midnight in the system timezone to avoid day-offset issues.
                        val localDate = Instant.ofEpochMilli(millis)
                            .atZone(ZoneOffset.UTC)
                            .toLocalDate()
                        pendingDate = localDate.atStartOfDay(ZoneId.systemDefault())
                        showTimePicker = true
                    }
                }) { Text(stringResource(R.string.btn_next)) }
            },
            dismissButton = {
                TextButton(onClick = { showDatePicker = false }) { Text(stringResource(R.string.btn_cancel)) }
            }
        ) {
            DatePicker(state = datePickerState)
        }
    }

    if (showTimePicker) {
        AlertDialog(
            onDismissRequest = { showTimePicker = false },
            title = { Text(stringResource(R.string.dialog_title_select_time)) },
            text = {
                Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) {
                    TimePicker(state = timePickerState)
                }
            },
            confirmButton = {
                TextButton(onClick = {
                    showTimePicker = false
                    pendingDate?.let { date ->
                        val result = date
                            .withHour(timePickerState.hour)
                            .withMinute(timePickerState.minute)
                            .withSecond(0)
                            .withNano(0)
                        onValueSelected(result)
                    }
                }) { Text(stringResource(R.string.btn_ok)) }
            },
            dismissButton = {
                TextButton(onClick = { showTimePicker = false }) { Text(stringResource(R.string.btn_cancel)) }
            }
        )
    }
}
