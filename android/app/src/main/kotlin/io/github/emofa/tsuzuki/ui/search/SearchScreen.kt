package io.github.emofa.tsuzuki.ui.search

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Clear
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.outlined.SearchOff
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import io.github.emofa.tsuzuki.TsuzukiApplication
import io.github.emofa.tsuzuki.core.Media
import io.github.emofa.tsuzuki.ui.common.ErrorState
import io.github.emofa.tsuzuki.ui.common.Loading
import io.github.emofa.tsuzuki.ui.common.MediaRow
import io.github.emofa.tsuzuki.ui.common.Message
import io.github.emofa.tsuzuki.ui.common.rememberApp
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

class SearchViewModel(private val app: TsuzukiApplication) : ViewModel() {
    var query by mutableStateOf("")
        private set
    var results by mutableStateOf<List<Media>?>(null)
        private set
    var loading by mutableStateOf(false)
        private set
    var error by mutableStateOf<String?>(null)
        private set
    private var job: Job? = null

    /** Searches as the user types, once they pause. */
    fun type(q: String) {
        query = q
        search(debounce = true)
    }

    fun search(debounce: Boolean = false) {
        job?.cancel()
        val q = query.trim()
        if (q.isEmpty()) {
            results = null
            loading = false
            error = null
            return
        }
        job = viewModelScope.launch {
            if (debounce) delay(400)
            loading = true
            error = null
            try {
                results = app.core.search(q)
            } catch (e: Exception) {
                error = e.message
            }
            loading = false
        }
    }
}

@Composable
fun SearchScreen(onOpen: (id: Int) -> Unit) {
    val app = rememberApp()
    val vm: SearchViewModel = viewModel { SearchViewModel(app) }
    val focus = remember { FocusRequester() }
    val keyboard = LocalSoftwareKeyboardController.current
    LaunchedEffect(Unit) { if (vm.query.isEmpty()) focus.requestFocus() }

    Column(Modifier.fillMaxSize()) {
        OutlinedTextField(
            value = vm.query,
            onValueChange = vm::type,
            placeholder = { Text("Search anime") },
            leadingIcon = { Icon(Icons.Default.Search, null) },
            trailingIcon = {
                if (vm.query.isNotEmpty()) {
                    IconButton(onClick = { vm.type("") }) { Icon(Icons.Default.Clear, "Clear") }
                }
            },
            singleLine = true,
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
            keyboardActions = KeyboardActions(onSearch = {
                keyboard?.hide()
                vm.search()
            }),
            modifier = Modifier.fillMaxWidth().padding(16.dp).focusRequester(focus),
        )
        val results = vm.results
        when {
            vm.error != null -> ErrorState(vm.error.orEmpty(), onRetry = { vm.search() })
            vm.loading && results == null -> Loading()
            results == null -> Message(Icons.Default.Search, "Find a show", "Search AniList by any of a show's titles.")
            results.isEmpty() -> Message(Icons.Outlined.SearchOff, "No shows match “${vm.query.trim()}”")
            else -> LazyColumn(Modifier.fillMaxSize()) {
                items(results, key = { it.id }) { m ->
                    MediaRow(m, onClick = {
                        keyboard?.hide()
                        onOpen(m.id)
                    })
                }
            }
        }
    }
}
