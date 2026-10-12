package io.github.emofa.tsuzuki.update

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInstaller
import android.net.Uri
import android.os.Build
import android.provider.Settings
import io.github.emofa.tsuzuki.core.AppUpdate
import io.github.emofa.tsuzuki.core.Core
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.withContext
import java.io.File
import java.net.HttpURLConnection
import java.net.URL

/** Where updating stands. */
sealed interface UpdateState {
    data object Idle : UpdateState
    data object Checking : UpdateState
    data class Checked(val update: AppUpdate) : UpdateState
    data class Downloading(val update: AppUpdate, val fraction: Float) : UpdateState
    /** Android is asking the user to confirm the install. */
    data class Installing(val update: AppUpdate) : UpdateState
    data class Failed(val message: String) : UpdateState
}

/**
 * Finds newer releases on GitHub and installs their APK, replacing this
 * install the way the terminal's `tsuzuki upgrade` replaces its binary.
 */
class Updater(private val context: Context, private val core: () -> Core) {
    private val _state = MutableStateFlow<UpdateState>(UpdateState.Idle)
    val state: StateFlow<UpdateState> = _state.asStateFlow()

    private val abi: String get() = Build.SUPPORTED_ABIS.firstOrNull().orEmpty()

    /** Checks for a newer release; returns it when it's the first time it's been seen. */
    suspend fun check(firstNotice: Boolean = false): AppUpdate? {
        _state.value = UpdateState.Checking
        return try {
            val u = core().checkUpdate(abi, firstNotice)
            _state.value = UpdateState.Checked(u)
            u.takeIf { it.notify }
        } catch (e: Exception) {
            _state.value = UpdateState.Failed(e.message.orEmpty())
            null
        }
    }

    /** Whether Android lets this app install updates; if not, [allowInstalls] asks. */
    val canInstall: Boolean get() = context.packageManager.canRequestPackageInstalls()

    /** Opens the setting that lets this app install updates. */
    fun allowInstalls() {
        context.startActivity(
            Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:${context.packageName}"))
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
        )
    }

    /** Downloads [u]'s APK and asks Android to install it. */
    suspend fun install(u: AppUpdate) {
        if (u.apk.isEmpty()) {
            _state.value = UpdateState.Failed("This release has no APK for your phone. Get it from ${u.page}")
            return
        }
        try {
            val apk = download(u)
            _state.value = UpdateState.Installing(u)
            withContext(Dispatchers.IO) { commit(apk) }
        } catch (e: Exception) {
            _state.value = UpdateState.Failed("Couldn't update: ${e.message}")
        }
    }

    private suspend fun download(u: AppUpdate): File = withContext(Dispatchers.IO) {
        val dir = File(context.cacheDir, "updates").apply { mkdirs() }
        dir.listFiles()?.forEach { it.delete() }
        val file = File(dir, "tsuzuki-${u.latest}.apk")
        val conn = URL(u.apk).openConnection() as HttpURLConnection
        try {
            conn.instanceFollowRedirects = true
            if (conn.responseCode !in 200..299) error("GitHub answered HTTP ${conn.responseCode}")
            val total = conn.contentLengthLong
            conn.inputStream.use { input ->
                file.outputStream().use { out ->
                    val buf = ByteArray(64 * 1024)
                    var done = 0L
                    while (true) {
                        val n = input.read(buf)
                        if (n < 0) break
                        out.write(buf, 0, n)
                        done += n
                        if (total > 0) _state.value = UpdateState.Downloading(u, done.toFloat() / total)
                    }
                }
            }
        } finally {
            conn.disconnect()
        }
        file
    }

    private fun commit(apk: File) {
        val installer = context.packageManager.packageInstaller
        val params = PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL)
        params.setAppPackageName(context.packageName)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            params.setRequireUserAction(PackageInstaller.SessionParams.USER_ACTION_NOT_REQUIRED)
        }
        val id = installer.createSession(params)
        installer.openSession(id).use { session ->
            session.openWrite("tsuzuki.apk", 0, apk.length()).use { out ->
                apk.inputStream().use { it.copyTo(out) }
                session.fsync(out)
            }
            val callback = PendingIntent.getBroadcast(
                context, id, Intent(context, InstallReceiver::class.java),
                PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_MUTABLE,
            )
            session.commit(callback.intentSender)
        }
    }
}

/** Hears back from Android's installer, and shows its confirmation when it asks for one. */
class InstallReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        when (intent.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE)) {
            PackageInstaller.STATUS_PENDING_USER_ACTION -> {
                @Suppress("DEPRECATION")
                val confirm = intent.getParcelableExtra<Intent>(Intent.EXTRA_INTENT) ?: return
                context.startActivity(confirm.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
            }
        }
    }
}
