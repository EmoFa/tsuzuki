package io.github.emofa.tsuzuki.ui.theme

import android.content.Context
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.toArgb
import androidx.core.content.edit
import androidx.core.graphics.ColorUtils
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/** The colours the app can take. Pink is the 続き logo's, and the terminal's accent. */
enum class Accent(val label: String, val seed: Color) {
    Pink("Pink", Color(0xFFCF8598)),
    Lavender("Lavender", Color(0xFFA98BDB)),
    Blue("Blue", Color(0xFF6E9DD6)),
    Teal("Teal", Color(0xFF4EB5AA)),
    Green("Green", Color(0xFF7FB56B)),
    Amber("Amber", Color(0xFFE0A35A)),
    Red("Red", Color(0xFFE07272)),
}

enum class Mode(val label: String) {
    Dark("Dark"),
    Light("Light"),
    System("Follow system"),
}

data class Appearance(val accent: Accent = Accent.Pink, val mode: Mode = Mode.Dark)

/** The app's appearance, kept on the phone: it's the app's own, not tsuzuki's config. */
class AppearanceStore(context: Context) {
    private val prefs = context.getSharedPreferences("appearance", Context.MODE_PRIVATE)
    private val _state = MutableStateFlow(
        Appearance(
            accent = Accent.entries.firstOrNull { it.name == prefs.getString("accent", null) } ?: Accent.Pink,
            mode = Mode.entries.firstOrNull { it.name == prefs.getString("mode", null) } ?: Mode.Dark,
        ),
    )
    val state: StateFlow<Appearance> = _state.asStateFlow()

    fun set(a: Appearance) {
        _state.value = a
        prefs.edit {
            putString("accent", a.accent.name)
            putString("mode", a.mode.name)
        }
    }
}

/**
 * A full Material colour scheme from one accent: containers, surfaces and
 * text all take its hue, so every accent looks of a piece in both modes.
 */
fun schemeFor(accent: Accent, dark: Boolean): ColorScheme {
    val hsl = FloatArray(3)
    ColorUtils.colorToHSL(accent.seed.toArgb(), hsl)
    val h = hsl[0]
    val s = hsl[1]
    fun c(sat: Float, light: Float, hue: Float = h) = Color(ColorUtils.HSLToColor(floatArrayOf((hue + 360) % 360, sat.coerceIn(0f, 1f), light)))
    return if (dark) {
        darkColorScheme(
            primary = accent.seed,
            onPrimary = c(s, 0.14f),
            primaryContainer = c(s * 0.55f, 0.27f),
            onPrimaryContainer = c(s, 0.90f),
            inversePrimary = c(s, 0.40f),
            secondary = c(0.25f, 0.80f),
            onSecondary = c(0.25f, 0.20f),
            secondaryContainer = c(0.20f, 0.30f),
            onSecondaryContainer = c(0.40f, 0.90f),
            tertiary = c(s * 0.6f, 0.76f, h + 40),
            onTertiary = c(s * 0.6f, 0.18f, h + 40),
            background = c(0.16f, 0.08f),
            onBackground = c(0.10f, 0.91f),
            surface = c(0.16f, 0.08f),
            onSurface = c(0.10f, 0.91f),
            surfaceVariant = c(0.10f, 0.30f),
            onSurfaceVariant = c(0.12f, 0.80f),
            surfaceContainerLowest = c(0.16f, 0.05f),
            surfaceContainerLow = c(0.14f, 0.10f),
            surfaceContainer = c(0.13f, 0.12f),
            surfaceContainerHigh = c(0.12f, 0.16f),
            surfaceContainerHighest = c(0.11f, 0.20f),
            outline = c(0.08f, 0.58f),
            outlineVariant = c(0.08f, 0.30f),
        )
    } else {
        lightColorScheme(
            // The seed itself is too pale for text and icons on white.
            primary = c(s, minOf(hsl[2], 0.42f)),
            onPrimary = Color.White,
            primaryContainer = c(s, 0.90f),
            onPrimaryContainer = c(s, 0.15f),
            inversePrimary = accent.seed,
            secondary = c(0.20f, 0.40f),
            onSecondary = Color.White,
            secondaryContainer = c(0.35f, 0.90f),
            onSecondaryContainer = c(0.30f, 0.15f),
            tertiary = c(s * 0.6f, 0.40f, h + 40),
            onTertiary = Color.White,
            background = c(0.40f, 0.985f),
            onBackground = c(0.15f, 0.10f),
            surface = c(0.40f, 0.985f),
            onSurface = c(0.15f, 0.10f),
            surfaceVariant = c(0.25f, 0.90f),
            onSurfaceVariant = c(0.10f, 0.30f),
            surfaceContainerLowest = Color.White,
            surfaceContainerLow = c(0.35f, 0.965f),
            surfaceContainer = c(0.30f, 0.945f),
            surfaceContainerHigh = c(0.28f, 0.925f),
            surfaceContainerHighest = c(0.25f, 0.905f),
            outline = c(0.08f, 0.50f),
            outlineVariant = c(0.15f, 0.82f),
        )
    }
}
