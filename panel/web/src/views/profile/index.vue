<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import {
  CircleCheck,
  Key,
  Lock,
  RefreshRight,
  User,
  Clock,
  Avatar,
  Star,
  InfoFilled,
  Camera,
  Delete,
} from "@element-plus/icons-vue";
import { authApi } from "@/api/auth";
import { securityApi } from "@/api/security";
import {
  sponsorApi,
  type SponsorRecord,
  type SponsorSummary,
} from "@/api/sponsor";
import { useAuthStore } from "@/stores/auth";
import { createQrCodeDataUrl } from "@/utils/qrcode";
import { useResponsive } from "@/composables/useResponsive";
import SponsorWall from "./components/SponsorWall.vue";

const authStore = useAuthStore();
const { dialogFullscreen } = useResponsive();
const activeTab = ref<"profile" | "security" | "sponsors">("profile");

const passwordForm = ref({
  oldPassword: "",
  newPassword: "",
  confirmPassword: "",
});
const passwordSaving = ref(false);

const twoFAEnabled = ref(false);
const twoFASecret = ref("");
const twoFAUri = ref("");
const twoFAQrUrl = ref("");
const twoFACode = ref("");
const showSetup2FA = ref(false);
const twoFALoading = ref(false);
const twoFADisabling = ref(false);

const sponsors = ref<SponsorRecord[]>([]);
const sponsorSummary = ref<SponsorSummary | null>(null);
const sponsorLoading = ref(false);
const sponsorRefreshing = ref(false);
const sponsorFetchedOnce = ref(false);
let sponsorRefreshTimer: ReturnType<typeof setInterval> | null = null;

const sponsorRefreshInterval = 24 * 60 * 60 * 1000;

const roleLabel = computed(() => {
  const role = authStore.user?.role;
  if (role === "admin") return "管理员";
  if (role === "operator") return "运维用户";
  return "只读用户";
});

const roleTagType = computed(() => {
  const role = authStore.user?.role;
  if (role === "admin") return "danger";
  if (role === "operator") return "warning";
  return "info";
});

function formatTime(value?: string | null) {
  if (!value) return "-";
  return new Date(value).toLocaleString();
}

function buildEmptySponsorSummary(unavailable = false): SponsorSummary {
  return {
    sponsors: [],
    count: 0,
    total_amount: 0,
    updated_at: null,
    unavailable,
  };
}

function applySponsorSummary(summary: SponsorSummary) {
  const normalizedSponsors = Array.isArray(summary.sponsors)
    ? summary.sponsors
    : [];
  sponsors.value = normalizedSponsors;
  sponsorSummary.value = {
    ...buildEmptySponsorSummary(),
    ...summary,
    sponsors: normalizedSponsors,
    count: summary.count || normalizedSponsors.length,
  };
}

async function loadSponsors(options: { silent?: boolean } = {}) {
  const useSilentRefresh = options.silent === true && sponsorFetchedOnce.value;
  if (useSilentRefresh) {
    sponsorRefreshing.value = true;
  } else {
    sponsorLoading.value = true;
  }

  try {
    const res = (await sponsorApi.list()) as any;
    applySponsorSummary({
      ...buildEmptySponsorSummary(),
      ...(res.data || {}),
      sponsors: res.data?.sponsors || [],
    });
  } catch {
    if (!sponsorFetchedOnce.value) {
      applySponsorSummary(buildEmptySponsorSummary(true));
    } else {
      sponsorSummary.value = {
        ...(sponsorSummary.value || buildEmptySponsorSummary()),
        unavailable: true,
      };
    }
  } finally {
    sponsorFetchedOnce.value = true;
    sponsorLoading.value = false;
    sponsorRefreshing.value = false;
  }
}

function stopSponsorRefreshTimer() {
  if (sponsorRefreshTimer) {
    clearInterval(sponsorRefreshTimer);
    sponsorRefreshTimer = null;
  }
}

function startSponsorRefreshTimer() {
  stopSponsorRefreshTimer();
  sponsorRefreshTimer = setInterval(() => {
    void loadSponsors({ silent: true });
  }, sponsorRefreshInterval);
}

async function activateSponsorTab() {
  if (!sponsorFetchedOnce.value) {
    await loadSponsors();
  }
  startSponsorRefreshTimer();
}

async function handleManualSponsorRefresh() {
  await loadSponsors({ silent: sponsorFetchedOnce.value });
}

const sponsorTabHint = computed(() => {
  if (sponsorLoading.value && !sponsorFetchedOnce.value) {
    return "正在同步赞助名单";
  }
  if (sponsorRefreshing.value) {
    return "后台同步中";
  }
  if (sponsorSummary.value?.unavailable && sponsors.value.length > 0) {
    return "当前展示最近一次同步结果";
  }
  if (sponsorSummary.value?.unavailable) {
    return "服务暂不可用，页面会自动重试";
  }
  if (sponsorSummary.value?.updated_at) {
    return `更新于 ${formatTime(sponsorSummary.value.updated_at)}`;
  }
  return "本页每 24 小时静默同步一次";
});

const editingUsername = ref(false);
const newUsername = ref("");
const usernameSaving = ref(false);

function startEditUsername() {
  newUsername.value = authStore.user?.username || "";
  editingUsername.value = true;
}

async function saveUsername() {
  const name = newUsername.value.trim();
  if (!name) {
    ElMessage.warning("用户名不能为空");
    return;
  }
  if (!/^[\p{L}\p{N}_]{1,32}$/u.test(name)) {
    ElMessage.warning("用户名需 1-32 位，支持中文、字母、数字和下划线");
    return;
  }
  if (name === authStore.user?.username) {
    editingUsername.value = false;
    return;
  }
  usernameSaving.value = true;
  try {
    await authApi.changeUsername(name);
    ElMessage.success("用户名修改成功，请重新登录");
    editingUsername.value = false;
    authStore.logout();
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.error || "修改用户名失败");
  } finally {
    usernameSaving.value = false;
  }
}

const usernameInitial = computed(() => {
  const name = authStore.user?.username || "";
  if (!name) return "用";
  return (name[0] ?? "用").toUpperCase();
});

const avatarCacheBuster = ref(Date.now());
const avatarLoadFailed = ref(false);
const hasAvatar = computed(() => Boolean(authStore.user?.avatar_url));
const avatarUrl = computed(() => {
  const url = authStore.user?.avatar_url || "";
  if (!url || avatarLoadFailed.value) return "";
  const separator = url.includes("?") ? "&" : "?";
  return `${url}${separator}t=${avatarCacheBuster.value}`;
});
const avatarUploading = ref(false);
const avatarInputRef = ref<HTMLInputElement | null>(null);

watch(
  () => authStore.user?.avatar_url,
  () => {
    avatarLoadFailed.value = false;
  },
  { immediate: true },
);

function triggerAvatarUpload() {
  avatarInputRef.value?.click();
}

function handleAvatarLoadError() {
  avatarLoadFailed.value = true;
}

async function handleAvatarFileChange(e: Event) {
  const input = e.target as HTMLInputElement;
  const file = input.files?.[0];
  if (!file) return;
  input.value = "";

  const maxSize = 2 * 1024 * 1024;
  if (file.size > maxSize) {
    ElMessage.warning("头像文件不能超过 2MB");
    return;
  }

  const allowed = ["image/jpeg", "image/png", "image/gif", "image/webp"];
  if (!allowed.includes(file.type)) {
    ElMessage.warning("仅支持 JPG、PNG、GIF、WebP 格式");
    return;
  }

  avatarUploading.value = true;
  try {
    await authApi.uploadAvatar(file);
    ElMessage.success("头像上传成功");
    avatarLoadFailed.value = false;
    await authStore.fetchUser();
    avatarCacheBuster.value = Date.now();
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.error || "头像上传失败");
  } finally {
    avatarUploading.value = false;
  }
}

async function handleDeleteAvatar() {
  try {
    await ElMessageBox.confirm("确定要删除当前头像吗？", "确认", {
      type: "warning",
    });
  } catch {
    return;
  }
  try {
    await authApi.deleteAvatar();
    ElMessage.success("头像已删除");
    avatarLoadFailed.value = false;
    await authStore.fetchUser();
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.error || "删除头像失败");
  }
}

async function load2FAStatus() {
  try {
    const res = await securityApi.get2FAStatus();
    twoFAEnabled.value = res.data.enabled;
  } catch {
    ElMessage.error("加载 2FA 状态失败");
  }
}

async function handleChangePassword() {
  if (!passwordForm.value.oldPassword || !passwordForm.value.newPassword) {
    ElMessage.warning("请完整填写密码信息");
    return;
  }
  if (passwordForm.value.newPassword.length < 6) {
    ElMessage.warning("新密码至少 6 位");
    return;
  }
  if (passwordForm.value.newPassword !== passwordForm.value.confirmPassword) {
    ElMessage.warning("两次输入的新密码不一致");
    return;
  }

  passwordSaving.value = true;
  try {
    await authApi.changePassword(
      passwordForm.value.oldPassword,
      passwordForm.value.newPassword,
    );
    ElMessage.success("密码修改成功，即将重新登录");
    passwordForm.value = {
      oldPassword: "",
      newPassword: "",
      confirmPassword: "",
    };
    const LOGOUT_DELAY_MS = 1200;
    setTimeout(() => {
      authStore.logout();
    }, LOGOUT_DELAY_MS);
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.error || "密码修改失败");
  } finally {
    passwordSaving.value = false;
  }
}

async function handleSetup2FA() {
  twoFALoading.value = true;
  try {
    const res = await securityApi.setup2FA();
    twoFASecret.value = res.data.secret;
    twoFAUri.value = res.data.uri;
    twoFAQrUrl.value = await createQrCodeDataUrl(res.data.uri, 220);
    twoFACode.value = "";
    showSetup2FA.value = true;
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.error || "初始化 2FA 失败");
  } finally {
    twoFALoading.value = false;
  }
}

async function handleVerify2FA() {
  if (!twoFACode.value.trim()) {
    ElMessage.warning("请输入验证码");
    return;
  }
  try {
    await securityApi.verify2FA(twoFACode.value.trim());
    ElMessage.success("2FA 已启用");
    twoFAEnabled.value = true;
    showSetup2FA.value = false;
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.error || "验证码错误");
  }
}

async function handleDisable2FA() {
  let prompted: { value: string };
  try {
    prompted = (await ElMessageBox.prompt(
      "为了确认操作本人持有认证器，请输入当前的 6 位动态验证码后再禁用 2FA。",
      "禁用双因素认证",
      {
        inputPattern: /^\d{6}$/,
        inputErrorMessage: "请输入 6 位数字验证码",
        confirmButtonText: "确认禁用",
        cancelButtonText: "取消",
        type: "warning",
        inputPlaceholder: "6 位数字验证码",
        closeOnClickModal: false,
      },
    )) as { value: string };
  } catch {
    return;
  }

  twoFADisabling.value = true;
  try {
    await securityApi.disable2FA(prompted.value.trim());
    twoFAEnabled.value = false;
    ElMessage.success("2FA 已禁用");
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.error || "禁用 2FA 失败");
  } finally {
    twoFADisabling.value = false;
  }
}

onMounted(async () => {
  if (!authStore.user) {
    try {
      await authStore.fetchUser();
    } catch (err: any) {
      ElMessage.error(
        err?.response?.data?.error || "获取用户信息失败，请重新登录",
      );
      return;
    }
  }
  load2FAStatus();
});

watch(activeTab, (tab) => {
  if (tab === "sponsors") {
    void activateSponsorTab();
    return;
  }
  stopSponsorRefreshTimer();
});

onUnmounted(() => {
  stopSponsorRefreshTimer();
});
</script>

<template>
  <div class="profile-page dd-scroll-page">
    <!-- ================= Hero ================= -->
    <header class="profile-hero">
      <div class="profile-hero-aura" aria-hidden="true"></div>

      <div class="profile-hero-main">
        <div class="profile-hero-left">
          <div class="profile-avatar-wrap">
            <div
              class="profile-avatar"
              @click="triggerAvatarUpload"
              :class="{ 'is-uploading': avatarUploading }"
            >
              <img
                v-if="avatarUrl"
                :src="avatarUrl"
                alt="用户头像"
                class="profile-avatar-img"
                @error="handleAvatarLoadError"
              />
              <span v-else class="profile-avatar-initial">{{
                usernameInitial
              }}</span>
              <span class="profile-avatar-ring"></span>
              <div class="profile-avatar-overlay">
                <el-icon :size="18"><Camera /></el-icon>
              </div>
              <input
                ref="avatarInputRef"
                type="file"
                accept="image/jpeg,image/png,image/gif,image/webp"
                class="profile-avatar-input"
                @change="handleAvatarFileChange"
              />
            </div>
            <div class="profile-avatar-camera" @click="triggerAvatarUpload">
              <el-icon :size="12"><Camera /></el-icon>
            </div>
            <el-button
              v-if="hasAvatar"
              class="avatar-delete-btn"
              :icon="Delete"
              circle
              size="small"
              @click.stop="handleDeleteAvatar"
              title="删除头像"
            />
          </div>

          <div class="profile-hero-body">
            <h1 class="profile-hero-name">
              {{ authStore.user?.username || "当前用户" }}
            </h1>
            <div class="profile-hero-meta-row">
              <span class="hero-chip hero-chip--green">
                <el-icon :size="13"><Avatar /></el-icon>
                <span>{{ roleLabel }}</span>
              </span>
              <span
                class="hero-chip hero-chip--2fa"
                :class="{ 'hero-chip--2fa-on': twoFAEnabled }"
              >
                <span
                  class="hero-chip-dot"
                  :class="{ 'hero-chip-dot--on': twoFAEnabled }"
                ></span>
                <span>2FA {{ twoFAEnabled ? "已启用" : "未启用" }}</span>
              </span>
            </div>
            <div class="profile-hero-login">
              <el-icon :size="13"><Clock /></el-icon>
              <span
                >最近登录: {{ formatTime(authStore.user?.last_login_at) }}</span
              >
            </div>
          </div>
        </div>

        <div class="profile-hero-badge" aria-hidden="true">
          <svg
            viewBox="0 0 80 96"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
            class="shield-icon"
          >
            <path
              d="M40 4L8 20V44C8 66 22 86 40 92C58 86 72 66 72 44V20L40 4Z"
              fill="currentColor"
              opacity="0.12"
              stroke="currentColor"
              stroke-width="2"
              stroke-opacity="0.2"
            />
            <path
              d="M34 48L38 52L50 40"
              stroke="currentColor"
              stroke-width="3"
              stroke-linecap="round"
              stroke-linejoin="round"
              opacity="0.3"
            />
          </svg>
        </div>
      </div>
    </header>

    <!-- ================= Body: sidebar tabs + content ================= -->
    <div class="profile-body">
      <!-- Left sidebar tabs -->
      <nav class="profile-sidebar">
        <button
          class="sidebar-tab"
          :class="{ active: activeTab === 'profile' }"
          @click="activeTab = 'profile'"
        >
          <el-icon :size="15"><User /></el-icon>
          <span>基本资料</span>
        </button>
        <button
          class="sidebar-tab"
          :class="{ active: activeTab === 'security' }"
          @click="activeTab = 'security'"
        >
          <el-icon :size="15"><Lock /></el-icon>
          <span>安全设置</span>
        </button>
        <button
          class="sidebar-tab"
          :class="{ active: activeTab === 'sponsors' }"
          @click="activeTab = 'sponsors'"
        >
          <el-icon :size="15"><Star /></el-icon>
          <span>赞助信息</span>
        </button>
      </nav>

      <!-- Right content area -->
      <div class="profile-content">
        <!-- ===== 基本资料 ===== -->
        <template v-if="activeTab === 'profile'">
          <section class="profile-card">
            <header class="profile-card-header">
              <span class="card-title">
                <el-icon :size="15"><User /></el-icon>
                <span>账户信息</span>
              </span>
            </header>
            <div class="info-grid">
              <div class="info-cell">
                <span class="info-label">用户名</span>
                <div
                  v-if="editingUsername"
                  style="display: flex; align-items: center; gap: 8px"
                >
                  <el-input
                    v-model="newUsername"
                    size="small"
                    style="width: 180px"
                    placeholder="输入新用户名"
                    @keyup.enter="saveUsername"
                  />
                  <el-button
                    size="small"
                    type="primary"
                    :loading="usernameSaving"
                    @click="saveUsername"
                    >保存</el-button
                  >
                  <el-button size="small" @click="editingUsername = false"
                    >取消</el-button
                  >
                </div>
                <span v-else class="info-value">
                  {{ authStore.user?.username || "-" }}
                  <el-button
                    text
                    size="small"
                    style="margin-left: 8px"
                    @click="startEditUsername"
                    >修改</el-button
                  >
                </span>
              </div>
              <div class="info-cell">
                <span class="info-label">角色</span>
                <span class="info-value">
                  <el-tag :type="roleTagType" size="small" effect="light">{{
                    roleLabel
                  }}</el-tag>
                </span>
              </div>
              <div class="info-cell">
                <span class="info-label">注册时间</span>
                <span class="info-value">{{
                  formatTime(authStore.user?.created_at)
                }}</span>
              </div>
              <div class="info-cell">
                <span class="info-label">最近登录</span>
                <span class="info-value">{{
                  formatTime(authStore.user?.last_login_at)
                }}</span>
              </div>
            </div>
          </section>

          <section class="profile-card">
            <header class="profile-card-header">
              <span class="card-title">
                <el-icon :size="15"><InfoFilled /></el-icon>
                <span>安全建议</span>
              </span>
            </header>
            <ul class="tip-list">
              <li>
                <span class="tip-bullet">1</span>
                <span>密码建议至少 12 位，包含大小写、数字和特殊字符。</span>
              </li>
              <li>
                <span class="tip-bullet">2</span>
                <span>启用 2FA 后，即使密码泄露，账户仍有第二层保护。</span>
              </li>
              <li>
                <span class="tip-bullet">3</span>
                <span
                  >禁用 2FA 也需要动态验证码，防止会话被劫持后被人关掉
                  2FA。</span
                >
              </li>
              <li>
                <span class="tip-bullet">4</span>
                <span>修改密码后当前会话之外的其它登录都会被撤销。</span>
              </li>
            </ul>
          </section>
        </template>

        <!-- ===== 安全设置 ===== -->
        <template v-if="activeTab === 'security'">
          <section class="profile-card">
            <header class="profile-card-header">
              <span class="card-title">
                <el-icon :size="15"><Lock /></el-icon>
                <span>修改密码</span>
              </span>
            </header>
            <el-form label-position="top" class="security-form">
              <el-form-item label="当前密码">
                <el-input
                  v-model="passwordForm.oldPassword"
                  type="password"
                  show-password
                  placeholder="请输入当前密码"
                />
              </el-form-item>
              <el-form-item label="新密码">
                <el-input
                  v-model="passwordForm.newPassword"
                  type="password"
                  show-password
                  placeholder="至少 6 位"
                />
              </el-form-item>
              <el-form-item label="确认新密码">
                <el-input
                  v-model="passwordForm.confirmPassword"
                  type="password"
                  show-password
                  placeholder="再次输入新密码"
                  @keyup.enter="handleChangePassword"
                />
              </el-form-item>
              <el-form-item>
                <el-button
                  type="primary"
                  :loading="passwordSaving"
                  class="primary-cta"
                  @click="handleChangePassword"
                >
                  <el-icon><Lock /></el-icon>
                  <span>更新密码</span>
                </el-button>
              </el-form-item>
            </el-form>
          </section>

          <section
            class="profile-card profile-card--twofa"
            :class="{ 'is-on': twoFAEnabled }"
          >
            <div class="twofa-halo" aria-hidden="true"></div>
            <header class="profile-card-header">
              <span class="card-title">
                <el-icon :size="15"><Key /></el-icon>
                <span>双因素认证</span>
              </span>
              <span
                class="twofa-status"
                :class="{ 'twofa-status--on': twoFAEnabled }"
              >
                <span class="twofa-status-dot"></span>
                <span>{{ twoFAEnabled ? "已启用" : "未启用" }}</span>
              </span>
            </header>

            <p class="twofa-desc">
              <template v-if="twoFAEnabled">
                你已经开启 2FA，登录时会要求输入认证器应用里的 6
                位动态码。禁用前需要再次输入当前动态码确认操作。
              </template>
              <template v-else>
                启用后，登录除了账号密码还需要输入认证器（Google / Microsoft
                Authenticator 等）生成的 6 位动态码。
              </template>
            </p>

            <div class="twofa-actions">
              <el-button
                v-if="!twoFAEnabled"
                type="primary"
                class="primary-cta"
                :loading="twoFALoading"
                @click="handleSetup2FA"
              >
                <el-icon><Key /></el-icon>
                <span>启用 2FA</span>
              </el-button>
              <el-button
                v-else
                class="danger-outline-btn"
                :loading="twoFADisabling"
                @click="handleDisable2FA"
              >
                <el-icon><Key /></el-icon>
                <span>禁用 2FA（需动态码）</span>
              </el-button>
            </div>
          </section>
        </template>

        <!-- ===== 赞助信息 ===== -->
        <template v-if="activeTab === 'sponsors'">
          <section class="sponsor-panel">
            <div class="sponsor-toolbar">
              <div class="sponsor-toolbar-copy">
                <div class="sponsor-toolbar-title-row">
                  <h3>赞助名单</h3>
                  <span class="sponsor-toolbar-hint">{{ sponsorTabHint }}</span>
                </div>
                <p class="sponsor-toolbar-intro">
                  本项目长期以公益方式维护，感谢以下赞助人员对持续开发、服务开销与后续迭代的支持。
                </p>
              </div>
              <el-button
                class="sponsor-refresh-btn"
                :loading="sponsorLoading || sponsorRefreshing"
                @click="handleManualSponsorRefresh"
              >
                <el-icon><RefreshRight /></el-icon>
                <span>立即刷新</span>
              </el-button>
            </div>

            <SponsorWall
              :sponsors="sponsors"
              :summary="sponsorSummary"
              :loading="sponsorLoading && sponsors.length === 0"
            />
          </section>
        </template>
      </div>
    </div>

    <!-- ================= Setup 2FA dialog ================= -->
    <el-dialog
      v-model="showSetup2FA"
      width="520px"
      :fullscreen="dialogFullscreen"
      :close-on-click-modal="false"
      class="setup-2fa-dialog"
    >
      <template #header>
        <div class="setup-dialog-header">
          <div class="setup-dialog-badge" aria-hidden="true">
            <el-icon :size="16"><Key /></el-icon>
          </div>
          <div>
            <div class="setup-dialog-title">设置双因素认证</div>
            <div class="setup-dialog-sub">
              扫码 / 抄密钥 / 输入验证码，三步开启
            </div>
          </div>
        </div>
      </template>

      <div class="setup-2fa">
        <div class="setup-step">
          <div class="step-head">
            <span class="step-num">1</span>
            <span class="step-title">扫描二维码</span>
          </div>
          <div class="qr-wrapper">
            <img
              v-if="twoFAQrUrl"
              :src="twoFAQrUrl"
              alt="2FA QR Code"
              class="qr-image"
            />
          </div>
          <div class="step-hint">
            推荐使用 Google Authenticator、Microsoft Authenticator 或
            1Password。
          </div>
        </div>

        <div class="setup-step">
          <div class="step-head">
            <span class="step-num">2</span>
            <span class="step-title">或手动输入密钥</span>
          </div>
          <div class="secret-box">
            <code>{{ twoFASecret }}</code>
          </div>
        </div>

        <div class="setup-step">
          <div class="step-head">
            <span class="step-num">3</span>
            <span class="step-title">输入 6 位验证码</span>
          </div>
          <el-input
            v-model="twoFACode"
            maxlength="6"
            placeholder="认证器上的 6 位数字"
            size="large"
            class="totp-input"
            @keyup.enter="handleVerify2FA"
          />
        </div>
      </div>

      <template #footer>
        <div class="setup-dialog-footer">
          <el-button @click="showSetup2FA = false">取消</el-button>
          <el-button type="primary" class="primary-cta" @click="handleVerify2FA"
            >验证并启用</el-button
          >
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.profile-page {
  --profile-accent: #3b82f6;
  --profile-border: var(--el-border-color-lighter);
  --profile-surface: var(--el-bg-color);
  --profile-surface-muted: var(--el-fill-color-light);

  display: flex;
  flex-direction: column;
  gap: 16px;
  font-family: var(--dd-font-ui);
}

/* ================= 入场动画 =================
   克制的淡入上移：hero 与卡片轻微错落进入。
   时长用 --dd-motion-page、缓动用 --dd-ease-decelerate，
   prefers-reduced-motion 时令牌被压到 1ms 自动降级。 */
@keyframes dd-profile-rise-in {
  from {
    opacity: 0;
    transform: translateY(10px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.profile-hero,
.profile-sidebar,
.profile-content > .profile-card,
.profile-content > .sponsor-panel {
  animation: dd-profile-rise-in var(--dd-motion-page) var(--dd-ease-decelerate)
    both;
}

/* 轻微错落：侧栏稍晚于 hero，内容卡片依次再延后；
   切换 Tab 时卡片由 v-if 重新挂载，会再次播放进入动画 */
.profile-sidebar {
  animation-delay: 50ms;
}

.profile-content > .profile-card:nth-child(1),
.profile-content > .sponsor-panel {
  animation-delay: 90ms;
}

.profile-content > .profile-card:nth-child(2) {
  animation-delay: 140ms;
}

/* ================= Hero ================= */
.profile-hero {
  position: relative;
  overflow: hidden;
  padding: 30px 34px;
  /* 圆角对齐令牌；阴影改用表面质感令牌，明暗双主题自动切换。
     注意：暗色下 hero 背景由 global.scss 的 `.profile-hero` 覆盖接管，此处仅负责明色渐变 */
  border-radius: var(--dd-card-radius);
  background:
    linear-gradient(
      135deg,
      rgba(167, 139, 250, 0.18) 0%,
      rgba(96, 165, 250, 0.12) 50%,
      rgba(34, 197, 94, 0.06) 100%
    ),
    var(--profile-surface);
  border: 1px solid
    color-mix(in srgb, var(--profile-accent) 12%, var(--profile-border));
  box-shadow: var(--dd-shadow-card);
}

.profile-hero-aura {
  position: absolute;
  inset: auto -100px -140px auto;
  width: 340px;
  height: 340px;
  border-radius: 50%;
  background: radial-gradient(
    circle at center,
    rgba(139, 92, 246, 0.18) 0%,
    transparent 70%
  );
  pointer-events: none;
}

.profile-hero-main {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
}

.profile-hero-left {
  display: flex;
  align-items: center;
  gap: 20px;
  min-width: 0;
  flex: 1;
}

.profile-avatar-wrap {
  position: relative;
  flex-shrink: 0;
}

.profile-avatar {
  position: relative;
  width: 72px;
  height: 72px;
  border-radius: 18px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-family: var(--dd-font-ui);
  font-size: 28px;
  font-weight: 700;
  background: linear-gradient(135deg, #10b981 0%, #3b82f6 100%);
  box-shadow: 0 8px 20px -6px rgba(16, 185, 129, 0.45);
  flex-shrink: 0;
  cursor: pointer;
  overflow: hidden;
  transition: transform 0.18s;

  &:hover {
    transform: scale(1.04);

    .profile-avatar-overlay {
      opacity: 1;
    }
  }

  &.is-uploading {
    opacity: 0.6;
    pointer-events: none;
  }
}

.profile-avatar-img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  border-radius: inherit;
  z-index: 1;
}

.profile-avatar-overlay {
  position: absolute;
  inset: 0;
  z-index: 2;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.4);
  border-radius: inherit;
  opacity: 0;
  transition: opacity 0.2s;
  color: #fff;
}

.profile-avatar-input {
  display: none;
}

.profile-avatar-camera {
  position: absolute;
  top: -2px;
  right: -2px;
  z-index: 3;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--profile-surface);
  border: 2px solid var(--profile-border);
  color: var(--el-text-color-secondary);
  cursor: pointer;
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.1);
  transition: all 0.2s;

  &:hover {
    color: var(--profile-accent);
    border-color: var(--profile-accent);
  }
}

.avatar-delete-btn {
  position: absolute;
  bottom: -2px;
  right: -2px;
  z-index: 3;
  color: var(--el-color-danger) !important;
  border-color: var(--profile-border) !important;
  background: var(--profile-surface) !important;
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.1);

  &:hover {
    color: #fff !important;
    background: var(--el-color-danger) !important;
    border-color: var(--el-color-danger) !important;
  }
}

.profile-avatar-initial {
  position: relative;
  z-index: 1;
  letter-spacing: 0.5px;
}

.profile-avatar-ring {
  position: absolute;
  inset: -4px;
  border-radius: inherit;
  border: 2.5px solid rgba(34, 197, 94, 0.25);
  z-index: 0;
}

.profile-hero-body {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
  flex: 1;
}

.profile-hero-name {
  margin: 0;
  font-size: 24px;
  font-weight: 700;
  letter-spacing: 0.2px;
  color: var(--el-text-color-primary);
  line-height: 1.2;
}

.profile-hero-meta-row {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin-top: 4px;
}

.hero-chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 24px;
  padding: 0 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 500;
  letter-spacing: 0.2px;
  /* 基础 chip 底色改用填充令牌，暗色不串色（实际 chip 均带 --green/--2fa 修饰覆盖此值） */
  background: var(--el-fill-color-light);
  border: 1px solid var(--profile-border);
  color: var(--el-text-color-regular);
}

.hero-chip--green {
  color: #16a34a;
  background: rgba(34, 197, 94, 0.08);
  border-color: rgba(34, 197, 94, 0.22);
}

.hero-chip--2fa {
  background: rgba(245, 108, 108, 0.06);
  border-color: rgba(245, 108, 108, 0.18);
  color: var(--el-color-danger);
}

.hero-chip--2fa-on {
  background: rgba(34, 197, 94, 0.08);
  border-color: rgba(34, 197, 94, 0.22);
  color: #16a34a;
}

.hero-chip-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--el-color-danger);
}

.hero-chip-dot--on {
  background: var(--profile-accent);
  box-shadow: 0 0 0 3px rgba(34, 197, 94, 0.18);
}

.profile-hero-login {
  display: flex;
  align-items: center;
  gap: 5px;
  margin-top: 2px;
  font-size: 12.5px;
  color: var(--el-text-color-secondary);
}

/* ================= Hero Badge (shield) ================= */
.profile-hero-badge {
  flex-shrink: 0;
  color: var(--profile-accent);
}

.shield-icon {
  width: 72px;
  height: 86px;
}

/* ================= Body layout ================= */
.profile-body {
  display: flex;
  gap: 20px;
  min-height: 0;
}

/* ================= Sidebar tabs ================= */
.profile-sidebar {
  flex-shrink: 0;
  width: 160px;
  display: flex;
  flex-direction: column;
  gap: 4px;
  background: var(--profile-surface);
  border: 1px solid var(--profile-border);
  /* 圆角/阴影对齐令牌，与卡片外壳风格统一 */
  border-radius: var(--dd-card-radius);
  padding: 12px;
  box-shadow: var(--dd-shadow-card);
  align-self: flex-start;
}

.sidebar-tab {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  border-radius: 8px;
  border: none;
  background: transparent;
  color: var(--el-text-color-regular);
  font-size: 13.5px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s;
  white-space: nowrap;
  text-align: left;

  &:hover {
    background: var(--profile-surface-muted);
    color: var(--el-text-color-primary);
  }

  &.active {
    background: rgba(34, 197, 94, 0.08);
    color: #16a34a;
    font-weight: 600;
  }
}

/* ================= Content area ================= */
.profile-content {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 18px;
}

/* ================= Cards ================= */
.profile-card {
  position: relative;
  background: var(--profile-surface);
  border: 1px solid var(--profile-border);
  /* 圆角/阴影对齐表面质感令牌（明暗双主题自动切换） */
  border-radius: var(--dd-card-radius);
  padding: 20px 24px;
  overflow: hidden;
  box-shadow: var(--dd-shadow-card);
  transition: box-shadow var(--dd-motion-normal) var(--dd-ease-standard);

  &:hover {
    box-shadow: var(--dd-shadow-card-hover);
  }
}

.profile-card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 16px;
}

.card-title {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 15px;
  font-weight: 700;
  color: var(--el-text-color-primary);
}

/* Info grid */
.info-grid {
  display: flex;
  flex-direction: column;
  gap: 0;
}

.info-cell {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  padding: 11px 0;
  border-bottom: 1px dashed var(--profile-border);
  font-size: 13.5px;

  &:last-child {
    border-bottom: none;
    padding-bottom: 4px;
  }

  &:first-child {
    padding-top: 0;
  }
}

.info-label {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  letter-spacing: 0.2px;
}

.info-value {
  font-weight: 600;
  color: var(--el-text-color-primary);
  text-align: right;
  word-break: break-all;
  font-size: 13px;
}

/* Tips */
.tip-list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;

  li {
    display: flex;
    gap: 10px;
    padding: 10px 14px;
    border-radius: 10px;
    background: var(--profile-surface-muted);
    border: 1px solid var(--profile-border);
    font-size: 12.5px;
    line-height: 1.6;
    color: var(--el-text-color-regular);
  }
}

.tip-bullet {
  flex-shrink: 0;
  width: 20px;
  height: 20px;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
  font-weight: 700;
  font-family: var(--dd-font-mono);
  color: var(--profile-accent);
  background: rgba(34, 197, 94, 0.12);
}

/* Password */
.security-form {
  :deep(.el-form-item) {
    margin-bottom: 16px;
  }

  :deep(.el-form-item__label) {
    font-size: 13px;
    font-weight: 600;
    color: var(--el-text-color-secondary);
  }

  :deep(.el-input__wrapper) {
    border-radius: 8px;
  }
}

.primary-cta {
  border-radius: 8px;
  height: 38px;
  padding: 0 20px;
  font-weight: 600;
  background: linear-gradient(135deg, #22c55e 0%, #16a34a 100%);
  border: none;
  box-shadow: 0 4px 12px -4px rgba(34, 197, 94, 0.45);

  &:hover,
  &:focus {
    background: linear-gradient(135deg, #16a34a 0%, #15803d 100%);
    border: none;
  }
}

/* Two-factor card */
.profile-card--twofa {
  border-color: rgba(245, 108, 108, 0.18);
  background:
    linear-gradient(135deg, rgba(245, 108, 108, 0.03) 0%, transparent 55%),
    var(--profile-surface);

  &.is-on {
    border-color: rgba(34, 197, 94, 0.22);
    background:
      linear-gradient(135deg, rgba(34, 197, 94, 0.04) 0%, transparent 60%),
      var(--profile-surface);
  }
}

.twofa-halo {
  position: absolute;
  inset: auto -60px -80px auto;
  width: 180px;
  height: 180px;
  border-radius: 50%;
  background: radial-gradient(
    circle,
    rgba(99, 102, 241, 0.14) 0%,
    transparent 70%
  );
  pointer-events: none;
}

.twofa-status {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 24px;
  padding: 0 10px;
  border-radius: 999px;
  font-size: 11.5px;
  font-weight: 700;
  font-family: var(--dd-font-mono);
  letter-spacing: 0.5px;
  background: rgba(245, 108, 108, 0.08);
  color: var(--el-color-danger);

  &--on {
    background: rgba(34, 197, 94, 0.1);
    color: #16a34a;
  }
}

.twofa-status-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
}

.twofa-desc {
  margin: 0 0 16px;
  font-size: 13px;
  line-height: 1.7;
  color: var(--el-text-color-regular);
}

.twofa-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.danger-outline-btn {
  border-radius: 8px;
  height: 38px;
  padding: 0 18px;
  font-weight: 600;
  color: var(--el-color-danger);
  background: transparent;
  border: 1px solid rgba(245, 108, 108, 0.4);

  &:hover {
    color: #fff;
    background: var(--el-color-danger);
    border-color: var(--el-color-danger);
  }
}

/* ================= Sponsor panel ================= */
.sponsor-panel {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.sponsor-toolbar {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
  flex-wrap: wrap;
  padding: 18px 22px;
  /* 圆角/阴影对齐令牌（保留赞助卡的琥珀色渐变外观） */
  border-radius: var(--dd-card-radius);
  border: 1px solid rgba(245, 158, 11, 0.16);
  background:
    linear-gradient(
      135deg,
      rgba(245, 158, 11, 0.06) 0%,
      rgba(245, 158, 11, 0.02) 100%
    ),
    var(--profile-surface);
  box-shadow: var(--dd-shadow-card);
}

.sponsor-toolbar-copy {
  min-width: 0;
  flex: 1;
}

.sponsor-toolbar-title-row {
  display: flex;
  align-items: baseline;
  gap: 12px;
  flex-wrap: wrap;

  h3 {
    margin: 0;
    font-size: 17px;
    font-weight: 700;
    color: #b45309;
  }
}

.sponsor-toolbar-hint {
  font-size: 11.5px;
  color: #a16207;
}

.sponsor-toolbar-intro {
  margin: 6px 0 0;
  font-size: 12.5px;
  line-height: 1.6;
  color: #78350f;
  max-width: 680px;
}

.sponsor-refresh-btn {
  border-radius: 8px;
}

/* ================= Setup dialog ================= */
:deep(.setup-2fa-dialog) {
  .el-dialog {
    border-radius: 12px;
    overflow: hidden;
  }

  .el-dialog__header {
    padding: 18px 22px 14px;
    margin: 0;
    border-bottom: 1px solid var(--profile-border);
  }

  .el-dialog__body {
    padding: 20px 24px;
  }

  .el-dialog__footer {
    padding: 12px 22px 18px;
  }
}

.setup-dialog-header {
  display: flex;
  align-items: center;
  gap: 12px;
}

.setup-dialog-badge {
  width: 36px;
  height: 36px;
  border-radius: 10px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  background: linear-gradient(135deg, #1890ff, #36cfc9);
}

.setup-dialog-title {
  font-size: 15px;
  font-weight: 700;
  color: var(--el-text-color-primary);
}

.setup-dialog-sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 2px;
}

.setup-2fa {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.setup-step {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.step-head {
  display: flex;
  align-items: center;
  gap: 8px;
}

.step-num {
  width: 22px;
  height: 22px;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  font-weight: 700;
  font-family: var(--dd-font-mono);
  color: #fff;
  background: linear-gradient(135deg, #22c55e, #16a34a);
}

.step-title {
  font-size: 13.5px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.step-hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.6;
}

.qr-wrapper {
  display: flex;
  justify-content: center;
  padding: 8px 0;
}

.qr-image {
  width: 220px;
  height: 220px;
  padding: 10px;
  border-radius: 12px;
  background: #fff;
  border: 1px solid var(--profile-border);
}

.secret-box {
  padding: 14px 16px;
  border-radius: 10px;
  background: var(--profile-surface-muted);
  border: 1px dashed var(--profile-border);
  text-align: center;

  code {
    font-family: var(--dd-font-mono);
    font-size: 14.5px;
    font-weight: 700;
    letter-spacing: 0.18em;
    user-select: all;
    color: var(--el-text-color-primary);
    word-break: break-all;
  }
}

.totp-input {
  :deep(.el-input__wrapper) {
    border-radius: 10px;
  }

  :deep(.el-input__inner) {
    font-family: var(--dd-font-mono);
    font-size: 18px;
    letter-spacing: 0.5em;
    text-align: center;
    font-weight: 600;
  }
}

.setup-dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

/* ================= Mobile ================= */
@media (max-width: 768px) {
  .profile-body {
    flex-direction: column;
  }

  .profile-sidebar {
    width: 100%;
    flex-direction: row;
    overflow-x: auto;
    padding: 8px;
    gap: 4px;
  }

  .sidebar-tab {
    padding: 8px 14px;
    font-size: 13px;
  }
}

@media (max-width: 600px) {
  .profile-hero {
    padding: 20px;
  }

  .profile-hero-left {
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
  }

  .profile-hero-name {
    font-size: 20px;
  }

  .profile-hero-meta-row {
    gap: 6px;
  }

  .hero-chip {
    font-size: 11.5px;
  }

  .profile-card {
    padding: 16px 18px;
  }

  .shield-icon {
    width: 48px;
    height: 58px;
  }
}

/* ================= 暗色修补 =================
   赞助卡的琥珀文字（深棕）在暗色底上对比过低，仅在暗色下提亮，
   不影响明色下的品牌色观感 */
html.dark {
  .sponsor-toolbar-title-row h3 {
    color: #fbbf24;
  }

  .sponsor-toolbar-hint {
    color: #d6a85f;
  }

  .sponsor-toolbar-intro {
    color: var(--el-text-color-regular);
  }
}
</style>
