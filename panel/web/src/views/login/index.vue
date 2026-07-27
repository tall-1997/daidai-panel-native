<script setup lang="ts">
import { ref, onMounted, computed, onUnmounted, watch } from "vue";
import { useRouter } from "vue-router";
import { useAuthStore } from "@/stores/auth";
import { useThemeStore } from "@/stores/theme";
import { authApi } from "@/api/auth";
import { ElMessage } from "element-plus";
import {
  Hide,
  Key,
  Lock,
  Moon,
  Sunny,
  User,
  View,
} from "@element-plus/icons-vue";
import Characters, { type CharacterMood } from "./Characters.vue";
import {
  createGeeTestInstance,
  type GeeTestInstance,
  type GeeTestValidateResult,
} from "@/utils/geetest";

const router = useRouter();
const authStore = useAuthStore();
const themeStore = useThemeStore();

const isInit = ref(false);
const checkingInit = ref(true);
const loading = ref(false);
const mood = ref<CharacterMood>("idle");
const mousePos = ref({ x: 0, y: 0 });
const pwdVisible = ref(false);
const focusField = ref<"none" | "username" | "password">("none");
const containerRef = ref<HTMLDivElement>();

const panelVersion = ref("");
const require2FA = ref(false);
const show2FADialog = ref(false);
const totpError = ref("");
const totpVerifying = ref(false);
const captchaConfig = ref({
  enabled: false,
  captcha_id: "",
  configured: false,
  required: false,
  require_after_failures: 0,
  message: "",
});
const captchaVisible = ref(false);
const captchaPreparing = ref(false);
const captchaReady = ref(false);
const captchaVerified = ref(false);
const captchaResult = ref<GeeTestValidateResult | null>(null);
const captchaStatusText = ref("");
let captchaInstance: GeeTestInstance | null = null;
let captchaInitPromise: Promise<GeeTestInstance> | null = null;
let pendingSubmitAfterCaptcha = false;

const lockCountdown = ref(0);
let lockTimer: ReturnType<typeof setInterval> | null = null;

function startLockCountdown(seconds: number) {
  if (lockTimer) clearInterval(lockTimer);
  lockCountdown.value = seconds;
  lockTimer = setInterval(() => {
    lockCountdown.value--;
    if (lockCountdown.value <= 0) {
      lockCountdown.value = 0;
      if (lockTimer) {
        clearInterval(lockTimer);
        lockTimer = null;
      }
    }
  }, 1000);
}

onUnmounted(() => {
  if (lockTimer) clearInterval(lockTimer);
  resetCaptchaProof();
});

const form = ref({
  username: "",
  password: "",
  confirmPassword: "",
  totp_code: "",
});

watch(
  () => form.value.username,
  () => {
    captchaConfig.value.required = false;
    pendingSubmitAfterCaptcha = false;
    resetCaptchaProof();
  },
);

onMounted(async () => {
  try {
    const res = await authApi.checkInit();
    isInit.value = !res.need_init;
  } catch {
    isInit.value = true;
  } finally {
    checkingInit.value = false;
  }
  if (isInit.value) {
    await loadCaptchaConfig(form.value.username);
  }
  try {
    const vRes = await fetch("/api/system/public-version");
    const vData = await vRes.json();
    if (vData.version) panelVersion.value = vData.version;
  } catch {}
});

function handleMouseMove(e: MouseEvent) {
  if (!containerRef.value) return;
  const rect = containerRef.value.getBoundingClientRect();
  const cx = rect.left + rect.width / 2;
  const cy = rect.top + rect.height / 2;
  const x = Math.max(-1, Math.min(1, (e.clientX - cx) / (rect.width / 2)));
  const y = Math.max(-1, Math.min(1, (e.clientY - cy) / (rect.height / 2)));
  mousePos.value = { x, y };
}

function resetCaptchaProof(keepVisible = false) {
  captchaVerified.value = false;
  captchaResult.value = null;
  if (captchaInstance) {
    captchaInstance.reset();
  }
  captchaReady.value = Boolean(captchaInstance);
  if (!keepVisible) {
    captchaVisible.value = false;
    captchaStatusText.value = "";
  }
}

async function loadCaptchaConfig(
  username = form.value.username,
  silent = true,
) {
  try {
    const res = await authApi.captchaConfig(username || undefined);
    captchaConfig.value = {
      enabled: res.enabled,
      captcha_id: res.captcha_id || "",
      configured: res.configured,
      required: res.required,
      require_after_failures: res.require_after_failures ?? 0,
      message: res.message || "",
    };

    if (!res.enabled) {
      pendingSubmitAfterCaptcha = false;
      resetCaptchaProof();
      return captchaConfig.value;
    }

    if (!captchaVerified.value) {
      captchaStatusText.value = res.required
        ? "请先完成滑块验证，验证成功后会自动登录"
        : res.message || "";
    }

    return captchaConfig.value;
  } catch (error) {
    if (!silent) {
      ElMessage.error("加载验证码配置失败");
    }
    return null;
  }
}

async function ensureCaptchaInstance() {
  if (captchaInstance) {
    return captchaInstance;
  }
  if (!captchaConfig.value.enabled || !captchaConfig.value.captcha_id) {
    throw new Error("验证码未启用或未配置完整");
  }

  if (!captchaInitPromise) {
    captchaPreparing.value = true;
    captchaInitPromise = createGeeTestInstance(
      {
        captchaId: captchaConfig.value.captcha_id,
        language: "zho",
        product: "bind",
      },
      {
        onReady() {
          captchaReady.value = true;
          captchaStatusText.value = "请拖动滑块完成拼图";
        },
        onSuccess(result) {
          captchaResult.value = result;
          captchaVerified.value = true;
          captchaVisible.value = false;
          captchaStatusText.value = "验证成功，正在登录";
          ElMessage.success("验证成功，正在登录");

          if (pendingSubmitAfterCaptcha) {
            pendingSubmitAfterCaptcha = false;
            void handleSubmit();
          }
        },
        onError(error) {
          captchaVerified.value = false;
          captchaResult.value = null;
          captchaVisible.value = false;
          pendingSubmitAfterCaptcha = false;
          captchaStatusText.value = error.message || "验证码异常，请重试";
          ElMessage.error(captchaStatusText.value);
        },
        onClose() {
          if (captchaVerified.value) return;
          captchaVisible.value = false;
          pendingSubmitAfterCaptcha = false;
          captchaStatusText.value = "验证已取消";
        },
      },
    )
      .then((instance) => {
        captchaInstance = instance;
        return instance;
      })
      .finally(() => {
        captchaPreparing.value = false;
        captchaInitPromise = null;
      });
  }

  return captchaInitPromise;
}

async function triggerCaptcha() {
  captchaVisible.value = true;
  try {
    if (!captchaConfig.value.enabled || !captchaConfig.value.captcha_id) {
      const latest = await loadCaptchaConfig(form.value.username, false);
      if (!latest?.enabled) {
        throw new Error(latest?.message || "验证码未启用");
      }
    }

    if (captchaVerified.value) {
      resetCaptchaProof(true);
    }

    captchaStatusText.value = "请拖动滑块完成拼图";
    const instance = await ensureCaptchaInstance();
    instance.show();
  } catch (error) {
    captchaVisible.value = false;
    throw error;
  }
}

function handleUsernameFocus() {
  focusField.value = "username";
  mood.value = "typing";
  pwdVisible.value = false;
}

function handleUsernameBlur() {
  handleBlur();
  void loadCaptchaConfig(form.value.username);
}

function handlePasswordFocus() {
  focusField.value = "password";
  mood.value = pwdVisible.value ? "peek" : "password";
}

function handleBlur() {
  focusField.value = "none";
  if (mood.value !== "success" && mood.value !== "error") {
    mood.value = "idle";
  }
}

function togglePwdVisible() {
  pwdVisible.value = !pwdVisible.value;
  if (focusField.value === "password") {
    mood.value = pwdVisible.value ? "peek" : "password";
  }
}

function isGatewayLikeErrorStatus(status?: number) {
  return status === 502 || status === 503 || status === 504;
}

function resolveLoginErrorMessage(err: any) {
  const status = err?.response?.status;
  const data = err?.response?.data;

  if (typeof data?.error === "string" && data.error.trim()) {
    return data.error.trim();
  }

  if (isGatewayLikeErrorStatus(status)) {
    return "登录服务暂时不可用或面板正在重启，请稍后重试";
  }

  if (typeof data === "string" && data.trim()) {
    return data.trim();
  }

  return err?.message || "操作失败";
}

function handleTOTPDialogClose() {
  show2FADialog.value = false;
  require2FA.value = false;
  form.value.totp_code = "";
  totpError.value = "";
}

async function handleTOTPSubmit() {
  if (form.value.totp_code.length !== 6) {
    totpError.value = "请输入 6 位数字";
    return;
  }
  totpVerifying.value = true;
  try {
    await handleSubmit();
  } finally {
    totpVerifying.value = false;
  }
}

async function handleSubmit() {
  if (!form.value.username || !form.value.password) {
    ElMessage.warning("请输入用户名和密码");
    return;
  }
  if (!isInit.value) {
    if (!form.value.confirmPassword) {
      ElMessage.warning("请再次输入密码");
      return;
    }
    if (form.value.password !== form.value.confirmPassword) {
      ElMessage.error("两次输入的密码不一致");
      return;
    }
  }
  if (require2FA.value && !form.value.totp_code) {
    ElMessage.warning("请输入两步验证码");
    return;
  }

  loading.value = true;
  const submittedCaptcha = captchaResult.value
    ? { ...captchaResult.value }
    : null;
  try {
    if (!isInit.value) {
      await authApi.init(form.value.username, form.value.password);
      ElMessage.success("初始化成功");
      isInit.value = true;
      form.value.confirmPassword = "";
      await loadCaptchaConfig(form.value.username);
    }

    const latestCaptchaConfig = await loadCaptchaConfig(form.value.username);
    if (latestCaptchaConfig?.enabled && !captchaVerified.value) {
      captchaVisible.value = true;
      captchaStatusText.value = "请先完成滑块验证，验证成功后会自动登录";
      pendingSubmitAfterCaptcha = true;
      await triggerCaptcha();
      return;
    }

    await authStore.login(
      form.value.username,
      form.value.password,
      form.value.totp_code,
      submittedCaptcha,
    );
    require2FA.value = false;
    show2FADialog.value = false;
    totpError.value = "";
    form.value.totp_code = "";
    captchaConfig.value.required = false;
    pendingSubmitAfterCaptcha = false;
    resetCaptchaProof();
    mood.value = "success";
    ElMessage.success("登录成功");
    setTimeout(() => {
      router.push("/");
    }, 600);
  } catch (err: any) {
    mood.value = "error";
    const data = err?.response?.data;
    const status = err?.response?.status;
    if (data?.two_factor_required) {
      require2FA.value = true;
      show2FADialog.value = true;
      resetCaptchaProof(false);
      pendingSubmitAfterCaptcha = false;
      captchaConfig.value.required = Boolean(captchaConfig.value.enabled);
      if (form.value.totp_code) {
        totpError.value = data?.error || "两步验证码错误";
        form.value.totp_code = "";
      } else {
        totpError.value = "";
      }
    }
    if (data?.locked && data?.remaining_seconds > 0) {
      startLockCountdown(data.remaining_seconds);
    }
    if (data?.captcha_id) {
      captchaConfig.value.captcha_id = data.captcha_id;
    }
    if (typeof data?.require_after_failures === "number") {
      captchaConfig.value.require_after_failures = data.require_after_failures;
    }
    if (data?.captcha_required) {
      captchaConfig.value.enabled = true;
      captchaConfig.value.required = true;
      captchaVisible.value = true;
      pendingSubmitAfterCaptcha = !data?.captcha_service_unavailable;
      if (data?.captcha_service_unavailable) {
        resetCaptchaProof(true);
        captchaStatusText.value =
          data?.error || "验证码服务暂时不可用，请稍后重试";
      } else {
        captchaStatusText.value = data?.captcha_invalid
          ? "验证码已失效，请重新完成滑块验证"
          : "请先完成滑块验证，验证成功后会自动登录";
        resetCaptchaProof(true);
        void triggerCaptcha().catch((err: any) => {
          captchaStatusText.value =
            err?.message || "验证码加载失败，请检查网络后刷新重试";
          ElMessage.error(captchaStatusText.value);
        });
      }
    } else if (submittedCaptcha) {
      resetCaptchaProof(false);
      pendingSubmitAfterCaptcha = false;
    } else if (!data || isGatewayLikeErrorStatus(status)) {
      pendingSubmitAfterCaptcha = false;
    }
    const msg = resolveLoginErrorMessage(err);
    ElMessage.error(msg);
    setTimeout(() => {
      mood.value = "idle";
    }, 2000);
  } finally {
    loading.value = false;
  }
}

const titleText = computed(() => (isInit.value ? "欢迎回来!" : "初始化管理员"));
const subtitleText = computed(() =>
  isInit.value ? "请输入您的登录信息" : "首次使用，请设置管理员账号",
);
const btnText = computed(() => (isInit.value ? "登 录" : "初始化并登录"));
const themeIcon = computed(() => (themeStore.isDark ? Sunny : Moon));
</script>

<template>
  <div class="login-page" @mousemove="handleMouseMove">
    <div class="theme-toggle">
      <el-button
        :icon="themeIcon"
        text
        circle
        size="large"
        class="theme-toggle-btn"
        @click="themeStore.toggleTheme"
      />
    </div>

    <div class="login-container" ref="containerRef">
      <div class="login-left">
        <div class="characters-wrap">
          <Characters :mouseX="mousePos.x" :mouseY="mousePos.y" :mood="mood" />
        </div>
      </div>

      <div class="login-right">
        <div v-if="checkingInit" class="login-loading">
          <LoadingMotion
            variant="infinity"
            size="lg"
            label="正在加载..."
            tone="neutral"
          />
        </div>
        <template v-else>
          <div class="login-header">
            <div class="login-logo">
              <img src="/favicon.svg" alt="呆呆面板" width="48" height="48" />
            </div>
            <h2>{{ titleText }}</h2>
            <p>{{ subtitleText }}</p>
          </div>

          <el-form @submit.prevent="handleSubmit" class="login-form">
            <el-form-item>
              <el-input
                v-model="form.username"
                placeholder="用户名"
                :prefix-icon="User"
                size="large"
                @focus="handleUsernameFocus"
                @blur="handleUsernameBlur"
              />
            </el-form-item>
            <el-form-item>
              <el-input
                v-model="form.password"
                :type="pwdVisible ? 'text' : 'password'"
                placeholder="密码"
                :prefix-icon="Lock"
                size="large"
                @focus="handlePasswordFocus"
                @blur="handleBlur"
                @keyup.enter="handleSubmit"
              >
                <template #suffix>
                  <el-icon class="pwd-toggle" @click="togglePwdVisible">
                    <View v-if="pwdVisible" />
                    <Hide v-else />
                  </el-icon>
                </template>
              </el-input>
            </el-form-item>
            <el-form-item v-if="!isInit">
              <el-input
                v-model="form.confirmPassword"
                :type="pwdVisible ? 'text' : 'password'"
                placeholder="确认密码"
                :prefix-icon="Key"
                size="large"
                @focus="handlePasswordFocus"
                @blur="handleBlur"
                @keyup.enter="handleSubmit"
              />
            </el-form-item>
            <el-form-item>
              <el-button
                type="primary"
                size="large"
                :loading="loading"
                :disabled="lockCountdown > 0"
                class="login-btn"
                @click="handleSubmit"
              >
                {{
                  lockCountdown > 0
                    ? `${Math.floor(lockCountdown / 60)}:${String(lockCountdown % 60).padStart(2, "0")} 后重试`
                    : btnText
                }}
              </el-button>
            </el-form-item>
          </el-form>

          <div class="login-version">
            呆呆面板{{ panelVersion ? ` v${panelVersion}` : "" }}
          </div>
        </template>
      </div>
    </div>

    <!-- 2FA modal: only path to satisfy two_factor_required; no UI bypass. -->
    <el-dialog
      v-model="show2FADialog"
      width="420px"
      align-center
      :close-on-click-modal="false"
      :show-close="false"
      class="totp-dialog"
      @close="handleTOTPDialogClose"
    >
      <template #header>
        <div class="totp-dialog-header">
          <div class="totp-dialog-badge" aria-hidden="true">
            <el-icon :size="18"><Lock /></el-icon>
          </div>
          <div>
            <div class="totp-dialog-title">两步验证</div>
            <div class="totp-dialog-sub">
              请输入认证器 App 上的 6 位动态验证码
            </div>
          </div>
        </div>
      </template>

      <div class="totp-dialog-body">
        <el-input
          v-model="form.totp_code"
          maxlength="6"
          placeholder="6 位数字验证码"
          size="large"
          class="totp-field"
          :autofocus="true"
          @input="totpError = ''"
          @keyup.enter="handleTOTPSubmit"
        />
        <div class="totp-hint" :class="{ 'totp-hint--error': !!totpError }">
          <template v-if="totpError">{{ totpError }}</template>
          <template v-else
            >丢失认证器？请联系管理员在用户管理里重置 2FA。</template
          >
        </div>
      </div>

      <template #footer>
        <div class="totp-dialog-footer">
          <el-button @click="handleTOTPDialogClose">取消</el-button>
          <el-button
            type="primary"
            :loading="totpVerifying || loading"
            :disabled="form.totp_code.length !== 6"
            class="totp-submit-btn"
            @click="handleTOTPSubmit"
          >
            验证并登录
          </el-button>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
/* ================= 2FA dialog ================= */
:deep(.totp-dialog) {
  .el-dialog {
    border-radius: 16px;
    overflow: hidden;
  }

  .el-dialog__header {
    padding: 20px 22px 14px;
    margin: 0;
    border-bottom: 1px solid var(--el-border-color-lighter);
  }

  .el-dialog__body {
    padding: 18px 22px;
  }

  .el-dialog__footer {
    padding: 12px 22px 18px;
  }
}

.totp-dialog-header {
  display: flex;
  align-items: center;
  gap: 12px;
}

.totp-dialog-badge {
  width: 38px;
  height: 38px;
  border-radius: 10px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  background: linear-gradient(135deg, #1677ff, #22c3aa);
  box-shadow: 0 6px 16px -8px rgba(24, 144, 255, 0.45);
  flex-shrink: 0;
}

.totp-dialog-title {
  font-size: 16px;
  font-weight: 700;
  color: var(--el-text-color-primary);
}

.totp-dialog-sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 2px;
}

.totp-dialog-body {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.totp-field {
  :deep(.el-input__wrapper) {
    border-radius: 12px;
    padding: 6px 14px;
  }

  :deep(.el-input__inner) {
    font-family: var(--dd-font-mono);
    font-size: 22px;
    letter-spacing: 0.6em;
    text-align: center;
    font-weight: 700;
    padding-left: 0.3em;
  }
}

.totp-hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  min-height: 18px;
  line-height: 1.4;
  transition: color 0.2s;

  &--error {
    color: var(--el-color-danger);
    font-weight: 600;
  }
}

.totp-dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.totp-submit-btn {
  border-radius: 10px;
  padding: 0 18px;
  background: linear-gradient(135deg, #1677ff, #22c3aa);
  border: none;
  box-shadow: 0 8px 20px -12px rgba(22, 119, 255, 0.35);

  &:hover,
  &:focus {
    background: linear-gradient(135deg, #1565d8, #1da98f);
    border: none;
  }

  &:disabled {
    background: var(--el-color-info-light-5);
    opacity: 0.6;
  }
}

.login-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #eef1f5;
  padding: 24px;
  overflow: hidden;
  position: relative;
  transition: background 0.4s ease;
}

.theme-toggle {
  position: fixed;
  top: 24px;
  right: 24px;
  z-index: 10;

  .theme-toggle-btn {
    width: 44px;
    height: 44px;
    font-size: 20px;
    color: #666;
    background: rgba(255, 255, 255, 0.7);
    backdrop-filter: blur(8px);
    border: 1px solid rgba(0, 0, 0, 0.06);
    transition: all 0.3s;

    &:hover {
      background: rgba(255, 255, 255, 0.9);
      transform: translateY(-1px);
    }
  }
}

.login-container {
  display: flex;
  width: 940px;
  max-width: 100%;
  min-height: 560px;
  border-radius: 24px;
  overflow: hidden;
  box-shadow:
    0 12px 40px rgba(0, 0, 0, 0.08),
    0 4px 12px rgba(0, 0, 0, 0.04);
  animation: loginSlideUp 0.6s ease-out;
  transition: box-shadow 0.4s ease;
}

@keyframes loginSlideUp {
  from {
    opacity: 0;
    transform: translateY(30px) scale(0.97);
  }
  to {
    opacity: 1;
    transform: translateY(0) scale(1);
  }
}

.login-left {
  flex: 1;
  background: linear-gradient(180deg, #f7fafc 0%, #eef4f8 100%);
  display: flex;
  align-items: flex-end;
  justify-content: center;
  position: relative;
  overflow: hidden;
  padding: 32px 24px 0;
  cursor: default;
  transition: background 0.4s ease;
}

.characters-wrap {
  width: 100%;
  max-width: 360px;
  transition: transform 0.1s ease-out;
}

.login-right {
  flex: 1;
  background: #fff;
  display: flex;
  flex-direction: column;
  justify-content: center;
  padding: 52px 44px;
  transition: background 0.4s ease;
}

.login-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 60px 0;
  color: #8c8c8c;
  font-size: 14px;
}

.login-header {
  text-align: center;
  margin-bottom: 36px;

  .login-logo {
    width: 48px;
    height: 48px;
    margin: 0 auto 18px;
    display: flex;
    align-items: center;
    justify-content: center;

    img {
      border-radius: 12px;
    }
  }

  h2 {
    font-size: 28px;
    font-weight: 700;
    color: #1f1f1f;
    margin: 0 0 8px;
    transition: color 0.4s;
  }

  p {
    font-size: 14px;
    color: #8c8c8c;
    margin: 0;
    transition: color 0.4s;
  }
}

.login-form {
  :deep(.el-form-item) {
    margin-bottom: 18px;
  }

  :deep(.el-input__wrapper) {
    border-radius: 10px;
    height: 46px;
    box-shadow: 0 0 0 1px #e0e0e0 inset;
    transition: all 0.3s;

    &:hover {
      box-shadow: 0 0 0 1px #1890ff inset;
    }

    &.is-focus {
      box-shadow:
        0 0 0 1px #1890ff inset,
        0 0 0 3px rgba(24, 144, 255, 0.15);
    }
  }
}

.pwd-toggle {
  cursor: pointer;
  color: #8c8c8c;
  transition: color 0.3s;

  &:hover {
    color: #1890ff;
  }
}

.login-btn {
  width: 100%;
  height: 46px;
  border-radius: 10px;
  font-weight: 600;
  font-size: 15px;
  background: #1f1f1f;
  border: none;
  transition: all 0.3s;

  &:hover {
    background: #333 !important;
    transform: translateY(-1px);
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.2);
  }

  &:active {
    transform: translateY(0);
  }
}

.login-version {
  text-align: center;
  margin-top: 16px;
  font-size: 12px;
  color: #bfbfbf;
  transition: color 0.4s;
}

@media (max-width: 768px) {
  .login-container {
    flex-direction: column;
    width: 100%;
    min-height: auto;
  }

  .login-left {
    min-height: 200px;
    padding: 30px 20px 0;
  }

  .characters-wrap {
    max-width: 240px;
  }

  .login-right {
    padding: 32px 24px;
  }
}
</style>

<style lang="scss">
html.dark {
  .login-page {
    background: #101828;
  }

  .theme-toggle .theme-toggle-btn {
    color: #c0c0c0;
    background: rgba(255, 255, 255, 0.08);
    border-color: rgba(255, 255, 255, 0.1);

    &:hover {
      background: rgba(255, 255, 255, 0.15);
      color: #ffd666;
    }
  }

  .login-container {
    box-shadow: 0 20px 60px rgba(0, 0, 0, 0.4);
  }

  .login-left {
    background: linear-gradient(180deg, #17212f 0%, #111927 100%);
  }

  .login-right {
    background: #111827;
  }

  .login-loading {
    color: #666;
  }

  .login-header {
    h2 {
      color: #e8e8ec;
    }
    p {
      color: #6b6b80;
    }
  }

  .login-form {
    .el-input__wrapper {
      background: #252540;
      box-shadow: 0 0 0 1px #3a3a55 inset;

      &:hover {
        box-shadow: 0 0 0 1px #1890ff inset;
      }

      &.is-focus {
        box-shadow:
          0 0 0 1px #1890ff inset,
          0 0 0 3px rgba(24, 144, 255, 0.2);
      }
    }

    .el-input__inner {
      color: #e0e0e8;

      &::placeholder {
        color: #555568;
      }
    }

    .el-input__prefix .el-icon,
    .el-input__suffix .el-icon {
      color: #555568;
    }
  }

  .pwd-toggle {
    color: #555568;

    &:hover {
      color: #36cfc9;
    }
  }

  .login-btn {
    background: #1890ff;

    &:hover {
      background: #1677ff !important;
      box-shadow: 0 4px 16px rgba(24, 144, 255, 0.35);
    }
  }

  .login-version {
    color: #4a4a60;
  }
}
</style>
