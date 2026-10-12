package io.github.emofa.tsuzuki.ui.discover

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.grid.LazyGridState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowDropDown
import androidx.compose.material.icons.outlined.Explore
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.PrimaryScrollableTabRow
import androidx.compose.material3.Tab
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.LifecycleResumeEffect
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import io.github.emofa.tsuzuki.TsuzukiApplication
import io.github.emofa.tsuzuki.core.DiscoverList
import io.github.emofa.tsuzuki.core.ListStatus
import io.github.emofa.tsuzuki.core.Media
import io.github.emofa.tsuzuki.core.formatLabel
import io.github.emofa.tsuzuki.ui.common.ErrorState
import io.github.emofa.tsuzuki.ui.common.Loading
import io.github.emofa.tsuzuki.ui.common.MediaCard
import io.github.emofa.tsuzuki.ui.common.Message
import io.github.emofa.tsuzuki.ui.common.rememberApp
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch

private val formats = listOf("TV", "MOVIE", "ONA", "OVA", "SPECIAL", "TV_SHORT")

class DiscoverViewModel(private val app: TsuzukiApplication) : ViewModel() {
    var lists by mutableStateOf<List<DiscoverList>>(emptyList())
        private set
    var tab by mutableStateOf(0)
        private set
    var genres by mutableStateOf<List<String>>(emptyList())
        private set
    var genre by mutableStateOf<String?>(null)
        private set
    var format by mutableStateOf<String?>(null)
        private set

    var items by mutableStateOf<List<Media>>(emptyList())
        private set
    var hasNext by mutableStateOf(false)
        private set
    var loading by mutableStateOf(false)
        private set
    var error by mutableStateOf<String?>(null)
        private set

    /** Where each show is on the user's list, to mark it. */
    var statuses by mutableStateOf<Map<Int, ListStatus>>(emptyMap())
        private set

    private var page = 1
    private var job: Job? = null

    init {
        viewModelScope.launch {
            try {
                lists = app.core.discoverLists()
                reload()
            } catch (e: Exception) {
                error = e.message
            }
            runCatching { genres = app.core.genres() }
        }
    }

    fun refreshStatuses() {
        viewModelScope.launch {
            runCatching {
                statuses = app.core.listEntries().mapNotNull { e -> ListStatus.of(e.status)?.let { e.mediaId to it } }.toMap()
            }
        }
    }

    fun selectTab(i: Int) {
        tab = i
        reload()
    }

    fun filter(genre: String?, format: String?) {
        this.genre = genre
        this.format = format
        reload()
    }

    fun reload() {
        job?.cancel()
        items = emptyList()
        page = 1
        hasNext = false
        error = null
        loadMore(force = true)
    }

    fun loadMore(force: Boolean = false) {
        if (lists.isEmpty() || (!force && (loading || !hasNext))) return
        val q = lists[tab].query.copy(
            page = page,
            perPage = 30,
            genres = genre?.let { listOf(it) },
            formats = format?.let { listOf(it) },
        )
        loading = true
        job = viewModelScope.launch {
            try {
                val p = app.core.browse(q)
                items = (items + p.media.orEmpty()).distinctBy { it.id }
                hasNext = p.hasNext
                page++
            } catch (e: Exception) {
                error = e.message
            }
            loading = false
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DiscoverScreen(onOpen: (id: Int) -> Unit) {
    val app = rememberApp()
    val vm: DiscoverViewModel = viewModel { DiscoverViewModel(app) }
    LifecycleResumeEffect(Unit) {
        vm.refreshStatuses()
        onPauseOrDispose {}
    }

    Column(Modifier.fillMaxSize()) {
        TopAppBar(title = { Text("Discover") })
        if (vm.lists.isNotEmpty()) {
            PrimaryScrollableTabRow(selectedTabIndex = vm.tab, edgePadding = 16.dp) {
                vm.lists.forEachIndexed { i, l ->
                    Tab(selected = vm.tab == i, onClick = { vm.selectTab(i) }, text = { Text(l.title) })
                }
            }
        }
        Row(
            Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            FilterMenu("Genre", vm.genre, vm.genres, label = { it }) { vm.filter(it, vm.format) }
            FilterMenu("Format", vm.format, formats, label = ::formatLabel) { vm.filter(vm.genre, it) }
        }
        // A fresh scroll state for each list, so a new list starts at the top.
        // Sharing one, the grid would keep its place, or follow a show that's
        // also in the new list.
        val grid = remember(vm.tab, vm.genre, vm.format) { LazyGridState() }
        val nearEnd by remember(grid) {
            derivedStateOf {
                val last = grid.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0
                last >= grid.layoutInfo.totalItemsCount - 6
            }
        }
        LaunchedEffect(grid) {
            snapshotFlow { nearEnd }.collect { if (it) vm.loadMore() }
        }
        when {
            vm.items.isEmpty() && vm.error != null -> ErrorState(vm.error.orEmpty(), onRetry = vm::reload)
            vm.items.isEmpty() && (vm.loading || vm.lists.isEmpty()) -> Loading()
            vm.items.isEmpty() -> Message(Icons.Outlined.Explore, "No shows match these filters")
            else -> LazyVerticalGrid(
                state = grid,
                columns = GridCells.Adaptive(120.dp),
                contentPadding = PaddingValues(12.dp),
                modifier = Modifier.fillMaxSize(),
            ) {
                items(vm.items, key = { it.id }) { m ->
                    val status = vm.statuses[m.id]
                    MediaCard(
                        m,
                        onClick = { onOpen(m.id) },
                        subtitle = listOfNotNull(status?.label, m.meta.ifEmpty { null }).joinToString(" · "),
                    )
                }
                if (vm.loading) {
                    item(span = { GridItemSpan(maxLineSpan) }) {
                        Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) { Loading() }
                    }
                }
            }
        }
    }
}

/** A chip that opens a menu of [options], with "Any" to clear it. */
@Composable
private fun <T : Any> FilterMenu(
    name: String,
    selected: T?,
    options: List<T>,
    label: (T) -> String,
    onSelect: (T?) -> Unit,
) {
    var open by remember { mutableStateOf(false) }
    Box {
        FilterChip(
            selected = selected != null,
            onClick = { open = true },
            label = { Text(selected?.let(label) ?: name) },
            trailingIcon = { Icon(Icons.Default.ArrowDropDown, null) },
        )
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            DropdownMenuItem(
                text = { Text("Any ${name.lowercase()}", color = MaterialTheme.colorScheme.onSurfaceVariant) },
                onClick = { open = false; onSelect(null) },
            )
            options.forEach { o ->
                DropdownMenuItem(text = { Text(label(o)) }, onClick = { open = false; onSelect(o) })
            }
        }
    }
}
