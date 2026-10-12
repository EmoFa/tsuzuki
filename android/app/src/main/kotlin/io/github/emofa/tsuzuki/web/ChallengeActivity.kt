package io.github.emofa.tsuzuki.web

import android.annotation.SuppressLint
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.webkit.CookieManager
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.lifecycle.lifecycleScope
import io.github.emofa.tsuzuki.core.json
import io.github.emofa.tsuzuki.go.mobile.Mobile
import io.github.emofa.tsuzuki.ui.theme.TsuzukiTheme
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import java.util.concurrent.CompletableFuture
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

@Serializable
private data class Clearance(
    @SerialName("UserAgent") val userAgent: String,
    @SerialName("Cookies") val cookies: List<Cookie>,
)

@Serializable
private data class Cookie(@SerialName("Name") val name: String, @SerialName("Value") val value: String)

/**
 * A site's bot check (Cloudflare, DDoS-Guard), shown in a WebView. Automatic
 * checks pass by themselves in a few seconds; a checkbox needs a tap, which
 * tsuzuki never does for you. Once the page is past the check, its cookies and
 * the WebView's User-Agent go to the core, which then fetches as this WebView.
 */
class ChallengeActivity : ComponentActivity() {
    private var result: CompletableFuture<String>? = null
    private var web: WebView? = null

    @OptIn(ExperimentalMaterial3Api::class)
    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val url = intent.getStringExtra(EXTRA_URL)
        result = pending[intent.getIntExtra(EXTRA_ID, -1)]
        if (url == null || result == null) return finish()
        val host = Uri.parse(url).host.orEmpty()

        setContent {
            TsuzukiTheme {
                BackHandler { giveUp() }
                Scaffold(topBar = {
                    TopAppBar(
                        title = {
                            Column {
                                Text("Checking you're not a bot")
                                Text(host, style = MaterialTheme.typography.bodySmall)
                            }
                        },
                        navigationIcon = { IconButton(onClick = ::giveUp) { Icon(Icons.Default.Close, "Cancel") } },
                    )
                }) { padding ->
                    Column(Modifier.padding(padding)) {
                        Text(
                            "$host asks browsers to prove they're human. This usually passes by itself; if a checkbox appears, tap it.",
                            style = MaterialTheme.typography.bodyMedium,
                            modifier = Modifier.padding(16.dp),
                        )
                        AndroidView(
                            factory = { ctx ->
                                WebView(ctx).apply {
                                    web = this
                                    settings.javaScriptEnabled = true
                                    settings.domStorageEnabled = true
                                    webViewClient = WebViewClient()
                                    CookieManager.getInstance().setAcceptCookie(true)
                                    loadUrl(url)
                                }
                            },
                            modifier = Modifier.fillMaxSize(),
                        )
                    }
                }
            }
        }

        // Watch for the page to get past the check.
        lifecycleScope.launch {
            while (true) {
                delay(1000)
                val view = web ?: continue
                if (!stillChallenged(view)) {
                    // Let the site set its cookies after the redirect.
                    delay(500)
                    finishWith(url, view)
                    return@launch
                }
            }
        }
    }

    private suspend fun stillChallenged(view: WebView): Boolean {
        val answer = CompletableFuture<String>()
        view.evaluateJavascript("(${Mobile.ChallengeScript})()") { answer.complete(it) }
        while (!answer.isDone) delay(50)
        return answer.get() != "false"
    }

    private fun finishWith(url: String, view: WebView) {
        val cookies = CookieManager.getInstance().getCookie(url).orEmpty()
            .split(";")
            .mapNotNull { part ->
                val (name, value) = part.trim().split("=", limit = 2).takeIf { it.size == 2 } ?: return@mapNotNull null
                Cookie(name, value)
            }
        CookieManager.getInstance().flush()
        result?.complete(json.encodeToString(Clearance(view.settings.userAgentString, cookies)))
        finish()
    }

    private fun giveUp() {
        result?.complete(json.encodeToString(Clearance("", emptyList())))
        finish()
    }

    override fun onDestroy() {
        super.onDestroy()
        // Closed some other way: the core stops waiting.
        if (isFinishing) result?.complete(json.encodeToString(Clearance("", emptyList())))
        web?.destroy()
    }

    companion object {
        private const val EXTRA_URL = "url"
        private const val EXTRA_ID = "id"
        private val ids = AtomicInteger()
        private val pending = ConcurrentHashMap<Int, CompletableFuture<String>>()

        /** Shows the check and blocks until it's passed or abandoned; call off the main thread. */
        fun solve(context: Context, url: String): String {
            val id = ids.incrementAndGet()
            val future = CompletableFuture<String>()
            pending[id] = future
            try {
                context.startActivity(
                    Intent(context, ChallengeActivity::class.java)
                        .putExtra(EXTRA_URL, url)
                        .putExtra(EXTRA_ID, id)
                        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
                )
                return future.get(5, TimeUnit.MINUTES)
            } finally {
                pending.remove(id)
            }
        }
    }
}
