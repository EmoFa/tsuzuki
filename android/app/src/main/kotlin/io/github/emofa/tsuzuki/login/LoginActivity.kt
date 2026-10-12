package io.github.emofa.tsuzuki.login

import android.annotation.SuppressLint
import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.ui.Modifier
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.foundation.layout.padding
import androidx.lifecycle.lifecycleScope
import io.github.emofa.tsuzuki.TsuzukiApplication
import io.github.emofa.tsuzuki.ui.theme.TsuzukiTheme
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch

/**
 * AniList's approval page, in the app. AniList then redirects to tsuzuki's
 * login callback, which the core serves on localhost while it waits, so the
 * login completes inside this WebView; the core's Login call then returns and
 * [done] closes the page.
 */
class LoginActivity : ComponentActivity() {
    @OptIn(ExperimentalMaterial3Api::class)
    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val url = intent.getStringExtra(EXTRA_URL) ?: return finish()
        done.value = false
        lifecycleScope.launch {
            done.first { it }
            finish()
        }
        setContent {
            TsuzukiTheme {
                BackHandler { cancel() }
                Scaffold(topBar = {
                    TopAppBar(
                        title = { Text("Log in to AniList") },
                        navigationIcon = {
                            IconButton(onClick = { cancel() }) { Icon(Icons.Default.Close, "Cancel") }
                        },
                    )
                }) { padding ->
                    Column(Modifier.padding(padding)) {
                        AndroidView(
                            factory = { ctx ->
                                WebView(ctx).apply {
                                    settings.javaScriptEnabled = true
                                    settings.domStorageEnabled = true
                                    webViewClient = object : WebViewClient() {
                                        override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest) = false
                                    }
                                    loadUrl(url)
                                }
                            },
                            modifier = Modifier.fillMaxSize(),
                        )
                    }
                }
            }
        }
    }

    private fun cancel() {
        val core = (application as TsuzukiApplication).core
        lifecycleScope.launch {
            runCatching { core.cancelLogin() }
            finish()
        }
    }

    companion object {
        private const val EXTRA_URL = "url"

        /** Set once the core has the login, to close the page. */
        val done = MutableStateFlow(false)

        fun intent(context: Context, url: String) =
            Intent(context, LoginActivity::class.java).putExtra(EXTRA_URL, url)
    }
}
