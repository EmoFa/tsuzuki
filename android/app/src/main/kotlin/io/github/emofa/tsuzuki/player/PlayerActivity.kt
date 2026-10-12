package io.github.emofa.tsuzuki.player

import android.app.PictureInPictureParams
import android.content.pm.ActivityInfo
import android.content.res.Configuration
import android.os.Build
import android.os.Bundle
import android.os.PowerManager
import android.os.SystemClock
import android.util.Rational
import android.view.ViewGroup
import android.view.WindowManager
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.annotation.OptIn
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.FastForward
import androidx.compose.material.icons.filled.SkipNext
import androidx.compose.material.icons.filled.SkipPrevious
import androidx.compose.material.icons.filled.SwapHoriz
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.view.WindowCompat
import androidx.lifecycle.Lifecycle
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.media3.common.util.UnstableApi
import androidx.media3.ui.DefaultTimeBar
import androidx.media3.ui.PlayerView
import io.github.emofa.tsuzuki.TsuzukiApplication
import io.github.emofa.tsuzuki.core.episodeLabel
import io.github.emofa.tsuzuki.ui.theme.TsuzukiTheme
import io.github.emofa.tsuzuki.watch.WatchController
import io.github.emofa.tsuzuki.watch.WatchState
import kotlinx.coroutines.delay

/** The full-screen player: video, and what the session is doing. */
class PlayerActivity : ComponentActivity() {
    private val watch: WatchController get() = (application as TsuzukiApplication).watch
    private var inPip by mutableStateOf(false)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        WindowCompat.setDecorFitsSystemWindows(window, false)
        WindowInsetsControllerCompat(window, window.decorView).apply {
            hide(WindowInsetsCompat.Type.systemBars())
            systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        }
        updatePipParams()
        setContent {
            TsuzukiTheme(dark = true) {
                val state by watch.state.collectAsState()
                val st = state
                LaunchedEffect(st?.over) {
                    if (st == null || st.over) finish()
                }
                BackHandler { leave() }
                if (st != null) {
                    PlayerScreen(
                        st,
                        inPip = inPip,
                        onBack = ::leave,
                        onNext = watch::next,
                        onPrevious = watch::previous,
                        onOtherProvider = watch::otherProvider,
                    )
                }
            }
        }
    }

    private fun leave() {
        watch.stop()
        finish()
    }

    override fun onDestroy() {
        super.onDestroy()
        // Closing the player any other way (the task swiped away) stops the watch too.
        if (isFinishing && watch.state.value?.over == false) watch.stop()
        watch.dismiss()
    }

    private fun updatePipParams() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val params = PictureInPictureParams.Builder().setAspectRatio(Rational(16, 9))
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) params.setAutoEnterEnabled(true)
        setPictureInPictureParams(params.build())
    }

    override fun onUserLeaveHint() {
        super.onUserLeaveHint()
        if (Build.VERSION.SDK_INT in Build.VERSION_CODES.O until Build.VERSION_CODES.S &&
            watch.state.value?.session != null
        ) {
            enterPictureInPictureMode(PictureInPictureParams.Builder().setAspectRatio(Rational(16, 9)).build())
        }
    }

    override fun onPictureInPictureModeChanged(isInPictureInPictureMode: Boolean, newConfig: Configuration) {
        super.onPictureInPictureModeChanged(isInPictureInPictureMode, newConfig)
        inPip = isInPictureInPictureMode
        // Leaving PiP without coming back to the screen means its window was
        // closed. The player outlives this activity, so stop it, or it plays
        // on with no way back to the video.
        if (!isInPictureInPictureMode && !lifecycle.currentState.isAtLeast(Lifecycle.State.STARTED)) leave()
    }

    override fun onStop() {
        super.onStop()
        // Some phones stop the activity before saying PiP has ended. A stop
        // while in PiP is the window being closed, unless the screen went off.
        if (inPip && getSystemService(PowerManager::class.java).isInteractive) leave()
    }
}

@OptIn(UnstableApi::class)
@Composable
private fun PlayerScreen(
    st: WatchState,
    inPip: Boolean,
    onBack: () -> Unit,
    onNext: () -> Unit,
    onPrevious: () -> Unit,
    onOtherProvider: () -> Unit,
) {
    var controlsVisible by remember { mutableStateOf(true) }
    val accent = MaterialTheme.colorScheme.primary.toArgb()
    Box(Modifier.fillMaxSize().background(Color.Black)) {
        val session = st.session
        if (session != null) {
            AndroidView(
                factory = { ctx ->
                    PlayerView(ctx).apply {
                        layoutParams = ViewGroup.LayoutParams(
                            ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT,
                        )
                        setShowSubtitleButton(true)
                        // Media3 draws its own seek bar; give it the app's colour.
                        findViewById<DefaultTimeBar>(androidx.media3.ui.R.id.exo_progress)?.apply {
                            setPlayedColor(accent)
                            setScrubberColor(accent)
                        }
                        setShowNextButton(false)
                        setShowPreviousButton(false)
                        setControllerVisibilityListener(
                            PlayerView.ControllerVisibilityListener { v -> controlsVisible = v == android.view.View.VISIBLE },
                        )
                    }
                },
                update = { view ->
                    if (view.player !== session.player) {
                        view.player = session.player
                        // Show the controls as each episode starts; they and the
                        // title bar then fade out together.
                        if (!inPip) view.showController()
                    }
                    view.useController = !inPip
                },
                modifier = Modifier.fillMaxSize(),
            )
        } else {
            // Between episodes, or still finding the first one.
            Column(
                Modifier.align(Alignment.Center).padding(24.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                if (!st.over) CircularProgressIndicator()
                Spacer(Modifier.size(16.dp))
                st.log.takeLast(3).forEach { line ->
                    Text(line, color = Color.White.copy(alpha = 0.8f), style = MaterialTheme.typography.bodyMedium)
                }
            }
        }

        if (inPip) return@Box

        AnimatedVisibility(
            visible = controlsVisible || session == null,
            enter = fadeIn(),
            exit = fadeOut(),
            modifier = Modifier.align(Alignment.TopStart),
        ) {
            TopBar(st, onBack, onNext, onPrevious, onOtherProvider)
        }

        session?.let { s ->
            val osd by s.osd.collectAsState()
            osd?.let {
                Surface(
                    color = Color.Black.copy(alpha = 0.6f),
                    shape = RoundedCornerShape(8.dp),
                    modifier = Modifier.align(Alignment.TopCenter).safeDrawingPadding().padding(top = 56.dp),
                ) {
                    Text(it, color = Color.White, modifier = Modifier.padding(horizontal = 12.dp, vertical = 6.dp))
                }
            }
            SkipButton(st, s, controlsVisible, onSkip = s::skip, modifier = Modifier.align(Alignment.BottomEnd))
        }
    }
}

@Composable
private fun TopBar(
    st: WatchState,
    onBack: () -> Unit,
    onNext: () -> Unit,
    onPrevious: () -> Unit,
    onOtherProvider: () -> Unit,
) {
    Row(
        Modifier
            .fillMaxWidth()
            .background(Color.Black.copy(alpha = 0.45f))
            .safeDrawingPadding()
            .padding(horizontal = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        IconButton(onClick = onBack) {
            Icon(Icons.AutoMirrored.Filled.ArrowBack, "Stop", tint = Color.White)
        }
        Column(Modifier.weight(1f).padding(horizontal = 4.dp)) {
            Text(
                st.title, color = Color.White, style = MaterialTheme.typography.titleMedium,
                maxLines = 1, overflow = TextOverflow.Ellipsis,
            )
            val sub = buildList {
                if (st.episode > 0) add("Episode ${episodeLabel(st.episode)}")
                if (st.provider.isNotEmpty()) add(st.provider)
                if (st.streamLabel.isNotEmpty()) add(st.streamLabel)
                if (st.watched) add("✓ watched")
            }.joinToString(" · ")
            if (sub.isNotEmpty()) {
                Text(sub, color = Color.White.copy(alpha = 0.7f), style = MaterialTheme.typography.bodySmall, maxLines = 1)
            }
        }
        IconButton(onClick = onPrevious, enabled = st.episode > 1) {
            Icon(Icons.Default.SkipPrevious, "Previous episode", tint = Color.White)
        }
        IconButton(onClick = onNext, enabled = st.episode > 0) {
            Icon(Icons.Default.SkipNext, "Next episode", tint = Color.White)
        }
        IconButton(onClick = onOtherProvider, enabled = st.provider.isNotEmpty()) {
            Icon(Icons.Default.SwapHoriz, "Try another provider", tint = Color.White)
        }
    }
}

/**
 * Offers to skip an opening, ending or recap: by itself when the session
 * first offers it, and again whenever the controls are up while it lasts.
 */
@Composable
private fun SkipButton(
    st: WatchState,
    session: PlayerSession,
    controlsVisible: Boolean,
    onSkip: () -> Unit,
    modifier: Modifier,
) {
    val prompt = st.skipPrompt
    var visible by remember { mutableStateOf(false) }
    LaunchedEffect(prompt, controlsVisible) {
        while (prompt != null) {
            val auto = SystemClock.elapsedRealtime() < prompt.autoUntil
            visible = prompt.covers(session.player.currentPosition) && (auto || controlsVisible)
            delay(250)
        }
        visible = false
    }
    AnimatedVisibility(
        visible = visible,
        enter = fadeIn(),
        exit = fadeOut(),
        modifier = modifier.safeDrawingPadding().padding(end = 24.dp, bottom = 96.dp),
    ) {
        ExtendedFloatingActionButton(
            onClick = onSkip,
            icon = { Icon(Icons.Default.FastForward, null) },
            text = { Text("Skip ${prompt?.kind.orEmpty()}") },
            modifier = Modifier.widthIn(min = 120.dp),
        )
    }
}
