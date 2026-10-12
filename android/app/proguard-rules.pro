# gomobile's generated classes are called from native code by name.
-keep class go.** { *; }
-keep class io.github.emofa.tsuzuki.go.** { *; }
# So are the app's implementations of the core's interfaces (Host, Player, ...).
-keep class * implements io.github.emofa.tsuzuki.go.mobile.Host { *; }
-keep class * implements io.github.emofa.tsuzuki.go.mobile.Player { *; }
-keep class * implements io.github.emofa.tsuzuki.go.mobile.StatusListener { *; }
