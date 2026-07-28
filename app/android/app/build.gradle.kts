import java.io.FileInputStream
import java.util.zip.ZipFile
import java.util.Properties

plugins {
    id("com.android.application")
    id("kotlin-android")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

// ── Release signing ──
// Priority: key.properties file > environment variables > fallback to debug
val keystoreProperties = Properties()
val keystorePropertiesFile = rootProject.file("key.properties")
if (keystorePropertiesFile.exists()) {
    keystoreProperties.load(FileInputStream(keystorePropertiesFile))
}

fun resolveSigningValue(propertyKey: String, envKey: String): String? {
    val propertyValue = keystoreProperties.getProperty(propertyKey)?.trim()
    if (!propertyValue.isNullOrEmpty()) {
        return propertyValue
    }
    return System.getenv(envKey)?.trim()?.takeIf { it.isNotEmpty() }
}

val releaseStoreFile = resolveSigningValue("storeFile", "KEYSTORE_FILE")
val releaseStorePassword = resolveSigningValue("storePassword", "KEYSTORE_PASSWORD")
val releaseKeyAlias = resolveSigningValue("keyAlias", "KEY_ALIAS")
val releaseKeyPassword = resolveSigningValue("keyPassword", "KEY_PASSWORD")
val hasReleaseSigning =
    !releaseStoreFile.isNullOrEmpty() &&
        !releaseStorePassword.isNullOrEmpty() &&
        !releaseKeyAlias.isNullOrEmpty() &&
        !releaseKeyPassword.isNullOrEmpty()

android {
    namespace = "com.daidai.daidai_app"
    compileSdk = 36
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
        isCoreLibraryDesugaringEnabled = true
    }

    kotlin {
        compilerOptions {
            jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17)
        }
    }

    defaultConfig {
        applicationId = "com.daidai.daidai_app"
        minSdk = 28
        targetSdk = 35
        versionCode = flutter.versionCode
        versionName = flutter.versionName
        ndk {
            abiFilters += "arm64-v8a"
        }
    }

    signingConfigs {
        create("release") {
            if (hasReleaseSigning) {
                storeFile = file(releaseStoreFile!!)
                storePassword = releaseStorePassword
                keyAlias = releaseKeyAlias
                keyPassword = releaseKeyPassword
            }
        }
    }

    buildTypes {
        release {
            if (hasReleaseSigning) {
                signingConfig = signingConfigs.getByName("release")
            } else {
                signingConfig = signingConfigs.getByName("debug")
            }
            isMinifyEnabled = false
            isShrinkResources = false
        }
    }

    packaging {
        jniLibs {
            useLegacyPackaging = true
        }
    }

    lint {
        disable.add("MissingVersion")
    }

    buildFeatures {
        aidl = true
    }

    sourceSets {
        getByName("main") {
            assets.srcDirs("../../../runtime")
        }
    }
}

dependencies {
    coreLibraryDesugaring("com.android.tools:desugar_jdk_libs:2.1.4")
    implementation("com.xeonyu:bsdiff:1.0.4")
    implementation("org.nanohttpd:nanohttpd:2.3.1")
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.json:json:20250517")
    if (file("libs/mobilecore.aar").exists()) {
        implementation(files("libs/mobilecore.aar"))
    }
}

val verifyMobileCoreAar = tasks.register("verifyMobileCoreAar") {
    group = "verification"
    description = "Fails when the CI-produced gomobile AAR is missing."
    doLast {
        val aar = file("libs/mobilecore.aar")
        check(aar.isFile) {
            "Missing android/app/libs/mobilecore.aar. Build it from daidai-panel-native/server with gomobile before assembling Android."
        }
        ZipFile(aar).use { archive ->
            check(archive.getEntry("classes.jar") != null) {
                "Invalid mobilecore.aar: classes.jar is missing."
            }
            check(archive.getEntry("jni/arm64-v8a/libgojni.so") != null) {
                "Invalid mobilecore.aar: jni/arm64-v8a/libgojni.so is missing."
            }
            val temporaryJar = layout.buildDirectory.file("mobilecore-verification/classes.jar").get().asFile
            temporaryJar.parentFile.mkdirs()
            archive.getInputStream(archive.getEntry("classes.jar")).use { input ->
                temporaryJar.outputStream().use { output -> input.copyTo(output) }
            }
            ZipFile(temporaryJar).use { classes ->
                check(classes.getEntry("mobilecore/mobilecore/Mobilecore.class") != null) {
                    "Invalid mobilecore.aar: mobilecore/mobilecore/Mobilecore.class is missing."
                }
            }
        }
    }
}

val verifyRuntimeMetadata = tasks.register("verifyRuntimeMetadata") {
    group = "verification"
    description = "Fails when runtime manifest metadata is missing."
    doLast {
        val manifest = rootProject.file("../../runtime/manifest.json")
        val compatibility = rootProject.file("../../runtime/compatibility.json")
        check(manifest.isFile) {
            "Missing runtime/manifest.json in repository root."
        }
        check(compatibility.isFile) {
            "Missing runtime/compatibility.json in repository root."
        }
    }
}

tasks.named("preBuild").configure {
    dependsOn(verifyMobileCoreAar)
    dependsOn(verifyRuntimeMetadata)
}

flutter {
    source = "../.."
}
