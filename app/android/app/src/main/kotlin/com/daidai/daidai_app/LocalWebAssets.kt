package com.daidai.daidai_app

import android.content.Context
import java.io.File

internal object LocalWebAssets {
    fun ensureExtracted(context: Context): File {
        val destination = File(context.filesDir, "local-web")
        copyTree(context, "local-web", destination)
        require(File(destination, "index.html").isFile) { "Packaged local web index is missing" }
        return destination
    }

    private fun copyTree(context: Context, assetPath: String, destination: File) {
        val children = context.assets.list(assetPath).orEmpty()
        if (children.isEmpty()) {
            destination.parentFile?.mkdirs()
            context.assets.open(assetPath).use { input -> destination.outputStream().use(input::copyTo) }
            return
        }
        destination.mkdirs()
        children.forEach { child -> copyTree(context, "$assetPath/$child", File(destination, child)) }
    }
}
