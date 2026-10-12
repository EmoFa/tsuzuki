package io.github.emofa.tsuzuki.player

import android.app.PendingIntent
import android.content.Intent
import androidx.media3.session.MediaSession
import androidx.media3.session.MediaSessionService
import io.github.emofa.tsuzuki.TsuzukiApplication
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch

/**
 * Keeps a watch playing with the screen off or the app in the background, and
 * gives it a media notification and lock-screen controls. It runs from the
 * start of a watch to its end, following the episode playing now.
 */
class PlaybackService : MediaSessionService() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)
    private var session: MediaSession? = null

    override fun onCreate() {
        super.onCreate()
        val watch = (application as TsuzukiApplication).watch
        scope.launch {
            watch.state
                .map { st -> st?.session?.takeUnless { st.over } to (st == null || st.over) }
                .distinctUntilChanged()
                .collect { (playing, over) ->
                    if (playing?.player !== session?.player) follow(playing)
                    if (over) stopSelf()
                }
        }
    }

    /** Points the media session at [playing]'s player, or at nothing between episodes. */
    private fun follow(playing: PlayerSession?) {
        session?.let {
            removeSession(it)
            it.release()
        }
        session = playing?.let { p ->
            MediaSession.Builder(this, p.player)
                .setId("watch-${System.identityHashCode(p)}")
                .setSessionActivity(
                    PendingIntent.getActivity(
                        this, 0,
                        Intent(this, PlayerActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
                        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
                    ),
                )
                .build()
                .also { addSession(it) }
        }
    }

    override fun onGetSession(controllerInfo: MediaSession.ControllerInfo): MediaSession? = session

    override fun onDestroy() {
        scope.cancel()
        session?.release()
        session = null
        super.onDestroy()
    }
}
