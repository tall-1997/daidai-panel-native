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
    description = "Verifies the dual-distribution rootfs assets and pinned 16 KB-aligned Android native tools."
    doLast {
        val distroCommands = mapOf(
            "alpine" to listOf("apk", "bash", "python3", "pip3", "node", "npm", "uv", "pnpm"),
            "ubuntu" to listOf("apt-get", "bash", "python3", "pip3", "node", "npm", "pnpm"),
        )
        val distroPackages = mapOf(
            "alpine" to setOf("bash", "python3", "py3-pip", "py3-pycryptodome", "nodejs", "npm", "uv", "pnpm", "ca-certificates"),
            "ubuntu" to setOf("bash", "python3", "python3-pip", "nodejs", "npm", "pnpm", "ca-certificates"),
        )
        val distroCapabilities = mapOf(
            "alpine" to mapOf(
                "package_manager" to listOf("apk"),
                "shell" to listOf("bash"),
                "python" to listOf("python3", "pip3", "uv"),
                "node" to listOf("node", "npm", "pnpm"),
            ),
            "ubuntu" to mapOf(
                "package_manager" to listOf("apt-get"),
                "shell" to listOf("bash"),
                "python" to listOf("python3", "pip3"),
                "node" to listOf("node", "npm", "pnpm"),
            ),
        )
        requestedAbis.forEach { abi ->
            val nativeDir = file("src/main/jniLibs/$abi")
            val proot = listOf("libdaidai_proot.so").map { file("$nativeDir/$it") }.firstOrNull { it.isFile }
            check(proot != null && isArm64Elf(proot) && hasMinimumElfLoadAlignment(proot, 16384L)) {
                "Android PRoot runner for $abi must be an arm64 ELF with 16 KB PT_LOAD alignment."
            }
            check(file("$nativeDir/libproot_loader.so").isFile) { "Missing PRoot loader for $abi." }
            check(file("$nativeDir/libyaegi_exec.so").isFile) { "Missing Yaegi runtime for $abi." }

            // 双发行版 rootfs 资产，每一份都必须自洽（sha256 + manifest 一致）。
            // 至少一个发行版必须内置；未内置的发行版由 App 在运行时按需下载。
            val presentDistributions = listOf("alpine", "ubuntu").filter { distribution ->
                file("src/main/assets/android-runtime/$abi/$distribution/rootfs.tar.gz.bin").isFile
            }
            check(presentDistributions.isNotEmpty()) {
                "No rootfs assets present for $abi; at least one distribution is required."
            }
            presentDistributions.forEach { distribution ->
                val assetDir = file("src/main/assets/android-runtime/$abi/$distribution")
                val rootfs = file("$assetDir/rootfs.tar.gz.bin")
                val rootfsSha = file("$assetDir/rootfs.tar.gz.bin.sha256")
                val rootfsManifestFile = file("$assetDir/runtime-manifest.json")
                check(rootfs.isFile && rootfsSha.isFile && rootfsManifestFile.isFile) {
                    "Missing $distribution rootfs assets or manifest for $abi."
                }
                val expected = rootfsSha.readText().trim().substringBefore(' ')
                val actualRootfsSha = sha256(rootfs)
                check(expected.length == 64 && expected.equals(actualRootfsSha, ignoreCase = true)) { "$distribution rootfs checksum mismatch for $abi." }

                @Suppress("UNCHECKED_CAST")
                val rootfsManifest = JsonSlurper().parse(rootfsManifestFile) as Map<String, Any>
                check((rootfsManifest["schema_version"] as? Number)?.toInt() == 2) { "$distribution rootfs manifest schema must be version 2 for $abi." }
                check(rootfsManifest["abi"] == abi && rootfsManifest["distribution"] == distribution && rootfsManifest["sha256"] == actualRootfsSha) { "$distribution rootfs manifest identity or checksum mismatch for $abi." }
                check((rootfsManifest["size"] as? Number)?.toLong() == rootfs.length()) { "$distribution rootfs manifest size mismatch for $abi." }
                check((rootfsManifest["required_commands"] as? List<*>) == distroCommands[distribution]) { "$distribution rootfs required_commands mismatch for $abi." }
                check(distroPackages[distribution]!!.all { it in (rootfsManifest["packages"] as? List<*>).orEmpty() }) { "$distribution rootfs required package list is incomplete for $abi." }
                @Suppress("UNCHECKED_CAST")
                val capabilities = rootfsManifest["capabilities"] as? Map<String, Any> ?: error("$distribution rootfs capabilities are missing for $abi.")
                distroCapabilities[distribution]!!.forEach { (name, commands) -> check(capabilities[name] == commands) { "$distribution rootfs capability $name mismatch for $abi." } }
                check(capabilities["tls_ca_certificates"] == true) { "$distribution rootfs TLS CA capability is missing for $abi." }
            }

            val nativeManifestFile = file("src/main/assets/android-runtime/$abi/native-runtime-manifest.json")
            check(nativeManifestFile.isFile) { "Missing pinned native runtime manifest for $abi." }
            @Suppress("UNCHECKED_CAST")
            val nativeManifest = JsonSlurper().parse(nativeManifestFile) as Map<String, Any>
            check((nativeManifest["schema_version"] as? Number)?.toInt() == 1 && nativeManifest["abi"] == abi) { "Android native runtime manifest identity mismatch for $abi." }
            check((nativeManifest["minimum_load_alignment"] as? Number)?.toLong() == 16384L) { "Android native runtime alignment policy mismatch for $abi." }
            @Suppress("UNCHECKED_CAST")
            val provenance = nativeManifest["provenance"] as? Map<String, Any> ?: error("Android native runtime provenance is missing for $abi.")
            check(provenance["strategy"] == "self-contained-source-build" && provenance["source_build"] == true) {
                "Android native runtime must be a self-contained source build for $abi."
            }
            @Suppress("UNCHECKED_CAST")
            val upstreamSource = provenance["upstream_source"] as? List<Map<String, Any>> ?: error("Pinned upstream source metadata is missing for $abi.")
            check(upstreamSource.size >= 3 && upstreamSource.all { (it["sha256"] as? String)?.matches(Regex("[0-9a-f]{64}")) == true }) {
                "Upstream sources are not pinned for $abi."
            }
            @Suppress("UNCHECKED_CAST")
            val artifacts = nativeManifest["artifacts"] as? List<Map<String, Any>> ?: error("Android native artifacts are missing for $abi.")
            val requiredNativeFiles = setOf("libdaidai_proot.so", "libproot_loader.so", "libdaidai_busybox.so")
            check(requiredNativeFiles.all { required -> artifacts.any { it["name"] == required } }) { "Android PRoot/BusyBox dependency manifest is incomplete for $abi." }
            artifacts.forEach { artifact ->
                val name = artifact["name"] as? String ?: error("Android native artifact has no name for $abi.")
                val binary = file("$nativeDir/$name")
                check(binary.isFile && artifact["sha256"] == sha256(binary) && (artifact["size"] as? Number)?.toLong() == binary.length()) {
                    "Android native artifact checksum or size mismatch: $name."
                }
                check(isArm64Elf(binary) && hasMinimumElfLoadAlignment(binary, 16384L)) {
                    "Android native artifact must be arm64 and 16 KB PT_LOAD aligned: $name."
                }
            }
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

}

dependencies {
    implementation("org.apache.commons:commons-compress:1.27.1")
    implementation("org.tukaani:xz:1.10")
}

flutter {
    source = "../.."
}
