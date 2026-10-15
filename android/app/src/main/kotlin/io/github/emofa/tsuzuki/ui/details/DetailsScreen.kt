package io.github.emofa.tsuzuki.ui.details

import androidx.compose.animation.animateContentSize
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.ui.draw.clip
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Replay
import androidx.compose.material.icons.filled.Subtitles
import androidx.compose.material.icons.outlined.BookmarkAdd
import androidx.compose.material.icons.outlined.Numbers
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Button
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Slider
import androidx.compose.material3.SuggestionChip
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.LifecycleResumeEffect
import androidx.lifecycle.viewmodel.compose.viewModel
import coil3.compose.AsyncImage
import io.github.emofa.tsuzuki.core.EpisodeRow
import io.github.emofa.tsuzuki.core.ListStatus
import io.github.emofa.tsuzuki.core.Media
import io.github.emofa.tsuzuki.ui.LocalSnackbar
import io.github.emofa.tsuzuki.ui.common.Cover
import io.github.emofa.tsuzuki.ui.common.ErrorState
import io.github.emofa.tsuzuki.ui.common.Loading
import io.github.emofa.tsuzuki.ui.common.coverColor
import io.github.emofa.tsuzuki.ui.common.rememberApp
import io.github.emofa.tsuzuki.watch.WatchController
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DetailsScreen(id: Int, mode: String, onBack: () -> Unit, onOpen: (id: Int) -> Unit) {
    val app = rememberApp()
    val vm: DetailsViewModel = viewModel(key = "details-$id") { DetailsViewModel(app, id, mode) }
    val snackbar = LocalSnackbar.current
    LaunchedEffect(vm) { vm.notes.collect { snackbar.showSnackbar(it) } }
    // Back from the player, progress has moved on.
    LifecycleResumeEffect(vm) {
        vm.refresh()
        onPauseOrDispose {}
    }

    var menu by remember { mutableStateOf(false) }
    var sheet by remember { mutableStateOf<Sheet?>(null) }
    val list = rememberLazyListState()
    val scope = rememberCoroutineScope()
    // Past the header, the bar needs a background and says what's showing.
    val scrolled by remember { derivedStateOf { list.firstVisibleItemIndex > 0 } }

    Box(Modifier.fillMaxSize()) {
        val media = vm.media
        when {
            vm.error != null && media == null -> ErrorState(vm.error.orEmpty(), onRetry = vm::load)
            media == null -> Loading()
            else -> LazyColumn(state = list, modifier = Modifier.fillMaxSize()) {
                item { Header(media, vm) }
                item { Actions(media, vm, onSheet = { sheet = it }, onOpen = onOpen) }
                item { HorizontalDivider() }
                val rows = vm.episodes?.rows.orEmpty()
                when {
                    media.notYetAired -> item { Note("${media.premiereLabel()}. Episodes will appear here once it airs.") }
                    vm.episodes == null -> item { Loading(Modifier.height(120.dp)) }
                    rows.isEmpty() && vm.episodes?.loading == true -> item { Loading(Modifier.height(120.dp)) }
                    rows.isEmpty() && vm.episodes?.providerError?.isNotEmpty() == true ->
                        item { Note("Couldn't list episodes: ${vm.episodes?.providerError?.lineSequence()?.first()}") }
                    rows.isEmpty() -> item { Note("No episodes have aired yet.") }
                    else -> {
                        item { EpisodesHeader(rows) }
                        items(rows, key = { it.number }) { row ->
                            Episode(
                                row,
                                media,
                                onPlay = { vm.play(row.number) },
                                onLongPress = { sheet = Sheet.Episode(row) },
                            )
                        }
                    }
                }
            }
        }

        // Over the header image, so it stays reachable.
        TopAppBar(
            title = {
                if (scrolled) Text(vm.media?.displayTitle.orEmpty(), maxLines = 1, overflow = TextOverflow.Ellipsis)
            },
            colors = TopAppBarDefaults.topAppBarColors(
                containerColor = if (scrolled) MaterialTheme.colorScheme.surface else Color.Transparent,
            ),
            navigationIcon = {
                IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back") }
            },
            actions = {
                if (!vm.episodes?.rows.isNullOrEmpty()) {
                    IconButton(onClick = { sheet = Sheet.Jump }) { Icon(Icons.Outlined.Numbers, "Jump to episode") }
                }
                IconButton(onClick = { menu = true }) { Icon(Icons.Default.MoreVert, "More") }
                DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                    DropdownMenuItem(
                        text = { Text("Rewatch from episode 1") },
                        leadingIcon = { Icon(Icons.Default.Replay, null) },
                        onClick = { menu = false; sheet = Sheet.Rewatch },
                    )
                    DropdownMenuItem(
                        text = { Text("Subtitles for this show") },
                        leadingIcon = { Icon(Icons.Default.Subtitles, null) },
                        onClick = { menu = false; sheet = Sheet.Subtitles },
                    )
                }
            },
        )
    }

    when (val s = sheet) {
        null -> {}
        Sheet.Status -> StatusSheet(vm, onDismiss = { sheet = null })
        Sheet.Subtitles -> SubtitlesSheet(vm, onDismiss = { sheet = null })
        Sheet.Rate -> RateDialog(vm.entry?.score ?: 0.0, onDismiss = { sheet = null }, onRate = vm::setScore)
        Sheet.Rewatch -> AlertDialog(
            onDismissRequest = { sheet = null },
            title = { Text("Watch it again?") },
            text = { Text("Progress starts again from episode 1, and your list says Rewatching. What you've watched so far stays in your history.") },
            confirmButton = { TextButton(onClick = { sheet = null; vm.rewatch() }) { Text("Rewatch") } },
            dismissButton = { TextButton(onClick = { sheet = null }) { Text("Cancel") } },
        )
        Sheet.Jump -> JumpDialog(onDismiss = { sheet = null }) { n ->
            val i = vm.episodes?.rows.orEmpty().indexOfFirst { it.number >= n }
            // After the header, actions, divider and the episodes heading.
            if (i >= 0) scope.launch { list.animateScrollToItem(i + 4) }
        }
        is Sheet.Episode -> EpisodeSheet(s.row, vm, onDismiss = { sheet = null })
    }
}

private sealed interface Sheet {
    data object Status : Sheet
    data object Subtitles : Sheet
    data object Rate : Sheet
    data object Rewatch : Sheet
    data object Jump : Sheet
    data class Episode(val row: EpisodeRow) : Sheet
}

@Composable
private fun Header(media: Media, vm: DetailsViewModel) {
    val surface = MaterialTheme.colorScheme.surface
    Column {
        Box(Modifier.fillMaxWidth().height(200.dp)) {
            AsyncImage(
                model = media.bannerImage.ifEmpty { media.coverImage.extraLarge },
                contentDescription = null,
                contentScale = ContentScale.Crop,
                modifier = Modifier.fillMaxSize().background(media.coverColor(MaterialTheme.colorScheme.surfaceVariant)),
            )
            Box(
                Modifier.fillMaxSize().background(
                    Brush.verticalGradient(listOf(surface.copy(alpha = 0.3f), surface.copy(alpha = 0.6f), surface)),
                ),
            )
        }
        Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(16.dp)) {
            Cover(media, Modifier.width(110.dp))
            Column(Modifier.weight(1f).align(Alignment.Bottom)) {
                Text(media.displayTitle, style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.SemiBold)
                if (media.title.romaji.isNotEmpty() && media.title.romaji != media.displayTitle) {
                    Text(
                        media.title.romaji,
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Spacer(Modifier.height(4.dp))
                Text(media.meta, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                val round = vm.episodes?.round ?: 1
                if (round > 1) {
                    Text(
                        "Rewatch · round $round",
                        style = MaterialTheme.typography.labelLarge,
                        color = MaterialTheme.colorScheme.primary,
                    )
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
private fun Actions(media: Media, vm: DetailsViewModel, onSheet: (Sheet) -> Unit, onOpen: (Int) -> Unit) {
    Column(Modifier.fillMaxWidth().padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        vm.continueLabel?.let { label ->
            Button(onClick = { vm.play() }, modifier = Modifier.fillMaxWidth().height(52.dp)) {
                Icon(Icons.Default.PlayArrow, null)
                Spacer(Modifier.width(8.dp))
                Text(label, style = MaterialTheme.typography.titleMedium)
            }
        }
        if (media.notYetAired) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Outlined.Schedule, null, tint = MaterialTheme.colorScheme.primary)
                Spacer(Modifier.width(8.dp))
                Text(media.premiereLabel(), style = MaterialTheme.typography.titleSmall)
            }
        }
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            SingleChoiceSegmentedButtonRow {
                listOf("sub" to "Sub", "dub" to "Dub").forEachIndexed { i, (key, label) ->
                    SegmentedButton(
                        selected = vm.mode == key,
                        onClick = { if (vm.mode != key) vm.toggleMode() },
                        shape = SegmentedButtonDefaults.itemShape(i, 2),
                    ) { Text(label) }
                }
            }
            Spacer(Modifier.weight(1f))
            val status = ListStatus.of(vm.entry?.status)
            AssistChip(
                onClick = { onSheet(Sheet.Status) },
                label = { Text(status?.label ?: "Add to list") },
                leadingIcon = { Icon(if (status != null) Icons.Default.Check else Icons.Outlined.BookmarkAdd, null) },
            )
        }
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            val entry = vm.entry
            if (entry != null) {
                SuggestionChip(
                    onClick = { onSheet(Sheet.Rate) },
                    label = { Text(if (entry.score > 0) "★ ${formatScore(entry.score)}/10" else "Rate") },
                )
            }
            SuggestionChip(
                onClick = { onSheet(Sheet.Subtitles) },
                icon = { Icon(Icons.Default.Subtitles, null, Modifier.size(18.dp)) },
                label = {
                    val langs = vm.subtitleLanguages.joinToString(", ").ifEmpty { "any language" }
                    Text(if (vm.subtitlesShown) langs else "$langs, hidden")
                },
            )
            vm.prequels.forEach { p ->
                SuggestionChip(onClick = { onOpen(p.id) }, label = { Text("Prequel: ${p.displayTitle}", maxLines = 1) })
            }
            vm.sequels.forEach { s ->
                SuggestionChip(onClick = { onOpen(s.id) }, label = { Text("Sequel: ${s.displayTitle}", maxLines = 1) })
            }
        }
        if (!media.genres.isNullOrEmpty()) {
            Text(
                media.genres.joinToString(" · "),
                style = MaterialTheme.typography.labelLarge,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Description(media.plainDescription)
        if (vm.busy) LinearProgressIndicator(Modifier.fillMaxWidth())
    }
}

@Composable
private fun Description(text: String) {
    if (text.isEmpty()) return
    var expanded by remember { mutableStateOf(false) }
    Text(
        text,
        style = MaterialTheme.typography.bodyMedium,
        maxLines = if (expanded) Int.MAX_VALUE else 4,
        overflow = TextOverflow.Ellipsis,
        modifier = Modifier.animateContentSize().clickable { expanded = !expanded },
    )
}

/** An episode as a card: a still from it, its title, and how it stands. */
@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun Episode(row: EpisodeRow, media: Media, onPlay: () -> Unit, onLongPress: () -> Unit) {
    val colors = MaterialTheme.colorScheme
    val next = row.state == "next"
    val dimmed = row.watched || row.state == "earlier"
    Row(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = 12.dp, vertical = 4.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(if (next) colors.primaryContainer.copy(alpha = 0.45f) else Color.Transparent)
            .combinedClickable(onClick = onPlay, onLongClick = onLongPress)
            .padding(8.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            Modifier
                .width(136.dp)
                .aspectRatio(16f / 9f)
                .clip(RoundedCornerShape(8.dp))
                .background(media.coverColor(colors.surfaceVariant)),
        ) {
            // A thumbnail that fails to load falls back to the show's art, like none at all.
            var thumbnailFailed by remember(row.thumbnail) { mutableStateOf(false) }
            val hasThumbnail = row.thumbnail.isNotEmpty() && !thumbnailFailed
            val still = if (hasThumbnail) row.thumbnail else media.bannerImage.ifEmpty { media.coverImage.large }
            AsyncImage(
                model = still,
                onError = { if (hasThumbnail) thumbnailFailed = true },
                contentDescription = null,
                contentScale = ContentScale.Crop,
                alpha = if (dimmed) 0.55f else 1f,
                modifier = Modifier.fillMaxSize(),
            )
            if (!hasThumbnail) {
                // The show's art isn't this episode's, so say which one it is.
                Box(Modifier.fillMaxSize().background(Color.Black.copy(alpha = 0.35f)))
                Text(
                    row.label,
                    style = MaterialTheme.typography.headlineSmall,
                    fontWeight = FontWeight.Bold,
                    color = Color.White,
                    modifier = Modifier.align(Alignment.Center),
                )
            }
            when {
                next -> Icon(
                    Icons.Default.PlayArrow,
                    "Up next",
                    tint = colors.onPrimary,
                    modifier = Modifier.align(Alignment.Center).size(36.dp).clip(CircleShape)
                        .background(colors.primary).padding(6.dp),
                )
                row.watched || row.state == "earlier" -> Icon(
                    Icons.Default.Check,
                    "Watched",
                    tint = if (row.state == "earlier") Color.White else colors.onPrimary,
                    modifier = Modifier.align(Alignment.TopEnd).padding(4.dp).size(22.dp).clip(CircleShape)
                        .background(if (row.state == "earlier") Color.Black.copy(alpha = 0.6f) else colors.primary)
                        .padding(3.dp),
                )
            }
            if (row.state == "partial") {
                LinearProgressIndicator(
                    progress = { row.fraction },
                    modifier = Modifier.align(Alignment.BottomCenter).fillMaxWidth().height(4.dp),
                    trackColor = Color.Black.copy(alpha = 0.5f),
                    drawStopIndicator = {},
                )
            }
        }
        Column(Modifier.weight(1f)) {
            Text(
                "Episode ${row.label}",
                style = MaterialTheme.typography.labelMedium,
                color = if (next) colors.primary else colors.onSurfaceVariant,
            )
            Text(
                row.title.ifEmpty { "Episode ${row.label}" },
                style = MaterialTheme.typography.titleSmall,
                color = if (dimmed) colors.onSurfaceVariant else colors.onSurface,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
            episodeDetail(row)?.let {
                Text(
                    it,
                    style = MaterialTheme.typography.bodySmall,
                    color = if (row.kind.isNotEmpty()) colors.tertiary else colors.onSurfaceVariant,
                    modifier = Modifier.padding(top = 2.dp),
                )
            }
        }
    }
}

/** "28 episodes · 14 watched" above the list. */
@Composable
private fun EpisodesHeader(rows: List<EpisodeRow>) {
    val watched = rows.count { it.watched }
    Text(
        buildString {
            append(if (rows.size == 1) "1 episode" else "${rows.size} episodes")
            if (watched > 0) append(" · $watched watched")
        },
        style = MaterialTheme.typography.titleMedium,
        modifier = Modifier.padding(start = 20.dp, end = 20.dp, top = 16.dp, bottom = 4.dp),
    )
}

private fun episodeDetail(row: EpisodeRow): String? = buildList {
    when (row.state) {
        "partial" -> add("${WatchController.clock(row.durationNs - row.positionNs)} left")
        "next" -> add("Up next")
        "earlier" -> add("Watched before this rewatch")
    }
    when (row.kind) {
        "filler" -> add("Filler")
        "mixed" -> add("Mixed canon/filler")
        "recap" -> add("Recap")
    }
}.joinToString(" · ").ifEmpty { null }

@Composable
private fun Note(text: String) {
    Text(
        text,
        style = MaterialTheme.typography.bodyMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(24.dp),
    )
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun StatusSheet(vm: DetailsViewModel, onDismiss: () -> Unit) {
    val current = ListStatus.of(vm.entry?.status)
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Text("List status", style = MaterialTheme.typography.titleMedium, modifier = Modifier.padding(16.dp))
        ListStatus.entries.forEach { s ->
            ListItem(
                headlineContent = { Text(s.label) },
                trailingContent = { if (s == current) Icon(Icons.Default.Check, null) },
                modifier = Modifier.clickable {
                    onDismiss()
                    vm.setStatus(s)
                },
            )
        }
        Spacer(Modifier.navigationBarsPadding().height(24.dp))
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun EpisodeSheet(row: EpisodeRow, vm: DetailsViewModel, onDismiss: () -> Unit) {
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Text("Episode ${row.label}", style = MaterialTheme.typography.titleMedium, modifier = Modifier.padding(16.dp))
        SheetAction("Play") { onDismiss(); vm.play(row.number) }
        if (row.watched) {
            SheetAction("Mark as not watched") { onDismiss(); vm.setWatched(row.number, row.number, false) }
        } else {
            SheetAction("Mark as watched") { onDismiss(); vm.setWatched(row.number, row.number, true) }
        }
        SheetAction("Mark everything up to here as watched") { onDismiss(); vm.watchedThrough(row.number) }
        Spacer(Modifier.navigationBarsPadding().height(24.dp))
    }
}

@Composable
private fun SheetAction(label: String, onClick: () -> Unit) {
    ListItem(headlineContent = { Text(label) }, modifier = Modifier.clickable(onClick = onClick))
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun SubtitlesSheet(vm: DetailsViewModel, onDismiss: () -> Unit) {
    var langs by remember { mutableStateOf(vm.subtitleLanguages.joinToString(", ")) }
    var show by remember { mutableStateOf(vm.subtitlesShown) }
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text("Subtitles for this show", style = MaterialTheme.typography.titleMedium)
            Text(
                if (vm.ownSubtitles) "This show has its own subtitle settings." else "This show uses your default subtitle settings.",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            OutlinedTextField(
                value = langs,
                onValueChange = { langs = it },
                label = { Text("Languages, most preferred first") },
                placeholder = { Text("en, es") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text("Show subtitles when an episode starts", Modifier.weight(1f))
                Switch(checked = show, onCheckedChange = { show = it })
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                if (vm.ownSubtitles) {
                    TextButton(onClick = { onDismiss(); vm.resetSubtitles() }) { Text("Use my defaults") }
                }
                Spacer(Modifier.weight(1f))
                Button(onClick = { onDismiss(); vm.saveSubtitles(langs, show) }) { Text("Save") }
            }
            Spacer(Modifier.navigationBarsPadding().height(24.dp))
        }
    }
}

@Composable
private fun RateDialog(current: Double, onDismiss: () -> Unit, onRate: (Double) -> Unit) {
    var score by remember { mutableFloatStateOf(if (current > 0) current.toFloat() else 7f) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Rate it") },
        text = {
            Column {
                Text("★ ${formatScore(score.toDouble())}/10", style = MaterialTheme.typography.headlineSmall)
                Slider(value = score, onValueChange = { score = it }, valueRange = 1f..10f, steps = 17)
            }
        },
        confirmButton = { TextButton(onClick = { onDismiss(); onRate(score.toDouble()) }) { Text("Rate") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

/** 8.0 reads "8", 8.5 "8.5". */
fun formatScore(s: Double): String = if (s == Math.floor(s)) s.toInt().toString() else "%.1f".format(s)

@Composable
private fun JumpDialog(onDismiss: () -> Unit, onJump: (Double) -> Unit) {
    var text by remember { mutableStateOf("") }
    val n = text.trim().toDoubleOrNull()
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Jump to episode") },
        text = {
            OutlinedTextField(
                value = text,
                onValueChange = { text = it },
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                placeholder = { Text("Episode number") },
            )
        },
        confirmButton = {
            TextButton(enabled = n != null, onClick = { onDismiss(); n?.let(onJump) }) { Text("Jump") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}
