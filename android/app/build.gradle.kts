plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
}

// The release version comes from the git tag (make android passes it), so the
// app and the terminal release always agree. versionCode orders releases:
// 1.2.3 is 10203, and builds that aren't releases count as 1.
val appVersion = (findProperty("tsuzukiVersion") as String?)?.removePrefix("v") ?: "0.0.0-dev"
val appVersionCode = Regex("""^(\d+)\.(\d+)\.(\d+)$""").find(appVersion)?.destructured
    ?.let { (major, minor, patch) -> major.toInt() * 10000 + minor.toInt() * 100 + patch.toInt() }
    ?: 1

android {
    namespace = "io.github.emofa.tsuzuki"
    compileSdk = 37

    defaultConfig {
        applicationId = "io.github.emofa.tsuzuki"
        minSdk = 26
        targetSdk = 37
        versionCode = appVersionCode
        versionName = appVersion
    }

    signingConfigs {
        // CI signs releases with the keystore from its secrets.
        System.getenv("TSUZUKI_KEYSTORE")?.let { path ->
            create("release") {
                storeFile = file(path)
                storePassword = System.getenv("TSUZUKI_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("TSUZUKI_KEY_ALIAS")
                keyPassword = System.getenv("TSUZUKI_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            signingConfig = signingConfigs.findByName("release") ?: signingConfigs["debug"]
        }
        debug {
            applicationIdSuffix = ".debug"
        }
    }

    splits {
        abi {
            isEnable = true
            reset()
            include("arm64-v8a", "armeabi-v7a", "x86_64")
            isUniversalApk = true
        }
    }

    buildFeatures {
        compose = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

dependencies {
    // tsuzuki's Go core, built by `make android-aar`.
    implementation(files("libs/tsuzuki.aar"))

    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.lifecycle.process)
    implementation(libs.androidx.navigation.compose)
    implementation(platform(libs.compose.bom))
    implementation(libs.compose.ui)
    implementation(libs.compose.ui.tooling.preview)
    implementation(libs.compose.material3)
    implementation(libs.compose.material.icons)
    implementation(libs.media3.exoplayer)
    implementation(libs.media3.exoplayer.hls)
    implementation(libs.media3.ui)
    implementation(libs.media3.session)
    implementation(libs.kotlinx.serialization.json)
    implementation(libs.coil.compose)
    implementation(libs.coil.network)
    implementation(libs.androidx.webkit)
    debugImplementation(libs.compose.ui.tooling)
}
