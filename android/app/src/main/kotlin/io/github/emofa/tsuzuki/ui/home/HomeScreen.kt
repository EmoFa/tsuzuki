package io.github.emofa.tsuzuki.ui.home

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.VideoLibrary
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledIconButton
import androidx.compose.material3.Icon
import androidx.compose.material3.PrimaryScrollableTabRow
import androidx.compose.material3.Tab
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.LifecycleResumeEffect
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import io.github.emofa.tsuzuki.TsuzukiApplication
import io.github.emofa.tsuzuki.core.HomeItem
import io.github.emofa.tsuzuki.core.HomeTab
import io.github.emofa.tsuzuki.core.ListStatus
import io.github.emofa.tsuzuki.core.WatchRequest
import io.github.emofa.tsuzuki.core.episodeLabel
import io.github.emofa.tsuzuki.ui.LocalSnackbar
import io.github.emofa.tsuzuki.ui.common.ErrorState
import io.github.emofa.tsuzuki.ui.common.Loading
import io.github.emofa.tsuzuki.ui.common.MediaCard
import io.github.emofa.tsuzuki.ui.common.Message
import io.github.emofa.tsuzuki.ui.common.rememberApp
import io.github.emofa.tsuzuki.watch.WatchController
import kotlinx.coroutines.launch

/** Continue watching, then the list's statuses. */
private val homeTabs: List<Pair<String, ListStatus?>> =
    listOf("Continue watching" to null) + ListStatus.entries.map { it.label to it }

sealed interface TabState {
    data object Loading : TabState
    data class Loaded(val tab: HomeTab) : TabState
    data class Failed(val message: String) : TabState
}

class HomeViewModel(private val app: TsuzukiApplication) : ViewModel() {
    val tabs = mutableStateMapOf<Int, TabState>()
    var defaultMode by mutableStateOf("sub")
        private set

    init {
        viewModelScope.launch { runCatching { defaultMode = app.core.config().general.mode } }
    }

    /** The page being refreshed by a pull, or -1. */
    var refreshing by mutableStateOf(-1)
        private set

    fun load(index: Int, pulled: Boolean = false) {
        if (tabs[index] == null) tabs[index] = TabState.Loading
        if (pulled) refreshing = index
        viewModelScope.launch {
            tabs[index] = try {
                TabState.Loaded(app.core.home(homeTabs[index].second))
            } catch (e: Exception) {
                TabState.Failed(e.message.orEmpty())
            }
            if (refreshing == index) refreshing = -1
        }
    }

    /** Plays the next episode of [item], or says why there isn't one. */
    fun resume(item: HomeItem): String? {
        val m = item.media
        return when {
            m.notYetAired -> "${m.displayTitle} hasn't aired yet · ${m.premiereLabel()}"
            item.caughtUp -> "You're caught up on ${m.displayTitle}."
            else -> {
                app.watch.watch(
                    WatchRequest(
                        media = m,
                        mode = item.progress?.mode?.ifEmpty { null } ?: defaultMode,
                        provider = item.progress?.provider.orEmpty(),
                    )
                )
                null
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HomeScreen(onOpen: (id: Int, mode: String) -> Unit) {
    val app = rememberApp()
    val vm: HomeViewModel = viewModel { HomeViewModel(app) }
    val pager = rememberPagerState { homeTabs.size }
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current

    // Back from the player or details, what's on the tab may have changed.
    LifecycleResumeEffect(pager.currentPage) {
        vm.load(pager.currentPage)
        onPauseOrDispose {}
    }

    Column(Modifier.fillMaxSize()) {
        TopAppBar(title = { Text("続き tsuzuki") })
        PrimaryScrollableTabRow(selectedTabIndex = pager.currentPage, edgePadding = 16.dp) {
            homeTabs.forEachIndexed { i, (label, _) ->
                Tab(
                    selected = pager.currentPage == i,
                    onClick = { scope.launch { pager.animateScrollToPage(i) } },
                    text = { Text(label) },
                )
            }
        }
        HorizontalPager(pager, Modifier.fillMaxSize()) { page ->
            LaunchedEffect(page) { if (vm.tabs[page] == null) vm.load(page) }
            PullToRefreshBox(isRefreshing = vm.refreshing == page, onRefresh = { vm.load(page, pulled = true) }) {
                when (val st = vm.tabs[page] ?: TabState.Loading) {
                    TabState.Loading -> Loading()
                    is TabState.Failed -> ErrorState(st.message, onRetry = { vm.load(page) })
                    is TabState.Loaded -> HomeGrid(
                        st.tab,
                        continueTab = homeTabs[page].second == null,
                        onOpen = { onOpen(it.media.id, it.progress?.mode.orEmpty()) },
                        onResume = { item -> vm.resume(item)?.let { scope.launch { snackbar.showSnackbar(it) } } },
                    )
                }
            }
        }
    }
}

@Composable
private fun HomeGrid(
    tab: HomeTab,
    continueTab: Boolean,
    onOpen: (HomeItem) -> Unit,
    onResume: (HomeItem) -> Unit,
) {
    val items = tab.items.orEmpty()
    if (items.isEmpty()) {
        when {
            tab.allFinished -> Message(Icons.Outlined.CheckCircle, "All caught up", "Everything you've watched is finished. Find something new in Discover.")
            continueTab -> Message(Icons.Outlined.VideoLibrary, "Nothing to continue yet", "Search for a show or browse Discover, and it'll wait here for you.")
            else -> Message(Icons.Outlined.VideoLibrary, "Nothing here yet")
        }
        return
    }
    LazyVerticalGrid(
        columns = GridCells.Adaptive(120.dp),
        contentPadding = PaddingValues(12.dp),
        modifier = Modifier.fillMaxSize(),
    ) {
        items(items, key = { it.media.id }) { item ->
            Box {
                MediaCard(
                    item.media,
                    onClick = { onOpen(item) },
                    subtitle = itemLine(item),
                    progress = item.progress?.takeIf { !it.completed && it.durationNs > 0 }
                        ?.let { (it.positionNs.toFloat() / it.durationNs).coerceIn(0f, 1f) },
                )
                if (!item.caughtUp && !item.media.notYetAired) {
                    FilledIconButton(
                        onClick = { onResume(item) },
                        modifier = Modifier.align(Alignment.TopEnd).padding(10.dp).size(36.dp),
                    ) {
                        Icon(Icons.Default.PlayArrow, "Continue")
                    }
                }
            }
        }
    }
}

/** What the card says about where the show is up to. */
private fun itemLine(item: HomeItem): String {
    val p = item.progress
    val e = item.entry
    val m = item.media
    return when {
        m.notYetAired -> m.premiereLabel()
        p != null && !p.completed && p.durationNs > 0 ->
            "Episode ${episodeLabel(p.episode)} · ${WatchController.clock(p.durationNs - p.positionNs)} left"
        item.caughtUp -> "Caught up"
        p != null -> "Next: episode ${episodeLabel(p.next)}"
        e != null && e.score > 0 -> "${e.progress}/${m.episodes.takeIf { it > 0 } ?: "?"} · ★ ${e.score}"
        e != null -> "${e.progress}/${m.episodes.takeIf { it > 0 } ?: "?"} watched"
        else -> m.meta
    }
}
