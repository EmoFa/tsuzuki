package io.github.emofa.tsuzuki.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import io.github.emofa.tsuzuki.TsuzukiApplication

/** Whether the app is dark now: its setting, or the phone's when it follows the system. */
@Composable
fun isAppDark(): Boolean {
    val appearance by appearanceStore().state.collectAsState()
    return when (appearance.mode) {
        Mode.Dark -> true
        Mode.Light -> false
        Mode.System -> isSystemInDarkTheme()
    }
}

@Composable
fun appearanceStore(): AppearanceStore =
    (LocalContext.current.applicationContext as TsuzukiApplication).appearance

/** The app's colours: the chosen accent, in its mode, or dark when [dark] says so. */
@Composable
fun TsuzukiTheme(dark: Boolean? = null, content: @Composable () -> Unit) {
    val appearance by appearanceStore().state.collectAsState()
    val isDark = dark ?: isAppDark()
    val colors = remember(appearance.accent, isDark) { schemeFor(appearance.accent, isDark) }
    MaterialTheme(colorScheme = colors, content = content)
}
