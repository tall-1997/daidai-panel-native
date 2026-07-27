package com.daidai.daidai_app

import java.security.SecureRandom
import java.util.Base64

object PanelProcessLocalToken {
    // Service instances share this token; a process restart replaces both token and Go Core.
    val value: String by lazy {
        val bytes = ByteArray(32).also(SecureRandom()::nextBytes)
        Base64.getUrlEncoder().withoutPadding().encodeToString(bytes)
    }
}
