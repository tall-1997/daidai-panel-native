package com.daidai.daidai_app.ui.screens

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

/**
 * 覆盖 LoginViewModel 的 init/login 流程，特别是修复过的竞态：
 * 在 check-init 异步刷新未完成（CheckingInitialization）时提交，
 * 必须先完成 check-init 再决定是否执行 init，避免未初始化面板直接
 * 跳过 init 而登录 401。
 */
@OptIn(ExperimentalCoroutinesApi::class)
class LoginViewModelTest {

    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() {
        Dispatchers.setMain(dispatcher)
    }

    @After
    fun tearDown() {
        Dispatchers.resetMain()
    }

    private class FakeRepository(
        var needInit: Boolean,
        val initCalls: MutableList<Pair<String, String>> = mutableListOf(),
        val loginCalls: MutableList<Pair<String, String>> = mutableListOf(),
    ) : LoginRepository {
        override suspend fun checkInit(): Boolean = needInit
        override suspend fun init(username: String, password: String) {
            initCalls.add(username to password)
            needInit = false
        }
        override suspend fun login(username: String, password: String, totpCode: String?): LoginResult =
            LoginResult(accessToken = "token-$username", username = username).also {
                loginCalls.add(username to password)
            }
    }

    @Test
    fun `submit during CheckingInitialization waits for check-init then init then login`() = runTest {
        val repo = FakeRepository(needInit = true)
        val vm = LoginViewModel(repository = repo)
        // 在 StandardTestDispatcher 下 init{} 的 check-init 尚未推进，
        // phase 仍处于初始态（不是 Ready），submit 应自行完成 check-init 再 init。
        vm.setUsername("admin")
        vm.setPassword("daidai123")

        var navigated = false
        vm.submit(onSuccess = { navigated = true })
        advanceUntilIdle()

        // 竞态修复结果：一定先跑了 init（因为 needInit=true），再 login
        assertEquals(listOf("admin" to "daidai123"), repo.initCalls)
        assertEquals(listOf("admin" to "daidai123"), repo.loginCalls)
        assertEquals(LoginUiState.Phase.Success, vm.uiState.value.phase)
        assertTrue(navigated)
    }

    @Test
    fun `submit when already initialized skips init`() = runTest {
        val repo = FakeRepository(needInit = false)
        val vm = LoginViewModel(repository = repo)
        advanceUntilIdle() // 让 init{} 的 check-init 完成，phase 变为 Ready
        assertEquals(LoginUiState.Phase.Ready, vm.uiState.value.phase)
        vm.setUsername("admin")
        vm.setPassword("daidai123")

        var navigated = false
        vm.submit(onSuccess = { navigated = true })
        advanceUntilIdle()

        assertTrue(repo.initCalls.isEmpty())
        assertEquals(listOf("admin" to "daidai123"), repo.loginCalls)
        assertEquals(LoginUiState.Phase.Success, vm.uiState.value.phase)
        assertTrue(navigated)
    }

    @Test
    fun `blank credentials show error without calling repository`() = runTest {
        val repo = FakeRepository(needInit = false)
        val vm = LoginViewModel(repository = repo)
        vm.setUsername("")
        vm.setPassword("")
        vm.submit(onSuccess = {})
        advanceUntilIdle()

        assertTrue(repo.loginCalls.isEmpty())
        assertTrue(repo.initCalls.isEmpty())
        assertEquals("请输入用户名和密码", vm.uiState.value.errorMessage)
    }
}