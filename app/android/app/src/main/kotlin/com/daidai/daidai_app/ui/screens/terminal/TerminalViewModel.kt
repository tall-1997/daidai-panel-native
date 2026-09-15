package com.daidai.daidai_app.ui.screens.terminal

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.daidai.daidai_app.data.model.TerminalSessionSnapshot
import com.daidai.daidai_app.data.repository.TerminalRepository
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class TerminalUiState(
    val session: TerminalSessionSnapshot? = null,
    val transcript: String = "",
    val errorMessage: String? = null,
    val sending: Boolean = false,
    val stopping: Boolean = false,
    val closed: Boolean = false,
) {
    val isRunning: Boolean get() = session?.status == "running" && !closed
}

/**
 * PTY 终端 ViewModel（A2）。
 *
 * 生命周期：init 创建会话 → 启动输出轮询 → 用户输入 → stop（终止 PTY + DELETE 释放）。
 * ViewModel onCleared（页面退出）自动 stop + close，保证资源回收。
 *
 * transcript 采用 StringBuilder 增量拼接 + 上限裁剪，避免无界内存增长。
 */
class TerminalViewModel(
    private val repository: TerminalRepository? = null,
) : ViewModel() {

    private val _uiState = MutableStateFlow(TerminalUiState())
    val uiState: StateFlow<TerminalUiState> = _uiState.asStateFlow()

    private val transcriptBuilder = StringBuilder()
    private var pollJob: Job? = null
    private var cursor = 0L

    init {
        createSession()
    }

    /** 新建（或重建）终端会话：结束旧会话 → 创建 → 轮询。 */
    fun restart() {
        val repo = repository ?: return
        val oldId = _uiState.value.session?.id
        pollJob?.cancel()
        pollJob = null
        if (oldId != null) viewModelScope.launch { runCatching { repo.close(oldId) } }
        transcriptBuilder.setLength(0)
        cursor = 0L
        _uiState.value = TerminalUiState()
        createSession()
    }

    private fun createSession() {
        val repo = repository ?: run {
            _uiState.update {
                it.copy(errorMessage = "终端后端未装配，请先配置服务器并登录", closed = true)
            }
            return
        }
        viewModelScope.launch {
            try {
                val snapshot = repo.createSession(ROWS, COLUMNS)
                _uiState.update { it.copy(session = snapshot, errorMessage = null) }
                consume(snapshot)
                startPolling(repo, snapshot.id)
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                _uiState.update {
                    it.copy(
                        errorMessage = error.message?.takeIf(String::isNotBlank)
                            ?: "终端会话创建失败，请检查登录角色（需 operator）与 Linux 运行时",
                        closed = true,
                    )
                }
            }
        }
    }

    fun sendInput(text: String) {
        val repo = repository ?: return
        val id = _uiState.value.session?.id ?: return
        if (text.isEmpty() || !_uiState.value.isRunning) return
        viewModelScope.launch {
            _uiState.update { it.copy(sending = true) }
            try {
                repo.sendInput(id, text)
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                _uiState.update { it.copy(errorMessage = error.message ?: "输入发送失败") }
            } finally {
                _uiState.update { it.copy(sending = false) }
            }
        }
    }

    /** 回车 / 退格 / 方向键 / Ctrl+C 等控制序列。 */
    fun sendControlKey(key: ControlKey) {
        sendInput(key.sequence)
    }

    fun stop() {
        val repo = repository ?: return
        val id = _uiState.value.session?.id ?: return
        pollJob?.cancel()
        pollJob = null
        viewModelScope.launch {
            _uiState.update { it.copy(stopping = true, errorMessage = null) }
            try {
                val snapshot = repo.stop(id)
                consume(snapshot)
                _uiState.update { it.copy(session = snapshot, stopping = false, closed = true) }
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                _uiState.update {
                    it.copy(
                        stopping = false,
                        closed = true,
                        errorMessage = error.message ?: "停止终端失败",
                    )
                }
            }
            runCatching { repo.close(id) }
        }
    }

    fun clearError() {
        _uiState.update { it.copy(errorMessage = null) }
    }

    override fun onCleared() {
        super.onCleared()
        pollJob?.cancel()
        pollJob = null
        val repo = repository ?: return
        val id = _uiState.value.session?.id ?: return
        if (_uiState.value.closed) return
        // 页面退出：尽力 stop + close 回收 PTY 资源（fire-and-forget，服务端也有空闲/TTL 兜底）。
        viewModelScope.launch {
            runCatching { repo.stop(id) }
            runCatching { repo.close(id) }
        }
    }

    // ------------------------------------------------------------------

    private fun startPolling(repo: TerminalRepository, id: String) {
        pollJob?.cancel()
        pollJob = viewModelScope.launch {
            while (true) {
                try {
                    val snapshot = repo.getSession(id, cursor)
                    consume(snapshot)
                    _uiState.update {
                        it.copy(
                            session = snapshot,
                            closed = snapshot.status != "running",
                        )
                    }
                    if (snapshot.status != "running") break
                } catch (error: Exception) {
                    if (error is CancellationException) throw error
                    _uiState.update {
                        it.copy(errorMessage = error.message ?: "终端输出轮询失败")
                    }
                    break
                }
                delay(POLL_INTERVAL_MS)
            }
        }
    }

    /** 把 snapshot 中 cursor 之后的新输出并入 transcript，并推进 cursor。 */
    private fun consume(snapshot: TerminalSessionSnapshot) {
        var appended = false
        snapshot.output.forEach { chunk ->
            if (chunk.cursor > cursor) {
                transcriptBuilder.append(chunk.decodeToString())
                appended = true
            }
        }
        if (snapshot.cursor > cursor) cursor = snapshot.cursor
        trimTranscript()
        if (appended) {
            _uiState.update { it.copy(transcript = transcriptBuilder.toString()) }
        }
    }

    private fun trimTranscript() {
        if (transcriptBuilder.length <= MAX_TRANSCRIPT_CHARS) return
        transcriptBuilder.deleteRange(0, transcriptBuilder.length - MAX_TRANSCRIPT_CHARS)
    }

    private companion object {
        const val ROWS = 30
        const val COLUMNS = 80
        const val POLL_INTERVAL_MS = 200L
        const val MAX_TRANSCRIPT_CHARS = 64 * 1024
    }
}

/** 常用控制键序列（bash / xterm 约定）。 */
enum class ControlKey(val sequence: String) {
    Enter("\u000d"),
    Backspace("\u007f"),
    Tab("\u0009"),
    Escape("\u001b"),
    CtrlC("\u0003"),
    CtrlD("\u0004"),
    CtrlL("\u000c"),
    Up("\u001b[A"),
    Down("\u001b[B"),
    Right("\u001b[C"),
    Left("\u001b[D"),
}

/**
 * PTY 输出的简化转写渲染：剥离 CSI / OSC / 字符集选择等 ANSI 序列，
 * `\r\n` 与孤立 `\r` 归一为换行，退格回删一个显示字符，其余 C0 控制符丢弃。
 *
 * 诚实限制：不做 VT100 全屏重绘模拟（无光标定位 / alt screen），
 * 交互式程序（vim / top）在此仅呈现为近似线性转写。
 */
internal fun renderTranscriptForDisplay(raw: String): String {
    val out = StringBuilder(raw.length)
    var i = 0
    while (i < raw.length) {
        val c = raw[i]
        when {
            c == '\u001b' -> i = skipEscapeSequence(raw, i, out)
            c == '\r' -> {
                out.append('\n')
                i += if (i + 1 < raw.length && raw[i + 1] == '\n') 2 else 1
            }
            c == '\u0008' -> {
                if (out.isNotEmpty()) out.deleteCharAt(out.length - 1)
                i += 1
            }
            c.code < 0x20 && c != '\n' && c != '\t' -> i += 1
            else -> {
                out.append(c)
                i += 1
            }
        }
    }
    return out.toString()
}

/** 从 ESC 起始处吞掉一条转义序列，返回下一个可读字符的下标。 */
private fun skipEscapeSequence(raw: String, start: Int, out: StringBuilder): Int {
    if (start + 1 >= raw.length) return raw.length
    return when (raw[start + 1]) {
        '[' -> {
            var j = start + 2
            while (j < raw.length && raw[j] !in '\u0040'..'\u007e') j++
            j + 1
        }
        ']' -> {
            var j = start + 2
            while (j < raw.length && raw[j] != '\u0007' && !(raw[j] == '\u001b' && j + 1 < raw.length && raw[j + 1] == '\\')) j++
            when {
                j >= raw.length -> raw.length
                raw[j] == '\u0007' -> j + 1
                else -> j + 2
            }
        }
        '(', ')' -> start + 3
        else -> {
            val next = raw[start + 1]
            if (next in '@'..'Z' || next in 'a'..'z') start + 2
            else {
                out.append(' ')
                start + 1
            }
        }
    }
}
