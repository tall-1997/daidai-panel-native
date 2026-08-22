package com.daidai.daidai_app

import android.content.Context
import fi.iki.elonen.NanoHTTPD
import java.io.InputStream
import java.security.MessageDigest
import java.security.SecureRandom
import java.util.Base64
import java.util.concurrent.ConcurrentHashMap

internal class LocalBrowserCredentials(
    private val nowMillis: () -> Long = System::currentTimeMillis,
    private val tokenFactory: () -> String = {
        ByteArray(32).also(SecureRandom()::nextBytes)
            .let { Base64.getUrlEncoder().withoutPadding().encodeToString(it) }
    },
) {
    private val tickets = ConcurrentHashMap<String, Long>()
    private val sessions = ConcurrentHashMap<String, Long>()

    fun issueTicket(): String {
        purgeExpired()
        return tokenFactory().also { tickets[digest(it)] = nowMillis() + TICKET_TTL_MILLIS }
    }

    fun redeem(ticket: String): String? {
        purgeExpired()
        if (ticket.isBlank()) return null
        val expiry = tickets.remove(digest(ticket)) ?: return null
        if (expiry <= nowMillis()) return null
        return tokenFactory().also { sessions[digest(it)] = nowMillis() + SESSION_TTL_MILLIS }
    }

    fun hasSession(token: String): Boolean {
        purgeExpired()
        return token.isNotBlank() && (sessions[digest(token)] ?: 0L) > nowMillis()
    }

    fun clear() {
        tickets.clear()
        sessions.clear()
    }

    private fun purgeExpired() {
        val now = nowMillis()
        tickets.entries.removeIf { it.value <= now }
        sessions.entries.removeIf { it.value <= now }
    }

    private fun digest(value: String): String = MessageDigest.getInstance("SHA-256")
        .digest(value.toByteArray())
        .joinToString("") { "%02x".format(it) }

    companion object {
        private const val TICKET_TTL_MILLIS = 30_000L
        private const val SESSION_TTL_MILLIS = 15 * 60_000L
    }
}

internal class LocalBrowserAccess(
    private val context: Context,
    private val endpoint: () -> String,
    private val nowMillis: () -> Long = System::currentTimeMillis,
) {
    private val credentials = LocalBrowserCredentials(nowMillis)

    fun createUrl(): String {
        val ticket = credentials.issueTicket()
        return "${endpoint()}/local-ui/#ticket=$ticket"
    }

    fun hasSession(headers: Map<String, String>): Boolean {
        val cookie = header(headers, "cookie").orEmpty()
            .split(';')
            .map(String::trim)
            .firstOrNull { it.startsWith("$COOKIE_NAME=") }
            ?.substringAfter('=')
            ?: return false
        return credentials.hasSession(cookie)
    }

    fun serve(session: NanoHTTPD.IHTTPSession, authority: String): NanoHTTPD.Response {
        if (header(session.headers, "host") != authority) return response(NanoHTTPD.Response.Status.BAD_REQUEST, "text/plain", "Invalid Host")
        val origin = header(session.headers, "origin")
        if (origin != null && origin != endpoint()) return response(NanoHTTPD.Response.Status.FORBIDDEN, "text/plain", "Invalid Origin")
        if (session.uri == "/local-ui/session") {
            if (session.method != NanoHTTPD.Method.POST) {
                return response(NanoHTTPD.Response.Status.METHOD_NOT_ALLOWED, "text/plain", "Method not allowed")
            }
            return exchange(session)
        }
        if (session.method != NanoHTTPD.Method.GET && session.method != NanoHTTPD.Method.HEAD) {
            return response(NanoHTTPD.Response.Status.METHOD_NOT_ALLOWED, "text/plain", "Method not allowed")
        }
        if (session.uri == "/local-ui") {
            return response(NanoHTTPD.Response.Status.REDIRECT, "text/plain", "Redirecting").apply {
                addHeader("Location", "/local-ui/")
            }
        }
        return staticAsset(session.uri)
    }

    fun clear() {
        credentials.clear()
    }

    private fun exchange(session: NanoHTTPD.IHTTPSession): NanoHTTPD.Response {
        val files = mutableMapOf<String, String>()
        runCatching { session.parseBody(files) }
            .getOrElse { return response(NanoHTTPD.Response.Status.BAD_REQUEST, "text/plain", "Invalid ticket") }
        val ticket = files["postData"].orEmpty().take(1024)
        val sessionToken = credentials.redeem(ticket)
            ?: return response(NanoHTTPD.Response.Status.UNAUTHORIZED, "text/plain", "Invalid ticket")
        return response(NanoHTTPD.Response.Status.NO_CONTENT, "text/plain", "").apply {
            addHeader("Set-Cookie", "$COOKIE_NAME=$sessionToken; HttpOnly; SameSite=Strict; Path=/; Max-Age=900")
        }
    }

    private fun staticAsset(uri: String): NanoHTTPD.Response {
        val relative = uri.removePrefix("/local-ui/").ifBlank { "index.html" }
        if (relative.split('/').any { it == ".." || it.isBlank() }) return response(NanoHTTPD.Response.Status.NOT_FOUND, "text/plain", "Not found")
        val assetPath = "local-web/$relative"
        val input = runCatching { context.assets.open(assetPath) }.getOrNull()
            ?: run {
                if (relative.contains('.')) {
                    return response(NanoHTTPD.Response.Status.NOT_FOUND, "text/plain", "Not found")
                }
                runCatching { context.assets.open("local-web/index.html") }.getOrNull()
                    ?: return response(NanoHTTPD.Response.Status.NOT_FOUND, "text/plain", "Not found")
            }
        return streamResponse(input, contentType(relative))
    }

    private fun streamResponse(input: InputStream, contentType: String): NanoHTTPD.Response =
        NanoHTTPD.newChunkedResponse(NanoHTTPD.Response.Status.OK, contentType, input).also(::secureHeaders)

    private fun response(status: NanoHTTPD.Response.Status, type: String, body: String): NanoHTTPD.Response =
        NanoHTTPD.newFixedLengthResponse(status, type, body).also(::secureHeaders)

    private fun secureHeaders(response: NanoHTTPD.Response) {
        response.addHeader("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
        response.addHeader("X-Content-Type-Options", "nosniff")
        response.addHeader("X-Frame-Options", "DENY")
        response.addHeader("Referrer-Policy", "no-referrer")
        response.addHeader("Cache-Control", "no-store")
    }

    private fun header(headers: Map<String, String>, name: String): String? = headers.entries.singleOrNull { it.key.equals(name, true) }?.value
    private fun contentType(path: String): String = when (path.substringAfterLast('.', "")) {
        "html" -> "text/html; charset=utf-8"
        "js" -> "application/javascript; charset=utf-8"
        "css" -> "text/css; charset=utf-8"
        "json", "map" -> "application/json; charset=utf-8"
        "svg" -> "image/svg+xml"
        "png" -> "image/png"
        "woff2" -> "font/woff2"
        else -> "application/octet-stream"
    }

    companion object {
        private const val COOKIE_NAME = "daidai_local_session"
    }
}
