package io.github.emofa.tsuzuki.ui.common

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CloudOff
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import coil3.compose.AsyncImage
import io.github.emofa.tsuzuki.TsuzukiApplication
import io.github.emofa.tsuzuki.core.Media

/** The application, for screens to reach the core and the watch controller. */
@Composable
fun rememberApp(): TsuzukiApplication = LocalContext.current.applicationContext as TsuzukiApplication

/** The show's AniList colour, for placeholders while covers load. */
fun Media.coverColor(fallback: Color): Color =
    runCatching { Color(android.graphics.Color.parseColor(coverImage.color)) }.getOrDefault(fallback)

@Composable
fun Cover(media: Media, modifier: Modifier = Modifier) {
    AsyncImage(
        model = media.coverImage.large.ifEmpty { media.coverImage.extraLarge },
        contentDescription = null,
        contentScale = ContentScale.Crop,
        modifier = modifier
            .aspectRatio(0.7f)
            .clip(RoundedCornerShape(8.dp))
            .background(media.coverColor(MaterialTheme.colorScheme.surfaceVariant)),
    )
}

/** A show in a grid: its cover, title, and a line about it. */
@Composable
fun MediaCard(
    media: Media,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    subtitle: String = media.meta,
    progress: Float? = null,
) {
    Column(modifier.clickable(onClick = onClick).padding(4.dp)) {
        Box {
            Cover(media, Modifier.fillMaxWidth())
            if (progress != null) {
                LinearProgressIndicator(
                    progress = { progress },
                    modifier = Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(6.dp).height(4.dp)
                        .clip(RoundedCornerShape(2.dp)),
                )
            }
        }
        Spacer(Modifier.height(6.dp))
        Text(
            media.displayTitle,
            style = MaterialTheme.typography.titleSmall,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
        if (subtitle.isNotEmpty()) {
            Text(
                subtitle,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

/** A show in a list: a small cover beside its title and details. */
@Composable
fun MediaRow(media: Media, onClick: () -> Unit, subtitle: String = media.meta, trailing: @Composable () -> Unit = {}) {
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Cover(media, Modifier.width(56.dp))
        Column(Modifier.weight(1f)) {
            Text(media.displayTitle, style = MaterialTheme.typography.titleMedium, maxLines = 2, overflow = TextOverflow.Ellipsis)
            if (subtitle.isNotEmpty()) {
                Text(
                    subtitle,
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        trailing()
    }
}

@Composable
fun Loading(modifier: Modifier = Modifier) {
    Box(modifier.fillMaxSize().padding(32.dp), contentAlignment = Alignment.Center) {
        CircularProgressIndicator()
    }
}

/** An error that stops a screen, with a way to try again. */
@Composable
fun ErrorState(message: String, onRetry: (() -> Unit)? = null, modifier: Modifier = Modifier) {
    Message(
        icon = Icons.Outlined.CloudOff,
        title = "Something went wrong",
        body = message,
        modifier = modifier,
        action = onRetry?.let { { Button(onClick = it) { Text("Try again") } } },
    )
}

/** A centred message for empty or failed screens. */
@Composable
fun Message(
    icon: ImageVector,
    title: String,
    body: String = "",
    modifier: Modifier = Modifier,
    action: (@Composable () -> Unit)? = null,
) {
    Column(
        modifier.fillMaxSize().padding(32.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Icon(icon, null, Modifier.size(48.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
        Spacer(Modifier.height(16.dp))
        Text(title, style = MaterialTheme.typography.titleMedium, textAlign = TextAlign.Center)
        if (body.isNotEmpty()) {
            Spacer(Modifier.height(4.dp))
            Text(
                body,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                textAlign = TextAlign.Center,
            )
        }
        if (action != null) {
            Spacer(Modifier.height(16.dp))
            action()
        }
    }
}
