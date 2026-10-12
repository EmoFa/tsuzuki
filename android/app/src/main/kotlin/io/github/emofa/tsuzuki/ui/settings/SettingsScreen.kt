package io.github.emofa.tsuzuki.ui.settings

import androidx.activity.ComponentActivity
import androidx.activity.compose.LocalActivity
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.outlined.AccountCircle
import androidx.compose.material.icons.outlined.FastForward
import androidx.compose.material.icons.outlined.Hub
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.MonitorHeart
import androidx.compose.material.icons.outlined.PlayCircle
import androidx.compose.material.icons.outlined.Subtitles
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.outlined.Palette
import androidx.compose.runtime.collectAsState
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import io.github.emofa.tsuzuki.ui.theme.Accent
import io.github.emofa.tsuzuki.ui.theme.Appearance
import io.github.emofa.tsuzuki.ui.theme.Mode
import io.github.emofa.tsuzuki.ui.theme.appearanceStore
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import io.github.emofa.tsuzuki.TsuzukiApplication
import io.github.emofa.tsuzuki.core.Account
import io.github.emofa.tsuzuki.core.Config
import io.github.emofa.tsuzuki.core.ConfigOptions
import io.github.emofa.tsuzuki.login.LoginActivity
import io.github.emofa.tsuzuki.ui.LocalSnackbar
import io.github.emofa.tsuzuki.ui.common.Loading
import io.github.emofa.tsuzuki.ui.common.rememberApp
import io.github.emofa.tsuzuki.update.UpdateRow
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.launch

class SettingsViewModel(private val app: TsuzukiApplication) : ViewModel() {
    private val core get() = app.core
    var config by mutableStateOf<Config?>(null)
        private set
    var options by mutableStateOf(ConfigOptions())
        private set
    var account by mutableStateOf(Account())
        private set
    var version by mutableStateOf("")
        private set
    var working by mutableStateOf<String?>(null)
        private set

    private val _notes = Channel<String>(Channel.BUFFERED)
    val notes = _notes.receiveAsFlow()

    init {
        viewModelScope.launch {
            runCatching { options = core.configOptions() }
            runCatching { version = core.version() }
            load()
        }
    }

    private suspend fun load() {
        try {
            config = core.config()
            account = core.account()
        } catch (e: Exception) {
            _notes.send(e.message.orEmpty())
        }
    }

    /** Saves a change; the screen shows the saved settings, or why not. */
    fun update(change: (Config) -> Config) {
        val cur = config ?: return
        val next = change(cur)
        config = next
        viewModelScope.launch {
            try {
                core.setConfig(next)
            } catch (e: Exception) {
                _notes.send("Couldn't save: ${e.message}")
            }
            load()
        }
    }

    fun login() = run("Logging in…") {
        try {
            val name = core.login()
            LoginActivity.done.value = true
            account = core.account()
            if (account.syncs) {
                "Logged in as $name. " + runCatching { core.sync() }.getOrElse { "Couldn't fetch your list yet: ${it.message}" }
            } else {
                "Logged in as $name. Tracking is local, so nothing syncs until you switch it to AniList."
            }
        } finally {
            LoginActivity.done.value = true
        }
    }

    fun logout() = run(null) {
        core.logout()
        account = core.account()
        "Logged out. Your list stays on this phone."
    }

    fun sync() = run("Syncing…") { core.sync() }

    private fun run(progress: String?, block: suspend () -> String) {
        viewModelScope.launch {
            working = progress
            val note = try {
                block()
            } catch (e: Exception) {
                e.message.orEmpty()
            }
            working = null
            _notes.send(note)
        }
    }
}

/** Settings' pages. */
enum class SettingsSection(val title: String, val icon: ImageVector) {
    Appearance("Appearance", Icons.Outlined.Palette),
    AniList("AniList", Icons.Outlined.AccountCircle),
    Playback("Playback", Icons.Outlined.PlayCircle),
    Skipping("Skipping", Icons.Outlined.FastForward),
    Subtitles("Subtitles", Icons.Outlined.Subtitles),
    Providers("Providers", Icons.Outlined.Hub),
    About("About", Icons.Outlined.Info),
}

/** One settings view model for the list and its pages, so they agree. */
@Composable
private fun settingsViewModel(): SettingsViewModel {
    val app = rememberApp()
    val activity = LocalActivity.current as ComponentActivity
    return viewModel(viewModelStoreOwner = activity) { SettingsViewModel(app) }
}

/** The settings' sections, each with what it's set to now. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(onSection: (SettingsSection) -> Unit, onDiagnostics: () -> Unit) {
    val vm = settingsViewModel()
    val snackbar = LocalSnackbar.current
    LaunchedEffect(vm) { vm.notes.collect { snackbar.showSnackbar(it) } }

    Column(Modifier.fillMaxSize()) {
        TopAppBar(title = { Text("Settings") })
        val cfg = vm.config
        if (cfg == null) {
            Loading()
        } else Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
            val look by appearanceStore().state.collectAsState()
            SettingsSection.entries.forEach { section ->
                ListItem(
                    leadingContent = { Icon(section.icon, null, tint = MaterialTheme.colorScheme.primary) },
                    headlineContent = { Text(section.title) },
                    supportingContent = { Text(summary(section, cfg, vm, look), maxLines = 1, overflow = TextOverflow.Ellipsis) },
                    modifier = Modifier.clickable { onSection(section) },
                )
            }
            ListItem(
                leadingContent = { Icon(Icons.Outlined.MonitorHeart, null, tint = MaterialTheme.colorScheme.primary) },
                headlineContent = { Text("Diagnostics") },
                supportingContent = { Text("Check AniList, the providers and site verifications") },
                modifier = Modifier.clickable(onClick = onDiagnostics),
            )
        }
    }
}

/** What a section is set to, in a line. */
private fun summary(section: SettingsSection, cfg: Config, vm: SettingsViewModel, look: Appearance): String = when (section) {
    SettingsSection.Appearance -> "${look.mode.label} · ${look.accent.label}"
    SettingsSection.AniList -> when {
        cfg.tracking.backend == "local" -> "Not syncing: your list stays on this phone"
        vm.account.loggedIn -> "Syncing as ${vm.account.user}"
        else -> "Not logged in"
    }
    SettingsSection.Playback -> listOf(
        if (cfg.general.mode == "dub") "Dub" else "Sub",
        qualityLabel(cfg.general.quality),
        if (cfg.general.autoplayNext) "autoplay on" else "autoplay off",
    ).joinToString(" · ")
    SettingsSection.Skipping -> "Openings: ${skipShort(cfg.skip.opening)} · Endings: ${skipShort(cfg.skip.ending)} · " +
        "Recaps: ${skipShort(cfg.skip.recap)}"
    SettingsSection.Subtitles -> cfg.subtitles.languages.orEmpty().joinToString(", ").ifEmpty { "Any language" } +
        if (cfg.subtitles.show) "" else " · hidden at first"
    SettingsSection.Providers -> cfg.providers.order.orEmpty().joinToString(", ") { providerLabel(it) }
    SettingsSection.About -> "Version ${vm.version}"
}

private fun skipShort(a: String) = when (a) {
    "auto" -> "skip"
    "prompt" -> "button"
    else -> "off"
}

/** One section of the settings. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsSectionScreen(section: SettingsSection, onBack: () -> Unit) {
    val vm = settingsViewModel()
    val snackbar = LocalSnackbar.current
    LaunchedEffect(vm) { vm.notes.collect { snackbar.showSnackbar(it) } }

    Column(Modifier.fillMaxSize()) {
        TopAppBar(
            title = { Text(section.title) },
            navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back") } },
        )
        val cfg = vm.config ?: return@Column Loading()
        Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
            when (section) {
                SettingsSection.Appearance -> AppearanceSection()
                SettingsSection.AniList -> AccountSection(vm, cfg)
                SettingsSection.Playback -> {
                    Choice("Audio", cfg.general.mode, vm.options.modes, label = { if (it == "dub") "Dub" else "Sub" }) { v ->
                        vm.update { it.copy(general = it.general.copy(mode = v)) }
                    }
                    Choice("Quality", cfg.general.quality, vm.options.qualities, label = ::qualityLabel) { v ->
                        vm.update { it.copy(general = it.general.copy(quality = v)) }
                    }
                    Toggle("Play the next episode automatically", cfg.general.autoplayNext) { v ->
                        vm.update { it.copy(general = it.general.copy(autoplayNext = v)) }
                    }
                    Choice(
                        "Counts as watched at",
                        cfg.general.watchedThreshold.toString(),
                        listOf("0.75", "0.8", "0.85", "0.9", "0.95", "1.0"),
                        label = { "${(it.toDouble() * 100).toInt()}% of the episode" },
                    ) { v -> vm.update { it.copy(general = it.general.copy(watchedThreshold = v.toDouble())) } }
                    Choice(
                        "When resuming, go back",
                        cfg.general.resumeRewindSeconds.toString(),
                        listOf("0", "5", "10", "30"),
                        label = { if (it == "0") "No" else "$it seconds" },
                    ) { v -> vm.update { it.copy(general = it.general.copy(resumeRewindSeconds = v.toInt())) } }
                }
                SettingsSection.Skipping -> {
                    listOf(
                        Triple("Openings", cfg.skip.opening) { c: Config, v: String -> c.copy(skip = c.skip.copy(opening = v)) },
                        Triple("Endings", cfg.skip.ending) { c: Config, v: String -> c.copy(skip = c.skip.copy(ending = v)) },
                        Triple("Recaps", cfg.skip.recap) { c: Config, v: String -> c.copy(skip = c.skip.copy(recap = v)) },
                    ).forEach { (name, value, set) ->
                        Choice(name, value, vm.options.skipActions, label = ::skipLabel) { v -> vm.update { set(it, v) } }
                    }
                    Toggle("Skip filler episodes when autoplaying", cfg.skip.fillerEpisodes) { v ->
                        vm.update { it.copy(skip = it.skip.copy(fillerEpisodes = v)) }
                    }
                    Toggle("Skip recap episodes when autoplaying", cfg.skip.recapEpisodes) { v ->
                        vm.update { it.copy(skip = it.skip.copy(recapEpisodes = v)) }
                    }
                }
                SettingsSection.Subtitles -> {
                    Languages(cfg.subtitles.languages.orEmpty()) { langs ->
                        vm.update { it.copy(subtitles = it.subtitles.copy(languages = langs)) }
                    }
                    Toggle("Show subtitles when an episode starts", cfg.subtitles.show) { v ->
                        vm.update { it.copy(subtitles = it.subtitles.copy(show = v)) }
                    }
                }
                SettingsSection.Providers -> Providers(cfg.providers.order.orEmpty(), vm.options.providers) { order ->
                    vm.update { it.copy(providers = it.providers.copy(order = order)) }
                }
                SettingsSection.About -> {
                    ListItem(headlineContent = { Text("tsuzuki") }, supportingContent = { Text("Version ${vm.version}") })
                    UpdateRow()
                    ListItem(
                        headlineContent = { Text("Source code") },
                        supportingContent = { Text("github.com/EmoFa/tsuzuki") },
                    )
                }
            }
            Spacer(Modifier.height(32.dp))
        }
    }
}

/** Light or dark, and the app's colour. */
@Composable
private fun AppearanceSection() {
    val store = appearanceStore()
    val look by store.state.collectAsState()
    Choice("Theme", look.mode.name, Mode.entries.map { it.name }, label = { Mode.valueOf(it).label }) {
        store.set(look.copy(mode = Mode.valueOf(it)))
    }
    ListItem(headlineContent = { Text("Colour") }, supportingContent = { Text(look.accent.label) })
    // One row, spread across the width, whatever the screen's density.
    Row(
        Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        Accent.entries.forEach { accent ->
            val selected = accent == look.accent
            Box(
                Modifier
                    .size(40.dp)
                    .clip(CircleShape)
                    .background(accent.seed)
                    .border(if (selected) 3.dp else 0.dp, MaterialTheme.colorScheme.onSurface, CircleShape)
                    .clickable { store.set(look.copy(accent = accent)) }
                    .semantics { contentDescription = accent.label },
                contentAlignment = Alignment.Center,
            ) {
                if (selected) Icon(Icons.Default.Check, null, tint = Color.Black.copy(alpha = 0.7f))
            }
        }
    }
}

@Composable
private fun AccountSection(vm: SettingsViewModel, cfg: Config) {
    val acc = vm.account
    ListItem(
        headlineContent = {
            Text(
                when {
                    acc.loggedIn -> "Logged in as ${acc.user}"
                    acc.expired -> "Login for ${acc.user} has expired"
                    else -> "Not logged in"
                },
            )
        },
        supportingContent = {
            Text(
                when {
                    cfg.tracking.backend == "local" -> "Tracking is local: your list stays on this phone."
                    acc.loggedIn -> "Each episode you finish is recorded on AniList."
                    else -> "Log in to keep your AniList up to date."
                },
            )
        },
    )
    Row(Modifier.padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        if (acc.loggedIn) {
            OutlinedButton(onClick = vm::logout, enabled = vm.working == null) { Text("Log out") }
            if (acc.syncs) Button(onClick = vm::sync, enabled = vm.working == null) { Text("Sync now") }
        } else {
            Button(onClick = vm::login, enabled = vm.working == null) { Text("Log in") }
        }
    }
    vm.working?.let {
        Text(it, Modifier.padding(16.dp, 8.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
    Toggle("Sync my list with AniList", cfg.tracking.backend == "anilist") { on ->
        vm.update { it.copy(tracking = it.tracking.copy(backend = if (on) "anilist" else "local")) }
    }
}

@Composable
private fun Toggle(title: String, checked: Boolean, onChange: (Boolean) -> Unit) {
    ListItem(
        headlineContent = { Text(title) },
        trailingContent = { Switch(checked = checked, onCheckedChange = onChange) },
        modifier = Modifier.clickable { onChange(!checked) },
    )
}

/** A setting with a few values, chosen in a dialog. */
@Composable
private fun Choice(title: String, value: String, options: List<String>, label: (String) -> String, onChange: (String) -> Unit) {
    var open by remember { mutableStateOf(false) }
    ListItem(
        headlineContent = { Text(title) },
        supportingContent = { Text(label(value)) },
        modifier = Modifier.clickable { open = true },
    )
    if (open) {
        AlertDialog(
            onDismissRequest = { open = false },
            title = { Text(title) },
            text = {
                Column {
                    options.forEach { o ->
                        Row(
                            Modifier.fillMaxWidth().clickable { open = false; onChange(o) }.padding(vertical = 4.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            RadioButton(selected = o == value, onClick = { open = false; onChange(o) })
                            Text(label(o))
                        }
                    }
                }
            },
            confirmButton = { TextButton(onClick = { open = false }) { Text("Cancel") } },
        )
    }
}

@Composable
private fun Languages(languages: List<String>, onChange: (List<String>) -> Unit) {
    var open by remember { mutableStateOf(false) }
    ListItem(
        headlineContent = { Text("Languages") },
        supportingContent = { Text(languages.joinToString(", ").ifEmpty { "Any" } + " · shows can have their own") },
        modifier = Modifier.clickable { open = true },
    )
    if (open) {
        var text by remember { mutableStateOf(languages.joinToString(", ")) }
        AlertDialog(
            onDismissRequest = { open = false },
            title = { Text("Subtitle languages") },
            text = {
                Column {
                    Text("Language codes, most preferred first.", style = MaterialTheme.typography.bodyMedium)
                    Spacer(Modifier.height(8.dp))
                    OutlinedTextField(value = text, onValueChange = { text = it }, singleLine = true, placeholder = { Text("en, es") })
                }
            },
            confirmButton = {
                TextButton(onClick = {
                    open = false
                    onChange(text.split(",").map { it.trim().lowercase() }.filter { it.isNotEmpty() })
                }) { Text("Save") }
            },
            dismissButton = { TextButton(onClick = { open = false }) { Text("Cancel") } },
        )
    }
}

/** The providers to try, in order; unticked ones aren't used. */
@Composable
private fun Providers(order: List<String>, all: List<String>, onChange: (List<String>) -> Unit) {
    val rows = order + all.filter { it !in order }
    Text(
        "Tried from the top until one has the episode.",
        style = MaterialTheme.typography.bodyMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(horizontal = 16.dp),
    )
    rows.forEach { name ->
        val on = name in order
        val i = order.indexOf(name)
        ListItem(
            leadingContent = {
                Checkbox(checked = on, onCheckedChange = { checked ->
                    if (checked) onChange(order + name) else if (order.size > 1) onChange(order - name)
                })
            },
            headlineContent = { Text(providerLabel(name)) },
            trailingContent = {
                if (on) {
                    Row {
                        IconButton(onClick = { onChange(order.move(i, i - 1)) }, enabled = i > 0) {
                            Icon(Icons.Default.KeyboardArrowUp, "Move up")
                        }
                        IconButton(onClick = { onChange(order.move(i, i + 1)) }, enabled = i < order.size - 1) {
                            Icon(Icons.Default.KeyboardArrowDown, "Move down")
                        }
                    }
                }
            },
        )
    }
}

private fun List<String>.move(from: Int, to: Int): List<String> =
    toMutableList().apply { add(to, removeAt(from)) }

private fun providerLabel(name: String) = when (name) {
    "anikoto" -> "Anikoto"
    "senshi" -> "Senshi"
    "animepahe" -> "AnimePahe"
    "kickassanime" -> "KickAssAnime"
    else -> name
}

private fun qualityLabel(q: String) = when (q) {
    "best" -> "Best available"
    "worst" -> "Lowest available"
    else -> "${q}p"
}

private fun skipLabel(a: String) = when (a) {
    "auto" -> "Skip automatically"
    "prompt" -> "Offer a Skip button"
    "off" -> "Don't skip"
    else -> a
}
