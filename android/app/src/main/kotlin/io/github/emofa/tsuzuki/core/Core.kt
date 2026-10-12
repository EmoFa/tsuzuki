package io.github.emofa.tsuzuki.core

import io.github.emofa.tsuzuki.go.mobile.App as GoApp
import io.github.emofa.tsuzuki.go.mobile.StatusListener
import io.github.emofa.tsuzuki.go.mobile.Watching
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json

/** An error from tsuzuki's core, with its message ready to show. */
class CoreException(message: String) : Exception(message)

val json = Json {
    ignoreUnknownKeys = true
    explicitNulls = false
    coerceInputValues = true
    encodeDefaults = true
}

/**
 * Kotlin's view of tsuzuki's Go core. Every call runs off the main thread and
 * throws [CoreException] with Go's error message. The core starts on the first
 * call (it opens the database), so that's off the main thread too.
 */
class Core(start: () -> GoApp) {
    private val go by lazy(start)

    private suspend fun <T> call(block: (GoApp) -> T): T = withContext(Dispatchers.IO) {
        try {
            block(go)
        } catch (e: CoreException) {
            throw e
        } catch (e: Exception) {
            throw CoreException(e.message ?: e.toString())
        }
    }

    private inline fun <reified T> decode(s: String): T = json.decodeFromString(s)
    private inline fun <reified T> list(s: String): List<T> = decode<List<T>?>(s).orEmpty()

    /** Continue watching for null, otherwise the list entries with [status]. */
    suspend fun home(status: ListStatus?): HomeTab = call { decode(it.home(status?.key ?: "")) }

    suspend fun config(): Config = call { decode(it.config()) }

    suspend fun configOptions(): ConfigOptions = call { decode(it.configOptions()) }

    /** Checks setup, services and providers; takes up to a minute. */
    suspend fun diagnose(streams: Boolean): List<Diagnosis> = call { list(it.diagnose(streams)) }

    /** Forgets every site's stored bot-check clearance. */
    suspend fun clearVerifications(): Unit = call { it.clearVerifications() }

    /** Looks for a newer release for this phone's [abi]; at most daily. */
    suspend fun checkUpdate(abi: String, firstNotice: Boolean): AppUpdate =
        call { decode(it.checkUpdate(abi, firstNotice)) }

    suspend fun version(): String = call { it.version() }

    /** Saves new settings; throws with what's wrong when they're invalid. */
    suspend fun setConfig(config: Config): Unit = call { it.setConfig(json.encodeToString(config)) }

    suspend fun search(query: String): List<Media> = call { list(it.search(query)) }

    suspend fun browse(query: BrowseQuery): BrowsePage =
        call { decode(it.browse(json.encodeToString(query))) }

    suspend fun discoverLists(): List<DiscoverList> = call { list(it.discoverLists()) }

    suspend fun genres(): List<String> = call { list(it.genres()) }

    suspend fun media(id: Int): Media = call { decode(it.media(id.toLong())) }

    suspend fun sequels(id: Int): List<Media> = call { list(it.sequels(id.toLong())) }

    suspend fun prequels(id: Int): List<Media> = call { list(it.prequels(id.toLong())) }

    /** The latest progress of each recently watched show, newest first. */
    suspend fun recentShows(limit: Int = 30): List<Progress> = call { list(it.recentShows(limit.toLong())) }

    suspend fun showProgress(id: Int): List<Progress> = call { list(it.showProgress(id.toLong())) }

    suspend fun episodes(media: Media, mode: String, remote: Boolean): EpisodeList =
        call { decode(it.episodes(json.encodeToString(media), mode, remote)) }

    /** List entries with status, or every entry for null. */
    suspend fun listEntries(status: ListStatus? = null): List<ListEntry> =
        call { list(it.listEntries(status?.key ?: "")) }

    suspend fun listEntry(id: Int): ListEntry? = call { decode(it.listEntry(id.toLong())) }

    /** Each returns a note to show the user. */
    suspend fun setListStatus(id: Int, status: ListStatus): String =
        call { it.setListStatus(id.toLong(), status.key) }

    suspend fun setScore(id: Int, score: Double): String = call { it.setScore(id.toLong(), score) }

    suspend fun setWatched(media: Media, from: Double, to: Double, watched: Boolean): String =
        call { it.setWatched(json.encodeToString(media), from, to, watched) }

    suspend fun startRewatch(id: Int): String = call { it.startRewatch(id.toLong()) }

    suspend fun round(id: Int): Round = call { decode(it.round(id.toLong())) }

    suspend fun showPrefs(id: Int): ShowPrefs? = call { decode(it.showPrefs(id.toLong())) }

    suspend fun saveShowPrefs(prefs: ShowPrefs): Unit = call { it.saveShowPrefs(json.encodeToString(prefs)) }

    suspend fun deleteShowPrefs(id: Int): Unit = call { it.deleteShowPrefs(id.toLong()) }

    suspend fun account(): Account = call { decode(it.account()) }

    /** Logs in to AniList through the login page, returning the user name. */
    suspend fun login(): String = call { it.login() }

    /** Gives up on a login in progress. */
    suspend fun cancelLogin(): Unit = call { it.cancelLogin() }

    suspend fun logout(): Unit = call { it.logout() }

    suspend fun sync(): String = call { it.sync() }

    /** Starts watching; [onStatus] is called on a background thread. */
    suspend fun watch(request: WatchRequest, onStatus: (WatchStatus) -> Unit): Watching = call {
        it.watch(json.encodeToString(request), StatusListener { s ->
            onStatus(json.decodeFromString<WatchStatus>(s))
        })
    }
}
