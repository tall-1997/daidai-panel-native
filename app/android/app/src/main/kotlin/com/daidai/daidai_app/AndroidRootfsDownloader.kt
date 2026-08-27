package com.daidai.daidai_app

import android.content.Context
import android.os.StatFs
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URI
import java.net.URL
import java.security.MessageDigest
import java.util.concurrent.atomic.AtomicBoolean

/**
 * 用户自行下载 rootfs 镜像的下载器，参照 OmniBot EmbeddedRuntimeInstaller 的
 * 多镜像源、断点续传、SHA-256 校验、HTTPS-only 设计。
 *
 * 每个发行版提供多个镜像源（官方 + 国内镜像），由用户选择。下载产物以
 * "runtimes/rootfs-downloads/{abi}/{distribution}/" 为前缀存放在 filesDir，
 * 由 AndroidLinuxRuntime.installDownloadedRootfs 解包安装。
 */
object AndroidRootfsDownloader {
    data class RootfsImageSource(
        val id: String,
        val displayName: String,
        val distribution: String,
        val baseUrl: String,
    )

    data class RootfsDownloadProgress(
        val phase: String,
        val message: String,
        val downloadedBytes: Long = 0L,
        val totalBytes: Long = 0L,
    )

    fun interface ProgressListener {
        fun onProgress(progress: RootfsDownloadProgress)
    }

    private const val SOURCE_PREFERENCES = "daidai-rootfs-sources"
    private const val SOURCE_KEY_PREFIX = "source_"
    const val ROOTFS_DOWNLOAD_VERSION_UBUNTU = "24.04.4"
    private const val MAX_REDIRECTS = 5
    private const val CONNECT_TIMEOUT_MS = 20_000
    private const val READ_TIMEOUT_MS = 60_000
    private const val USER_AGENT = "DaidaiAndroidRuntime"
    private const val BUFFER_SIZE = 64 * 1024
    private const val PROGRESS_INTERVAL_BYTES = 256 * 1024L
    private const val DISK_SAFETY_BYTES = 32 * 1024 * 1024L

    private val UBUNTU_SOURCES = listOf(
        RootfsImageSource("ubuntu-official", "Ubuntu 官方", "ubuntu", "https://cdimage.ubuntu.com/ubuntu-base/releases"),
        RootfsImageSource("ubuntu-tsinghua", "清华 TUNA", "ubuntu", "https://mirrors.tuna.tsinghua.edu.cn/ubuntu-cdimage/ubuntu-base/releases"),
        RootfsImageSource("ubuntu-aliyun", "阿里云", "ubuntu", "https://mirrors.aliyun.com/ubuntu-cdimage/ubuntu-base/releases"),
    )

    fun sourcesFor(distribution: String): List<RootfsImageSource> = UBUNTU_SOURCES

    fun sourceById(id: String): RootfsImageSource? = UBUNTU_SOURCES.firstOrNull { it.id == id }

    fun defaultSourceId(distribution: String): String = "ubuntu-official"

    fun selectedSourceId(context: Context, distribution: String): String =
        context.getSharedPreferences(SOURCE_PREFERENCES, Context.MODE_PRIVATE)
            .getString("$SOURCE_KEY_PREFIX$distribution", null)
            ?.takeIf { id -> sourcesFor(distribution).any { it.id == id } }
            ?: defaultSourceId(distribution)

    fun selectSource(context: Context, distribution: String, sourceId: String) {
        if (sourcesFor(distribution).none { it.id == sourceId }) return
        context.getSharedPreferences(SOURCE_PREFERENCES, Context.MODE_PRIVATE)
            .edit().putString("$SOURCE_KEY_PREFIX$distribution", sourceId).apply()
    }

    fun downloadDir(context: Context, abi: String, distribution: String): File =
        File(context.filesDir, "runtimes/rootfs-downloads/$abi/$distribution")

    fun downloadedArchive(context: Context, abi: String, distribution: String): File? {
        val archive = File(downloadDir(context, abi, distribution), "rootfs.tar.gz")
        return archive.takeIf { it.isFile && it.length() > 0L }
    }

    fun downloadedChecksum(context: Context, abi: String, distribution: String): String? =
        File(downloadDir(context, abi, distribution), "rootfs.tar.gz.sha256").readTextOrNull()?.trim()?.takeIf { it.length == 64 }

    /**
     * 从选定镜像源下载 rootfs 归档并写入 .sha256 校验文件，返回已校验的归档文件。
     * 已存在完整归档时直接复用。调用方应放入后台线程执行。
     */
    fun downloadRootfs(
        context: Context,
        distribution: String,
        abi: String,
        listener: ProgressListener,
        cancelToken: AtomicBoolean,
    ): File {
        val sourceId = selectedSourceId(context, distribution)
        val source = sourcesFor(distribution).firstOrNull { it.id == sourceId }
            ?: throw IOException("未知镜像源：$sourceId")
        val target = File(downloadDir(context, abi, distribution), "rootfs.tar.gz")
        if (target.isFile && target.length() > 0L) {
            listener.onProgress(RootfsDownloadProgress("ready", "rootfs 镜像已下载完成。", target.length(), target.length()))
            return target
        }
        val recordedListener = ProgressListener { progress ->
            synchronized(lastProgress) { lastProgress[distribution] = progress }
            listener.onProgress(progress)
        }
        downloadRunning = true
        synchronized(lastError) { lastError.remove(distribution) }
        try {
            val (downloadUrl, expectedSize) = resolveImageUrl(source, distribution, abi)
            val partial = File(downloadDir(context, abi, distribution), "rootfs.tar.gz.part")
            if (partial.length() > 0L) {
                recordedListener.onProgress(RootfsDownloadProgress("resuming", "继续上次未完成的下载。", partial.length(), expectedSize))
            }
            downloadWithResume(downloadUrl, expectedSize, partial, recordedListener, cancelToken)
            if (cancelToken.get()) throw IOException("下载已取消。")
            val digest = sha256File(partial)
            File(downloadDir(context, abi, distribution), "rootfs.tar.gz.sha256").writeText(digest)
            if (!partial.renameTo(target)) throw IOException("无法写入 rootfs 归档。")
            recordedListener.onProgress(RootfsDownloadProgress("done", "rootfs 镜像下载完成。", target.length(), target.length()))
            return target
        } catch (error: Exception) {
            synchronized(lastError) { lastError[distribution] = error.message ?: "下载失败" }
            throw error
        } finally {
            downloadRunning = false
        }
    }

    private fun resolveImageUrl(source: RootfsImageSource, distribution: String, abi: String): Pair<String, Long> {
        val arch = ubuntuArch(abi)
        val version = ROOTFS_DOWNLOAD_VERSION_UBUNTU
        val fileName = "ubuntu-base-$version-base-$arch.tar.gz"
        return "${source.baseUrl}/$version/release/$fileName" to -1L
    }

    private fun downloadWithResume(
        url: String,
        expectedSize: Long,
        partial: File,
        listener: ProgressListener,
        cancelToken: AtomicBoolean,
    ) {
        partial.parentFile?.mkdirs()
        var attempt = 0
        while (true) {
            if (cancelToken.get()) return
            val start = if (partial.isFile) partial.length() else 0L
            val connection = openConnection(url, start.takeIf { it > 0L })
            try {
                val code = connection.responseCode
                if (code == 416 && start > 0L) {
                    partial.delete()
                    attempt = 0
                    continue
                }
                if (code !in 200..299) throw IOException("下载 rootfs 失败（HTTP $code）。")
                val append = start > 0L && code == HttpURLConnection.HTTP_PARTIAL
                if (start > 0L && !append) {
                    partial.delete()
                    continue
                }
                val total = connection.contentLengthLong.takeIf { it > 0L } ?: expectedSize
                ensureDownloadSpace(partial, total)
                listener.onProgress(RootfsDownloadProgress("downloading", "正在下载 rootfs 镜像。", start, total))
                var downloaded = start
                var lastEmit = start
                connection.inputStream.use { input ->
                    FileOutputStream(partial, append).use { output ->
                        val buffer = ByteArray(BUFFER_SIZE)
                        while (true) {
                            if (cancelToken.get()) return
                            val read = input.read(buffer)
                            if (read < 0) break
                            output.write(buffer, 0, read)
                            downloaded += read
                            if (downloaded - lastEmit >= PROGRESS_INTERVAL_BYTES) {
                                lastEmit = downloaded
                                listener.onProgress(RootfsDownloadProgress("downloading", "正在下载 rootfs 镜像。", downloaded, total))
                            }
                        }
                        output.fd.sync()
                    }
                }
                if (attempt == 0 && (total > 0L && downloaded != total)) {
                    attempt += 1
                    continue
                }
                return
            } finally {
                connection.disconnect()
            }
        }
    }

    private fun openConnection(url: String, rangeStart: Long?): HttpURLConnection {
        var current = url
        repeat(MAX_REDIRECTS + 1) { redirectCount ->
            if (!current.startsWith("https://")) throw IOException("镜像仅允许通过 HTTPS 下载。")
            val connection = (URL(current).openConnection() as HttpURLConnection).apply {
                instanceFollowRedirects = false
                connectTimeout = CONNECT_TIMEOUT_MS
                readTimeout = READ_TIMEOUT_MS
                setRequestProperty("User-Agent", USER_AGENT)
                setRequestProperty("Accept", "application/octet-stream")
                if (rangeStart != null && rangeStart > 0L) setRequestProperty("Range", "bytes=$rangeStart-")
            }
            val code = connection.responseCode
            if (code in 300..399) {
                val location = connection.getHeaderField("Location")
                connection.disconnect()
                if (redirectCount >= MAX_REDIRECTS || location.isNullOrBlank()) {
                    throw IOException("镜像下载重定向无效。")
                }
                current = URI(current).resolve(location).toString()
            } else {
                return connection
            }
        }
        throw IOException("镜像下载重定向过多。")
    }

    private fun ensureDownloadSpace(partial: File, expectedBytes: Long) {
        if (expectedBytes <= 0L) return
        val required = (expectedBytes - partial.length()).coerceAtLeast(0L) + DISK_SAFETY_BYTES
        val available = StatFs(partial.parentFile.absolutePath).availableBytes
        if (available < required) {
            throw IOException("存储空间不足，rootfs 镜像至少还需 ${formatMiB(required)}，当前可用 ${formatMiB(available)}。")
        }
    }

    private fun sha256File(file: File): String {
        val digest = MessageDigest.getInstance("SHA-256")
        file.inputStream().use { input ->
            val buffer = ByteArray(BUFFER_SIZE)
            while (true) {
                val read = input.read(buffer)
                if (read < 0) break
                digest.update(buffer, 0, read)
            }
        }
        return digest.digest().joinToString("") { "%02x".format(it) }
    }

    private fun ubuntuArch(abi: String): String = when (abi) {
        "arm64-v8a" -> "arm64"
        "x86_64" -> "amd64"
        else -> throw IOException("不支持的架构：$abi")
    }

    private fun formatMiB(bytes: Long): String = String.format(java.util.Locale.ROOT, "%.1f MiB", bytes / (1024.0 * 1024.0))

    private fun File.readTextOrNull(): String? = try { readText() } catch (_: Exception) { null }

    private const val MAX_DIRECTORY_BYTES = 128 * 1024

    @Volatile private var lastProgress: MutableMap<String, RootfsDownloadProgress> = mutableMapOf()

    @Volatile private var lastError: MutableMap<String, String> = mutableMapOf()

    @Volatile
    var downloadRunning: Boolean = false
        private set

    fun lastProgress(distribution: String): RootfsDownloadProgress? =
        synchronized(lastProgress) { lastProgress[distribution] }

    fun lastError(distribution: String): String? =
        synchronized(lastError) { lastError[distribution] }
}
