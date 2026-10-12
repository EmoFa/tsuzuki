package io.github.emofa.tsuzuki.update

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.ListItem
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import io.github.emofa.tsuzuki.core.AppUpdate
import io.github.emofa.tsuzuki.ui.LocalSnackbar
import io.github.emofa.tsuzuki.ui.common.rememberApp
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import androidx.compose.material3.SnackbarHostState

/** Starts installing [u], first sending the user to allow installs if needed. */
fun CoroutineScope.startUpdate(updater: Updater, u: AppUpdate, snackbar: SnackbarHostState) {
    if (!updater.canInstall) {
        updater.allowInstalls()
        launch { snackbar.showSnackbar("Allow tsuzuki to install updates, then tap Update again.") }
        return
    }
    launch { updater.install(u) }
}

/** Settings' row for updates: check, and install when there's one. */
@Composable
fun UpdateRow() {
    val app = rememberApp()
    val updater = app.updater
    val state by updater.state.collectAsState()
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current

    val (line, update) = when (val s = state) {
        UpdateState.Idle -> "Tap to check for a newer release" to null
        UpdateState.Checking -> "Checking…" to null
        is UpdateState.Checked -> when {
            s.update.dev -> "This build isn't a release (latest is ${s.update.latest})" to null
            s.update.available -> "tsuzuki ${s.update.latest} is available" to s.update
            s.update.latest.isEmpty() -> "Update checks are off" to null
            else -> "Up to date (${s.update.latest})" to null
        }
        is UpdateState.Downloading -> "Downloading ${s.update.latest}…" to null
        is UpdateState.Installing -> "Installing ${s.update.latest}…" to null
        is UpdateState.Failed -> s.message to null
    }
    Column {
        ListItem(
            headlineContent = { Text("Updates") },
            supportingContent = { Text(line) },
            trailingContent = {
                if (update != null) TextButton(onClick = { scope.startUpdate(updater, update, snackbar) }) { Text("Update") }
            },
            modifier = Modifier.clickable { scope.launch { updater.check() } },
        )
        (state as? UpdateState.Downloading)?.let {
            LinearProgressIndicator(progress = { it.fraction }, modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp))
        }
    }
}
