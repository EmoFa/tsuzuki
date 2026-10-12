package io.github.emofa.tsuzuki.ui.finish

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Slider
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import io.github.emofa.tsuzuki.core.ListStatus
import io.github.emofa.tsuzuki.core.Media
import io.github.emofa.tsuzuki.core.WatchRequest
import io.github.emofa.tsuzuki.ui.LocalSnackbar
import io.github.emofa.tsuzuki.ui.common.MediaRow
import io.github.emofa.tsuzuki.ui.common.rememberApp
import io.github.emofa.tsuzuki.ui.details.formatScore
import kotlinx.coroutines.launch

/** After the last episode of a show: rate it, then maybe start the sequel. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun FinishSheet(media: Media, onDismiss: () -> Unit, onOpen: (id: Int) -> Unit) {
    val app = rememberApp()
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    var rating by remember { mutableStateOf(true) }
    var score by remember { mutableFloatStateOf(8f) }
    var sequels by remember { mutableStateOf<List<Media>?>(null) }
    LaunchedEffect(media.id) {
        runCatching { app.core.listEntry(media.id) }.getOrNull()?.score?.takeIf { it > 0 }?.let { score = it.toFloat() }
        sequels = runCatching { app.core.sequels(media.id) }.getOrDefault(emptyList())
    }
    val note = { s: String -> scope.launch { snackbar.showSnackbar(s) } }

    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text("You finished ${media.displayTitle}", style = MaterialTheme.typography.titleLarge)
            if (rating) {
                Text("How would you rate it?", style = MaterialTheme.typography.bodyLarge)
                Text("★ ${formatScore(score.toDouble())}/10", style = MaterialTheme.typography.headlineMedium)
                Slider(value = score, onValueChange = { score = it }, valueRange = 1f..10f, steps = 17)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    TextButton(onClick = { rating = false }) { Text("Skip") }
                    Spacer(Modifier.weight(1f))
                    Button(onClick = {
                        rating = false
                        scope.launch {
                            note(runCatching { app.core.setScore(media.id, score.toDouble()) }.getOrElse { "Couldn't rate it: ${it.message}" })
                        }
                    }) { Text("Rate") }
                }
            } else {
                val list = sequels
                when {
                    list == null -> Text("Looking for a sequel…")
                    list.isEmpty() -> Text("There's no sequel on AniList yet.", color = MaterialTheme.colorScheme.onSurfaceVariant)
                    else -> {
                        Text("Up next", style = MaterialTheme.typography.titleMedium)
                        list.forEach { s ->
                            MediaRow(s, onClick = { onOpen(s.id) })
                            Row(Modifier.padding(start = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                if (!s.notYetAired) {
                                    Button(onClick = {
                                        onDismiss()
                                        scope.launch {
                                            val mode = runCatching { app.core.config().general.mode }.getOrDefault("sub")
                                            app.watch.watch(WatchRequest(media = s, episode = 1.0, mode = mode))
                                        }
                                    }) { Text("Watch") }
                                }
                                OutlinedButton(onClick = {
                                    scope.launch {
                                        note(runCatching { app.core.setListStatus(s.id, ListStatus.Planning) }.getOrElse { "Couldn't add it: ${it.message}" })
                                    }
                                }) { Text("Add to Planning") }
                            }
                        }
                    }
                }
                TextButton(onClick = onDismiss, modifier = Modifier.padding(bottom = 8.dp)) { Text("Done") }
            }
            Spacer(Modifier.navigationBarsPadding().height(16.dp))
        }
    }
}
