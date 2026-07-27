<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, computed } from "vue";
import { subscriptionApi } from "@/api/subscription";
import { sshKeyApi } from "@/api/notification";
import { configApi } from "@/api/system";
import { ElMessage, ElMessageBox } from "element-plus";
import {
  openAuthorizedEventStream,
  type EventStreamConnection,
} from "@/utils/sse";
import { useResponsive } from "@/composables/useResponsive";
import { ansiToHtml, normalizeAnsi } from "@/utils/ansi";

const subList = ref<any[]>([]);
const loading = ref(false);
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);
const keyword = ref("");
const selectedIds = ref<number[]>([]);
const selectedIdSet = computed(() => new Set(selectedIds.value));
const { isMobile, dialogFullscreen } = useResponsive();
const typeFilter = ref<"" | "git-repo" | "single-file" | "disabled">("");

const filteredSubList = computed(() => {
  if (!typeFilter.value) return subList.value;
  if (typeFilter.value === "disabled")
    return subList.value.filter((s) => !s.enabled);
  return subList.value.filter((s) => s.type === typeFilter.value);
});

const showEditDialog = ref(false);
const showLogDialog = ref(false);
const showSettingsDialog = ref(false);
const isCreate = ref(true);
const qlCommand = ref("");

const settingsLoading = ref(false);
const settingsSaving = ref(false);
const settingsForm = ref({
  github_mirror: "",
  auto_add_cron: true,
  auto_del_cron: true,
  subscription_force_overwrite: true,
  default_cron_rule: "",
  repo_file_extensions: "",
});

const GITHUB_MIRROR_STORAGE_KEY = "subscription.github_mirror";
const DEFAULT_GITHUB_MIRROR = "https://gh-proxy.com/";
const githubMirror = ref(
  localStorage.getItem(GITHUB_MIRROR_STORAGE_KEY) || DEFAULT_GITHUB_MIRROR,
);

function normalizeMirror(u: string): string {
  const t = u.trim();
  if (!t) return "";
  return t.endsWith("/") ? t : t + "/";
}

const editForm = ref({
  id: 0,
  name: "",
  type: "git-repo",
  url: "",
  branch: "",
  schedule: "",
  whitelist: "",
  blacklist: "",
  depend_on: "",
  hook_script: "",
  auto_add_task: false,
  auto_del_task: false,
  save_dir: "",
  sub_path: "",
  auth_type: "" as "" | "ssh" | "token",
  ssh_key_id: null as number | null,
  auth_username: "",
  auth_token: "",
  has_auth_token: false,
  alias: "",
});

const sshKeys = ref<any[]>([]);
const showSSHKeyManageDialog = ref(false);
const showSSHKeyDialog = ref(false);
const isCreateSSHKey = ref(true);
const sshKeyForm = ref({ id: 0, name: "", private_key: "" });
const sshKeyLoading = ref(false);

const logList = ref<any[]>([]);
const logTotal = ref(0);
const logPage = ref(1);
const logSubId = ref(0);
const logLoading = ref(false);

const showLogDetail = ref(false);
const logDetailContent = ref("");
const logDetailContentHtml = computed(() =>
  ansiToHtml(normalizeAnsi(logDetailContent.value || "(无日志内容)")),
);

const showPullLog = ref(false);
const pullLogLines = ref<string[]>([]);
const pullLogLineHtmlList = computed(() =>
  pullLogLines.value.map((line) => ansiToHtml(normalizeAnsi(line))),
);
const pullRunning = ref(false);
const pullingSubId = ref<number | null>(null);
let pullEventSource: EventStreamConnection | null = null;
const pullLogRef = ref<HTMLElement>();
let pullBuffer: string[] = [];
let pullFlushRaf = 0;

async function loadData() {
  loading.value = true;
  try {
    const res = await subscriptionApi.list({
      keyword: keyword.value || undefined,
      type:
        typeFilter.value && typeFilter.value !== "disabled"
          ? typeFilter.value
          : undefined,
      enabled: typeFilter.value === "disabled" ? false : undefined,
      page: page.value,
      page_size: pageSize.value,
    });
    subList.value = res.data || [];
    total.value = res.total || 0;
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.error || "加载订阅列表失败");
  } finally {
    loading.value = false;
  }
}

async function loadSSHKeys() {
  sshKeyLoading.value = true;
  try {
    const res = await sshKeyApi.list();
    sshKeys.value = res.data || [];
  } catch {
    /* ignore */
  } finally {
    sshKeyLoading.value = false;
  }
}

onMounted(() => {
  loadData();
  loadSSHKeys();
});

onBeforeUnmount(() => {
  closePullStream();
  if (pullFlushRaf) {
    cancelAnimationFrame(pullFlushRaf);
    pullFlushRaf = 0;
  }
});

function handleSearch() {
  page.value = 1;
  loadData();
}

function handleTypeFilter(value: "" | "git-repo" | "single-file" | "disabled") {
  if (typeFilter.value === value) {
    return;
  }
  typeFilter.value = value;
  page.value = 1;
  loadData();
}

function openCreate() {
  isCreate.value = true;
  qlCommand.value = "";
  editForm.value = {
    id: 0,
    name: "",
    type: "git-repo",
    url: "",
    branch: "",
    schedule: "",
    whitelist: "",
    blacklist: "",
    depend_on: "",
    hook_script: "",
    auto_add_task: false,
    auto_del_task: false,
    save_dir: "",
    sub_path: "",
    auth_type: "",
    ssh_key_id: null,
    auth_username: "",
    auth_token: "",
    has_auth_token: false,
    alias: "",
  };
  showEditDialog.value = true;
}

function addGithubMirror(url: string): string {
  if (!url) return url;
  const mirror = normalizeMirror(githubMirror.value);
  if (!mirror) return url;
  const githubPattern = /^https?:\/\/github\.com\//;
  // 已经包含镜像（任何协议）就不再重复包裹
  const mirrorHost = mirror.replace(/^https?:\/\//, "").replace(/\/$/, "");
  if (mirrorHost && url.includes(mirrorHost)) return url;
  if (githubPattern.test(url)) {
    return url.replace(
      /^https?:\/\/github\.com\//,
      mirror + "https://github.com/",
    );
  }
  return url;
}

function readCfgBool(
  cfgs: Record<string, any>,
  key: string,
  fallback: boolean,
): boolean {
  const entry = cfgs[key];
  const raw = String(
    entry?.value ?? entry?.default_value ?? (fallback ? "true" : "false"),
  )
    .trim()
    .toLowerCase();
  if (["true", "1", "yes", "on"].includes(raw)) return true;
  if (["false", "0", "no", "off"].includes(raw)) return false;
  return fallback;
}

function readCfgStr(
  cfgs: Record<string, any>,
  key: string,
  fallback = "",
): string {
  const entry = cfgs[key];
  const raw = entry?.value ?? entry?.default_value ?? fallback;
  return raw === null || raw === undefined ? fallback : String(raw);
}

async function handleOpenSettings() {
  showSettingsDialog.value = true;
  settingsForm.value.github_mirror = githubMirror.value;
  settingsLoading.value = true;
  try {
    const res = await configApi.list();
    const cfgs = res.data || {};
    settingsForm.value.auto_add_cron = readCfgBool(cfgs, "auto_add_cron", true);
    settingsForm.value.auto_del_cron = readCfgBool(cfgs, "auto_del_cron", true);
    settingsForm.value.subscription_force_overwrite = readCfgBool(
      cfgs,
      "subscription_force_overwrite",
      true,
    );
    settingsForm.value.default_cron_rule = readCfgStr(
      cfgs,
      "default_cron_rule",
      "",
    );
    settingsForm.value.repo_file_extensions = readCfgStr(
      cfgs,
      "repo_file_extensions",
      "",
    );
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.error || "加载订阅设置失败");
  } finally {
    settingsLoading.value = false;
  }
}

async function handleSaveSettings() {
  const mirrorRaw = (settingsForm.value.github_mirror || "").trim();
  if (mirrorRaw && !/^https?:\/\/.+/.test(mirrorRaw)) {
    ElMessage.warning("镜像地址需以 http:// 或 https:// 开头");
    return;
  }
  settingsSaving.value = true;
  try {
    await configApi.batchSet({
      auto_add_cron: settingsForm.value.auto_add_cron ? "true" : "false",
      auto_del_cron: settingsForm.value.auto_del_cron ? "true" : "false",
      subscription_force_overwrite: settingsForm.value
        .subscription_force_overwrite
        ? "true"
        : "false",
      default_cron_rule: settingsForm.value.default_cron_rule,
      repo_file_extensions: settingsForm.value.repo_file_extensions,
    });
    const mirror = mirrorRaw || DEFAULT_GITHUB_MIRROR;
    githubMirror.value = normalizeMirror(mirror);
    localStorage.setItem(GITHUB_MIRROR_STORAGE_KEY, githubMirror.value);
    ElMessage.success("订阅设置已保存");
    showSettingsDialog.value = false;
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.error || "保存失败");
  } finally {
    settingsSaving.value = false;
  }
}

function deriveSubscriptionSaveDir(url: string): string {
  const trimmed = url
    .trim()
    .replace(/\/+$/, "")
    .replace(/\.git$/i, "");
  if (!trimmed) return "";
  const parts = trimmed.split("/").filter(Boolean);
  if (parts.length >= 2) {
    const owner = parts[parts.length - 2];
    const repo = parts[parts.length - 1];
    if (owner && repo) {
      return `${owner}_${repo}`;
    }
  }
  return parts[parts.length - 1] || "";
}

function normalizeRecognizedHookScript(raw: string): string {
  return raw
    .replace(
      /(?:\$\{?QL_DIR\}?|%QL_DIR%)[/\\]data[/\\](?:repo|scripts)[/\\][^/\\"'\s;]+/g,
      "$SUB_DIR",
    )
    .trim();
}

function parseQLCommand() {
  const cmd = qlCommand.value.trim();
  if (!cmd) return;

  const lines = cmd
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
  const qlLine = lines.find((line) => /^ql\s+(repo|raw)\b/.test(line)) || cmd;
  const hookScript = normalizeRecognizedHookScript(
    lines
      .filter((line) => line !== qlLine && !/^ql\s+(repo|raw)\b/.test(line))
      .join(" ; "),
  );

  const repoMatch = qlLine.match(
    /ql\s+repo\s+"?([^\s"]+)"?\s*"?([^"]*)"?\s*"?([^"]*)"?\s*"?([^"]*)"?\s*"?([^"]*)"?/,
  );
  if (repoMatch) {
    const [, url = "", whitelist, blacklist, dependOn, branch] = repoMatch;
    const repoName =
      url
        .replace(/\.git$/, "")
        .split("/")
        .pop() || "repo";
    editForm.value.type = "git-repo";
    editForm.value.url = addGithubMirror(url);
    editForm.value.name = repoName;
    editForm.value.save_dir = deriveSubscriptionSaveDir(url);
    editForm.value.whitelist = whitelist || "";
    editForm.value.blacklist = blacklist || "";
    editForm.value.branch = branch || "";
    editForm.value.depend_on = dependOn || "";
    if (hookScript) editForm.value.hook_script = hookScript;
    editForm.value.auto_add_task = true;
    ElMessage.success("已识别 ql repo 命令");
    qlCommand.value = "";
    return;
  }

  const rawMatch = qlLine.match(/ql\s+raw\s+"?([^\s"]+)"?/);
  if (rawMatch) {
    const url = rawMatch[1] || "";
    const fileName = url.split("/").pop() || "file";
    editForm.value.type = "single-file";
    editForm.value.url = addGithubMirror(url);
    editForm.value.name = fileName.replace(/\.[^/.]+$/, "");
    editForm.value.save_dir = deriveSubscriptionSaveDir(url) || "downloads";
    if (hookScript) editForm.value.hook_script = hookScript;
    editForm.value.auto_add_task = true;
    ElMessage.success("已识别 ql raw 命令");
    qlCommand.value = "";
    return;
  }

  if (
    cmd.includes("github.com") ||
    cmd.includes(".git") ||
    cmd.startsWith("http")
  ) {
    editForm.value.url = addGithubMirror(cmd);
    const repoName =
      cmd
        .replace(/\.git$/, "")
        .split("/")
        .pop() || "";
    if (repoName) editForm.value.name = repoName;
    editForm.value.save_dir = deriveSubscriptionSaveDir(cmd);
    editForm.value.type =
      cmd.endsWith(".js") ||
      cmd.endsWith(".py") ||
      cmd.endsWith(".ts") ||
      cmd.endsWith(".sh")
        ? "single-file"
        : "git-repo";
    ElMessage.success("已识别链接");
    qlCommand.value = "";
    return;
  }

  ElMessage.warning("无法识别命令格式，支持 ql repo/raw 命令或直接粘贴链接");
}

function openEdit(row: any) {
  isCreate.value = false;
  editForm.value = {
    id: row.id,
    name: row.name,
    type: row.type,
    url: row.url,
    branch: row.branch || "",
    schedule: row.schedule || "",
    whitelist: row.whitelist || "",
    blacklist: row.blacklist || "",
    depend_on: row.depend_on || "",
    hook_script: row.hook_script || "",
    auto_add_task: row.auto_add_task,
    auto_del_task: row.auto_del_task,
    save_dir: row.save_dir || "",
    sub_path: row.sub_path || "",
    auth_type: row.auth_type || "",
    ssh_key_id: row.ssh_key_id,
    auth_username: row.auth_username || "",
    auth_token: "",
    has_auth_token: !!row.has_auth_token,
    alias: row.alias || "",
  };
  showEditDialog.value = true;
}

async function handleSave() {
  if (!editForm.value.name.trim() || !editForm.value.url.trim()) {
    ElMessage.warning("名称和 URL 不能为空");
    return;
  }
  const mirror = normalizeMirror(githubMirror.value);
  const mirrorHost = mirror.replace(/^https?:\/\//, "").replace(/\/$/, "");
  const githubDirect =
    /^https?:\/\/github\.com\//.test(editForm.value.url) &&
    mirrorHost &&
    !editForm.value.url.includes(mirrorHost);
  if (githubDirect) {
    try {
      await ElMessageBox.confirm(
        "检测到 GitHub 直连地址，是否自动添加镜像加速？\n加速地址: " + mirror,
        "镜像加速",
        {
          confirmButtonText: "添加加速",
          cancelButtonText: "保持原样",
          type: "info",
        },
      );
      editForm.value.url = addGithubMirror(editForm.value.url);
    } catch {
      /* keep original */
    }
  }
  try {
    const data = { ...editForm.value };
    if (data.type !== "git-repo") {
      data.auth_type = "";
      data.ssh_key_id = null;
      data.auth_username = "";
      data.auth_token = "";
    } else if (data.auth_type === "ssh") {
      data.auth_username = "";
      data.auth_token = "";
    } else if (data.auth_type === "token") {
      data.ssh_key_id = null;
    } else {
      data.ssh_key_id = null;
      data.auth_username = "";
      data.auth_token = "";
    }
    delete (data as any).has_auth_token;
    if (isCreate.value) {
      await subscriptionApi.create(data);
      ElMessage.success("创建成功");
    } else {
      await subscriptionApi.update(data.id, data);
      ElMessage.success("更新成功");
    }
    showEditDialog.value = false;
    loadData();
  } catch (err: any) {
    ElMessage.error(
      err?.response?.data?.error || (isCreate.value ? "创建失败" : "更新失败"),
    );
  }
}

async function handleDelete(id: number) {
  try {
    await ElMessageBox.confirm("确定要删除该订阅吗？", "确认删除", {
      type: "warning",
    });
    await subscriptionApi.delete(id);
    ElMessage.success("删除成功");
    loadData();
  } catch {
    /* cancelled */
  }
}

async function handleToggle(row: any) {
  try {
    const enabling = !row.enabled;
    await ElMessageBox.confirm(
      enabling
        ? `确认启用订阅「${row.name}」吗？`
        : `确认禁用订阅「${row.name}」吗？禁用后将停止后续自动拉取。`,
      enabling ? "启用确认" : "禁用确认",
      { type: enabling ? "info" : "warning" },
    );
    if (row.enabled) {
      await subscriptionApi.disable(row.id);
    } else {
      await subscriptionApi.enable(row.id);
    }
    ElMessage.success(row.enabled ? "已禁用" : "已启用");
    loadData();
  } catch (err: any) {
    if (err === "cancel" || err?.toString?.() === "cancel") return;
    ElMessage.error(err?.response?.data?.error || "操作失败");
  }
}

async function handlePull(row: any) {
  if (pullingSubId.value === row.id && pullRunning.value) {
    showPullLog.value = true;
    return;
  }

  try {
    await ElMessageBox.confirm(
      `确认按订阅设置拉取订阅「${row.name}」吗？`,
      "拉取确认",
      {
        type: "warning",
        confirmButtonText: "立即拉取",
        cancelButtonText: "取消",
      },
    );
  } catch {
    return;
  }

  try {
    await subscriptionApi.pull(row.id);
    pullLogLines.value = [];
    pullRunning.value = true;
    pullingSubId.value = row.id;
    showPullLog.value = true;
    connectPullStream(row.id);
  } catch (err: any) {
    if (err?.response?.data?.error?.includes("拉取中")) {
      pullRunning.value = true;
      pullingSubId.value = row.id;
      showPullLog.value = true;
      connectPullStream(row.id);
      return;
    }
    ElMessage.error(err?.response?.data?.error || "拉取失败");
  }
}

async function handleStopPull() {
  if (!pullingSubId.value) {
    return;
  }

  try {
    await ElMessageBox.confirm("确认停止当前拉库任务吗？", "停止拉库", {
      type: "warning",
      confirmButtonText: "停止",
      cancelButtonText: "取消",
    });
    await subscriptionApi.stopPull(pullingSubId.value);
    ElMessage.success("已发送停止请求");
  } catch (err: any) {
    if (err === "cancel" || err?.toString?.() === "cancel") return;
    ElMessage.error(err?.response?.data?.error || "停止失败");
  }
}

function connectPullStream(id: number) {
  closePullStream();
  const base = import.meta.env.VITE_API_BASE || "/api/v1";
  const url = `${base}/subscriptions/${id}/pull-stream`;
  pullEventSource = openAuthorizedEventStream(url, {
    onMessage(data) {
      pullBuffer.push(data);
      if (!pullFlushRaf) {
        pullFlushRaf = requestAnimationFrame(() => {
          pullLogLines.value.push(...pullBuffer);
          pullBuffer = [];
          pullFlushRaf = 0;
          if (pullLogRef.value)
            pullLogRef.value.scrollTop = pullLogRef.value.scrollHeight;
        });
      }
    },
    onEvent(event) {
      if (event.event === "done") {
        pullRunning.value = false;
        pullingSubId.value = null;
        closePullStream();
        loadData();
      }
    },
    onError() {
      pullRunning.value = false;
      pullingSubId.value = null;
      closePullStream();
    },
  });
}

function closePullStream() {
  if (pullEventSource) {
    pullEventSource.close();
    pullEventSource = null;
  }
}

async function handleBatchDelete() {
  if (selectedIds.value.length === 0) return;
  try {
    await ElMessageBox.confirm(
      `确定要删除选中的 ${selectedIds.value.length} 个订阅吗？`,
      "批量删除",
      { type: "warning" },
    );
    await subscriptionApi.batchDelete(selectedIds.value);
    ElMessage.success("批量删除成功");
    selectedIds.value = [];
    loadData();
  } catch {
    /* cancelled */
  }
}

function handleSelectionChange(rows: any[]) {
  selectedIds.value = rows.map((r) => r.id);
}

function isSelected(id: number) {
  return selectedIdSet.value.has(id);
}

function toggleSelected(id: number, checked: boolean | string | number) {
  const next = new Set(selectedIds.value);
  if (checked) {
    next.add(id);
  } else {
    next.delete(id);
  }
  selectedIds.value = [...next];
}

async function openLogs(subId: number) {
  logSubId.value = subId;
  logPage.value = 1;
  showLogDialog.value = true;
  await loadLogs();
}

async function loadLogs() {
  logLoading.value = true;
  try {
    const res = await subscriptionApi.logs(logSubId.value, {
      page: logPage.value,
      page_size: 10,
    });
    logList.value = res.data || [];
    logTotal.value = res.total || 0;
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.error || "加载日志失败");
  } finally {
    logLoading.value = false;
  }
}

function getStatusTag(status: number) {
  return status === 0 ? "success" : "danger";
}

function getStatusText(status: number) {
  return status === 0 ? "正常" : "失败";
}

function openCreateSSHKey() {
  isCreateSSHKey.value = true;
  sshKeyForm.value = { id: 0, name: "", private_key: "" };
  showSSHKeyDialog.value = true;
}

function openEditSSHKey(row: any) {
  isCreateSSHKey.value = false;
  sshKeyForm.value = { id: row.id, name: row.name, private_key: "" };
  showSSHKeyDialog.value = true;
}

async function handleSaveSSHKey() {
  if (!sshKeyForm.value.name.trim()) {
    ElMessage.warning("名称不能为空");
    return;
  }
  if (isCreateSSHKey.value && !sshKeyForm.value.private_key.trim()) {
    ElMessage.warning("私钥不能为空");
    return;
  }
  try {
    const data: any = { name: sshKeyForm.value.name };
    if (sshKeyForm.value.private_key) {
      data.private_key = sshKeyForm.value.private_key;
    }
    if (isCreateSSHKey.value) {
      await sshKeyApi.create(data);
      ElMessage.success("创建成功");
    } else {
      await sshKeyApi.update(sshKeyForm.value.id, data);
      ElMessage.success("更新成功");
    }
    showSSHKeyDialog.value = false;
    loadSSHKeys();
  } catch {
    ElMessage.error(isCreateSSHKey.value ? "创建失败" : "更新失败");
  }
}

async function handleDeleteSSHKey(id: number) {
  try {
    await ElMessageBox.confirm("确定要删除该 SSH 密钥吗？", "确认删除", {
      type: "warning",
    });
    await sshKeyApi.delete(id);
    ElMessage.success("删除成功");
    loadSSHKeys();
  } catch {
    /* cancelled */
  }
}

function viewLogDetail(log: any) {
  logDetailContent.value = log.content || "(无日志内容)";
  showLogDetail.value = true;
}
</script>

<template>
  <div class="subscriptions-page dd-fixed-page dd-page-hide-heading">
    <div class="toolbar">
      <div class="toolbar__left">
        <div class="status-tabs">
          <button
            :class="['status-tab', { active: typeFilter === '' }]"
            @click="handleTypeFilter('')"
          >
            全部
          </button>
          <button
            :class="['status-tab', { active: typeFilter === 'git-repo' }]"
            @click="handleTypeFilter('git-repo')"
          >
            仓库
          </button>
          <button
            :class="['status-tab', { active: typeFilter === 'single-file' }]"
            @click="handleTypeFilter('single-file')"
          >
            单文件
          </button>
          <button
            :class="['status-tab', { active: typeFilter === 'disabled' }]"
            @click="handleTypeFilter('disabled')"
          >
            已禁用
          </button>
        </div>
        <el-input
          v-model="keyword"
          placeholder="搜索订阅名称或 URL"
          clearable
          class="toolbar__search"
          @keyup.enter="handleSearch"
          @clear="handleSearch"
        >
          <template #prefix
            ><el-icon><Search /></el-icon
          ></template>
        </el-input>
      </div>
      <div class="toolbar__right">
        <el-button
          @click="
            showSSHKeyManageDialog = true;
            loadSSHKeys();
          "
          title="SSH 密钥管理"
        >
          <el-icon><Key /></el-icon> SSH 密钥
        </el-button>
        <el-button @click="handleOpenSettings" title="订阅设置">
          <el-icon><Setting /></el-icon>
        </el-button>
        <el-button
          v-if="selectedIds.length > 0"
          type="danger"
          plain
          size="small"
          @click="handleBatchDelete"
        >
          <el-icon><Delete /></el-icon> 批量删除
        </el-button>
        <el-button type="primary" @click="openCreate">
          <el-icon><Plus /></el-icon> 新建订阅
        </el-button>
      </div>
    </div>

    <div v-if="isMobile" class="dd-mobile-list">
      <div v-for="row in filteredSubList" :key="row.id" class="dd-mobile-card">
        <div class="dd-mobile-card__header">
          <div class="dd-mobile-card__title-wrap">
            <div class="subscription-card__title-row">
              <div class="dd-mobile-card__selection">
                <el-checkbox
                  :model-value="isSelected(row.id)"
                  @change="toggleSelected(row.id, $event)"
                />
                <span class="dd-mobile-card__title">{{ row.name }}</span>
              </div>
              <el-tag
                size="small"
                :type="row.type === 'git-repo' ? '' : 'warning'"
              >
                {{ row.type === "git-repo" ? "Git 仓库" : "单文件" }}
              </el-tag>
            </div>
            <div class="dd-mobile-card__subtitle">{{ row.url }}</div>
          </div>
        </div>

        <div class="dd-mobile-card__body">
          <div class="dd-mobile-card__grid">
            <div class="dd-mobile-card__field">
              <span class="dd-mobile-card__label">分支</span>
              <span class="dd-mobile-card__value">{{ row.branch || "-" }}</span>
            </div>
            <div class="dd-mobile-card__field">
              <span class="dd-mobile-card__label">状态</span>
              <div class="dd-mobile-card__value">
                <el-tag size="small" :type="getStatusTag(row.status)">{{
                  getStatusText(row.status)
                }}</el-tag>
              </div>
            </div>
            <div class="dd-mobile-card__field">
              <span class="dd-mobile-card__label">定时拉取</span>
              <span class="dd-mobile-card__value">{{
                row.schedule || "手动拉取"
              }}</span>
            </div>
            <div class="dd-mobile-card__field">
              <span class="dd-mobile-card__label">启用</span>
              <div class="dd-mobile-card__value">
                <el-switch
                  :model-value="row.enabled"
                  size="small"
                  @change="handleToggle(row)"
                />
              </div>
            </div>
            <div class="dd-mobile-card__field">
              <span class="dd-mobile-card__label">最后拉取</span>
              <span class="dd-mobile-card__value">{{
                row.last_pull_at
                  ? new Date(row.last_pull_at).toLocaleString()
                  : "-"
              }}</span>
            </div>
          </div>

          <div class="dd-mobile-card__actions subscription-card__actions">
            <el-button size="small" type="success" @click="handlePull(row)"
              >拉取</el-button
            >
            <el-button size="small" @click="openLogs(row.id)">日志</el-button>
            <el-button size="small" type="primary" plain @click="openEdit(row)"
              >编辑</el-button
            >
            <el-button
              size="small"
              type="danger"
              plain
              @click="handleDelete(row.id)"
              >删除</el-button
            >
          </div>
        </div>
      </div>

      <el-empty
        v-if="!loading && filteredSubList.length === 0"
        description="暂无订阅"
      />
    </div>

    <div v-else class="table-card">
      <el-table
        :data="filteredSubList"
        v-loading="loading"
        @selection-change="handleSelectionChange"
        style="width: 100%"
        :header-cell-style="{
          background: '#f8fafc',
          color: '#64748b',
          fontWeight: 600,
          fontSize: '13px',
        }"
      >
        <el-table-column type="selection" width="40" />
        <el-table-column prop="name" label="名称" min-width="120">
          <template #default="{ row }">
            <div class="sub-name-cell">
              <span class="sub-name-text">{{ row.name }}</span>
              <el-tag
                size="small"
                :type="row.type === 'git-repo' ? '' : 'warning'"
                round
              >
                {{ row.type === "git-repo" ? "Git" : "文件" }}
              </el-tag>
            </div>
          </template>
        </el-table-column>
        <el-table-column
          prop="url"
          label="URL"
          min-width="160"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            <span class="url-text">{{ row.url }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="branch" label="分支" width="80" />
        <el-table-column prop="schedule" label="定时拉取" width="110">
          <template #default="{ row }">
            <code v-if="row.schedule" class="cron-text">{{
              row.schedule
            }}</code>
            <span v-else class="text-muted">手动</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="70" align="center">
          <template #default="{ row }">
            <el-tag size="small" :type="getStatusTag(row.status)" round>{{
              getStatusText(row.status)
            }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="启用" width="60" align="center">
          <template #default="{ row }">
            <el-switch
              :model-value="row.enabled"
              size="small"
              @change="handleToggle(row)"
            />
          </template>
        </el-table-column>
        <el-table-column prop="last_pull_at" label="最后拉取" width="150">
          <template #default="{ row }">
            <span v-if="row.last_pull_at" class="time-text">{{
              new Date(row.last_pull_at).toLocaleString()
            }}</span>
            <span v-else class="text-muted">-</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="200" fixed="right" align="center">
          <template #default="{ row }">
            <div class="action-btns">
              <el-button
                size="small"
                type="success"
                text
                @click="handlePull(row)"
                >拉取</el-button
              >
              <el-button size="small" text @click="openLogs(row.id)"
                >日志</el-button
              >
              <el-button size="small" text type="primary" @click="openEdit(row)"
                >编辑</el-button
              >
              <el-button
                size="small"
                text
                type="danger"
                @click="handleDelete(row.id)"
                >删除</el-button
              >
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <div class="pagination-bar">
      <span class="pagination-total">共 {{ total }} 条数据</span>
      <el-pagination
        v-model:current-page="page"
        v-model:page-size="pageSize"
        :total="total"
        :page-sizes="[20, 50, 100]"
        layout="sizes, prev, pager, next"
        @current-change="loadData"
        @size-change="
          () => {
            page = 1;
            loadData();
          }
        "
      />
    </div>

    <el-dialog
      v-model="showEditDialog"
      :title="isCreate ? '新建订阅' : '编辑订阅'"
      width="600px"
      :fullscreen="dialogFullscreen"
    >
      <el-form
        :model="editForm"
        :label-width="dialogFullscreen ? 'auto' : '100px'"
        :label-position="dialogFullscreen ? 'top' : 'right'"
      >
        <el-form-item v-if="isCreate" label="一键识别">
          <div style="display: flex; gap: 8px; width: 100%">
            <el-input
              v-model="qlCommand"
              placeholder="粘贴 ql repo/raw 命令或仓库链接"
              clearable
              @keyup.enter="parseQLCommand"
            />
            <el-button type="primary" @click="parseQLCommand">识别</el-button>
          </div>
        </el-form-item>
        <el-form-item label="名称">
          <el-input v-model="editForm.name" placeholder="订阅名称" />
        </el-form-item>
        <el-form-item label="类型">
          <el-radio-group v-model="editForm.type">
            <el-radio value="git-repo">Git 仓库</el-radio>
            <el-radio value="single-file">单文件</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="URL">
          <el-input
            v-model="editForm.url"
            placeholder="仓库地址或文件下载链接"
          />
        </el-form-item>
        <el-form-item v-if="editForm.type === 'git-repo'" label="分支">
          <el-input
            v-model="editForm.branch"
            placeholder="默认分支 (留空使用默认)"
          />
        </el-form-item>
        <el-form-item label="定时拉取">
          <el-input
            v-model="editForm.schedule"
            placeholder="cron 表达式 (留空不自动拉取)"
          />
        </el-form-item>
        <el-form-item label="保存目录">
          <el-input
            v-model="editForm.save_dir"
            placeholder="保存到 scripts 下的子目录"
          />
        </el-form-item>
        <el-form-item v-if="editForm.type === 'git-repo'" label="指定子目录">
          <el-input
            v-model="editForm.sub_path"
            placeholder="仅拉取仓库中的指定子目录 (逗号分隔多个)"
          />
          <div
            style="
              color: var(--el-text-color-secondary);
              font-size: 12px;
              margin-top: 4px;
              line-height: 1.4;
            "
          >
            留空拉取全部内容，填写后仅检出指定子目录（如 scripts/daily, utils）
          </div>
        </el-form-item>
        <el-form-item v-if="editForm.type === 'git-repo'" label="仓库鉴权">
          <el-radio-group v-model="editForm.auth_type">
            <el-radio value="">无鉴权</el-radio>
            <el-radio value="ssh">SSH 密钥</el-radio>
            <el-radio value="token">Access Token</el-radio>
          </el-radio-group>
          <div
            style="
              color: var(--el-text-color-secondary);
              font-size: 12px;
              margin-top: 4px;
              line-height: 1.4;
            "
          >
            私有仓库推荐使用权限更可控的 Token；公开仓库可留空。
          </div>
        </el-form-item>
        <el-form-item label="别名">
          <el-input v-model="editForm.alias" placeholder="目录/文件别名" />
        </el-form-item>
        <el-form-item
          v-if="editForm.type === 'git-repo' && editForm.auth_type === 'ssh'"
          label="SSH 密钥"
        >
          <el-select
            v-model="editForm.ssh_key_id"
            placeholder="选择 SSH 密钥 (可选)"
            clearable
            style="width: 100%"
          >
            <el-option
              v-for="key in sshKeys"
              :key="key.id"
              :label="key.name"
              :value="key.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item
          v-if="editForm.type === 'git-repo' && editForm.auth_type === 'token'"
          label="鉴权用户名"
        >
          <el-input
            v-model="editForm.auth_username"
            placeholder="留空默认 x-access-token（GitHub 适用）"
          />
          <div
            style="
              color: var(--el-text-color-secondary);
              font-size: 12px;
              margin-top: 4px;
              line-height: 1.4;
            "
          >
            GitHub 留空即可；Gitee 填用户名；GitLab 可填 oauth2 或
            private-token。
          </div>
        </el-form-item>
        <el-form-item
          v-if="editForm.type === 'git-repo' && editForm.auth_type === 'token'"
          label="Access Token"
        >
          <el-input
            v-model="editForm.auth_token"
            type="password"
            show-password
            :placeholder="
              editForm.has_auth_token
                ? '留空则保持当前已保存 Token'
                : '粘贴 Git 平台访问令牌'
            "
          />
          <div
            style="
              color: var(--el-text-color-secondary);
              font-size: 12px;
              margin-top: 4px;
              line-height: 1.4;
            "
          >
            {{
              editForm.has_auth_token
                ? "当前已保存 Token。若不需要更新，保持留空即可。"
                : "建议使用仅仓库读取权限的 Token。"
            }}
          </div>
        </el-form-item>
        <el-form-item label="白名单">
          <el-input
            v-model="editForm.whitelist"
            placeholder="文件名/路径白名单 (逗号分隔)"
          />
          <div
            style="
              color: var(--el-text-color-secondary);
              font-size: 12px;
              margin-top: 4px;
              line-height: 1.4;
            "
          >
            白名单会同时限制实际检出的文件：只有命中白名单的文件会落盘。若主脚本依赖同目录的辅助文件，请把辅助文件名也一并加入白名单。
          </div>
        </el-form-item>
        <el-form-item label="黑名单">
          <el-input
            v-model="editForm.blacklist"
            placeholder="文件名/路径黑名单 (逗号分隔，如 Backup)"
          />
        </el-form-item>
        <el-form-item label="依赖说明">
          <el-input
            v-model="editForm.depend_on"
            placeholder="用于记录订阅依赖、过滤说明或迁移信息（仅备注，不参与文件检出）"
          />
        </el-form-item>
        <el-form-item label="拉取后钩子">
          <el-input
            v-model="editForm.hook_script"
            type="textarea"
            :rows="4"
            placeholder="拉取成功后执行的 Shell 命令。支持使用 $SUB_DIR、$SCRIPTS_DIR、$QL_DIR 等变量。"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showEditDialog = false">取消</el-button>
        <el-button type="primary" @click="handleSave">{{
          isCreate ? "创建" : "保存"
        }}</el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="showLogDialog"
      title="拉取日志"
      width="700px"
      :fullscreen="dialogFullscreen"
    >
      <el-table :data="logList" v-loading="logLoading" max-height="400px">
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-tag
              size="small"
              :type="row.status === 0 ? 'success' : 'danger'"
            >
              {{ row.status === 0 ? "成功" : "失败" }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="content" label="内容" show-overflow-tooltip />
        <el-table-column prop="duration" label="耗时" width="100">
          <template #default="{ row }">{{
            typeof row.duration === "number"
              ? row.duration.toFixed(1) + "s"
              : "-"
          }}</template>
        </el-table-column>
        <el-table-column prop="created_at" label="时间" width="170">
          <template #default="{ row }">{{
            new Date(row.created_at).toLocaleString()
          }}</template>
        </el-table-column>
        <el-table-column label="操作" width="80" fixed="right" align="center">
          <template #default="{ row }">
            <el-button
              size="small"
              text
              type="primary"
              @click="viewLogDetail(row)"
              >查看</el-button
            >
          </template>
        </el-table-column>
      </el-table>
      <div
        class="pagination-container"
        v-if="logTotal > 10"
        style="margin-top: 12px"
      >
        <el-pagination
          v-model:current-page="logPage"
          :total="logTotal"
          :page-size="10"
          layout="prev, pager, next"
          @current-change="loadLogs"
        />
      </div>
    </el-dialog>

    <el-dialog
      v-model="showLogDetail"
      title="日志详情"
      width="900px"
      :fullscreen="dialogFullscreen"
    >
      <pre
        class="pull-log-content dd-log-surface"
        style="min-height: 100px"
        v-html="logDetailContentHtml"
      ></pre>
    </el-dialog>

    <el-dialog
      v-model="showPullLog"
      title="拉取日志"
      width="900px"
      :fullscreen="dialogFullscreen"
      :close-on-click-modal="false"
      @close="closePullStream"
    >
      <div ref="pullLogRef" class="pull-log-content dd-log-surface">
        <div
          v-for="(line, i) in pullLogLineHtmlList"
          :key="i"
          class="pull-log-line"
          v-html="line"
        ></div>
        <div v-if="pullRunning" class="pull-log-line pull-running">
          <LoadingMotion
            variant="dots"
            size="sm"
            tone="warning"
            :stacked="false"
          />
          <span>拉取中...</span>
        </div>
        <el-empty
          v-if="!pullRunning && pullLogLines.length === 0"
          description="暂无输出"
          :image-size="60"
        />
      </div>
      <template #footer>
        <el-tag
          v-if="pullRunning"
          type="warning"
          effect="plain"
          size="small"
          style="margin-right: auto"
          >运行中</el-tag
        >
        <el-tag
          v-else-if="pullLogLines.length > 0"
          type="success"
          effect="plain"
          size="small"
          style="margin-right: auto"
          >已完成</el-tag
        >
        <el-button v-if="pullRunning" type="danger" @click="handleStopPull"
          >停止</el-button
        >
        <el-button @click="showPullLog = false">关闭</el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="showSettingsDialog"
      title="订阅设置"
      width="560px"
      :fullscreen="dialogFullscreen"
    >
      <el-form
        v-loading="settingsLoading"
        :label-width="dialogFullscreen ? 'auto' : '140px'"
        :label-position="dialogFullscreen ? 'top' : 'right'"
      >
        <el-form-item label="GitHub 镜像地址">
          <el-input
            v-model="settingsForm.github_mirror"
            :placeholder="DEFAULT_GITHUB_MIRROR"
          />
          <div class="settings-hint">
            留空使用默认值 {{ DEFAULT_GITHUB_MIRROR }}，拉取 GitHub
            仓库时自动加速
          </div>
        </el-form-item>
        <el-form-item label="自动添加定时任务">
          <el-switch
            v-model="settingsForm.auto_add_cron"
            inline-prompt
            active-text="开"
            inactive-text="关"
          />
          <div class="settings-hint">拉取后根据脚本内容自动同步定时任务</div>
        </el-form-item>
        <el-form-item label="自动删除失效任务">
          <el-switch
            v-model="settingsForm.auto_del_cron"
            inline-prompt
            active-text="开"
            inactive-text="关"
          />
          <div class="settings-hint">
            订阅源删除脚本后，自动删除对应定时任务
          </div>
        </el-form-item>
        <el-form-item label="覆盖拉取">
          <el-switch
            v-model="settingsForm.subscription_force_overwrite"
            inline-prompt
            active-text="开"
            inactive-text="关"
          />
        </el-form-item>
        <el-form-item label="默认 Cron 规则">
          <el-input
            v-model="settingsForm.default_cron_rule"
            placeholder="0 9 * * *"
          />
          <div class="settings-hint">匹配不到定时规则时使用，如 0 9 * * *</div>
        </el-form-item>
        <el-form-item label="拉取文件后缀">
          <el-input
            v-model="settingsForm.repo_file_extensions"
            placeholder="py js sh ts"
          />
          <div class="settings-hint">空格分隔，如 py js sh ts</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showSettingsDialog = false">取消</el-button>
        <el-button
          type="primary"
          :loading="settingsSaving"
          @click="handleSaveSettings"
          >保存</el-button
        >
      </template>
    </el-dialog>

    <!-- SSH Key Management Dialog -->
    <el-dialog
      v-model="showSSHKeyManageDialog"
      title="SSH 密钥管理"
      width="600px"
      :fullscreen="dialogFullscreen"
    >
      <div
        style="margin-bottom: 12px; display: flex; justify-content: flex-end"
      >
        <el-button type="primary" size="small" @click="openCreateSSHKey">
          <el-icon><Plus /></el-icon> 新建密钥
        </el-button>
      </div>
      <el-table :data="sshKeys" v-loading="sshKeyLoading" style="width: 100%">
        <el-table-column prop="name" label="名称" min-width="180" />
        <el-table-column prop="created_at" label="创建时间" width="170">
          <template #default="{ row }">
            <span class="time-text">{{
              new Date(row.created_at).toLocaleString()
            }}</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="150" fixed="right" align="center">
          <template #default="{ row }">
            <div class="action-btns">
              <el-button
                size="small"
                text
                type="primary"
                @click="openEditSSHKey(row)"
                >编辑</el-button
              >
              <el-button
                size="small"
                text
                type="danger"
                @click="handleDeleteSSHKey(row.id)"
                >删除</el-button
              >
            </div>
          </template>
        </el-table-column>
      </el-table>
      <el-empty
        v-if="!sshKeyLoading && sshKeys.length === 0"
        description="暂无 SSH 密钥"
      />
    </el-dialog>

    <!-- SSH Key Edit Dialog -->
    <el-dialog
      v-model="showSSHKeyDialog"
      :title="isCreateSSHKey ? '新建 SSH 密钥' : '编辑 SSH 密钥'"
      width="550px"
      :fullscreen="dialogFullscreen"
      append-to-body
    >
      <el-form
        :model="sshKeyForm"
        :label-width="dialogFullscreen ? 'auto' : '80px'"
        :label-position="dialogFullscreen ? 'top' : 'right'"
      >
        <el-form-item label="名称">
          <el-input v-model="sshKeyForm.name" placeholder="密钥名称" />
        </el-form-item>
        <el-form-item label="私钥">
          <el-input
            v-model="sshKeyForm.private_key"
            type="textarea"
            :rows="8"
            :placeholder="isCreateSSHKey ? '粘贴 SSH 私钥内容' : '留空不修改'"
            spellcheck="false"
            style="font-family: monospace"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showSSHKeyDialog = false">取消</el-button>
        <el-button type="primary" @click="handleSaveSSHKey">{{
          isCreateSSHKey ? "创建" : "保存"
        }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.subscriptions-page {
  padding: 0;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 18px;
  gap: 16px;

  h2 {
    margin: 0;
    font-size: 22px;
    font-weight: 700;
    color: var(--el-text-color-primary);
    line-height: 1.3;
  }
  .page-subtitle {
    font-size: 13px;
    color: var(--el-text-color-secondary);
    margin: 4px 0 0;
  }
  .header-actions {
    display: flex;
    gap: 10px;
    flex-shrink: 0;
  }
}

// 工具条：与定时任务页/执行日志页对齐——上下统一间距、左右两区一行排布、gap 一致
.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin: 14px 0;
  gap: 12px;
  flex-wrap: wrap;
  &__left {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
    flex: 1;
    min-width: 0;
  }
  &__right {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  &__search {
    width: 260px;
  }
}

// 状态分段控件：与定时任务页/执行日志页一致的胶囊容器 + 选中态白底品牌色 + 卡片阴影令牌
.status-tabs {
  display: inline-flex;
  background: var(--el-fill-color-light);
  border-radius: var(--dd-radius-sm);
  padding: 3px;
  gap: 2px;
}

.status-tab {
  padding: 6px 14px;
  border-radius: 7px;
  border: none;
  background: transparent;
  color: var(--el-text-color-secondary);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition:
    color var(--dd-motion-fast) var(--dd-ease-standard),
    background-color var(--dd-motion-fast) var(--dd-ease-standard),
    box-shadow var(--dd-motion-fast) var(--dd-ease-standard);
  white-space: nowrap;
  &:hover {
    color: var(--el-text-color-primary);
  }
  &.active {
    background: var(--el-bg-color);
    color: var(--el-color-primary);
    box-shadow: var(--dd-shadow-card);
    font-weight: 600;
  }
}

// 表格卡：圆角/阴影/边框全部对齐卡片令牌（dd-fixed-page 下的 flex + 内部滚动由全局规则接管）
.table-card {
  background: var(--el-bg-color);
  border-radius: var(--dd-card-radius);
  box-shadow: var(--dd-shadow-card);
  border: 1px solid var(--el-border-color-lighter);
  overflow: hidden;
}

.sub-name-cell {
  display: flex;
  align-items: center;
  gap: 8px;
}
.sub-name-text {
  font-weight: 500;
  color: var(--el-text-color-primary);
}
.url-text {
  font-family: var(--dd-font-mono);
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.cron-text {
  font-family: var(--dd-font-mono);
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.time-text {
  font-family: var(--dd-font-mono);
  font-size: 12px;
  color: var(--el-text-color-regular);
}
.text-muted {
  color: var(--el-text-color-placeholder);
}
// 操作列：与定时任务页/执行日志页一致的轻量行内按钮组
.action-btns {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  :deep(.el-button) {
    padding: 4px 8px;
  }
}

// 分页条：与定时任务页/执行日志页一致的间距收敛
.pagination-bar {
  margin-top: 14px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0 4px;
}
.pagination-total {
  font-size: 13px;
  color: var(--el-text-color-secondary);
}

:deep(.el-table) {
  // 边框统一走令牌，明暗自动适配（原写死浅灰会在暗色串色）
  --el-table-border-color: var(--el-border-color-lighter);
  .el-table__header-wrapper th {
    border-bottom: 1px solid var(--el-border-color-light);
  }
  .el-table__row td {
    border-bottom: 1px solid var(--el-border-color-lighter);
  }
  .el-table__cell {
    padding: 12px 0;
  }
}

.subscription-card__title-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
}
.subscription-card__actions > * {
  flex: 1 1 calc(50% - 4px);
}

.pull-log-content {
  font-family: var(--dd-font-mono, monospace);
  font-size: 13px;
  line-height: 1.6;
  padding: 12px 16px;
  border-radius: 6px;
  max-height: 560px;
  overflow-y: auto;
  white-space: pre-wrap;
  word-break: break-all;
}
.pull-log-line {
  white-space: pre-wrap;
  word-break: break-all;
}
.pull-running {
  color: var(--el-color-warning);
  display: flex;
  align-items: center;
  gap: 8px;
}
.settings-hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  margin-top: 4px;
  line-height: 1.4;
}

@media (max-width: 768px) {
  .page-header {
    flex-direction: column;
    gap: 10px;
    margin-bottom: 14px;
    h2 {
      font-size: 18px;
    }
  }
  .toolbar {
    flex-direction: column;
    align-items: stretch;
    gap: 10px;
    &__left {
      flex-direction: column;
      gap: 10px;
    }
    &__search {
      width: 100% !important;
    }
    &__right {
      justify-content: flex-end;
    }
  }
  .status-tabs {
    width: 100%;
    overflow-x: auto;
  }
  .subscription-card__title-row {
    flex-direction: column;
  }
}

// ===== 入场动画 =====
// 与定时任务页/执行日志页统一：只对卡片级容器（工具条 / 表格卡 / 移动列表）做克制的淡入上移 + 轻微错落；
// 不给表格每一行或每张移动卡做 stagger。时长走令牌，prefers-reduced-motion 时令牌自动降为 1ms 即等效关闭。
@keyframes dd-subs-rise-in {
  from {
    opacity: 0;
    transform: translateY(12px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.toolbar,
.table-card,
.dd-mobile-list {
  animation: dd-subs-rise-in var(--dd-motion-page) var(--dd-ease-decelerate) both;
}

// 轻微错落：工具条先入，表格卡/移动列表略晚
.table-card,
.dd-mobile-list {
  animation-delay: 60ms;
}
</style>
