package io.github.emofa.tsuzuki.web

import android.annotation.SuppressLint
import android.content.Context
import android.os.Handler
import android.os.Looper
import android.util.Base64
import android.util.Log
import android.view.View
import android.webkit.CookieManager
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.webkit.WebViewCompat
import androidx.webkit.WebViewFeature
import io.github.emofa.tsuzuki.core.json
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import java.io.ByteArrayInputStream
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference

/** What the core asks to capture (mobile/browser.go's sniffRequest). */
@Serializable
private data class SniffRequest(
    @SerialName("URL") val url: String,
    @SerialName("Referer") val referer: String = "",
    @SerialName("Matches") val matches: List<SniffMatch> = emptyList(),
    @SerialName("TimeoutMillis") val timeoutMillis: Long = 30_000,
    @SerialName("Blocked") val blocked: List<String>? = null,
)

@Serializable
private data class SniffMatch(
    @SerialName("Name") val name: String,
    @SerialName("Pattern") val pattern: String,
    @SerialName("Body") val body: Boolean = false,
)

@Serializable
private data class Captured(
    @SerialName("URL") val url: String,
    @SerialName("Status") val status: Int,
    @SerialName("Body") val body: String? = null,
)

/**
 * Runs a page in a WebView nobody sees and reports the requests it makes, so
 * the core can use a site's own player when only its obfuscated JavaScript
 * knows the stream (Anikoto's megaplay embeds). The desktop does this with a
 * headless Chrome.
 */
object WebSniffer {
    private const val TAG = "tsuzuki.sniff"

    /** Mutes every audio and video element before it plays. */
    private const val MUTE_MEDIA = """(() => {
        const play = HTMLMediaElement.prototype.play;
        HTMLMediaElement.prototype.play = function () {
            this.muted = true;
            this.volume = 0;
            return play.apply(this, arguments);
        };
    })();"""
    private val main = Handler(Looper.getMainLooper())

    /** Blocks until every match is captured, or the timeout; call off the main thread. */
    @SuppressLint("SetJavaScriptEnabled")
    fun sniff(context: Context, requestJson: String): String {
        val req = json.decodeFromString<SniffRequest>(requestJson)
        val matches = req.matches.map { it to Regex(it.pattern) }
        val blocked = req.blocked.orEmpty().map(::glob)
        val captured = ConcurrentHashMap<String, Captured>()
        val done = CountDownLatch(1)
        val pageError = AtomicReference<String?>(null)
        var view: WebView? = null

        main.post {
            val web = WebView(context.applicationContext)
            view = web
            web.settings.javaScriptEnabled = true
            web.settings.domStorageEnabled = true
            web.settings.mediaPlaybackRequiresUserGesture = false
            // The player has to start for it to ask for its stream, but nobody
            // should hear it: an audible page takes audio focus, pausing music.
            if (WebViewFeature.isFeatureSupported(WebViewFeature.DOCUMENT_START_SCRIPT)) {
                WebViewCompat.addDocumentStartJavaScript(web, MUTE_MEDIA, setOf("*"))
            }
            web.settings.blockNetworkImage = true
            // Read here: WebView may only be touched on the main thread.
            val userAgent = web.settings.userAgentString
            // Never attached to a window, but laid out as a player would be.
            web.measure(
                View.MeasureSpec.makeMeasureSpec(1280, View.MeasureSpec.EXACTLY),
                View.MeasureSpec.makeMeasureSpec(720, View.MeasureSpec.EXACTLY),
            )
            web.layout(0, 0, 1280, 720)
            web.webViewClient = object : WebViewClient() {
                // Called on a background thread for every request the page makes.
                override fun shouldInterceptRequest(v: WebView, r: WebResourceRequest): WebResourceResponse? {
                    val url = r.url.toString()
                    if (blocked.any { it.matches(url) }) return empty()
                    val (m, _) = matches.firstOrNull { (m, re) -> !captured.containsKey(m.name) && re.containsMatchIn(url) }
                        ?: return null
                    if (!m.body || r.method != "GET") {
                        captured[m.name] = Captured(url, 200)
                        if (captured.size == matches.size) done.countDown()
                        return null
                    }
                    // WebView can't hand over a response body, so fetch it here
                    // and give the page the same response.
                    return try {
                        val res = fetch(url, r.requestHeaders, userAgent)
                        captured[m.name] = Captured(url, res.status, Base64.encodeToString(res.body, Base64.NO_WRAP))
                        if (captured.size == matches.size) done.countDown()
                        WebResourceResponse(
                            res.mimeType, res.encoding, res.status, res.reason.ifEmpty { "OK" }, res.headers,
                            ByteArrayInputStream(res.body),
                        )
                    } catch (e: Exception) {
                        Log.w(TAG, "fetching $url for the page", e)
                        null
                    }
                }

                override fun onReceivedHttpError(
                    v: WebView,
                    r: WebResourceRequest,
                    response: WebResourceResponse,
                ) {
                    if (r.isForMainFrame && response.statusCode >= 400) {
                        pageError.set("page ${req.url} returned HTTP ${response.statusCode}")
                        done.countDown()
                    }
                }
            }
            val headers = if (req.referer.isNotEmpty()) mapOf("Referer" to req.referer) else emptyMap()
            web.loadUrl(req.url, headers)
        }

        val finished = done.await(req.timeoutMillis, TimeUnit.MILLISECONDS)
        main.post { view?.destroy() }
        pageError.get()?.let { throw IllegalStateException(it) }
        if (!finished) {
            val missing = req.matches.map { it.name }.filter { !captured.containsKey(it) }
            throw IllegalStateException(
                "timed out after ${req.timeoutMillis / 1000}s waiting for ${missing.joinToString(", ")} from ${req.url}",
            )
        }
        return json.encodeToString(captured.toMap())
    }

    private class Fetched(
        val status: Int,
        val reason: String,
        val mimeType: String,
        val encoding: String,
        val headers: Map<String, String>,
        val body: ByteArray,
    )

    private fun fetch(url: String, requestHeaders: Map<String, String>, userAgent: String): Fetched {
        val conn = URL(url).openConnection() as HttpURLConnection
        try {
            conn.instanceFollowRedirects = true
            requestHeaders.forEach { (k, v) ->
                // The body is handed back as is, so it mustn't come compressed.
                if (!k.equals("Accept-Encoding", true)) conn.setRequestProperty(k, v)
            }
            conn.setRequestProperty("User-Agent", userAgent)
            CookieManager.getInstance().getCookie(url)?.let { conn.setRequestProperty("Cookie", it) }
            val status = conn.responseCode
            val body = (if (status >= 400) conn.errorStream else conn.inputStream)?.use { it.readBytes() } ?: ByteArray(0)
            val contentType = conn.contentType.orEmpty()
            val headers = conn.headerFields
                .filterKeys { it != null && !it.equals("Content-Encoding", true) && !it.equals("Content-Length", true) }
                .mapValues { (_, v) -> v.joinToString(", ") }
            return Fetched(
                status = status,
                reason = conn.responseMessage.orEmpty(),
                mimeType = contentType.substringBefore(';').trim().ifEmpty { "application/octet-stream" },
                encoding = Regex("charset=([^;]+)").find(contentType)?.groupValues?.get(1)?.trim() ?: "utf-8",
                headers = headers,
                body = body,
            )
        } finally {
            conn.disconnect()
        }
    }

    private fun empty() = WebResourceResponse("text/plain", "utf-8", 204, "No Content", emptyMap(), ByteArrayInputStream(ByteArray(0)))

    /** A pattern with * as a wildcard, matching a whole URL. */
    private fun glob(pattern: String): Regex =
        Regex(pattern.split("*").joinToString(".*") { Regex.escape(it) })
}
