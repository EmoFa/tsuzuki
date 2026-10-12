package io.github.emofa.tsuzuki.watch

import android.content.Context
import android.content.Intent
import android.os.SystemClock
import android.util.Log
import io.github.emofa.tsuzuki.core.Core
import io.github.emofa.tsuzuki.core.Media
import io.github.emofa.tsuzuki.core.PlayRequest
import io.github.emofa.tsuzuki.core.WatchRequest
import io.github.emofa.tsuzuki.core.WatchStatus
import io.github.emofa.tsuzuki.core.episodeLabel
import io.github.emofa.tsuzuki.core.json
import io.github.emofa.tsuzuki.go.mobile.Playback
import io.github.emofa.tsuzuki.go.mobile.Watching
import io.github.emofa.tsuzuki.player.PlaybackService
import io.github.emofa.tsuzuki.player.PlayerActivity
import io.github.emofa.tsuzuki.player.PlayerSession
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlin.time.Duration.Companion.nanoseconds

/** A watch as the player screen shows it. */
data class WatchState(
    val request: WatchRequest,
    val episode: Double,
    val provider: String = "",
    val streamLabel: String = "",
    /** The episode playing now; null while one is being found. */
    val session: PlayerSession? = null,
    /** What's happening, most recent last, like the terminal's log. */
    val log: List<String> = emptyList(),
    val skipPrompt: SkipPrompt? = null,
    val watched: Boolean = false,
    /** The last episode of a finished show was watched. */
    val finishedShow: Boolean = false,
    /** The watch is over: the player closes. */
    val over: Boolean = false,
    val error: String? = null,
) {
    val title: String get() = request.media.displayTitle
    val status: String? get() = log.lastOrNull()
}

/**
 * The session offers to skip [kind], which runs from [startMs] to [endMs] in
 * the episode. The button shows by itself until [autoUntil] (elapsed
 * realtime), and after that whenever the player's controls are up.
 */
data class SkipPrompt(val kind: String, val startMs: Long, val endMs: Long, val autoUntil: Long) {
    fun covers(positionMs: Long) = positionMs in (startMs - 1000) until endMs

    companion object {
        /** How long the button shows by itself. */
        const val AUTO_MS = 10_000L
    }
}

/**
 * Runs watches: tsuzuki's session finds and plays episodes, and this connects
 * it to the player screen. One watch runs at a time.
 */
class WatchController(private val context: Context, private val core: () -> Core) {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)
    private val _state = MutableStateFlow<WatchState?>(null)
    val state: StateFlow<WatchState?> = _state.asStateFlow()

    /** A show whose last episode was just watched, for the finishing sheet. */
    private val _finished = MutableStateFlow<Media?>(null)
    val finished: StateFlow<Media?> = _finished.asStateFlow()

    private var watching: Watching? = null
    private var job: Job? = null
    private var generation = 0

    /** Starts watching [request], replacing any watch running now. */
    fun watch(request: WatchRequest) {
        val gen = ++generation
        val previous = job
        val old = watching
        _state.update {
            WatchState(
                request = request,
                episode = request.episode,
                log = it?.takeIf { s -> !s.over }?.log.orEmpty() + startLine(request),
            )
        }
        job = scope.launch {
            // The old watch saves its progress before the new one starts, so
            // switching provider resumes where it stopped.
            old?.cancel()
            previous?.join()
            val error = try {
                val w = core().watch(request) { st -> scope.launch { if (gen == generation) apply(st) } }
                watching = w
                withContext(Dispatchers.IO) { runCatching { w.await() }.exceptionOrNull() }
            } catch (e: Exception) {
                e
            }
            if (gen == generation) {
                watching = null
                _state.update { it?.copy(over = true, session = null, error = error?.message) }
                if (_state.value?.finishedShow == true) _finished.value = request.media
            }
        }
        if (_state.value != null) showPlayer()
        // Keeps the watch going with the screen off; it stops itself at the end.
        context.startService(Intent(context, PlaybackService::class.java))
    }

    /** The user left the player. */
    fun stop() {
        watching?.cancel()
        _state.value?.session?.quit()
    }

    /** Clears a watch that's over, once the player has closed. */
    fun dismiss() {
        if (_state.value?.over == true) _state.value = null
    }

    fun next() = jump(+1.0)

    fun previous() = jump(-1.0)

    private fun jump(by: Double) {
        val st = _state.value ?: return
        val episode = st.episode + by
        if (st.episode <= 0 || episode < 1) return
        // A different episode starts afresh: providers skipped for this one may work.
        watch(st.request.copy(episode = episode, provider = st.provider, skipProviders = null))
    }

    /** Replays the current episode from another provider, resuming where it stopped. */
    fun otherProvider() {
        val st = _state.value ?: return
        if (st.provider.isEmpty() || st.episode <= 0) return
        log("Switching away from ${st.provider}…")
        watch(
            st.request.copy(
                episode = st.episode,
                provider = "",
                skipProviders = st.request.skipProviders.orEmpty() + st.provider,
            )
        )
    }

    fun finishedHandled() {
        _finished.value = null
    }

    /** Called by tsuzuki's session, on a Go thread, to play an episode. */
    fun play(requestJson: String, playback: Playback): PlayerSession {
        val request = json.decodeFromString<PlayRequest>(requestJson)
        Log.i(TAG, "playing ${request.kind} ${request.url}")
        val cover = _state.value?.request?.media?.coverImage
        val session = PlayerSession.start(context, request, playback, cover?.extraLarge?.ifEmpty { cover.large })
        session.onEnded = { ended -> _state.update { if (it?.session === ended) it.copy(session = null) else it } }
        _state.update { it?.copy(session = session, skipPrompt = null) }
        return session
    }

    private fun showPlayer() {
        context.startActivity(
            Intent(context, PlayerActivity::class.java).addFlags(
                Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP,
            )
        )
    }

    private fun log(line: String) {
        _state.update { it?.copy(log = (it.log + line).takeLast(LOG_LINES)) }
    }

    private fun startLine(r: WatchRequest): String =
        if (r.episode > 0) "Looking for episode ${episodeLabel(r.episode)}…" else "Looking for where you left off…"

    private fun apply(st: WatchStatus) {
        val cur = _state.value ?: return
        if (st.episode > 0 && st.episode != cur.episode) {
            // Autoplay moved on.
            _state.value = cur.copy(episode = st.episode, watched = false, skipPrompt = null)
        }
        val ep = episodeLabel(st.episode)
        when (st.kind) {
            "resolving" -> log("Looking for episode $ep on ${st.provider}…")
            "provider_failed" -> log("✗ ${st.err.lineSequence().first()}")
            "playing" -> {
                _state.update { it?.copy(provider = st.provider, streamLabel = st.stream.label) }
                var line = "Playing from ${st.provider} (${st.stream.label})"
                if (st.startNs > 0) line += " · resuming at ${clock(st.startNs)}"
                log(line)
            }
            "watched" -> {
                _state.update { it?.copy(watched = true) }
                log("✓ Episode $ep marked as watched")
            }
            "episode_skipped" -> {
                val range = if (st.through > st.episode) "episodes $ep–${episodeLabel(st.through)}" else "episode $ep"
                log("Skipping $range (${st.reason})")
            }
            "skipped" -> {
                _state.update { it?.copy(skipPrompt = null) }
                log("Skipped ${st.reason}")
            }
            "skip_prompt" -> _state.update {
                val start = st.positionNs / 1_000_000
                it?.copy(
                    skipPrompt = SkipPrompt(
                        kind = st.reason,
                        startMs = start,
                        endMs = start + st.durationNs / 1_000_000,
                        autoUntil = SystemClock.elapsedRealtime() + SkipPrompt.AUTO_MS,
                    ),
                )
            }
            "tracked" -> log(if (st.err.isNotEmpty()) "✗ Updating your list: ${st.err}" else st.reason)
            "stopped" -> if (st.reason == "eof") log("Episode $ep finished")
            "no_next_episode" -> log("No episode after $ep is available yet")
            "finished_show" -> _state.update { it?.copy(finishedShow = true) }
        }
    }

    companion object {
        private const val TAG = "tsuzuki.watch"
        private const val LOG_LINES = 6

        fun clock(ns: Long): String {
            val s = ns.nanoseconds.inWholeSeconds
            return if (s >= 3600) "%d:%02d:%02d".format(s / 3600, s / 60 % 60, s % 60)
            else "%d:%02d".format(s / 60, s % 60)
        }
    }
}
