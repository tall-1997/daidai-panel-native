package com.daidai.daidai_app.ui.screens

import android.annotation.SuppressLint
import android.os.Handler
import android.os.Looper
import android.webkit.JavascriptInterface
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.viewinterop.AndroidView
import com.daidai.daidai_app.ui.theme.AppColors
import org.json.JSONObject

@SuppressLint("SetJavaScriptEnabled")
@Composable
fun GeetestCaptchaDialog(
    captchaId: String,
    onVerified: (GeetestCaptchaPayload) -> Unit,
    onDismiss: () -> Unit,
) {
    val context = androidx.compose.ui.platform.LocalContext.current
    var webViewRef by remember { mutableStateOf<WebView?>(null) }
    var loadError by remember { mutableStateOf<String?>(null) }

    Dialog(onDismissRequest = onDismiss) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .widthIn(max = 420.dp)
                .padding(16.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text(
                text = "人机验证",
                style = MaterialTheme.typography.titleMedium,
                color = AppColors.primary,
            )
            Spacer(Modifier.height(12.dp))
            Text(
                text = "请完成下方滑块验证（由极验 GeeTest 提供）",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(8.dp))
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .then(if (loadError != null) Modifier else Modifier.padding(bottom = 8.dp)),
            ) {
                CaptchaWebView(
                    captchaId = captchaId,
                    onSuccess = onVerified,
                    onError = { loadError = it },
                    onWebViewReady = { webViewRef = it },
                )
            }
            loadError?.let { err ->
                Text(
                    text = err,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                )
                TextButton(
                    onClick = {
                        loadError = null
                        webViewRef?.reload()
                    },
                ) { Text("重新加载验证") }
            }
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = androidx.compose.foundation.layout.Arrangement.End,
            ) {
                TextButton(onClick = onDismiss) { Text("关闭") }
            }
        }
    }
}

@SuppressLint("SetJavaScriptEnabled")
@Composable
private fun CaptchaWebView(
    captchaId: String,
    onSuccess: (GeetestCaptchaPayload) -> Unit,
    onError: (String) -> Unit,
    onWebViewReady: (WebView) -> Unit,
) {
    val context = androidx.compose.ui.platform.LocalContext.current
    val webView = remember {
        WebView(context).apply {
            settings.javaScriptEnabled = true
            settings.domStorageEnabled = true
            settings.allowFileAccess = true
            webViewClient = WebViewClient()
            onWebViewReady(this)
        }
    }

    DisposableEffect(Unit) {
        webView.loadUrl("file:///android_asset/geetest_captcha.html?captcha_id=$captchaId")
        onDispose { webView.destroy() }
    }

    AndroidView(
        factory = { webView },
        update = { it },
        modifier = Modifier
            .fillMaxWidth()
            .height(320.dp),
    )

    DisposableEffect(webView, onSuccess, onError) {
        val bridge = GeetestWebViewBridge(onSuccess, onError)
        webView.addJavascriptInterface(bridge, "GeetestAndroidBridge")
        onDispose {
            runCatching { webView.removeJavascriptInterface("GeetestAndroidBridge") }
        }
    }
}

private class GeetestWebViewBridge(
    private val onSuccess: (GeetestCaptchaPayload) -> Unit,
    private val onError: (String) -> Unit,
) {
    @JavascriptInterface
    fun onVerified(jsonString: String) {
        try {
            val json = JSONObject(jsonString)
            onSuccess(
                GeetestCaptchaPayload(
                    lotNumber = json.optString("lot_number"),
                    captchaOutput = json.optString("captcha_output"),
                    passToken = json.optString("pass_token"),
                    genTime = json.optString("gen_time"),
                )
            )
        } catch (err: Exception) {
            onError("解析验证码结果失败：${err.message ?: "未知错误"}")
        }
    }

    @JavascriptInterface
    fun reportError(message: String) = onError(message)
}
