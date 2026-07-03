import java.time.LocalDate
import java.time.format.DateTimeFormatter
import java.util.Properties

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.hilt)
    alias(libs.plugins.kapt)
}

val localProperties = Properties()
val localPropertiesFile = rootProject.file("local.properties")
if (localPropertiesFile.exists()) {
    localProperties.load(localPropertiesFile.inputStream())
}

fun calculateVersion(): Pair<Int, String> {
    val runNumber = System.getenv("CI_BUILD_NUMBER")?.toIntOrNull() ?: 1
    val runAttempt = System.getenv("CI_BUILD_ATTEMPT")?.toIntOrNull() ?: 1

    val today = LocalDate.now()
    val dateFormat = DateTimeFormatter.ofPattern("yyMMdd")
    val dateString = today.format(dateFormat)
    val dailyRevision = runNumber % 100
    val versionName = "$dateString.$dailyRevision.$runAttempt"

    return Pair(runNumber, versionName)
}

fun resolveGitSha(): String {
    return try {
        providers.exec {
            commandLine("git", "rev-parse", "HEAD")
            isIgnoreExitValue = true
        }.standardOutput.asText.get().trim().ifBlank { "local" }
    } catch (e: Exception) {
        "local"
    }
}

android {
    namespace = "app.taskwiz"
    compileSdk = 36

    val (calculatedVersionCode, calculatedVersionName) = calculateVersion()
    val gitSha = resolveGitSha()

    defaultConfig {
        applicationId = "app.taskwiz"
        minSdk = 34
        targetSdk = 36
        versionCode = calculatedVersionCode
        versionName = calculatedVersionName
        buildConfigField("String", "GIT_SHA", "\"$gitSha\"")

        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    signingConfigs {
        create("release") {
            val storeFileProperty = localProperties.getProperty("RELEASE_STORE_FILE")
                ?: System.getenv("RELEASE_STORE_FILE")
            val storePasswordProperty = localProperties.getProperty("RELEASE_STORE_PASSWORD")
                ?: System.getenv("RELEASE_STORE_PASSWORD")
            val keyAliasProperty = localProperties.getProperty("RELEASE_KEY_ALIAS")
                ?: System.getenv("RELEASE_KEY_ALIAS")
            val keyPasswordProperty = localProperties.getProperty("RELEASE_KEY_PASSWORD")
                ?: System.getenv("RELEASE_KEY_PASSWORD")

            if (storeFileProperty != null) {
                storeFile = file(storeFileProperty)
                storePassword = storePasswordProperty
                keyAlias = keyAliasProperty
                keyPassword = keyPasswordProperty

                enableV2Signing = true
                enableV3Signing = true
                enableV4Signing = true
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
            val releaseSigningConfig = signingConfigs.getByName("release")
            if (releaseSigningConfig.storeFile != null) {
                signingConfig = releaseSigningConfig
            }

            // Note: connection string is embedded in the APK. App Insights ingestion keys are
            // low-sensitivity (write-only, no read access to data). If spoofed telemetry is a
            // concern, consider proxying ingestion through the backend.
            val appInsightsKey = localProperties.getProperty("APPINSIGHTS_CONNECTION_STRING")
                ?: System.getenv("APPINSIGHTS_CONNECTION_STRING") ?: ""
            buildConfigField("String", "APPINSIGHTS_CONNECTION_STRING", "\"$appInsightsKey\"")
        }

        debug {
            val appInsightsKey = localProperties.getProperty("APPINSIGHTS_CONNECTION_STRING")
                ?: System.getenv("APPINSIGHTS_CONNECTION_STRING") ?: ""
            buildConfigField("String", "APPINSIGHTS_CONNECTION_STRING", "\"$appInsightsKey\"")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    packaging {
        resources {
            excludes += "META-INF/INDEX.LIST"
            excludes += "META-INF/io.netty.versions.properties"
        }
    }
}

kotlin {
    compilerOptions {
        jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17)
    }
}

dependencies {
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.lifecycle.runtime.ktx)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.activity.compose)
    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.ui.graphics)
    implementation(libs.androidx.compose.ui.tooling.preview)
    implementation(libs.androidx.compose.material3)
    implementation(libs.androidx.compose.material.icons.extended)

    implementation(libs.androidx.navigation.compose)
    implementation(libs.androidx.hilt.navigation.compose)

    implementation(libs.msal)

    implementation(libs.hilt.android)
    kapt(libs.hilt.compiler)

    implementation(libs.okhttp)
    implementation(libs.okhttp.logging)
    implementation(libs.retrofit)
    implementation(libs.retrofit.gson)
    implementation(libs.gson)

    implementation(libs.androidx.work.runtime.ktx)

    implementation(libs.androidx.room.runtime)
    implementation(libs.androidx.room.ktx)
    kapt(libs.androidx.room.compiler)

    implementation(libs.androidx.glance.appwidget)
    implementation(libs.androidx.glance.material3)

    implementation(libs.opentelemetry.api)
    implementation(libs.opentelemetry.sdk)
    implementation(libs.opentelemetry.exporter.logging)

    testImplementation(libs.junit)
    testImplementation(libs.androidx.room.testing)
    testImplementation(libs.kotlinx.coroutines.test)
    androidTestImplementation(libs.androidx.junit)
    androidTestImplementation(libs.androidx.espresso.core)
    androidTestImplementation(platform(libs.androidx.compose.bom))
    androidTestImplementation(libs.androidx.compose.ui.test.junit4)
    debugImplementation(libs.androidx.compose.ui.tooling)
    debugImplementation(libs.androidx.compose.ui.test.manifest)
}
