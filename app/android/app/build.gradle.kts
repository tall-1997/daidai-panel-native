import java.io.File
import java.io.FileInputStream
import java.io.RandomAccessFile
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
val requestedAbis = listOf("arm64-v8a")

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
        minSdk = 24
        targetSdk = 35
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
        versionCode = flutter.versionCode
        versionName = flutter.versionName
        ndk {
            abiFilters += requestedAbis
        }
        externalNativeBuild {
            cmake {
                arguments += "-DANDROID_SUPPORT_FLEXIBLE_PAGE_SIZES=ON"
            }
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
            keepDebugSymbols += setOf(
                "**/liboperit_proot.so",
                "**/libproot_loader.so",
            )
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

    externalNativeBuild {
        cmake {
            path = file("src/main/jni/CMakeLists.txt")
        }
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
    description = "Verifies the rootfs contract and pinned 16 KB-aligned Android native tools."
    doLast {
        val requiredCommands = listOf("apk", "bash", "python3", "pip3", "node", "npm", "uv", "pnpm")
        val requiredPackages = setOf("bash", "python3", "py3-pip", "nodejs", "npm", "uv", "pnpm", "ca-certificates")
        val requiredCapabilities = mapOf(
            "package_manager" to listOf("apk"),
            "shell" to listOf("bash"),
            "python" to listOf("python3", "pip3", "uv"),
            "node" to listOf("node", "npm", "pnpm"),
        )
        requestedAbis.forEach { abi ->
            val assetDir = file("src/main/assets/android-runtime/$abi")
            val rootfs = file("$assetDir/rootfs.tar.gz.bin")
            val rootfsSha = file("$assetDir/rootfs.tar.gz.bin.sha256")
            val rootfsManifestFile = file("$assetDir/runtime-manifest.json")
            val nativeManifestFile = file("$assetDir/native-runtime-manifest.json")
            val nativeDir = file("src/main/jniLibs/$abi")
            val proot = listOf("libdaidai_proot.so", "liboperit_proot.so").map { file("$nativeDir/$it") }.firstOrNull { it.isFile }
            check(rootfs.isFile && rootfsSha.isFile && rootfsManifestFile.isFile) { "Missing Android Linux rootfs assets or manifest for $abi." }
            check(nativeManifestFile.isFile) { "Missing pinned native runtime manifest for $abi." }
            check(proot != null && isArm64Elf(proot) && hasMinimumElfLoadAlignment(proot, 16384L)) {
                "Android PRoot runner for $abi must be an arm64 ELF with 16 KB PT_LOAD alignment."
            }
            check(file("$nativeDir/libyaegi_exec.so").isFile) { "Missing Yaegi runtime for $abi." }
            val expected = rootfsSha.readText().trim().substringBefore(' ')
            val actualRootfsSha = sha256(rootfs)
            check(expected.length == 64 && expected.equals(actualRootfsSha, ignoreCase = true)) { "Android Linux rootfs checksum mismatch for $abi." }

            @Suppress("UNCHECKED_CAST")
            val rootfsManifest = JsonSlurper().parse(rootfsManifestFile) as Map<String, Any>
            check((rootfsManifest["schema_version"] as? Number)?.toInt() == 2) { "Android rootfs manifest schema must be version 2 for $abi." }
            check(rootfsManifest["abi"] == abi && rootfsManifest["sha256"] == actualRootfsSha) { "Android rootfs manifest identity or checksum mismatch for $abi." }
            check((rootfsManifest["size"] as? Number)?.toLong() == rootfs.length()) { "Android rootfs manifest size mismatch for $abi." }
            check((rootfsManifest["required_commands"] as? List<*>) == requiredCommands) { "Android rootfs required_commands mismatch for $abi." }
            check(requiredPackages.all { it in (rootfsManifest["packages"] as? List<*>).orEmpty() }) { "Android rootfs required package list is incomplete for $abi." }
            @Suppress("UNCHECKED_CAST")
            val capabilities = rootfsManifest["capabilities"] as? Map<String, Any> ?: error("Android rootfs capabilities are missing for $abi.")
            requiredCapabilities.forEach { (name, commands) -> check(capabilities[name] == commands) { "Android rootfs capability $name mismatch for $abi." } }
            check(capabilities["tls_ca_certificates"] == true) { "Android rootfs TLS CA capability is missing for $abi." }

            @Suppress("UNCHECKED_CAST")
            val nativeManifest = JsonSlurper().parse(nativeManifestFile) as Map<String, Any>
            check((nativeManifest["schema_version"] as? Number)?.toInt() == 1 && nativeManifest["abi"] == abi) { "Android native runtime manifest identity mismatch for $abi." }
            check((nativeManifest["minimum_load_alignment"] as? Number)?.toLong() == 16384L) { "Android native runtime alignment policy mismatch for $abi." }
            @Suppress("UNCHECKED_CAST")
            val provenance = nativeManifest["provenance"] as? Map<String, Any> ?: error("Android native runtime provenance is missing for $abi.")
            check(provenance["strategy"] == "pinned-termux-binary-packages" && provenance["source_build"] == false && provenance["source_patch_applied"] == false) {
                "Android native runtime provenance makes an unsupported source-build or patch claim for $abi."
            }
            @Suppress("UNCHECKED_CAST")
            val termuxRecipe = provenance["termux_recipe"] as? Map<String, Any> ?: error("Pinned Termux recipe metadata is missing for $abi.")
            @Suppress("UNCHECKED_CAST")
            val upstreamSource = provenance["upstream_source"] as? Map<String, Any> ?: error("Pinned upstream PRoot source metadata is missing for $abi.")
            check((termuxRecipe["commit"] as? String)?.matches(Regex("[0-9a-f]{40}")) == true) { "Termux recipe commit is not pinned for $abi." }
            check((upstreamSource["sha256"] as? String)?.matches(Regex("[0-9a-f]{64}")) == true) { "Upstream PRoot source hash is not pinned for $abi." }
            @Suppress("UNCHECKED_CAST")
            val packageSources = nativeManifest["packages"] as? List<Map<String, Any>> ?: error("Android native package sources are missing for $abi.")
            check(packageSources.isNotEmpty() && packageSources.all { (it["sha256"] as? String)?.matches(Regex("[0-9a-f]{64}")) == true }) {
                "Android native runtime contains an unpinned Termux package for $abi."
            }
            @Suppress("UNCHECKED_CAST")
            val artifacts = nativeManifest["artifacts"] as? List<Map<String, Any>> ?: error("Android native artifacts are missing for $abi.")
            val requiredNativeFiles = setOf("liboperit_proot.so", "libproot_loader.so", "liboperit_busybox.so", "libtalloc_2.so", "libandroid-shmem.so", "libbusybox_1_38_0.so")
            check(requiredNativeFiles.all { required -> artifacts.any { it["name"] == required } }) { "Android PRoot/BusyBox dependency manifest is incomplete for $abi." }
            @Suppress("UNCHECKED_CAST")
            val runtimeOverrides = provenance["runtime_overrides"] as? Map<String, Any> ?: error("Android PRoot loader override is missing for $abi.")
            check(runtimeOverrides["PROOT_LOADER"] == "libproot_loader.so") { "Android PRoot loader override contract mismatch for $abi." }
            val expectedDependencies = mapOf(
                "liboperit_proot.so" to listOf("libtalloc_2.so", "libandroid-shmem.so"),
                "liboperit_busybox.so" to listOf("libbusybox_1_38_0.so"),
                "libbusybox_1_38_0.so" to listOf("libandroid-selinux.so"),
                "libandroid-selinux.so" to listOf("libpcre2-8.so"),
            )
            artifacts.forEach { artifact ->
                val name = artifact["name"] as? String ?: error("Android native artifact has no name for $abi.")
                val binary = file("$nativeDir/$name")
                check(binary.isFile && artifact["sha256"] == sha256(binary) && (artifact["size"] as? Number)?.toLong() == binary.length()) {
                    "Android native artifact checksum or size mismatch: $name."
                }
                check(isArm64Elf(binary) && hasMinimumElfLoadAlignment(binary, 16384L)) {
                    "Android native artifact must be arm64 and 16 KB PT_LOAD aligned: $name."
                }
                val binaryStrings = binary.readText(Charsets.ISO_8859_1)
                check(expectedDependencies[name].orEmpty().all(binaryStrings::contains)) { "Android native artifact dependency contract mismatch: $name." }
            }
            check(file("$nativeDir/liboperit_proot.so").readText(Charsets.ISO_8859_1).contains("PROOT_LOADER")) {
                "Android PRoot binary does not support PROOT_LOADER for $abi."
            }
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
        val nativeAbis = listOf("arm64-v8a").filter { file("src/main/jniLibs/$it/libnode.so").isFile }
        check(nativeAbis.isNotEmpty()) { "No Android Node native libraries found." }
        val primaryAbi = nativeAbis.first()
        val nativeDir = file("src/main/jniLibs/$primaryAbi")
        val launcher = file("$nativeDir/libnode_exec.so")
        val libnode = file("$nativeDir/libnode.so")
        val legacyNodeEntrypoints = listOf("libnodejs_exec.so", "libnodelauncher.so")
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
        legacyNodeEntrypoints.forEach { legacy ->
            check(!file("$nativeDir/$legacy").exists()) {
                "Legacy Node runtime entry $legacy must not be packaged; run app/scripts/prepare-android-node-runtime.sh."
            }
        }
        check(launcher.isFile && isAndroidElf(launcher) && !launcher.readText(Charsets.ISO_8859_1).contains("RUNTIME_STUB_OK")) {
            "Missing real Android Node launcher. Run app/scripts/prepare-android-node-runtime.sh."
        }
        check(!launcher.readText(Charsets.ISO_8859_1).contains("/data/data/com.termux/files/usr/lib")) {
            "Android Node launcher must not contain Termux RUNPATH."
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
        val nativeAbis = listOf("arm64-v8a").filter { file("src/main/jniLibs/$it/libpython3.14.so").isFile }
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

fun hasMinimumElfLoadAlignment(file: File, minimumAlignment: Long): Boolean {
    RandomAccessFile(file, "r").use { elf ->
        val identification = ByteArray(16)
        if (elf.read(identification) != identification.size || identification.sliceArray(0..3).contentEquals(byteArrayOf(0x7f.toByte(), 'E'.code.toByte(), 'L'.code.toByte(), 'F'.code.toByte())).not()) return false
        if (identification[4] != 2.toByte() || identification[5] != 1.toByte()) return false
        fun readUnsignedShort(): Int {
            val bytes = ByteArray(2)
            elf.readFully(bytes)
            return (bytes[0].toInt() and 0xff) or ((bytes[1].toInt() and 0xff) shl 8)
        }
        fun readLongLittleEndian(): Long {
            val bytes = ByteArray(8)
            elf.readFully(bytes)
            return bytes.indices.fold(0L) { value, index -> value or ((bytes[index].toLong() and 0xffL) shl (index * 8)) }
        }
        elf.seek(32)
        val programHeaderOffset = readLongLittleEndian()
        elf.seek(54)
        val programHeaderSize = readUnsignedShort()
        val programHeaderCount = readUnsignedShort()
        val programHeadersLength = programHeaderSize.toLong() * programHeaderCount
        if (programHeaderSize < 56 || programHeaderCount == 0 || programHeaderOffset < 0 || programHeadersLength > elf.length() || programHeaderOffset > elf.length() - programHeadersLength) return false
        var loadSegments = 0
        repeat(programHeaderCount) { index ->
            val headerOffset = programHeaderOffset + index.toLong() * programHeaderSize
            elf.seek(headerOffset)
            val typeBytes = ByteArray(4)
            elf.readFully(typeBytes)
            val type = typeBytes.indices.fold(0) { value, byteIndex -> value or ((typeBytes[byteIndex].toInt() and 0xff) shl (byteIndex * 8)) }
            if (type == 1) {
                loadSegments++
                elf.seek(headerOffset + 48)
                if (readLongLittleEndian() < minimumAlignment) return false
            }
        }
        return loadSegments > 0
    }
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
