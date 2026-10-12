package io.github.emofa.tsuzuki.ui.details

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import io.github.emofa.tsuzuki.TsuzukiApplication
import io.github.emofa.tsuzuki.core.Config
import io.github.emofa.tsuzuki.core.EpisodeList
import io.github.emofa.tsuzuki.core.ListEntry
import io.github.emofa.tsuzuki.core.ListStatus
import io.github.emofa.tsuzuki.core.Media
import io.github.emofa.tsuzuki.core.ShowPrefs
import io.github.emofa.tsuzuki.core.WatchRequest
import io.github.emofa.tsuzuki.core.episodeLabel
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.launch

/** A show's details screen: its episodes, list entry and settings. */
class DetailsViewModel(
    private val app: TsuzukiApplication,
    private val id: Int,
    requestedMode: String,
) : ViewModel() {
    private val core get() = app.core

    var media by mutableStateOf<Media?>(null)
        private set
    var error by mutableStateOf<String?>(null)
        private set
    var episodes by mutableStateOf<EpisodeList?>(null)
        private set
    var entry by mutableStateOf<ListEntry?>(null)
        private set
    var sequels by mutableStateOf<List<Media>>(emptyList())
        private set
    var prequels by mutableStateOf<List<Media>>(emptyList())
        private set
    var prefs by mutableStateOf<ShowPrefs?>(null)
        private set
    var config by mutableStateOf(Config())
        private set
    var mode by mutableStateOf(requestedMode)
        private set
    /** Something is being changed (marks, status, rewatch). */
    var busy by mutableStateOf(false)
        private set

    private val _notes = Channel<String>(Channel.BUFFERED)

    /** Notes for the user, such as "Set to Planning". */
    val notes = _notes.receiveAsFlow()

    init {
        load()
    }

    fun load() {
        error = null
        viewModelScope.launch {
            runCatching { config = core.config() }
            if (mode.isEmpty()) mode = config.general.mode
            val m = try {
                core.media(id)
            } catch (e: Exception) {
                error = e.message
                return@launch
            }
            media = m
            launch { runCatching { sequels = core.sequels(id) } }
            launch { runCatching { prequels = core.prequels(id) } }
            launch { runCatching { prefs = core.showPrefs(id) } }
            refresh()
        }
    }

    /** Reloads what watching or marking changes: episodes and the list entry. */
    fun refresh() {
        val m = media ?: return
        viewModelScope.launch {
            runCatching { entry = core.listEntry(id) }
            try {
                // What's stored first, quickly; then filler and, if needed, the
                // provider's episode list.
                episodes = core.episodes(m, mode, remote = false)
                episodes = core.episodes(m, mode, remote = true)
            } catch (e: Exception) {
                note("Couldn't load episodes: ${e.message}")
            }
        }
    }

    private fun note(s: String) {
        _notes.trySend(s)
    }

    /** Runs a change, then shows its note and reloads. */
    private fun change(failure: String, block: suspend () -> String) {
        viewModelScope.launch {
            busy = true
            try {
                note(block())
            } catch (e: Exception) {
                note("$failure: ${e.message?.lineSequence()?.first()}")
            }
            busy = false
            refresh()
        }
    }

    fun toggleMode() {
        mode = if (mode == "dub") "sub" else "dub"
        note("Mode: $mode")
        refresh()
    }

    /** Plays [episode], or the next one for null. */
    fun play(episode: Double? = null) {
        val m = media ?: return
        if (m.notYetAired) {
            note("${m.displayTitle} hasn't aired yet · ${m.premiereLabel()}")
            return
        }
        val ep = episode ?: episodes?.nextUp ?: 0.0
        app.watch.watch(WatchRequest(media = m, episode = ep, mode = mode))
    }

    fun setWatched(from: Double, to: Double, watched: Boolean) {
        val m = media ?: return
        change("Couldn't change the marks") { core.setWatched(m, from, to, watched) }
    }

    /** Marks every episode up to [episode] watched. */
    fun watchedThrough(episode: Double) {
        val first = episodes?.rows?.firstOrNull()?.number ?: 1.0
        setWatched(first, episode, true)
    }

    fun setStatus(status: ListStatus) = change("Couldn't change list status") { core.setListStatus(id, status) }

    fun setScore(score: Double) = change("Couldn't rate it") { core.setScore(id, score) }

    fun rewatch() = change("Couldn't start a rewatch") { core.startRewatch(id) }

    /** The subtitle settings in force: the show's own, or the defaults. */
    val subtitleLanguages: List<String>
        get() = prefs?.subLanguages ?: config.subtitles.languages.orEmpty()
    val subtitlesShown: Boolean get() = prefs?.subShow ?: config.subtitles.show
    val ownSubtitles: Boolean get() = prefs?.subLanguages != null || prefs?.subShow != null

    fun saveSubtitles(languages: String, show: Boolean) {
        val langs = languages.split(",").map { it.trim().lowercase() }.filter { it.isNotEmpty() }
        viewModelScope.launch {
            try {
                core.saveShowPrefs(ShowPrefs(id, langs, show))
                prefs = core.showPrefs(id)
                note("Subtitles saved for this show")
            } catch (e: Exception) {
                note("Couldn't save subtitles: ${e.message}")
            }
        }
    }

    fun resetSubtitles() {
        viewModelScope.launch {
            try {
                core.deleteShowPrefs(id)
                prefs = null
                note("Subtitles: using your defaults")
            } catch (e: Exception) {
                note("Couldn't reset subtitles: ${e.message}")
            }
        }
    }

    /** What the main button does: continue, start, or nothing yet. */
    val continueLabel: String?
        get() {
            val m = media ?: return null
            val list = episodes ?: return null
            if (m.notYetAired) return null
            val rows = list.rows.orEmpty()
            val next = list.nextUp
            return when {
                rows.isEmpty() -> null
                rows.none { it.state.isNotEmpty() } && list.round == 1 -> "Start watching"
                rows.none { it.number >= next } -> null
                else -> "Continue · Episode ${episodeLabel(next)}"
            }
        }
}
