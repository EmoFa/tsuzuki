package io.github.emofa.tsuzuki.ui.diagnostics

import android.webkit.CookieManager
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Error
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import io.github.emofa.tsuzuki.TsuzukiApplication
import io.github.emofa.tsuzuki.core.Diagnosis
import io.github.emofa.tsuzuki.ui.LocalSnackbar
import io.github.emofa.tsuzuki.ui.common.rememberApp
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.launch

class DiagnosticsViewModel(private val app: TsuzukiApplication) : ViewModel() {
    var results by mutableStateOf<List<Diagnosis>?>(null)
        private set
    var running by mutableStateOf(false)
        private set
    var streams by mutableStateOf(false)

    private val _notes = Channel<String>(Channel.BUFFERED)
    val notes = _notes.receiveAsFlow()

    fun run() {
        if (running) return
        running = true
        viewModelScope.launch {
            try {
                results = app.core.diagnose(streams)
            } catch (e: Exception) {
                _notes.send(e.message.orEmpty())
            }
            running = false
        }
    }

    fun resetVerifications() {
        viewModelScope.launch {
            try {
                app.core.clearVerifications()
                // The WebView's cookies too, so the next check starts afresh.
                CookieManager.getInstance().removeAllCookies(null)
                _notes.send("Site verifications cleared. Sites that ask will check again next time.")
            } catch (e: Exception) {
                _notes.send("Couldn't clear them: ${e.message}")
            }
        }
    }
}

/** What `tsuzuki doctor` checks, on the phone. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DiagnosticsScreen(onBack: () -> Unit) {
    val app = rememberApp()
    val vm: DiagnosticsViewModel = viewModel { DiagnosticsViewModel(app) }
    val snackbar = LocalSnackbar.current
    LaunchedEffect(vm) { vm.notes.collect { snackbar.showSnackbar(it) } }

    Column(Modifier.fillMaxSize()) {
        TopAppBar(
            title = { Text("Diagnostics") },
            navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back") } },
        )
        Column(Modifier.padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(
                "Checks AniList, the skip-time and filler services, and a search on each provider.",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text("Also play-test a stream from each provider (slower)", Modifier.weight(1f))
                Switch(checked = vm.streams, onCheckedChange = { vm.streams = it }, enabled = !vm.running)
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(onClick = vm::run, enabled = !vm.running) { Text(if (vm.results == null) "Run checks" else "Run again") }
                OutlinedButton(onClick = vm::resetVerifications) { Text("Reset site verifications") }
            }
            if (vm.running) LinearProgressIndicator(Modifier.fillMaxWidth())
        }
        LazyColumn(Modifier.fillMaxSize().padding(top = 8.dp)) {
            items(vm.results.orEmpty()) { d ->
                ListItem(
                    leadingContent = {
                        when (d.status) {
                            "ok" -> Icon(Icons.Default.CheckCircle, "OK", tint = MaterialTheme.colorScheme.primary)
                            "warn" -> Icon(Icons.Default.Warning, "Warning", tint = MaterialTheme.colorScheme.tertiary)
                            else -> Icon(Icons.Default.Error, "Failed", tint = MaterialTheme.colorScheme.error)
                        }
                    },
                    headlineContent = { Text(d.name) },
                    supportingContent = { Text(d.detail) },
                )
            }
        }
    }
}
