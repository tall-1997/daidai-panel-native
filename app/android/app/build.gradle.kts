import java.io.File
import java.io.FileInputStream
import java.security.MessageDigest
import java.util.zip.ZipFile
import java.util.Properties
import groovy.json.JsonSlurper
import org.gradle.api.tasks.Sync

plugins {
    id("com.android.application")
    id("kotlin-android")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

// Release signing priority: key.properties, environment variables, then debug for snapshots.
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
val releaseKeyAlias = resolveSigningValue("keyAlias", "KEYSTORE_ALIAS")
val releaseKeyPassword = resolveSigningValue("keyPassword", "KEYSTORE_KEY_PASSWORD")
val hasReleaseSigning =
    !releaseStoreFile.isNullOrEmpty() &&
        !releaseStorePassword.isNullOrEmpty() &&
        !releaseKeyAlias.isNullOrEmpty() &&
        !releaseKeyPassword.isNullOrEmpty()
val requireReleaseSigning = System.getenv("REQUIRE_RELEASE_SIGNING") == "true"
val requestedAbis = System.getenv("DAIDAI_ANDROID_ABIS")
    ?.split(',')
    ?.map(String::trim)
    ?.filter(String::isNotEmpty)
    ?.takeIf(List<String>::isNotEmpty)
    ?: listOf("arm64-v8a")

check(!requireReleaseSigning || hasReleaseSigning) {
    "Release signing is required, but KEYSTORE_FILE, KEYSTORE_PASSWORD, KEYSTORE_ALIAS, or KEYSTORE_KEY_PASSWORD is missing."
}

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
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
        versionCode = flutter.versionCode
        versionName = flutter.versionName
        ndk {
            abiFilters += requestedAbis
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

    androidResources {
        ignoreAssetsPattern = "android-daidai-do-not-ignore-python-stdlib"
    }

    lint {
        disable.add("MissingVersion")
    }

    buildFeatures {
        aidl = true
    }

    sourceSets {
        getByName("main") {
            assets.srcDirs(
                "../../../runtime",
                layout.buildDirectory.dir("generated/localWebAssets").get().asFile,
            )
            // Python and Node payloads are packaged as archives under src/main/assets.
            // The generated pythonAssets/nodeAssets trees are build-time verification inputs;
            // adding them as Android asset roots duplicates archive metadata paths.
        }
    }
}

val panelWebDir = rootProject.file("../../panel/web")
val generatedLocalWebDir = layout.buildDirectory.dir("generated/localWebAssets/local-web")

val installLocalPanelWebDependencies = tasks.register<Exec>("installLocalPanelWebDependencies") {
    group = "build setup"
    description = "Installs locked Panel Web dependencies for the Android asset build."
    workingDir(panelWebDir)
    commandLine("npm", "ci", "--no-audit", "--no-fund")
    inputs.files(panelWebDir.resolve("package.json"), panelWebDir.resolve("package-lock.json"))
    outputs.dir(panelWebDir.resolve("node_modules"))
}

val buildLocalPanelWeb = tasks.register<Exec>("buildLocalPanelWeb") {
    dependsOn(installLocalPanelWebDependencies)
    group = "build"
    description = "Builds the loopback-only Panel Web bundle."
    workingDir(panelWebDir)
    commandLine("npm", "run", "build")
    environment("VITE_LOCAL_WEB_BUILD", "true")
    inputs.files(fileTree(panelWebDir.resolve("src")), panelWebDir.resolve("package.json"), panelWebDir.resolve("package-lock.json"), panelWebDir.resolve("vite.config.ts"))
    outputs.dir(panelWebDir.resolve("dist"))
}

val packageLocalPanelWeb = tasks.register<Sync>("packageLocalPanelWeb") {
    dependsOn(buildLocalPanelWeb)
    from(panelWebDir.resolve("dist"))
    into(generatedLocalWebDir)
}

val verifyLocalPanelWeb = tasks.register("verifyLocalPanelWeb") {
    dependsOn(packageLocalPanelWeb)
    group = "verification"
    doLast {
        val webRoot = generatedLocalWebDir.get().asFile
        check(webRoot.resolve("index.html").isFile) { "Panel Web index.html is missing from local-web assets." }
        check(webRoot.resolve("assets").isDirectory && webRoot.resolve("assets").walkTopDown().any { it.isFile }) {
            "Panel Web assets directory is missing or empty."
        }
    }
}

dependencies {
    coreLibraryDesugaring("com.android.tools:desugar_jdk_libs:2.1.4")
    implementation("com.xeonyu:bsdiff:1.0.4")
    implementation("org.nanohttpd:nanohttpd:2.3.1")
    implementation("androidx.work:work-runtime-ktx:2.11.1")
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.json:json:20250517")
    androidTestImplementation("androidx.test:core-ktx:1.6.1")
    androidTestImplementation("androidx.test:runner:1.6.2")
    androidTestImplementation("androidx.test.ext:junit-ktx:1.2.1")
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
            requestedAbis.forEach { abi ->
                check(archive.getEntry("jni/$abi/libgojni.so") != null) {
                    "Invalid mobilecore.aar: jni/$abi/libgojni.so is missing."
                }
            }
            val temporaryJar = layout.buildDirectory.file("mobilecore-verification/classes.jar").get().asFile
            temporaryJar.parentFile.mkdirs()
            archive.getInputStream(archive.getEntry("classes.jar")).use { input ->
                temporaryJar.outputStream().use { output -> input.copyTo(output) }
            }
            ZipFile(temporaryJar).use { classes ->
                val expectedClasses = listOf(
                    "mobilecore/Mobilecore.class",
                    "mobilecore/mobilecore/Mobilecore.class",
                )
                check(expectedClasses.any { classes.getEntry(it) != null }) {
                    "Invalid mobilecore.aar: Mobilecore.class is missing. Expected one of ${expectedClasses.joinToString()}."
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
        check(rootProject.file("../../runtime/smoke-evidence.json").isFile) {
            "Missing runtime/smoke-evidence.json in repository root."
        }
    }
}

val verifyLinuxRootfsRuntime = tasks.register("verifyLinuxRootfsRuntime") {
    group = "verification"
    description = "Fails when the packaged Alpine/Ubuntu rootfs or PRoot runner is missing."
    doLast {
        requestedAbis.forEach { abi ->
            val rootfs = file("src/main/assets/android-runtime/$abi/rootfs.tar.gz.bin")
            val rootfsSha = file("src/main/assets/android-runtime/$abi/rootfs.tar.gz.bin.sha256")
            val nativeDir = file("src/main/jniLibs/$abi")
            val proot = listOf("libdaidai_proot.so", "liboperit_proot.so").map { file("$nativeDir/$it") }.firstOrNull { it.isFile }
            check(rootfs.isFile && rootfsSha.isFile) { "Missing Android Linux rootfs assets for $abi." }
            check(proot != null && isAndroidElf(proot)) { "Missing Android PRoot runner for $abi." }
            check(file("$nativeDir/libyaegi_exec.so").isFile) { "Missing Yaegi runtime for $abi." }
            val expected = rootfsSha.readText().trim().substringBefore(' ')
            check(expected.length == 64 && expected.equals(sha256(rootfs), ignoreCase = true)) { "Android Linux rootfs checksum mismatch for $abi." }
        }
    }
}

val verifyNodeNativeRuntime = tasks.register("verifyNodeNativeRuntime") {
    group = "verification"
    description = "Fails when the Android Node, npm, npx, or TypeScript bundle is incomplete or inconsistent."
    doLast {
        val version = "18.20.4"
        val npmVersion = "10.9.4"
        val typescriptVersion = "5.9.3"
        val nativeAbis = listOf("arm64-v8a", "x86_64").filter { file("src/main/jniLibs/$it/libnode.so").isFile }
        check(nativeAbis.isNotEmpty()) { "No Android Node native libraries found." }
        val primaryAbi = nativeAbis.first()
        val nativeDir = file("src/main/jniLibs/$primaryAbi")
        val launcher = file("$nativeDir/libnode_exec.so")
        val runtimeLauncher = file("$nativeDir/libnodelauncher.so")
        val libnode = file("$nativeDir/libnode.so")
        val assets = file("src/main/nodeAssets/node-runtime/$version/usr")
        val metadataFile = file("$assets/runtime-metadata.json")
        val requiredAssets = listOf(
            "lib/node_modules/npm/package.json",
            "lib/node_modules/npm/bin/npm-cli.js",
            "lib/node_modules/npm/bin/npx-cli.js",
            "lib/node_modules/typescript/package.json",
            "lib/node_modules/typescript/bin/tsc",
            "etc/npmrc",
        )
        check(launcher.isFile && isAndroidElf(launcher) && !launcher.readText(Charsets.ISO_8859_1).contains("RUNTIME_STUB_OK")) {
            "Missing real Android Node launcher. Run app/scripts/prepare-android-node-runtime.sh."
        }
        check(runtimeLauncher.isFile && isAndroidElf(runtimeLauncher)) {
            "Missing Android 16-compatible Node runtime launcher: ${runtimeLauncher.path}."
        }
        check(libnode.isFile && isAndroidElf(libnode) && !libnode.readText(Charsets.ISO_8859_1).contains("RUNTIME_STUB_OK")) {
            "Missing real Android libnode.so. Run app/scripts/prepare-android-node-runtime.sh."
        }
        check(metadataFile.isFile) { "Missing Node runtime metadata: ${metadataFile.path}" }
        requiredAssets.forEach { relative -> check(file("$assets/$relative").isFile) { "Missing Node runtime asset: $relative" } }

        @Suppress("UNCHECKED_CAST")
        val metadata = JsonSlurper().parse(metadataFile) as Map<String, Any>
        @Suppress("UNCHECKED_CAST")
        val npmPackage = JsonSlurper().parse(file("$assets/lib/node_modules/npm/package.json")) as Map<String, Any>
        @Suppress("UNCHECKED_CAST")
        val typescriptPackage = JsonSlurper().parse(file("$assets/lib/node_modules/typescript/package.json")) as Map<String, Any>
        check(metadata["node_version"] == version && metadata["npm_version"] == npmVersion && metadata["typescript_version"] == typescriptVersion)
        check(npmPackage["version"] == npmVersion && typescriptPackage["version"] == typescriptVersion)
        check(metadata["launcher_sha256"] == sha256(launcher) && metadata["libnode_sha256"] == sha256(libnode))
        val bundleDigest = MessageDigest.getInstance("SHA-256")
        requiredAssets.forEach { relative ->
            bundleDigest.update(relative.toByteArray())
            bundleDigest.update(0.toByte())
            bundleDigest.update(file("$assets/$relative").readBytes())
        }
        check(metadata["bundle_sha256"] == bundleDigest.digest().joinToString("") { "%02x".format(it) })
        check(metadata["npm_ignore_scripts"] == true && file("$assets/etc/npmrc").readText().lineSequence().any { it == "ignore-scripts=true" })
        @Suppress("UNCHECKED_CAST")
        val manifest = JsonSlurper().parse(rootProject.file("../../runtime/manifest.json")) as Map<String, Any>
        @Suppress("UNCHECKED_CAST")
        val components = manifest["components"] as List<Map<String, Any>>
        mapOf("node-lts-android-arm64" to version, "typescript-stable" to typescriptVersion).forEach { (id, expectedVersion) ->
            val component = components.singleOrNull { it["id"] == id }
            check(component?.get("version") == expectedVersion && component["sha256"] == metadata["launcher_sha256"]) {
                "$id manifest version or hash mismatch."
            }
        }
    }
}

val verifyPythonNativeRuntime = tasks.register("verifyPythonNativeRuntime") {
    group = "verification"
    description = "Fails when the CPython 3.14 Android runtime, stdlib, native dependencies, or wheels are invalid."
    doLast {
        val nativeAbis = listOf("arm64-v8a", "x86_64").filter { file("src/main/jniLibs/$it/libpython3.14.so").isFile }
        check(nativeAbis.isNotEmpty()) { "No Android Python native libraries found." }
        val primaryAbi = nativeAbis.first()
        val nativeDir = file("src/main/jniLibs/$primaryAbi")
        val prefix = file("src/main/pythonAssets/python-runtime/3.14/prefix")
        val stdlib = file("$prefix/lib/python3.14")
        val wheelhouse = file("$prefix/wheelhouse")
        val launcher = file("$nativeDir/libpython_exec.so")
        val python = file("$nativeDir/libpython3.14.so")
        val requiredAssets = listOf(
            "ssl.py",
            "sqlite3/__init__.py",
            "venv/__init__.py",
            "ensurepip/__init__.py",
        ) + if (primaryAbi == "arm64-v8a") listOf(
            "lib-dynload/_ssl.cpython-314-aarch64-linux-android.so",
            "lib-dynload/_sqlite3.cpython-314-aarch64-linux-android.so",
        ) else listOf(
            "lib-dynload/_ssl.cpython-314-x86_64-linux-android.so",
            "lib-dynload/_sqlite3.cpython-314-x86_64-linux-android.so",
        )
        val requiredNativeLibraries = listOf(
            "libpython3.14.so",
            "libssl_python.so",
            "libcrypto_python.so",
            "libsqlite3_python.so",
        )
        check(prefix.isDirectory) { "Missing Python runtime assets. Run app/scripts/prepare-android-python-runtime.sh." }
        requiredAssets.forEach { relative -> check(file("$stdlib/$relative").isFile) { "Missing Python runtime asset: $relative" } }
        check(file("$prefix/etc/ssl/certs/cacert.pem").isFile) { "Missing Python CA certificate bundle." }
        check(file("$prefix/runtime-manifest.json").isFile) { "Missing Python runtime asset manifest." }
        check(file("$wheelhouse/wheelhouse-manifest.json").isFile) { "Missing Python wheelhouse manifest." }
        check(fileTree("$stdlib/ensurepip/_bundled").matching { include("pip-*-py3-none-any.whl") }.files.size == 1) {
            "CPython ensurepip must contain exactly one pure Python pip wheel."
        }
        check(launcher.isFile && isAndroidElf(launcher)) { "Python launcher must be an Android ELF." }
        check(launcher.readText(Charsets.ISO_8859_1).contains("libpython3.14.so")) { "Python launcher is not linked to libpython3.14.so." }
        requiredNativeLibraries.forEach { name ->
            val library = file("$nativeDir/$name")
            check(library.isFile && isAndroidElf(library)) { "$name must be an Android ELF." }
        }

        @Suppress("UNCHECKED_CAST")
        val wheelManifest = JsonSlurper().parse(file("$wheelhouse/wheelhouse-manifest.json")) as Map<String, Any>
        @Suppress("UNCHECKED_CAST")
        val manifestWheels = wheelManifest["wheels"] as? List<Map<String, Any>> ?: error("Python wheelhouse manifest has no wheels.")
        val expectedWheels = manifestWheels.map { it["filename"] as String }.toSet()
        val actualWheels = wheelhouse.listFiles { item -> item.isFile && item.extension == "whl" }.orEmpty().map { it.name }.toSet()
        check(expectedWheels.isNotEmpty() && expectedWheels == actualWheels) { "Python wheelhouse files do not match its manifest." }
        expectedWheels.forEach { name ->
            check(isCompatiblePythonWheel(name)) { "Incompatible Python wheel in APK assets: $name" }
            val metadata = manifestWheels.single { it["filename"] == name }
            check(metadata["sha256"] == sha256(file("$wheelhouse/$name"))) { "Python wheel checksum mismatch: $name" }
        }
        fileTree(prefix).matching { include("**/*.whl") }.files.forEach { wheel ->
            check(isCompatiblePythonWheel(wheel.name)) { "Incompatible Python wheel in APK assets: ${wheel.path}" }
        }
    }
}

fun isCompatiblePythonWheel(filename: String): Boolean {
    val lower = filename.lowercase()
    if (listOf("cp312", "manylinux", "musllinux").any(lower::contains)) return false
    return lower.endsWith("-py3-none-any.whl") ||
        Regex(".+-cp314-(cp314|abi3)-android_[0-9]+_arm64_v8a\\.whl").matches(lower) ||
        Regex(".+-cp314-(cp314|abi3)-android_[0-9]+_x86_64\\.whl").matches(lower)
}

fun isArm64Elf(file: File): Boolean {
    val header = file.inputStream().use { input -> ByteArray(20).also { input.read(it) } }
    if (header[0] != 0x7f.toByte() || header[1] != 'E'.code.toByte() || header[2] != 'L'.code.toByte() || header[3] != 'F'.code.toByte()) {
        return false
    }
    if (header[4] != 2.toByte() || header[5] != 1.toByte()) return false
    val machine = (header[18].toInt() and 0xff) or ((header[19].toInt() and 0xff) shl 8)
    return machine == 183
}

fun isAndroidElf(file: File): Boolean {
    val header = file.inputStream().use { input -> ByteArray(20).also { input.read(it) } }
    if (header[0] != 0x7f.toByte() || header[1] != 'E'.code.toByte() || header[2] != 'L'.code.toByte() || header[3] != 'F'.code.toByte()) {
        return false
    }
    if (header[4] !in listOf(1.toByte(), 2.toByte()) || header[5] != 1.toByte()) return false
    val machine = (header[18].toInt() and 0xff) or ((header[19].toInt() and 0xff) shl 8)
    return machine == 40 || machine == 183 || machine == 62
}

fun sha256(file: File): String {
    val digest = MessageDigest.getInstance("SHA-256")
    file.inputStream().use { input ->
        val buffer = ByteArray(DEFAULT_BUFFER_SIZE)
        while (true) {
            val count = input.read(buffer)
            if (count < 0) break
            digest.update(buffer, 0, count)
        }
    }
    return digest.digest().joinToString("") { "%02x".format(it) }
}

tasks.named("preBuild").configure {
    dependsOn(verifyMobileCoreAar)
    dependsOn(verifyRuntimeMetadata)
    dependsOn(verifyLinuxRootfsRuntime)
    dependsOn(verifyLocalPanelWeb)
    dependsOn(verifyNodeNativeRuntime)
    dependsOn(verifyPythonNativeRuntime)
    
}

dependencies {
    implementation("org.apache.commons:commons-compress:1.27.1")
}

flutter {
    source = "../.."
}
