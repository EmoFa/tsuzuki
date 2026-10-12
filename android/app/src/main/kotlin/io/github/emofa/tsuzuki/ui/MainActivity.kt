package io.github.emofa.tsuzuki.ui

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.SystemBarStyle
import androidx.activity.enableEdgeToEdge
import androidx.compose.animation.AnimatedContentTransitionScope
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.slideOutVertically
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.navigation.NavBackStackEntry
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Explore
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.outlined.Explore
import androidx.compose.material.icons.outlined.Home
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarResult
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.navigation.NavDestination.Companion.hasRoute
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.NavHostController
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import androidx.navigation.toRoute
import io.github.emofa.tsuzuki.ui.common.rememberApp
import io.github.emofa.tsuzuki.ui.details.DetailsScreen
import io.github.emofa.tsuzuki.ui.diagnostics.DiagnosticsScreen
import io.github.emofa.tsuzuki.ui.discover.DiscoverScreen
import io.github.emofa.tsuzuki.ui.finish.FinishSheet
import io.github.emofa.tsuzuki.ui.home.HomeScreen
import io.github.emofa.tsuzuki.ui.search.SearchScreen
import io.github.emofa.tsuzuki.ui.settings.SettingsScreen
import io.github.emofa.tsuzuki.ui.settings.SettingsSection
import io.github.emofa.tsuzuki.ui.settings.SettingsSectionScreen
import io.github.emofa.tsuzuki.ui.theme.TsuzukiTheme
import io.github.emofa.tsuzuki.ui.theme.isAppDark
import io.github.emofa.tsuzuki.update.startUpdate
import kotlinx.serialization.Serializable
import kotlin.reflect.KClass

@Serializable object HomeRoute
@Serializable object DiscoverRoute
@Serializable object SearchRoute
@Serializable object SettingsRoute
@Serializable object DiagnosticsRoute

/** A page of the settings; [section] names a [SettingsSection]. */
@Serializable data class SettingsSectionRoute(val section: String)

/** A show's details; [mode] is "sub" or "dub", or empty for the default. */
@Serializable data class DetailsRoute(val id: Int, val mode: String = "")

/** Notes for the user, such as "Set to Planning", from any screen. */
val LocalSnackbar = staticCompositionLocalOf { SnackbarHostState() }

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        setContent {
            // The system bars' icons follow the app's mode, not the phone's.
            val dark = isAppDark()
            DisposableEffect(dark) {
                val style = if (dark) {
                    SystemBarStyle.dark(android.graphics.Color.TRANSPARENT)
                } else {
                    SystemBarStyle.light(android.graphics.Color.TRANSPARENT, android.graphics.Color.TRANSPARENT)
                }
                enableEdgeToEdge(statusBarStyle = style, navigationBarStyle = style)
                onDispose {}
            }
            TsuzukiTheme { App() }
        }
    }
}

private data class Tab(val route: Any, val type: KClass<*>, val label: String, val icon: ImageVector, val selectedIcon: ImageVector)

private val tabs = listOf(
    Tab(HomeRoute, HomeRoute::class, "Home", Icons.Outlined.Home, Icons.Filled.Home),
    Tab(DiscoverRoute, DiscoverRoute::class, "Discover", Icons.Outlined.Explore, Icons.Filled.Explore),
    Tab(SearchRoute, SearchRoute::class, "Search", Icons.Filled.Search, Icons.Filled.Search),
    Tab(SettingsRoute, SettingsRoute::class, "Settings", Icons.Outlined.Settings, Icons.Filled.Settings),
)

@Composable
private fun App() {
    val nav = rememberNavController()
    val snackbar = remember { SnackbarHostState() }
    val entry by nav.currentBackStackEntryAsState()
    val app = rememberApp()
    val finished by app.watch.finished.collectAsState()
    val scope = rememberCoroutineScope()

    // Mention a new release once, like the terminal does.
    LaunchedEffect(Unit) {
        val u = app.updater.check(firstNotice = true) ?: return@LaunchedEffect
        val answer = snackbar.showSnackbar("tsuzuki ${u.latest} is available", "Update", duration = SnackbarDuration.Long)
        if (answer == SnackbarResult.ActionPerformed) scope.startUpdate(app.updater, u, snackbar)
    }

    CompositionLocalProvider(LocalSnackbar provides snackbar) {
        Scaffold(
            snackbarHost = { SnackbarHost(snackbar) },
            bottomBar = {
                // The bar belongs to the top-level screens; details has the whole screen.
                val dest = entry?.destination
                AnimatedVisibility(
                    visible = dest == null || tabs.any { dest.hasRoute(it.type) },
                    enter = slideInVertically(motion()) { it },
                    exit = slideOutVertically(motion()) { it },
                ) {
                    NavigationBar {
                        tabs.forEach { tab ->
                            val selected = dest?.hasRoute(tab.type) == true
                            NavigationBarItem(
                                selected = selected,
                                onClick = { nav.switchTab(tab.route) },
                                icon = { Icon(if (selected) tab.selectedIcon else tab.icon, null) },
                                label = { Text(tab.label) },
                            )
                        }
                    }
                }
            },
        ) { padding ->
            NavHost(
                nav,
                startDestination = HomeRoute,
                modifier = Modifier.padding(padding),
                // Tabs cross-fade; a page slides in over the one it came from,
                // and back slides it away again, the way Android apps move.
                enterTransition = {
                    if (initialState.isTab() && targetState.isTab()) fadeIn(tween(TAB_MS))
                    else slideIntoContainer(AnimatedContentTransitionScope.SlideDirection.Start, motion())
                },
                exitTransition = {
                    if (initialState.isTab() && targetState.isTab()) fadeOut(tween(TAB_MS))
                    else slideOutOfContainer(AnimatedContentTransitionScope.SlideDirection.Start, motion()) { it / 4 }
                },
                popEnterTransition = {
                    slideIntoContainer(AnimatedContentTransitionScope.SlideDirection.End, motion()) { it / 4 }
                },
                popExitTransition = {
                    slideOutOfContainer(AnimatedContentTransitionScope.SlideDirection.End, motion())
                },
                // Holding back (the gesture, or the back button on Android 16)
                // previews the same slide, instead of the default shrink.
                predictivePopEnterTransition = {
                    slideIntoContainer(AnimatedContentTransitionScope.SlideDirection.End, motion()) { it / 4 }
                },
                predictivePopExitTransition = {
                    slideOutOfContainer(AnimatedContentTransitionScope.SlideDirection.End, motion())
                },
            ) {
                val open = { id: Int, mode: String -> nav.navigate(DetailsRoute(id, mode)) }
                composable<HomeRoute> { Page { HomeScreen(onOpen = open) } }
                composable<DiscoverRoute> { Page { DiscoverScreen(onOpen = { open(it, "") }) } }
                composable<SearchRoute> { Page { SearchScreen(onOpen = { open(it, "") }) } }
                composable<SettingsRoute> {
                    Page {
                        SettingsScreen(
                            onSection = { nav.navigate(SettingsSectionRoute(it.name)) },
                            onDiagnostics = { nav.navigate(DiagnosticsRoute) },
                        )
                    }
                }
                composable<SettingsSectionRoute> { back ->
                    val section = SettingsSection.valueOf(back.toRoute<SettingsSectionRoute>().section)
                    Page { SettingsSectionScreen(section, onBack = { nav.popBackStack() }) }
                }
                composable<DiagnosticsRoute> { Page { DiagnosticsScreen(onBack = { nav.popBackStack() }) } }
                composable<DetailsRoute> { back ->
                    val route = back.toRoute<DetailsRoute>()
                    Page {
                        DetailsScreen(
                            id = route.id,
                            mode = route.mode,
                            onBack = { nav.popBackStack() },
                            onOpen = { open(it, route.mode) },
                        )
                    }
                }
            }
        }
        finished?.let { media ->
            FinishSheet(
                media = media,
                onDismiss = app.watch::finishedHandled,
                onOpen = { app.watch.finishedHandled(); nav.navigate(DetailsRoute(it, "")) },
            )
        }
    }
}

private const val TAB_MS = 200

/** The easing and pace of page slides. */
private fun <T> motion() = tween<T>(durationMillis = 320, easing = FastOutSlowInEasing)

private fun NavBackStackEntry.isTab() = tabs.any { destination.hasRoute(it.type) }

/** A screen's opaque backdrop, so a page sliding over another hides it. */
@Composable
private fun Page(content: @Composable () -> Unit) {
    Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background, content = content)
}

private fun NavHostController.switchTab(route: Any) {
    navigate(route) {
        popUpTo(graph.findStartDestination().id) { saveState = true }
        launchSingleTop = true
        restoreState = true
    }
}
