import java.io.File
import java.io.FileInputStream
import java.io.RandomAccessFile
import java.security.MessageDigest
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
@Suppress("UNCHECKED_CAST")
val androidAbiMatrix = JsonSlurper().parse(rootProject.file("../../runtime/android-abi-matrix.json")) as Map<String, Any>
check((androidAbiMatrix["schema_version"] as? Number)?.toInt() == 1) { "Android ABI matrix schema must be version 1." }
@Suppress("UNCHECKED_CAST")
val androidAbiConfigs = androidAbiMatrix["abis"] as? Map<String, Map<String, Any>>
    ?: error("Android ABI matrix must define ABIs.")
val defaultAndroidAbi = androidAbiMatrix["default_abi"] as? String
    ?: error("Android ABI matrix must define a default ABI.")
val requestedAbis = (System.getenv("ANDROID_RUNTIME_ABIS")?.replace(',', ' ')?.split(Regex("\\s+"))?.filter(String::isNotBlank)
    ?: listOf(defaultAndroidAbi)).distinct()
check(requestedAbis.size == 1) { "Android runtime APK builds must select exactly one ABI." }
val flutterSplitPerAbi = System.getenv("FLUTTER_SPLIT_PER_ABI") == "true"
requestedAbis.forEach { abi ->
    val config = androidAbiConfigs[abi] ?: error("Unknown Android runtime ABI: $abi")
    check(config["package"] == true) { "Android runtime ABI is not packageable: $abi" }
}

check(!requireReleaseSigning || hasReleaseSigning) {
    "Release signing is required, but KEYSTORE_FILE, KEYSTORE_PASSWORD, KEYSTORE_ALIAS, or KEYSTORE_KEY_PASSWORD is missing."
}

android {
    namespace = "com.daidai.daidai_app"
    compileSdk = 36
    ndkVersion = flutter.ndkVersion
    testBuildType = "release"

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
            if (!flutterSplitPerAbi) {
                abiFilters += requestedAbis
            }
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
            isDebuggable = false
        }
    }

    packaging {
        jniLibs {
            useLegacyPackaging = true
            excludes += setOf("**/libpython_exec.so")
            // Runtime manifests pin exact ELF bytes, so APK packaging must not strip them again.
            keepDebugSymbols += "**/*.so"
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
        buildConfig = true
    }

    defaultConfig {
        buildConfigField("String", "PACKAGED_RUNTIME_ABIS", "\"${requestedAbis.joinToString(",")}\"")
        buildConfigField("String", "RUNTIME_UBUNTU_ARCHES", "\"${androidAbiConfigs.entries.joinToString(",") { "${it.key}=${it.value["ubuntu_arch"]}" }}\"")
        buildConfigField("String", "RUNTIME_LINUX_MIRRORS", "\"${androidAbiConfigs.entries.joinToString(",") { "${it.key}=${it.value["ubuntu_mirror"]}" }}\"")
    }

    externalNativeBuild {
        cmake {
            path = file("src/main/jni/CMakeLists.txt")
        }
    }

    sourceSets {
        getByName("main") {
            assets.setSrcDirs(listOf(
                "../../../runtime",
                layout.buildDirectory.dir("generated/localWebAssets").get().asFile,
                layout.buildDirectory.dir("generated/runtimeAssets").get().asFile,
            ))
            // Python and Node payloads are packaged as archives under src/main/assets.
            // The generated pythonAssets/nodeAssets trees are build-time verification inputs;
            // adding them as Android asset roots duplicates archive metadata paths.
        }
    }
}

val panelWebDir = rootProject.file("../../panel/web")
val generatedLocalWebDir = layout.buildDirectory.dir("generated/localWebAssets/local-web")
val generatedRuntimeAssetsDir = layout.buildDirectory.dir("generated/runtimeAssets")
val nodeVersion = providers.exec { commandLine("node", "--version") }.standardOutput.asText.map(String::trim)
// Windows ships npm as npm.cmd, which CreateProcess cannot resolve by bare name.
val npmCommand = if (org.gradle.internal.os.OperatingSystem.current().isWindows) "npm.cmd" else "npm"
val npmVersion = providers.exec { commandLine(npmCommand, "--version") }.standardOutput.asText.map(String::trim)
val localWebBuildMode = "true"
val panelWebBuildInputs = fileTree(panelWebDir) {
    exclude("node_modules/**", "dist/**", ".git/**")
}

val installLocalPanelWebDependencies = tasks.register<Exec>("installLocalPanelWebDependencies") {
    group = "build setup"
    description = "Installs locked Panel Web dependencies for the Android asset build."
    workingDir(panelWebDir)
    commandLine(npmCommand, "ci", "--no-audit", "--no-fund")
    inputs.files(panelWebDir.resolve("package.json"), panelWebDir.resolve("package-lock.json"))
    inputs.property("nodeVersion", nodeVersion)
    inputs.property("npmVersion", npmVersion)
    outputs.dir(panelWebDir.resolve("node_modules"))
    doFirst {
        check(nodeVersion.get().matches(Regex("v20\\.19\\.\\d+"))) { "Android Panel Web build requires Node 20.19.x." }
        check(npmVersion.get() == "10.8.2") { "Android Panel Web build requires npm 10.8.2." }
    }
}

val buildLocalPanelWeb = tasks.register<Exec>("buildLocalPanelWeb") {
    dependsOn(installLocalPanelWebDependencies)
    group = "build"
    description = "Builds the loopback-only Panel Web bundle."
    workingDir(panelWebDir)
    commandLine(npmCommand, "run", "build")
    environment("VITE_LOCAL_WEB_BUILD", localWebBuildMode)
    inputs.files(panelWebBuildInputs)
    inputs.property("nodeVersion", nodeVersion)
    inputs.property("npmVersion", npmVersion)
    inputs.property("VITE_LOCAL_WEB_BUILD", localWebBuildMode)
    outputs.dir(panelWebDir.resolve("dist"))
}

val packageLocalPanelWeb = tasks.register<Sync>("packageLocalPanelWeb") {
    dependsOn(buildLocalPanelWeb)
    from(panelWebDir.resolve("dist"))
    into(generatedLocalWebDir)
}

val packageSelectedRuntimeAssets = tasks.register<Sync>("packageSelectedRuntimeAssets") {
    group = "build"
    description = "Stages common Android assets and the selected ABI runtime."
    from("src/main/assets") {
        exclude("android-runtime/**")
    }
    requestedAbis.forEach { abi ->
        from("src/main/assets/android-runtime/$abi") {
            into("android-runtime/$abi")
        }
    }
    into(generatedRuntimeAssetsDir)
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
    implementation("org.nanohttpd:nanohttpd:2.3.1")
    implementation("androidx.work:work-runtime-ktx:2.11.1")
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.json:json:20250517")
    androidTestImplementation("androidx.test:core-ktx:1.6.1")
    androidTestImplementation("androidx.test:runner:1.6.2")
    androidTestImplementation("androidx.test.ext:junit-ktx:1.2.1")
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
    description = "Verifies the Ubuntu rootfs assets and pinned 16 KB-aligned Android native tools."
    doLast {
        val distroCommands = mapOf(
            "ubuntu" to listOf("apt-get", "bash", "python3", "pip3", "node", "npm", "pnpm"),
        )
        val distroPackages = mapOf(
            "ubuntu" to setOf("bash", "python3", "python3-pip", "nodejs", "npm", "ca-certificates"),
        )
        val distroCapabilities = mapOf(
            "ubuntu" to mapOf(
                "package_manager" to listOf("apt-get"),
                "shell" to listOf("bash"),
                "python" to listOf("python3", "pip3"),
                "node" to listOf("node", "npm", "pnpm"),
            ),
        )
        @Suppress("UNCHECKED_CAST")
        val trustedRootfsSources = JsonSlurper().parse(rootProject.file("../scripts/rootfs-trusted-sources.json")) as Map<String, Any>
        check((trustedRootfsSources["schema_version"] as? Number)?.toInt() == 1) { "Trusted rootfs source schema must be version 1." }
        requestedAbis.forEach { abi ->
            val abiConfig = androidAbiConfigs.getValue(abi)
            val expectedMachine = (abiConfig["elf_machine"] as Number).toInt()
            val minimumAlignment = (abiConfig["minimum_load_alignment"] as Number).toLong()
            val nativeDir = file("src/main/jniLibs/$abi")
            val proot = listOf("libdaidai_proot.so").map { file("$nativeDir/$it") }.firstOrNull { it.isFile }
            check(proot != null && isExpectedElf(proot, expectedMachine) && hasMinimumElfLoadAlignment(proot, minimumAlignment)) {
                "Android PRoot runner for $abi must match ELF machine $expectedMachine and the load alignment policy."
            }
            check(file("$nativeDir/libproot_loader.so").isFile) { "Missing PRoot loader for $abi." }
            check(file("$nativeDir/libyaegi_exec.so").isFile) { "Missing Yaegi runtime for $abi." }

            // Ubuntu rootfs 资产必须自洽（sha256 + manifest 一致）。
            val presentDistributions = listOf("ubuntu").filter { distribution ->
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
                val aptPackages = (rootfsManifest["apt_packages"] as? List<*>)?.filterIsInstance<String>()
                    ?: error("$distribution rootfs apt_packages are missing for $abi.")
                check(distroPackages[distribution]!!.all { it in aptPackages }) { "$distribution rootfs required apt package list is incomplete for $abi." }
                @Suppress("UNCHECKED_CAST")
                val globalTools = rootfsManifest["global_tools"] as? Map<String, Any>
                    ?: error("$distribution rootfs global_tools are missing for $abi.")
                @Suppress("UNCHECKED_CAST")
                val pnpm = globalTools["pnpm"] as? Map<String, Any>
                    ?: error("$distribution rootfs pnpm metadata is missing for $abi.")
                check(pnpm["install_source"] == "npm-global") { "$distribution rootfs pnpm install source mismatch for $abi." }
                check((pnpm["version"] as? String)?.matches(Regex("[0-9]+(?:\\.[0-9]+){2}")) == true) {
                    "$distribution rootfs pnpm version is invalid for $abi."
                }
                check("pnpm" in (rootfsManifest["required_commands"] as? List<*>).orEmpty()) {
                    "$distribution rootfs pnpm command contract is missing for $abi."
                }
                @Suppress("UNCHECKED_CAST")
                val baseArchive = rootfsManifest["base_archive"] as? Map<String, Any> ?: error("$distribution base archive provenance is missing for $abi.")
                @Suppress("UNCHECKED_CAST")
                val distributionSources = trustedRootfsSources[distribution] as? Map<String, Any> ?: error("Trusted source distribution is missing: $distribution.")
                @Suppress("UNCHECKED_CAST")
                val versionSources = distributionSources[rootfsManifest["ubuntu_version"]] as? Map<String, Any> ?: error("Trusted source version is missing for $distribution.")
                @Suppress("UNCHECKED_CAST")
                val trustedBaseArchive = versionSources[rootfsManifest["ubuntu_arch"]] as? Map<String, Any> ?: error("Trusted source architecture is missing for $distribution.")
                check(baseArchive == trustedBaseArchive) { "$distribution base archive provenance mismatch for $abi." }
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
            val manifestNames = artifacts.mapNotNull { it["name"] as? String }.toSet()
            val packagedElfNames = nativeDir.listFiles().orEmpty()
                .filter { it.isFile && it.extension == "so" && isElf(it) }
                .map { it.name }
                .toSet()
            check(manifestNames == packagedElfNames) {
                "Android native manifest must cover every packaged ELF for $abi; missing=${packagedElfNames - manifestNames}, extra=${manifestNames - packagedElfNames}."
            }
            val requiredNativeFiles = setOf("libdaidai_proot.so", "libproot_loader.so", "libdaidai_busybox.so")
            check(requiredNativeFiles.all { required -> artifacts.any { it["name"] == required } }) { "Android PRoot/BusyBox dependency manifest is incomplete for $abi." }
            artifacts.forEach { artifact ->
                val name = artifact["name"] as? String ?: error("Android native artifact has no name for $abi.")
                val binary = file("$nativeDir/$name")
                check(binary.isFile && artifact["sha256"] == sha256(binary) && (artifact["size"] as? Number)?.toLong() == binary.length()) {
                    "Android native artifact checksum or size mismatch: $name."
                }
                check(isExpectedElf(binary, expectedMachine) && hasMinimumElfLoadAlignment(binary, minimumAlignment)) {
                    "Android native artifact must match $abi and its load alignment policy: $name."
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

fun isElf(file: File): Boolean {
    if (!file.isFile || file.length() < 20) return false
    val header = file.inputStream().use { input -> ByteArray(20).also { input.read(it) } }
    return header[0] == 0x7f.toByte() && header[1] == 'E'.code.toByte() && header[2] == 'L'.code.toByte() && header[3] == 'F'.code.toByte()
}

fun isExpectedElf(file: File, expectedMachine: Int): Boolean {
    val header = file.inputStream().use { input -> ByteArray(20).also { input.read(it) } }
    if (header[0] != 0x7f.toByte() || header[1] != 'E'.code.toByte() || header[2] != 'L'.code.toByte() || header[3] != 'F'.code.toByte()) {
        return false
    }
    if (header[4] != 2.toByte() || header[5] != 1.toByte()) return false
    val machine = (header[18].toInt() and 0xff) or ((header[19].toInt() and 0xff) shl 8)
    return machine == expectedMachine
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
    dependsOn(packageSelectedRuntimeAssets)
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
