package io.github.emofa.tsuzuki.player

import android.content.Context
import android.content.pm.ApplicationInfo
import android.net.Uri
import android.os.Handler
import android.os.Looper
import androidx.annotation.OptIn
import androidx.media3.common.AudioAttributes
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.MimeTypes
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DefaultHttpDataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.util.EventLogger
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import io.github.emofa.tsuzuki.core.PlayRequest
import io.github.emofa.tsuzuki.core.SubtitleTrack
import io.github.emofa.tsuzuki.go.mobile.Playback
import io.github.emofa.tsuzuki.go.mobile.Player as GoPlayer
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import kotlin.time.Duration.Companion.milliseconds
import kotlin.time.Duration.Companion.nanoseconds

/**
 * One episode playing in ExoPlayer, as tsuzuki's session drives it: it reports
 * what the player does to [playback] and carries out the session's commands.
 * The player lives here rather than in an activity, so it survives rotation and
 * picture-in-picture.
 */
@OptIn(UnstableApi::class)
class PlayerSession private constructor(
    val request: PlayRequest,
    private val playback: Playback,
    /** The show's cover, for the media notification. */
    private val artwork: String?,
) : GoPlayer {
    private val main = Handler(Looper.getMainLooper())
    lateinit var player: ExoPlayer
        private set

    private val _osd = MutableStateFlow<String?>(null)

    /** A short message over the video, such as "Skipped opening". */
    val osd: StateFlow<String?> = _osd.asStateFlow()

    /** The message the skip button sends; null until the session offers one. */
    @Volatile
    var skipMessage: String? = null
        private set

    private var subtitles: List<SubtitleTrack> = initialSubtitles(request)
    private var ended = false
    private val clearOsd = Runnable { _osd.value = null }

    private val ticker = object : Runnable {
        override fun run() {
            if (ended) return
            reportPosition()
            main.postDelayed(this, POSITION_INTERVAL_MS)
        }
    }

    private fun create(context: Context) {
        val http = DefaultHttpDataSource.Factory()
            .setAllowCrossProtocolRedirects(true)
            .setDefaultRequestProperties(request.headers.orEmpty())
        request.headers?.entries?.firstOrNull { it.key.equals("User-Agent", true) }?.let {
            http.setUserAgent(it.value)
        }
        player = ExoPlayer.Builder(context)
            .setMediaSourceFactory(DefaultMediaSourceFactory(http))
            .setHandleAudioBecomingNoisy(true)
            // Pause for calls and other apps' audio, and resume after.
            .setAudioAttributes(
                AudioAttributes.Builder().setUsage(C.USAGE_MEDIA).setContentType(C.AUDIO_CONTENT_TYPE_MOVIE).build(),
                true,
            )
            .build()
        player.trackSelectionParameters = player.trackSelectionParameters.buildUpon().apply {
            request.subLangs?.let { setPreferredTextLanguages(*it.toTypedArray()) }
            if (request.audioLang.isNotEmpty()) setPreferredAudioLanguage(request.audioLang)
            setTrackTypeDisabled(C.TRACK_TYPE_TEXT, request.hideSubs)
        }.build()
        player.addListener(listener)
        // Debug builds log every load and track change, for diagnosing streams.
        if (context.applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE != 0) player.addAnalyticsListener(EventLogger())
        player.setMediaItem(mediaItem(), request.startNs.nanoseconds.inWholeMilliseconds)
        player.playWhenReady = true
        player.prepare()
        main.post(ticker)
    }

    private fun mediaItem(): MediaItem = MediaItem.Builder()
        .setUri(request.url)
        .setMimeType(
            when (request.kind) {
                "hls" -> MimeTypes.APPLICATION_M3U8
                "mp4" -> MimeTypes.VIDEO_MP4
                else -> null
            }
        )
        .setMediaMetadata(
            MediaMetadata.Builder()
                .setTitle(request.title)
                .setArtworkUri(artwork?.takeIf { it.isNotEmpty() }?.let(Uri::parse))
                .build(),
        )
        .setSubtitleConfigurations(subtitles.mapIndexed { i, track ->
            MediaItem.SubtitleConfiguration.Builder(Uri.parse(track.url))
                .setMimeType(subtitleMimeType(track.url))
                .setLanguage(track.lang.ifEmpty { null })
                .setLabel(track.title.ifEmpty { null })
                // The first track is the preferred one (domain.SortSubtitles).
                .setSelectionFlags(if (i == 0) C.SELECTION_FLAG_DEFAULT else 0)
                .build()
        })
        .build()

    private val listener = object : Player.Listener {
        private var reportedDuration = 0L

        override fun onPlaybackStateChanged(state: Int) {
            when (state) {
                Player.STATE_READY -> {
                    val d = player.duration
                    if (d != C.TIME_UNSET && d != reportedDuration) {
                        reportedDuration = d
                        playback.duration(d.milliseconds.inWholeNanoseconds)
                    }
                }
                Player.STATE_ENDED -> end("eof", "")
            }
        }

        override fun onPlayWhenReadyChanged(playWhenReady: Boolean, reason: Int) {
            playback.paused(!playWhenReady)
        }

        override fun onPositionDiscontinuity(
            oldPosition: Player.PositionInfo,
            newPosition: Player.PositionInfo,
            reason: Int,
        ) {
            if (reason == Player.DISCONTINUITY_REASON_SEEK) {
                playback.seeked(newPosition.positionMs.milliseconds.inWholeNanoseconds)
            }
        }

        override fun onPlayerError(error: PlaybackException) {
            end("error", error.message ?: error.errorCodeName)
        }
    }

    private fun reportPosition() {
        val d = player.duration.takeIf { it != C.TIME_UNSET } ?: 0
        playback.position(
            player.currentPosition.milliseconds.inWholeNanoseconds,
            d.milliseconds.inWholeNanoseconds,
        )
    }

    /** Reports the end once, and lets the player go. Main thread only. */
    private fun end(reason: String, error: String) {
        if (ended) return
        reportPosition()
        ended = true
        main.removeCallbacks(ticker)
        player.removeListener(listener)
        player.release()
        playback.ended(reason, error)
        onEnded?.invoke(this)
    }

    /** Called on the main thread once playback is over. */
    var onEnded: ((PlayerSession) -> Unit)? = null

    /** The user asked to skip. */
    fun skip() {
        skipMessage?.let { playback.message(it) }
    }

    /** The user left the player. */
    fun quit() {
        main.post { end("quit", "") }
    }

    // The session's commands arrive on Go's threads.

    override fun seekTo(pos: Long) {
        main.post { if (!ended) player.seekTo(pos.nanoseconds.inWholeMilliseconds) }
    }

    override fun showText(text: String, d: Long) {
        main.post {
            _osd.value = text
            main.removeCallbacks(clearOsd)
            main.postDelayed(clearOsd, d.nanoseconds.inWholeMilliseconds)
        }
    }

    override fun addSubtitle(url: String, title: String, lang: String) {
        main.post {
            if (ended || subtitles.any { it.url == url }) return@post
            // ExoPlayer only takes side-loaded subtitles with the media item.
            subtitles = subtitles + SubtitleTrack(url, title, lang)
            val pos = player.currentPosition
            player.setMediaItem(mediaItem(), pos)
            player.prepare()
        }
    }

    override fun bindKey(key: String, message: String) {
        skipMessage = message
    }

    override fun stop() = quit()

    companion object {
        private const val POSITION_INTERVAL_MS = 500L

        /**
         * Starts playing [request]. Called on a Go thread; the player is made on
         * the main thread, which this waits for.
         */
        fun start(context: Context, request: PlayRequest, playback: Playback, artwork: String?): PlayerSession {
            val session = PlayerSession(request, playback, artwork)
            val made = CountDownLatch(1)
            var error: Throwable? = null
            Handler(Looper.getMainLooper()).post {
                try {
                    session.create(context)
                } catch (e: Throwable) {
                    error = e
                }
                made.countDown()
            }
            if (!made.await(10, TimeUnit.SECONDS)) throw IllegalStateException("the player didn't start")
            error?.let { throw it }
            return session
        }

        /** Every subtitle the request names, the preferred first. */
        private fun initialSubtitles(r: PlayRequest): List<SubtitleTrack> {
            val first = r.subtitles.orEmpty().map { SubtitleTrack(it) }
            return (first + r.moreSubtitles.orEmpty()).distinctBy { it.url }
        }

        private fun subtitleMimeType(url: String): String {
            val path = Uri.parse(url).lastPathSegment.orEmpty().lowercase()
            return when {
                path.endsWith(".srt") -> MimeTypes.APPLICATION_SUBRIP
                path.endsWith(".ass") || path.endsWith(".ssa") -> MimeTypes.TEXT_SSA
                path.endsWith(".ttml") || path.endsWith(".xml") -> MimeTypes.APPLICATION_TTML
                else -> MimeTypes.TEXT_VTT
            }
        }
    }
}
