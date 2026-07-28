package com.daidai.daidai_app

import org.json.JSONArray
import org.json.JSONObject
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.File
import java.nio.ByteBuffer
import java.nio.charset.StandardCharsets
import java.nio.file.Files
import java.security.GeneralSecurityException
import java.security.MessageDigest
import java.security.SecureRandom
import java.time.Instant
import java.util.Base64
import java.util.zip.ZipEntry
import java.util.zip.ZipInputStream
import java.util.zip.ZipOutputStream
import javax.crypto.Cipher
import javax.crypto.SecretKeyFactory
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.PBEKeySpec
import javax.crypto.spec.SecretKeySpec

class PortableBackupEnvelope(
    private val secureRandom: SecureRandom = SecureRandom(),
) {
    companion object {
        private val MAGIC = "DAIDAI-PBE1\n".toByteArray(StandardCharsets.US_ASCII)
        private const val ENVELOPE_VERSION = 1
        private const val GCM_TAG_BITS = 128
        private const val NONCE_BYTES = 12
        private const val ARCHIVE_KEY_BYTES = 32
        private const val KDF_ITERATIONS = 210_000
        private const val KDF_SALT_BYTES = 16
        private const val KDF_KEY_BITS = 256
        private const val MAX_MANIFEST_BYTES = 1024 * 1024

        fun readManifest(envelopeBytes: ByteArray): JSONObject = parse(envelopeBytes).manifest
    }

    fun exportDirectory(
        sourceDir: File,
        password: CharArray,
        runtimeRequirements: JSONObject = JSONObject(),
    ): ByteArray {
        require(sourceDir.isDirectory) { "sourceDir must be a directory" }
        require(password.isNotEmpty()) { "password must not be empty" }

        val fileEntries = collectFiles(sourceDir)
        val archivePlaintext = zip(sourceDir, fileEntries)
        val archiveKey = randomBytes(ARCHIVE_KEY_BYTES)
        val archiveNonce = randomBytes(NONCE_BYTES)
        val archiveCiphertext = aesGcmEncrypt(archiveKey, archiveNonce, archivePlaintext)

        val kdfSalt = randomBytes(KDF_SALT_BYTES)
        val wrapKey = deriveWrapKey(password, kdfSalt)
        val wrapNonce = randomBytes(NONCE_BYTES)
        val wrappedArchiveKey = aesGcmEncrypt(wrapKey, wrapNonce, archiveKey)
        archiveKey.fill(0)
        wrapKey.fill(0)

        val manifest = JSONObject()
            .put("version", ENVELOPE_VERSION)
            .put("createdAt", Instant.now().toString())
            .put("archive", JSONObject().put("cipher", "AES-256-GCM").put("nonce", b64(archiveNonce)))
            .put(
                "keyWrap",
                JSONObject()
                    .put("cipher", "AES-256-GCM")
                    .put("wrappedArchiveKey", b64(wrappedArchiveKey))
                    .put("nonce", b64(wrapNonce))
                    .put(
                        "kdf",
                        JSONObject()
                            .put("name", "PBKDF2-HMAC-SHA256")
                            .put("reason", "platform_available_kdf_for_argon2id_slot")
                            .put("salt", b64(kdfSalt))
                            .put("iterations", KDF_ITERATIONS)
                            .put("keyBits", KDF_KEY_BITS),
                    ),
            )
            .put("runtimeRequirements", runtimeRequirements)
            .put("files", JSONArray(fileEntries.map { it.toJson() }))
            .put("archiveSha256", sha256Hex(archivePlaintext))

        val manifestBytes = manifest.toString().toByteArray(StandardCharsets.UTF_8)
        require(manifestBytes.size <= MAX_MANIFEST_BYTES) { "manifest is too large" }
        return ByteArrayOutputStream().use { output ->
            output.write(MAGIC)
            output.write(ByteBuffer.allocate(4).putInt(manifestBytes.size).array())
            output.write(manifestBytes)
            output.write(archiveCiphertext)
            output.toByteArray()
        }
    }

    fun restoreDirectory(envelopeBytes: ByteArray, password: CharArray, targetDir: File) {
        val decryptedFiles = decryptAndVerify(envelopeBytes, password)
        atomicReplace(targetDir, decryptedFiles)
    }

    private fun decryptAndVerify(envelopeBytes: ByteArray, password: CharArray): List<RestoredFile> {
        val parsed = parse(envelopeBytes)
        val manifest = parsed.manifest
        if (manifest.optInt("version") != ENVELOPE_VERSION) {
            throw PortableBackupException("unsupported envelope version")
        }

        val keyWrap = manifest.getJSONObject("keyWrap")
        val kdf = keyWrap.getJSONObject("kdf")
        val wrapKey = deriveWrapKey(password, b64decode(kdf.getString("salt")), kdf.getInt("iterations"))
        val archiveKey = try {
            aesGcmDecrypt(
                wrapKey,
                b64decode(keyWrap.getString("nonce")),
                b64decode(keyWrap.getString("wrappedArchiveKey")),
            )
        } catch (error: GeneralSecurityException) {
            throw WrongBackupPasswordException("backup password rejected", error)
        } finally {
            wrapKey.fill(0)
        }

        val archivePlaintext = try {
            aesGcmDecrypt(
                archiveKey,
                b64decode(manifest.getJSONObject("archive").getString("nonce")),
                parsed.archiveCiphertext,
            )
        } catch (error: GeneralSecurityException) {
            throw PortableBackupException("backup archive authentication failed", error)
        } finally {
            archiveKey.fill(0)
        }

        val expectedArchiveHash = manifest.getString("archiveSha256")
        if (sha256Hex(archivePlaintext) != expectedArchiveHash) {
            throw PortableBackupException("backup archive hash mismatch")
        }
        val expected = expectedFiles(manifest)
        val restored = unzip(archivePlaintext)
        if (restored.size != expected.size) {
            throw PortableBackupException("backup file set mismatch")
        }
        restored.forEach { file ->
            val hash = expected[file.path]
                ?: throw PortableBackupException("unexpected backup file ${file.path}")
            if (sha256Hex(file.bytes) != hash) {
                throw PortableBackupException("backup file hash mismatch for ${file.path}")
            }
        }
        return restored
    }

    private fun atomicReplace(targetDir: File, files: List<RestoredFile>) {
        val parent = targetDir.parentFile ?: throw PortableBackupException("targetDir must have a parent")
        parent.mkdirs()
        val staging = Files.createTempDirectory(parent.toPath(), "${targetDir.name}.restore.").toFile()
        val rollback = File(parent, "${targetDir.name}.rollback.${System.nanoTime()}")
        try {
            files.forEach { restored ->
                val output = File(staging, restored.path).canonicalFile
                val stagingRoot = staging.canonicalFile
                if (!output.path.startsWith(stagingRoot.path + File.separator) && output != stagingRoot) {
                    throw PortableBackupException("backup entry escapes target directory")
                }
                output.parentFile?.mkdirs()
                output.outputStream().use { stream ->
                    stream.write(restored.bytes)
                    stream.fd.sync()
                }
            }
            syncDirectory(staging)
            if (targetDir.exists() && !targetDir.renameTo(rollback)) {
                throw PortableBackupException("failed to stage current data for rollback")
            }
            if (!staging.renameTo(targetDir)) {
                rollback.renameTo(targetDir)
                throw PortableBackupException("failed to activate restored data")
            }
            rollback.deleteRecursively()
        } catch (error: Exception) {
            staging.deleteRecursively()
            if (rollback.exists() && !targetDir.exists()) rollback.renameTo(targetDir)
            if (error is PortableBackupException) throw error
            throw PortableBackupException(error.message ?: "restore failed", error)
        }
    }

    private fun collectFiles(sourceDir: File): List<BackupFileEntry> = sourceDir.walkTopDown()
        .filter { it.isFile }
        .map { file ->
            val relativePath = sourceDir.toPath().relativize(file.toPath()).joinToString("/") { it.toString() }
            require(!relativePath.startsWith("../") && relativePath.isNotBlank()) { "invalid backup path" }
            val bytes = file.readBytes()
            BackupFileEntry(relativePath, bytes.size.toLong(), sha256Hex(bytes))
        }
        .sortedBy { it.path }
        .toList()

    private fun zip(sourceDir: File, entries: List<BackupFileEntry>): ByteArray = ByteArrayOutputStream().use { bytes ->
        ZipOutputStream(bytes).use { zip ->
            entries.forEach { entry ->
                zip.putNextEntry(ZipEntry(entry.path))
                File(sourceDir, entry.path).inputStream().use { it.copyTo(zip) }
                zip.closeEntry()
            }
        }
        bytes.toByteArray()
    }

    private fun unzip(bytes: ByteArray): List<RestoredFile> {
        val files = mutableListOf<RestoredFile>()
        ZipInputStream(ByteArrayInputStream(bytes)).use { zip ->
            while (true) {
                val entry = zip.nextEntry ?: break
                if (!entry.isDirectory) {
                    val path = normalizeZipPath(entry.name)
                    files += RestoredFile(path, zip.readBytes())
                }
                zip.closeEntry()
            }
        }
        return files.sortedBy { it.path }
    }

    private fun normalizeZipPath(path: String): String {
        val normalized = path.replace('\\', '/')
        if (normalized.isBlank() || normalized.startsWith("/") || normalized.contains("../") || normalized == "..") {
            throw PortableBackupException("unsafe backup entry path")
        }
        return normalized
    }

    private fun expectedFiles(manifest: JSONObject): Map<String, String> {
        val files = manifest.getJSONArray("files")
        val expected = mutableMapOf<String, String>()
        for (index in 0 until files.length()) {
            val file = files.getJSONObject(index)
            expected[file.getString("path")] = file.getString("sha256")
        }
        return expected
    }

    private fun randomBytes(size: Int): ByteArray = ByteArray(size).also(secureRandom::nextBytes)

    private fun deriveWrapKey(password: CharArray, salt: ByteArray, iterations: Int = KDF_ITERATIONS): ByteArray {
        val spec = PBEKeySpec(password, salt, iterations, KDF_KEY_BITS)
        return SecretKeyFactory.getInstance("PBKDF2WithHmacSHA256").generateSecret(spec).encoded
    }

    private fun aesGcmEncrypt(key: ByteArray, nonce: ByteArray, plaintext: ByteArray): ByteArray {
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, SecretKeySpec(key, "AES"), GCMParameterSpec(GCM_TAG_BITS, nonce))
        return cipher.doFinal(plaintext)
    }

    private fun aesGcmDecrypt(key: ByteArray, nonce: ByteArray, ciphertext: ByteArray): ByteArray {
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, SecretKeySpec(key, "AES"), GCMParameterSpec(GCM_TAG_BITS, nonce))
        return cipher.doFinal(ciphertext)
    }

    private fun syncDirectory(directory: File) {
        directory.walkTopDown().filter { it.isDirectory }.forEach { it.listFiles() }
    }

    private data class BackupFileEntry(val path: String, val size: Long, val sha256: String) {
        fun toJson(): JSONObject = JSONObject()
            .put("path", path)
            .put("size", size)
            .put("sha256", sha256)
    }

    private data class RestoredFile(val path: String, val bytes: ByteArray)
}

open class PortableBackupException(message: String, cause: Throwable? = null) : RuntimeException(message, cause)

class WrongBackupPasswordException(message: String, cause: Throwable? = null) : PortableBackupException(message, cause)

private data class ParsedEnvelope(val manifest: JSONObject, val archiveCiphertext: ByteArray)

private fun parse(envelopeBytes: ByteArray): ParsedEnvelope {
    val magic = "DAIDAI-PBE1\n".toByteArray(StandardCharsets.US_ASCII)
    if (envelopeBytes.size < magic.size + 4 || !envelopeBytes.copyOfRange(0, magic.size).contentEquals(magic)) {
        throw PortableBackupException("invalid portable backup envelope")
    }
    val manifestSize = ByteBuffer.wrap(envelopeBytes, magic.size, 4).int
    if (manifestSize <= 0 || manifestSize > 1024 * 1024) {
        throw PortableBackupException("invalid portable backup manifest size")
    }
    val manifestStart = magic.size + 4
    val manifestEnd = manifestStart + manifestSize
    if (manifestEnd > envelopeBytes.size) {
        throw PortableBackupException("truncated portable backup envelope")
    }
    val manifest = JSONObject(String(envelopeBytes, manifestStart, manifestSize, StandardCharsets.UTF_8))
    val ciphertext = envelopeBytes.copyOfRange(manifestEnd, envelopeBytes.size)
    return ParsedEnvelope(manifest, ciphertext)
}

private fun b64(bytes: ByteArray): String = Base64.getEncoder().encodeToString(bytes)

private fun b64decode(value: String): ByteArray = Base64.getDecoder().decode(value)

private fun sha256Hex(bytes: ByteArray): String = MessageDigest.getInstance("SHA-256")
    .digest(bytes)
    .joinToString("") { "%02x".format(it) }
