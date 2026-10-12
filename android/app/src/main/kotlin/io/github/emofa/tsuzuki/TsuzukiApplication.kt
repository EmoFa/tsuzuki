package io.github.emofa.tsuzuki

import android.app.Application
import android.content.Intent
import io.github.emofa.tsuzuki.core.Core
import io.github.emofa.tsuzuki.go.mobile.Host
import io.github.emofa.tsuzuki.go.mobile.Mobile
import io.github.emofa.tsuzuki.go.mobile.Playback
import io.github.emofa.tsuzuki.go.mobile.Player
import io.github.emofa.tsuzuki.login.LoginActivity
import io.github.emofa.tsuzuki.ui.theme.AppearanceStore
import io.github.emofa.tsuzuki.update.Updater
import io.github.emofa.tsuzuki.watch.WatchController
import io.github.emofa.tsuzuki.web.ChallengeActivity
import io.github.emofa.tsuzuki.web.WebSniffer

class TsuzukiApplication : Application() {
    lateinit var core: Core
        private set
    lateinit var watch: WatchController
        private set
    lateinit var updater: Updater
        private set
    lateinit var appearance: AppearanceStore
        private set

    override fun onCreate() {
        super.onCreate()
        core = Core { Mobile.newApp(filesDir.path, cacheDir.path, host) }
        watch = WatchController(this) { core }
        updater = Updater(this) { core }
        appearance = AppearanceStore(this)
    }

    /** What tsuzuki's core asks of the app. Its calls come on Go threads. */
    private val host = object : Host {
        override fun openURL(url: String) {
            startActivity(LoginActivity.intent(this@TsuzukiApplication, url).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        }

        override fun play(requestJSON: String, playback: Playback): Player = watch.play(requestJSON, playback)

        override fun sniff(requestJSON: String): String = WebSniffer.sniff(this@TsuzukiApplication, requestJSON)

        override fun solve(pageURL: String): String = ChallengeActivity.solve(this@TsuzukiApplication, pageURL)
    }
}
