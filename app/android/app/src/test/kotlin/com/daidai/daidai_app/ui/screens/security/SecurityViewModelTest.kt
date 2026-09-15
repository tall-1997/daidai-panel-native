package com.daidai.daidai_app.ui.screens.security

import com.daidai.daidai_app.data.model.AuditLog
import com.daidai.daidai_app.data.model.IpWhitelistEntry
import com.daidai.daidai_app.data.model.LoginLog
import com.daidai.daidai_app.data.model.SecurityLoadResult
import com.daidai.daidai_app.data.model.SecurityOverview
import com.daidai.daidai_app.data.model.Session
import com.daidai.daidai_app.data.model.SessionPolicy
import com.daidai.daidai_app.data.model.TwoFactorStatus
import com.daidai.daidai_app.data.remote.PanelApiException
import com.daidai.daidai_app.data.repository.SecurityDataSource
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

/**
 * 崩溃修复回归测试（历史缺陷：SecurityRepository.getSessions 遇到 HTTP 400
 * 抛出 PanelApiException → 主线程 FATAL）。
 *
 * 约定：repository 层承诺 HTTP 失败返回 [SecurityLoadResult.error] 而非抛异常；
 * 本测试用「抛出 PanelApiException(400)」的 fake 验证即使在 repository 漏容错
 * （抛异常）的情况下，ViewModel 也能把异常转换为分区错误态，页面不崩。
 */
@OptIn(ExperimentalCoroutinesApi::class)
class SecurityViewModelTest {

    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() {
        Dispatchers.setMain(dispatcher)
    }

    @After
    fun tearDown() {
        Dispatchers.resetMain()
    }

    /** 大多数分区成功，仅 sessions 抛 HTTP 400。 */
    private class SessionsThrowingDataSource : SecurityDataSource {
        override suspend fun getLoginLogs(): SecurityLoadResult<List<LoginLog>> =
            SecurityLoadResult(emptyList())

        override suspend fun getSessions(): SecurityLoadResult<List<Session>> =
            throw PanelApiException(400, """{"error":"bad request"}""")

        override suspend fun getAuditLogs(): SecurityLoadResult<List<AuditLog>> =
            SecurityLoadResult(emptyList())

        override suspend fun getSecurityOverview(): SecurityLoadResult<SecurityOverview> =
            SecurityLoadResult(SecurityOverview())

        override suspend fun getTwoFactorStatus(): SecurityLoadResult<TwoFactorStatus> =
            SecurityLoadResult(TwoFactorStatus())

        override suspend fun getIpWhitelist(): SecurityLoadResult<List<IpWhitelistEntry>> =
            SecurityLoadResult(emptyList())

        override suspend fun getSessionPolicy(): SecurityLoadResult<SessionPolicy> =
            SecurityLoadResult(SessionPolicy())

        override suspend fun revokeSession(id: Long): SecurityLoadResult<Unit> =
            SecurityLoadResult(Unit)

        override suspend fun revokeOtherSessions(): SecurityLoadResult<Unit> =
            SecurityLoadResult(Unit)

        override suspend fun addIpWhitelist(ip: String, remarks: String): SecurityLoadResult<IpWhitelistEntry?> =
            SecurityLoadResult(null)

        override suspend fun removeIpWhitelist(id: Long): SecurityLoadResult<Unit> =
            SecurityLoadResult(Unit)

        override suspend fun setSessionPolicy(maxWebSessions: Int, maxAppSessions: Int): SecurityLoadResult<Unit> =
            SecurityLoadResult(Unit)
    }

    @Test
    fun `sessions HTTP 400 does not crash and renders Error with message`() = runTest {
        val vm = SecurityViewModel(repository = SessionsThrowingDataSource())
        advanceUntilIdle()

        val state = vm.uiState.value
        // 不再进入 FATAL；漏容错的 repository 抛出的 HTTP 400 被 ViewModel 兜底
        // 转为 Error 态（而非崩溃），且错误信息被带到 UI。
        assertEquals(SecurityUiState.Phase.Error, state.phase)
        assertTrue(state.errorMessage.isNullOrBlank().not())
        // PanelApiException 的 message 优先取服务端 error 字段（"bad request"）。
        assertTrue(state.errorMessage!!.contains("bad request"))
    }

    @Test
    fun `revokeSession reports action result without crash`() = runTest {
        val fake = SessionsThrowingDataSource()
        val vm = SecurityViewModel(repository = fake)
        advanceUntilIdle()
        // 即使列表为空，调用撤销也不抛异常（空操作成功）。
        vm.revokeSession(123L)
        advanceUntilIdle()
        assertNotNull(vm.uiState.value.actionMessage)
    }

    @Test
    fun `null repository reports assembly error state`() {
        val vm = SecurityViewModel(repository = null)
        // init 内同步写入 Error，无需推进协程。
        assertEquals(SecurityUiState.Phase.Error, vm.uiState.value.phase)
        assertEquals("安全后端未装配", vm.uiState.value.errorMessage)
    }
}
