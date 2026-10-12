package io.github.emofa.tsuzuki.core

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import java.time.Instant
import java.time.LocalDate
import java.time.Month
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.TextStyle
import java.util.Locale
import kotlin.time.Duration
import kotlin.time.Duration.Companion.nanoseconds

// These mirror the Go core's JSON (see mobile/mobile.go). AniList types use
// AniList's field names; tsuzuki's own use their Go names, with durations in
// nanoseconds. Media goes back to Go as it came, so it models every field.

@Serializable
data class Media(
    val id: Int,
    val idMal: Int = 0,
    val title: Title = Title(),
    val synonyms: List<String>? = null,
    val format: String = "",
    val status: String = "",
    val episodes: Int = 0,
    val duration: Int = 0,
    val season: String = "",
    val seasonYear: Int = 0,
    val coverImage: Cover = Cover(),
    val bannerImage: String = "",
    val description: String = "",
    val averageScore: Int = 0,
    val genres: List<String>? = null,
    val isAdult: Boolean = false,
    val nextAiringEpisode: AiringEpisode? = null,
    val startDate: FuzzyDate = FuzzyDate(),
) {
    // These follow anilist.Media's methods of the same names.

    val displayTitle: String get() = title.english.ifEmpty { title.romaji }

    /** The show's first episode hasn't aired. */
    val notYetAired: Boolean
        get() {
            if (status != "NOT_YET_RELEASED") return false
            val e = nextAiringEpisode ?: return true
            if (e.episode > 1) return false
            // Details may be cached from before the premiere.
            return e.airingAt == 0L || Instant.now().isBefore(e.at)
        }

    /** How many episodes exist so far, or 0 when unknown. */
    val airedEpisodes: Int
        get() = when {
            nextAiringEpisode != null -> nextAiringEpisode.episode - 1
            status == "NOT_YET_RELEASED" -> 0
            else -> episodes
        }

    /** When a show that hasn't aired starts, as precisely as AniList knows. */
    fun premiereLabel(zone: ZoneId = ZoneId.systemDefault()): String {
        val e = nextAiringEpisode
        if (e != null && e.episode <= 1 && e.airingAt > 0) {
            val t = e.at.atZone(zone)
            val pattern = if (t.year != LocalDate.now(zone).year) "EEE, MMM d, yyyy" else "EEE, MMM d 'at' HH:mm"
            return "Episode 1 airs " + t.format(DateTimeFormatter.ofPattern(pattern, Locale.ENGLISH))
        }
        val d = startDate
        val month = { m: Int -> Month.of(m).getDisplayName(TextStyle.FULL, Locale.ENGLISH) }
        return when {
            d.year > 0 && d.month > 0 && d.day > 0 -> "Starts ${month(d.month)} ${d.day}, ${d.year}"
            d.year > 0 && d.month > 0 -> "Starts ${month(d.month)} ${d.year}"
            season.isNotEmpty() && seasonYear > 0 ->
                "Expected ${season.lowercase().replaceFirstChar { it.uppercase() }} $seasonYear"
            d.year > 0 -> "Expected ${d.year}"
            else -> "Release date not announced"
        }
    }

    /** "TV · 2023 · 28 episodes · 89%" */
    val meta: String
        get() = buildList {
            if (format.isNotBlank()) add(formatLabel(format))
            if (seasonYear > 0) add(seasonYear.toString())
            when {
                episodes > 0 -> add(if (episodes == 1) "1 episode" else "$episodes episodes")
                airedEpisodes > 0 -> add("$airedEpisodes aired")
            }
            if (averageScore > 0) add("$averageScore%")
        }.joinToString(" · ")

    /** The description without AniList's HTML. */
    val plainDescription: String
        get() = description.replace(Regex("<br\\s*/?>"), "\n").replace(Regex("<[^>]+>"), "")
            .replace("&amp;", "&").replace("&quot;", "\"").replace("&#039;", "'")
            .replace(Regex("\n{3,}"), "\n\n").trim()
}

fun formatLabel(format: String): String = when (format) {
    "TV" -> "TV"
    "TV_SHORT" -> "TV short"
    "MOVIE" -> "Movie"
    "SPECIAL" -> "Special"
    "OVA" -> "OVA"
    "ONA" -> "ONA"
    "MUSIC" -> "Music"
    else -> format.lowercase().replaceFirstChar { it.uppercase() }
}

@Serializable
data class Title(val romaji: String = "", val english: String = "", val native: String = "")

@Serializable
data class Cover(val extraLarge: String = "", val large: String = "", val color: String = "")

@Serializable
data class AiringEpisode(val episode: Int, val airingAt: Long) {
    val at: Instant get() = Instant.ofEpochSecond(airingAt)
}

@Serializable
data class FuzzyDate(val year: Int = 0, val month: Int = 0, val day: Int = 0) {
    fun toLocalDate(): LocalDate? =
        if (year > 0 && month > 0 && day > 0) LocalDate.of(year, month, day) else null
}

@Serializable
data class BrowseQuery(
    @SerialName("Sort") val sort: String = "",
    @SerialName("Season") val season: String = "",
    @SerialName("SeasonYear") val seasonYear: Int = 0,
    @SerialName("Status") val status: String = "",
    @SerialName("Genres") val genres: List<String>? = null,
    @SerialName("Formats") val formats: List<String>? = null,
    @SerialName("Page") val page: Int = 1,
    @SerialName("PerPage") val perPage: Int = 30,
)

@Serializable
data class BrowsePage(
    @SerialName("Media") val media: List<Media>? = null,
    @SerialName("HasNext") val hasNext: Boolean = false,
)

/** An episode's position in a show's history. */
@Serializable
data class Progress(
    @SerialName("MediaID") val mediaId: Int,
    @SerialName("Episode") val episode: Double,
    @SerialName("Round") val round: Int = 0,
    @SerialName("Position") val positionNs: Long = 0,
    @SerialName("Duration") val durationNs: Long = 0,
    @SerialName("Completed") val completed: Boolean = false,
    @SerialName("Provider") val provider: String = "",
    @SerialName("Mode") val mode: String = "",
    @SerialName("UpdatedAt") val updatedAt: String = "",
) {
    val position: Duration get() = positionNs.nanoseconds
    val duration: Duration get() = durationNs.nanoseconds

    /** The episode to continue with: this one if unfinished, otherwise the next. */
    val next: Double get() = if (completed) Math.floor(episode) + 1 else episode
}

@Serializable
data class ListEntry(
    @SerialName("MediaID") val mediaId: Int,
    @SerialName("Status") val status: String,
    @SerialName("Progress") val progress: Int = 0,
    @SerialName("Score") val score: Double = 0.0,
)

/** List statuses, as AniList names them. */
enum class ListStatus(val key: String, val label: String) {
    Watching("CURRENT", "Watching"),
    Planning("PLANNING", "Planning"),
    Completed("COMPLETED", "Completed"),
    Paused("PAUSED", "Paused"),
    Dropped("DROPPED", "Dropped"),
    Rewatching("REPEATING", "Rewatching");

    companion object {
        fun of(key: String?): ListStatus? = entries.firstOrNull { it.key == key }
    }
}

@Serializable
data class Account(
    @SerialName("Backend") val backend: String = "",
    @SerialName("User") val user: String = "",
    @SerialName("LoggedIn") val loggedIn: Boolean = false,
    @SerialName("Expired") val expired: Boolean = false,
) {
    val syncs: Boolean get() = backend == "anilist" && loggedIn
}

@Serializable
data class ShowPrefs(
    @SerialName("MediaID") val mediaId: Int,
    @SerialName("SubLanguages") val subLanguages: List<String>? = null,
    @SerialName("SubShow") val subShow: Boolean? = null,
)

@Serializable
data class Round(val round: Int = 1, val watchedBefore: Int = 0)

/** A show's episodes as the details screen lists them (mobile/episodes.go). */
@Serializable
data class EpisodeList(
    @SerialName("Rows") val rows: List<EpisodeRow>? = null,
    @SerialName("NextUp") val nextUp: Double = 1.0,
    @SerialName("Round") val round: Int = 1,
    @SerialName("Loading") val loading: Boolean = false,
    @SerialName("ProviderError") val providerError: String = "",
)

@Serializable
data class EpisodeRow(
    @SerialName("Number") val number: Double,
    @SerialName("Title") val title: String = "",
    @SerialName("Thumbnail") val thumbnail: String = "",
    @SerialName("State") val state: String = "",
    @SerialName("Position") val positionNs: Long = 0,
    @SerialName("Duration") val durationNs: Long = 0,
    @SerialName("Kind") val kind: String = "",
) {
    val label: String get() = episodeLabel(number)

    /** Watched in this round, here or according to the list. */
    val watched: Boolean get() = state == "watched" || state == "list"

    /** How far a partly watched episode got, from 0 to 1. */
    val fraction: Float
        get() = if (state == "partial" && durationNs > 0) (positionNs.toFloat() / durationNs).coerceIn(0f, 1f) else 0f
}

/** 12.0 reads "12"; 12.5 stays "12.5". */
fun episodeLabel(n: Double): String =
    if (n == Math.floor(n)) n.toLong().toString() else n.toString()

/** A subtitle preference: languages in order, and whether they show. */
@Serializable
data class SubtitlePrefs(
    @SerialName("Languages") val languages: List<String>? = null,
    @SerialName("Show") val show: Boolean = true,
)

/** What to watch (session.Request). Episode 0 continues where the user left off. */
@Serializable
data class WatchRequest(
    @SerialName("Media") val media: Media,
    @SerialName("Episode") val episode: Double = 0.0,
    @SerialName("Mode") val mode: String = "sub",
    @SerialName("Provider") val provider: String = "",
    @SerialName("SkipProviders") val skipProviders: List<String>? = null,
    @SerialName("Subtitles") val subtitles: SubtitlePrefs? = null,
)

@Serializable
data class Stream(
    val url: String = "",
    val height: Int = 0,
    val audio: String = "",
    val label: String = "",
)

/** A watch's progress, from the session (mobile/watch.go). */
@Serializable
data class WatchStatus(
    @SerialName("Kind") val kind: String,
    @SerialName("Media") val media: Media? = null,
    @SerialName("Episode") val episode: Double = 0.0,
    @SerialName("Provider") val provider: String = "",
    @SerialName("Stream") val stream: Stream = Stream(),
    @SerialName("Start") val startNs: Long = 0,
    @SerialName("Position") val positionNs: Long = 0,
    @SerialName("Duration") val durationNs: Long = 0,
    @SerialName("Paused") val paused: Boolean = false,
    @SerialName("Reason") val reason: String = "",
    @SerialName("Through") val through: Double = 0.0,
    @SerialName("Err") val err: String = "",
)

/** What the session asks the player to play (player.Request). */
@Serializable
data class PlayRequest(
    @SerialName("URL") val url: String,
    @SerialName("Kind") val kind: String = "",
    @SerialName("Title") val title: String = "",
    @SerialName("Start") val startNs: Long = 0,
    @SerialName("Headers") val headers: Map<String, String>? = null,
    @SerialName("Subtitles") val subtitles: List<String>? = null,
    @SerialName("MoreSubtitles") val moreSubtitles: List<SubtitleTrack>? = null,
    @SerialName("SubLangs") val subLangs: List<String>? = null,
    @SerialName("HideSubs") val hideSubs: Boolean = false,
    @SerialName("AudioLang") val audioLang: String = "",
)

@Serializable
data class SubtitleTrack(
    @SerialName("URL") val url: String,
    @SerialName("Title") val title: String = "",
    @SerialName("Lang") val lang: String = "",
)

/** A home tab (mobile/home.go): continue watching, or one list status. */
@Serializable
data class HomeTab(
    @SerialName("Items") val items: List<HomeItem>? = null,
    @SerialName("AllFinished") val allFinished: Boolean = false,
)

@Serializable
data class HomeItem(
    @SerialName("Media") val media: Media,
    @SerialName("Progress") val progress: Progress? = null,
    @SerialName("Entry") val entry: ListEntry? = null,
    @SerialName("CaughtUp") val caughtUp: Boolean = false,
)

/** tsuzuki's settings (config.Config), as far as the app shows them. */
@Serializable
data class Config(
    @SerialName("General") val general: General = General(),
    @SerialName("Providers") val providers: Providers = Providers(),
    @SerialName("Tracking") val tracking: Tracking = Tracking(),
    @SerialName("Skip") val skip: Skip = Skip(),
    @SerialName("Subtitles") val subtitles: Subtitles = Subtitles(),
) {
    @Serializable
    data class General(
        @SerialName("Mode") val mode: String = "sub",
        @SerialName("Quality") val quality: String = "best",
        @SerialName("AutoplayNext") val autoplayNext: Boolean = true,
        @SerialName("WatchedThreshold") val watchedThreshold: Double = 0.85,
        @SerialName("ResumeRewindSeconds") val resumeRewindSeconds: Int = 5,
        @SerialName("CheckUpdates") val checkUpdates: Boolean = true,
    )

    @Serializable
    data class Providers(
        @SerialName("Order") val order: List<String>? = null,
        @SerialName("HealthCheckTimeout") val healthCheckTimeout: String = "5s",
    )

    @Serializable
    data class Tracking(
        @SerialName("Backend") val backend: String = "anilist",
    )

    @Serializable
    data class Skip(
        @SerialName("Opening") val opening: String = "auto",
        @SerialName("Ending") val ending: String = "auto",
        @SerialName("Recap") val recap: String = "prompt",
        @SerialName("FillerEpisodes") val fillerEpisodes: Boolean = false,
        @SerialName("RecapEpisodes") val recapEpisodes: Boolean = false,
    )

    @Serializable
    data class Subtitles(
        @SerialName("Languages") val languages: List<String>? = null,
        @SerialName("Show") val show: Boolean = true,
    )
}

/** A Discover list, such as "This season" (anilist.DiscoverList). */
@Serializable
data class DiscoverList(
    @SerialName("Key") val key: String,
    @SerialName("Title") val title: String,
    @SerialName("Query") val query: BrowseQuery,
)

/** The values settings can take (config's enums). */
@Serializable
data class ConfigOptions(
    @SerialName("Modes") val modes: List<String> = emptyList(),
    @SerialName("Qualities") val qualities: List<String> = emptyList(),
    @SerialName("Providers") val providers: List<String> = emptyList(),
    @SerialName("Backends") val backends: List<String> = emptyList(),
    @SerialName("SkipActions") val skipActions: List<String> = emptyList(),
)

/** One diagnostic check (mobile/diagnose.go). */
@Serializable
data class Diagnosis(
    @SerialName("Name") val name: String,
    @SerialName("Status") val status: String,
    @SerialName("Detail") val detail: String = "",
)

/** What's known about a newer release (mobile/update.go). */
@Serializable
data class AppUpdate(
    @SerialName("Current") val current: String = "",
    @SerialName("Latest") val latest: String = "",
    @SerialName("Available") val available: Boolean = false,
    @SerialName("Dev") val dev: Boolean = false,
    @SerialName("Notify") val notify: Boolean = false,
    @SerialName("APK") val apk: String = "",
    @SerialName("Page") val page: String = "",
)
