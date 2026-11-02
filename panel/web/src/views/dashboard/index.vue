<script setup lang="ts">
import {
  ref,
  computed,
  onMounted,
  onUnmounted,
  onActivated,
  defineComponent,
  h,
  watch,
  defineAsyncComponent,
} from "vue";
import { useRouter } from "vue-router";
import { systemApi } from "@/api/system";
import { useAuthStore } from "@/stores/auth";
import { ElMessage } from "element-plus";
import {
  Timer,
  CirclePlus,
  Upload,
  Setting,
  Tickets,
  Connection,
  TrendCharts,
  CircleCheck,
  Loading,
  Refresh,
  ArrowRight,
  Cpu,
  Coin,
  FolderOpened,
  ArrowUp,
  ArrowDown,
  View,
  Document,
} from "@element-plus/icons-vue";
import { useResponsive } from "@/composables/useResponsive";
import { canAdminister, hasRequiredRole } from "@/utils/roles";
import { formatDuration } from "@/utils/duration";
import { formatDateTime } from "@/utils/datetime";

const ExecutionTrendChart = defineAsyncComponent(
  () => import("./components/ExecutionTrendChart.vue"),
);
const router = useRouter();
const authStore = useAuthStore();
const { isMobile } = useResponsive();
const LOG_STATUS_SUCCESS = 0;
const LOG_STATUS_FAILED = 1;
const LOG_STATUS_RUNNING = 2;
const LOG_STATUS_ABORTED = 3;

const showTrendChart = ref(false);
const trendChartHostRef = ref<HTMLElement | null>(null);
let trendChartObserver: IntersectionObserver | null = null;
let trendChartTimer: number | null = null;

const trendRange = ref<7 | 30>(7);
type LogFilter = "all" | "success" | "failed" | "aborted" | "running";
const logFilter = ref<LogFilter>("all");
const logFilterOptions: Array<{ label: string; value: LogFilter }> = [
  { label: "全部", value: "all" },
  { label: "成功", value: "success" },
  { label: "失败", value: "failed" },
  { label: "终止", value: "aborted" },
  { label: "运行中", value: "running" },
];
const refreshTimestamp = ref(new Date());
const hasLoadedOnce = ref(false);
const skipInitialActivated = ref(true);
const canViewSystemDetails = computed(() =>
  canAdminister(authStore.user?.role),
);

const CountUp = defineComponent({
  props: {
    endVal: { type: Number, default: 0 },
    duration: { type: Number, default: 1.2 },
    decimals: { type: Number, default: 0 },
    suffix: { type: String, default: "" },
    prefix: { type: String, default: "" },
  },
  setup(props) {
    const display = ref("0");
    let animFrame = 0;

    function animate() {
      const start = 0;
      const end = props.endVal;
      const dur = props.duration * 1000;
      const startTime = performance.now();

      function step(now: number) {
        const elapsed = now - startTime;
        const progress = Math.min(elapsed / dur, 1);
        const eased = 1 - Math.pow(1 - progress, 3);
        const current = start + (end - start) * eased;
        display.value = formatNumber(current, props.decimals);
        if (progress < 1) {
          animFrame = requestAnimationFrame(step);
        }
      }
      cancelAnimationFrame(animFrame);
      animFrame = requestAnimationFrame(step);
    }

    function formatNumber(n: number, decimals: number) {
      const fixed = n.toFixed(decimals);
      const [intPart = "0", decPart] = fixed.split(".");
      const grouped = intPart.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
      return decPart ? grouped + "." + decPart : grouped;
    }

    watch(
      () => props.endVal,
      () => animate(),
      { immediate: true },
    );
    onUnmounted(() => cancelAnimationFrame(animFrame));
    return () => h("span", {}, props.prefix + display.value + props.suffix);
  },
});

const dashboardData = ref<any>({});
const sysInfo = ref<any>({});
const recentLogs = computed(() => dashboardData.value.recent_logs || []);

const greeting = computed(() => {
  const hour = new Date().getHours();
  if (hour < 6) return "夜深了";
  if (hour < 12) return "早上好";
  if (hour < 14) return "中午好";
  if (hour < 18) return "下午好";
  return "晚上好";
});

const greetingSub = computed(() => {
  const hour = new Date().getHours();
  if (hour < 6) return "夜深了，记得早点休息哦~";
  if (hour < 12) return "欢迎回来！今天又是高效执行任务的一天！";
  if (hour < 14) return "该吃午饭啦，注意劳逸结合~";
  if (hour < 18) return "下午也要保持专注哦！";
  return "辛苦一天啦，看看任务运行情况吧~";
});

// hero 内的日期/星期文案：用 new Date() 直接推导，无需新增数据源
const heroDateLabel = computed(() => {
  void refreshTimestamp.value; // 刷新时一并更新展示
  const now = new Date();
  const weekdays = ["周日", "周一", "周二", "周三", "周四", "周五", "周六"];
  const m = String(now.getMonth() + 1).padStart(2, "0");
  const d = String(now.getDate()).padStart(2, "0");
  return `${m}月${d}日 · ${weekdays[now.getDay()]}`;
});

const todayLogs = computed(() => Number(dashboardData.value.today_logs) || 0);
const successLogs = computed(
  () => Number(dashboardData.value.success_logs) || 0,
);
const failedLogs = computed(() => Number(dashboardData.value.failed_logs) || 0);
const abortedLogs = computed(() => Number(dashboardData.value.aborted_logs) || 0);
const taskCount = computed(() => Number(dashboardData.value.task_count) || 0);
const prevTaskCount = computed(
  () => Number(dashboardData.value.prev_task_count) || 0,
);
const runningTasks = computed(
  () => Number(dashboardData.value.running_tasks) || 0,
);
const yesterdayLogs = computed(
  () => Number(dashboardData.value.yesterday_logs) || 0,
);
const yesterdaySuccess = computed(
  () => Number(dashboardData.value.yesterday_success) || 0,
);
const yesterdayFailed = computed(
  () => Number(dashboardData.value.yesterday_failed) || 0,
);
const yesterdayAborted = computed(
  () => Number(dashboardData.value.yesterday_aborted) || 0,
);
const todayFinishedLogs = computed(() => successLogs.value + failedLogs.value);
const yesterdayFinishedLogs = computed(() => yesterdaySuccess.value + yesterdayFailed.value);

const todaySuccessRate = computed(() => {
  if (!todayFinishedLogs.value) return 0;
  return Math.round((successLogs.value / todayFinishedLogs.value) * 1000) / 10;
});

const yesterdaySuccessRate = computed(() => {
  if (!yesterdayFinishedLogs.value) return 0;
  return Math.round((yesterdaySuccess.value / yesterdayFinishedLogs.value) * 1000) / 10;
});

const taskCountDelta = computed(() => taskCount.value - prevTaskCount.value);
const todayLogsDelta = computed(() => todayLogs.value - yesterdayLogs.value);
const successRateDelta = computed(() => {
  return (
    Math.round((todaySuccessRate.value - yesterdaySuccessRate.value) * 10) / 10
  );
});
const todayAbortSubText = computed(() => {
  // Aborted 单独展示，不参与成功率；这里仅提示今天/昨天的主动终止数量。
  if (abortedLogs.value > 0 || yesterdayAborted.value > 0) {
    return `终止 ${abortedLogs.value} / 昨日 ${yesterdayAborted.value}`;
  }
  return "较昨日";
});

const statCards = computed(() => [
  {
    key: "total",
    label: "任务总数",
    value: taskCount.value,
    sub: "已配置任务",
    delta: taskCountDelta.value,
    deltaSuffix: "",
    icon: Tickets,
    color: "#3b82f6",
    bgIcon: "rgba(59, 130, 246, 0.12)",
    link: "/tasks",
  },
  {
    key: "running",
    label: "运行中的任务",
    value: runningTasks.value,
    sub: "实时运行中",
    delta: null,
    icon: Loading,
    color: "#10b981",
    bgIcon: "rgba(16, 185, 129, 0.12)",
    link: "/tasks",
    spinning: runningTasks.value > 0,
  },
  {
    key: "today",
    label: "今日执行",
    value: todayLogs.value,
    sub: todayAbortSubText.value,
    delta: todayLogsDelta.value,
    deltaSuffix: "",
    icon: TrendCharts,
    color: "#f59e0b",
    bgIcon: "rgba(245, 158, 11, 0.12)",
    link: "/logs",
  },
  {
    key: "success-rate",
    label: "成功率",
    value: todaySuccessRate.value,
    sub: "较昨日",
    delta: successRateDelta.value,
    deltaSuffix: "%",
    icon: CircleCheck,
    color: "#06b6d4",
    bgIcon: "rgba(6, 182, 212, 0.12)",
    link: "/logs",
    suffix: "%",
    decimals: 1,
  },
]);

const quickActions = computed(() =>
  [
    {
      key: "create",
      label: "新建任务",
      icon: CirclePlus,
      color: "#3b82f6",
      bg: "rgba(59, 130, 246, 0.1)",
      minRole: "operator",
      action: () => router.push({ path: "/tasks", query: { create: "1" } }),
    },
    {
      key: "import",
      label: "导入脚本",
      icon: Upload,
      color: "#10b981",
      bg: "rgba(16, 185, 129, 0.1)",
      minRole: "operator",
      action: () => router.push({ path: "/scripts", query: { upload: "1" } }),
    },
    {
      key: "env",
      label: "环境变量",
      icon: Setting,
      color: "#f59e0b",
      bg: "rgba(245, 158, 11, 0.1)",
      minRole: "operator",
      action: () => router.push("/envs"),
    },
    {
      key: "log",
      label: "执行日志",
      icon: Tickets,
      color: "#06b6d4",
      bg: "rgba(6, 182, 212, 0.1)",
      minRole: "viewer",
      action: () => router.push("/logs"),
    },
    {
      key: "api",
      label: "接口文档",
      icon: Connection,
      color: "#06b6d4",
      bg: "rgba(6, 182, 212, 0.1)",
      minRole: "viewer",
      action: () => router.push("/docs/api"),
    },
  ].filter((action) => hasRequiredRole(authStore.user?.role, action.minRole)),
);

function isRunningLog(status: number | null | undefined) {
  return (
    status === LOG_STATUS_RUNNING || status === null || status === undefined
  );
}

function isSuccessLog(status: number | null | undefined) {
  return status === LOG_STATUS_SUCCESS;
}

function isFailedLog(status: number | null | undefined) {
  return status === LOG_STATUS_FAILED;
}

function isAbortedLog(status: number | null | undefined) {
  return status === LOG_STATUS_ABORTED;
}

function normalizeLabels(labels: unknown): string[] {
  if (Array.isArray(labels)) {
    return labels.map(String).filter(Boolean);
  }
  if (typeof labels === "string") {
    return labels
      .split(",")
      .map((item) => item.trim())
      .filter(Boolean);
  }
  return [];
}

function taskTypeOf(log: any) {
  return log?.task_type || log?.task?.task_type;
}

function labelsOf(log: any) {
  return normalizeLabels(log?.labels ?? log?.task?.labels);
}

const resourceItems = computed(() => {
  const s = sysInfo.value;
  return [
    {
      key: "cpu",
      label: "CPU",
      icon: Cpu,
      iconColor: "#3b82f6",
      iconBg: "rgba(59, 130, 246, 0.12)",
      detail: `${s.num_cpu || "-"} 核心`,
      percent: Number(s.cpu_usage) || 0,
      // 进度条用纯色，不用渐变，保持整体扁平观感
      barColor: "#3b82f6",
    },
    {
      key: "memory",
      label: "内存",
      icon: Coin,
      iconColor: "#06b6d4",
      iconBg: "rgba(6, 182, 212, 0.12)",
      detail: `${formatBytes(s.memory_used)} / ${formatBytes(s.memory_total)}`,
      percent: Number(s.memory_usage) || 0,
      barColor: "#06b6d4",
    },
    {
      key: "disk",
      label: "磁盘",
      icon: FolderOpened,
      iconColor: "#10b981",
      iconBg: "rgba(16, 185, 129, 0.12)",
      detail: `${formatBytes(s.disk_used)} / ${formatBytes(s.disk_total)}`,
      percent: Number(s.disk_usage) || 0,
      barColor: "#10b981",
    },
  ];
});

function formatBytes(bytes: number) {
  if (!bytes) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let val = bytes;
  while (val >= 1024 && i < units.length - 1) {
    val /= 1024;
    i++;
  }
  return val.toFixed(1) + " " + units[i];
}

function lastUpdatedText() {
  const diff = (Date.now() - refreshTimestamp.value.getTime()) / 1000;
  if (diff < 60) return "刚刚";
  if (diff < 3600) return Math.floor(diff / 60) + " 分钟前";
  return Math.floor(diff / 3600) + " 小时前";
}

const lastUpdatedTick = ref(0);
let lastUpdatedTimer: number | null = null;

const filteredLogs = computed(() => {
  const list = recentLogs.value;
  if (logFilter.value === "all") return list.slice(0, 5);
  if (logFilter.value === "running")
    return list.filter((l: any) => isRunningLog(l.status)).slice(0, 5);
  if (logFilter.value === "success")
    return list.filter((l: any) => isSuccessLog(l.status)).slice(0, 5);
  if (logFilter.value === "failed")
    return list.filter((l: any) => isFailedLog(l.status)).slice(0, 5);
  if (logFilter.value === "aborted")
    return list.filter((l: any) => isAbortedLog(l.status)).slice(0, 5);
  return list.slice(0, 5);
});

const taskStats = computed(() => {
  const dailyStats = (dashboardData.value.daily_stats || []) as Array<{
    success: number;
    failed: number;
    aborted?: number;
  }>;
  const totalSuccess = dailyStats.reduce((sum, d) => sum + (d.success || 0), 0);
  const totalFailed = dailyStats.reduce((sum, d) => sum + (d.failed || 0), 0);
  const totalAborted = dailyStats.reduce((sum, d) => sum + (d.aborted || 0), 0);
  const running = runningTasks.value;
  const total = totalSuccess + totalFailed + totalAborted + running;

  function pct(n: number) {
    if (!total) return 0;
    return Math.round((n / total) * 1000) / 10;
  }

  return {
    total,
    success: totalSuccess,
    failed: totalFailed,
    aborted: totalAborted,
    running,
    successPct: pct(totalSuccess),
    failedPct: pct(totalFailed),
    abortedPct: pct(totalAborted),
    runningPct: pct(running),
  };
});

// 平均执行时长（秒）。没有可用样本时返回 null，交给 formatDuration 显示 "-"，
// 避免把「没数据」渲染成 0，让人误以为任务是瞬间跑完的。
// 这里刻意不做预先四舍五入：提前把 59.96 修成 60.0 会让格式化结果跳成 "1m"，
// 精度统一交给 formatDuration 处理。
const avgDuration = computed<number | null>(() => {
  const list = recentLogs.value;
  if (!list.length) return null;
  const valid = list.filter((l: any) => l.duration != null);
  if (!valid.length) return null;
  const sum = valid.reduce((s: number, l: any) => s + (l.duration || 0), 0);
  return sum / valid.length;
});

const loadDashboard = async () => {
  try {
    const res = (await systemApi.dashboard(trendRange.value)) as any;
    dashboardData.value = res.data || {};
    refreshTimestamp.value = new Date();
  } catch {
    ElMessage.error("加载仪表盘数据失败");
  }
};

const loadSysInfo = async () => {
  try {
    const res = (await systemApi.info()) as any;
    sysInfo.value = res.data || {};
  } catch {
    ElMessage.error("加载系统信息失败");
  }
};

watch(trendRange, () => {
  loadDashboard();
});

function activateTrendChart() {
  if (showTrendChart.value || trendChartTimer) return;
  trendChartTimer = window.setTimeout(() => {
    showTrendChart.value = true;
    trendChartTimer = null;
  }, 120);
}

function stopObservingTrendChart() {
  if (trendChartObserver) {
    trendChartObserver.disconnect();
    trendChartObserver = null;
  }
}

function scheduleTrendChartRender() {
  if (showTrendChart.value || !trendChartHostRef.value) return;
  if (
    typeof window === "undefined" ||
    typeof IntersectionObserver === "undefined"
  ) {
    activateTrendChart();
    return;
  }
  stopObservingTrendChart();
  trendChartObserver = new IntersectionObserver(
    (entries) => {
      if (!entries.some((e) => e.isIntersecting)) return;
      stopObservingTrendChart();
      activateTrendChart();
    },
    { rootMargin: "160px 0px" },
  );
  trendChartObserver.observe(trendChartHostRef.value);
}

function loadDashboardPage() {
  loadDashboard();
  loadSysInfo();
}

function handleRefresh() {
  loadDashboardPage();
}

onMounted(() => {
  loadDashboardPage();
  hasLoadedOnce.value = true;
  scheduleTrendChartRender();
  lastUpdatedTimer = window.setInterval(() => {
    lastUpdatedTick.value++;
  }, 30 * 1000);
});

onActivated(() => {
  if (skipInitialActivated.value) {
    skipInitialActivated.value = false;
  } else if (hasLoadedOnce.value) {
    loadDashboardPage();
  }
  scheduleTrendChartRender();
});

onUnmounted(() => {
  stopObservingTrendChart();
  if (trendChartTimer) {
    clearTimeout(trendChartTimer);
    trendChartTimer = null;
  }
  if (lastUpdatedTimer) {
    clearInterval(lastUpdatedTimer);
    lastUpdatedTimer = null;
  }
});

const updatedHint = computed(() => {
  // 触发重新计算（通过 lastUpdatedTick）
  void lastUpdatedTick.value;
  return lastUpdatedText();
});

function statusBadgeType(status: number | null | undefined) {
  if (isRunningLog(status)) return "primary";
  if (isSuccessLog(status)) return "success";
  if (isAbortedLog(status)) return "warning";
  return "danger";
}

function statusBadgeText(status: number | null | undefined) {
  if (isRunningLog(status)) return "运行中";
  if (isSuccessLog(status)) return "成功";
  if (isAbortedLog(status)) return "已终止";
  return "失败";
}

function triggerLabel(taskType: string | undefined) {
  switch (taskType) {
    case "manual":
      return "手动执行";
    case "startup":
      return "启动执行";
    default:
      return "定时任务";
  }
}

function envLabel(log: any) {
  const labels = labelsOf(log);
  if (labels.length > 0) return labels[0] || "default";
  return taskTypeOf(log) === "manual" ? "manual" : "cron";
}

function envBadgeColor(env: string) {
  switch (env) {
    case "prod":
    case "production":
      return { bg: "rgba(16, 185, 129, 0.1)", color: "#10b981" };
    case "staging":
      return { bg: "rgba(245, 158, 11, 0.1)", color: "#f59e0b" };
    case "test":
      return { bg: "rgba(59, 130, 246, 0.1)", color: "#3b82f6" };
    case "local":
      return { bg: "rgba(148, 163, 184, 0.15)", color: "#64748b" };
    case "manual":
      return { bg: "rgba(245, 158, 11, 0.1)", color: "#f59e0b" };
    case "cron":
      return { bg: "rgba(59, 130, 246, 0.1)", color: "#3b82f6" };
    default:
      return { bg: "rgba(59, 130, 246, 0.1)", color: "#3b82f6" };
  }
}

function viewLog(log: any) {
  router.push({ path: "/logs", query: { task_id: log.task_id } });
}

function rerunLog(log: any) {
  router.push({
    path: "/tasks",
    query: { task_id: log.task_id, action: "run" },
  });
}
</script>

<template>
  <div class="dashboard-page dd-scroll-page">
    <!-- ============ 轻量问候条：坐在页面底色上，问候语 + 快捷操作按钮 ============ -->
    <section class="dash-welcome animate-fade-in-up">
      <!-- 左侧：问候语 + 日期/副标题元信息 -->
      <div class="dash-welcome__greet">
        <h2 class="dash-welcome__title">
          {{ greeting }}，{{ authStore.user?.username || "User" }} 👋
        </h2>
        <span class="dash-welcome__meta"
          >{{ heroDateLabel }} · {{ greetingSub }}</span
        >
      </div>
      <!-- 右侧：快捷操作按钮，复用 quickActions 数据与点击逻辑 -->
      <div class="dash-welcome__actions">
        <button
          v-for="action in quickActions"
          :key="action.key"
          class="dash-pill"
          @click="action.action"
        >
          <el-icon :size="15"><component :is="action.icon" /></el-icon>
          <span>{{ action.label }}</span>
        </button>
      </div>
    </section>

    <!-- ============ 4 Stat Cards ============ -->
    <section class="stat-grid animate-fade-in-up delay-50">
      <div
        v-for="card in statCards"
        :key="card.key"
        class="stat-card stat-card--cinematic"
        @click="router.push(card.link)"
      >
        <div class="stat-card__main">
          <span class="stat-card__label">{{ card.label }}</span>
          <span class="stat-card__value" :style="{ color: card.color }">
            <CountUp
              :end-val="card.value"
              :duration="1.2"
              :decimals="card.decimals || 0"
              :suffix="card.suffix || ''"
            />
          </span>
          <span class="stat-card__delta">
            <template v-if="card.delta !== null && card.delta !== undefined">
              <span class="stat-card__delta-prefix">{{ card.sub }}</span>
              <span
                v-if="card.delta === 0"
                class="stat-card__delta-value is-flat"
                >持平</span
              >
              <span
                v-else
                class="stat-card__delta-value"
                :class="card.delta > 0 ? 'is-up' : 'is-down'"
              >
                <el-icon :size="11">
                  <component :is="card.delta > 0 ? ArrowUp : ArrowDown" />
                </el-icon>
                {{ card.delta > 0 ? "+" : "" }}{{ card.delta
                }}{{ card.deltaSuffix || "" }}
              </span>
            </template>
            <template v-else>
              <span class="stat-card__delta-prefix">{{ card.sub }}</span>
            </template>
          </span>
        </div>
        <div
          class="stat-card__icon"
          :style="{ background: card.bgIcon, color: card.color }"
        >
          <el-icon :size="20" :class="{ 'icon-spin': card.spinning }">
            <component :is="card.icon" />
          </el-icon>
        </div>
      </div>
    </section>

    <!-- ============ 焦点行：执行趋势（大）+ 执行统计占比条 ============ -->
    <section class="focus-grid animate-fade-in-up delay-100">
      <!-- 执行趋势 -->
      <div class="panel panel--trend">
        <div class="panel-header">
          <div class="panel-header__title">
            <el-icon
              class="panel-header__icon"
              :size="14"
              style="color: #3b82f6"
              ><TrendCharts
            /></el-icon>
            <span>执行趋势</span>
          </div>
          <div class="panel-header__actions">
            <div class="seg-btn-group">
              <button
                class="seg-btn"
                :class="{ 'is-active': trendRange === 7 }"
                @click="trendRange = 7"
              >
                近7天
              </button>
              <button
                class="seg-btn"
                :class="{ 'is-active': trendRange === 30 }"
                @click="trendRange = 30"
              >
                近30天
              </button>
            </div>
          </div>
        </div>
        <div ref="trendChartHostRef" class="trend-chart-shell">
          <ExecutionTrendChart
            v-if="showTrendChart"
            :stats="dashboardData.daily_stats || []"
          />
          <div v-else class="trend-chart-placeholder">
            <div class="placeholder-bar"></div>
            <div class="placeholder-bar placeholder-bar--short"></div>
            <div class="placeholder-legend">
              <span></span><span></span><span></span>
            </div>
          </div>
        </div>
      </div>

      <!-- 执行统计：横向堆叠占比条 -->
      <div class="panel panel--stats">
        <div class="panel-header">
          <div class="panel-header__title">
            <el-icon
              class="panel-header__icon"
              :size="14"
              style="color: #10b981"
              ><TrendCharts
            /></el-icon>
            <span>执行统计</span>
            <span class="panel-header__hint">近{{ trendRange }}天</span>
          </div>
          <div class="panel-header__actions">
            <button class="text-link" @click="router.push('/logs')">
              查看更多 <el-icon :size="11"><ArrowRight /></el-icon>
            </button>
          </div>
        </div>
        <div class="task-stats-body">
          <div class="task-stats__total">
            <span class="task-stats__total-label">总执行数</span>
            <span class="task-stats__total-value">
              <CountUp :end-val="taskStats.total" :duration="1.2" />
            </span>
          </div>
          <!-- 横向堆叠占比条：各段宽度直接取 taskStats 已算好的百分比；
               总执行数为 0 时四段宽度均为 0，只剩条底的空态色，不存在除零 -->
          <div class="task-stats__bar">
            <span
              class="task-stats__seg is-success"
              :style="{ width: taskStats.successPct + '%' }"
            ></span>
            <span
              class="task-stats__seg is-failed"
              :style="{ width: taskStats.failedPct + '%' }"
            ></span>
            <span
              class="task-stats__seg is-running"
              :style="{ width: taskStats.runningPct + '%' }"
            ></span>
            <span
              class="task-stats__seg is-aborted"
              :style="{ width: taskStats.abortedPct + '%' }"
            ></span>
          </div>
          <div class="task-stats__legend">
            <div class="task-stats__legend-item">
              <span class="task-stats__swatch is-success"></span>
              <span class="task-stats__legend-label">成功</span>
              <span class="task-stats__legend-value">{{
                taskStats.success.toLocaleString()
              }}</span>
              <span class="task-stats__legend-pct"
                >({{ taskStats.successPct }}%)</span
              >
            </div>
            <div class="task-stats__legend-item">
              <span class="task-stats__swatch is-failed"></span>
              <span class="task-stats__legend-label">失败</span>
              <span class="task-stats__legend-value">{{
                taskStats.failed.toLocaleString()
              }}</span>
              <span class="task-stats__legend-pct"
                >({{ taskStats.failedPct }}%)</span
              >
            </div>
            <div class="task-stats__legend-item">
              <span class="task-stats__swatch is-running"></span>
              <span class="task-stats__legend-label">运行中</span>
              <span class="task-stats__legend-value">{{
                taskStats.running.toLocaleString()
              }}</span>
              <span class="task-stats__legend-pct"
                >({{ taskStats.runningPct }}%)</span
              >
            </div>
            <div class="task-stats__legend-item">
              <span class="task-stats__swatch is-aborted"></span>
              <span class="task-stats__legend-label">终止</span>
              <span class="task-stats__legend-value">{{
                taskStats.aborted.toLocaleString()
              }}</span>
              <span class="task-stats__legend-pct"
                >({{ taskStats.abortedPct }}%)</span
              >
            </div>
          </div>
        </div>
        <div class="task-stats-footer">
          <span class="task-stats-footer__label">平均执行时长</span>
          <span class="task-stats-footer__value">{{
            formatDuration(avgDuration)
          }}</span>
        </div>
      </div>
    </section>

    <!-- ============ 底部行：最近执行任务表（大）+ 系统资源 ============ -->
    <section class="bottom-grid animate-fade-in-up delay-150">
      <!-- 最近执行任务 -->
      <div class="panel panel--logs">
        <div class="panel-header">
          <div class="panel-header__title">
            <el-icon
              class="panel-header__icon"
              :size="14"
              style="color: #06b6d4"
              ><Document
            /></el-icon>
            <span>最近执行任务</span>
            <div class="seg-btn-group seg-btn-group--mini">
              <button
                v-for="opt in logFilterOptions"
                :key="opt.value"
                class="seg-btn"
                :class="{ 'is-active': logFilter === opt.value }"
                @click="logFilter = opt.value"
              >
                {{ opt.label }}
              </button>
            </div>
          </div>
          <div class="panel-header__actions">
            <button class="text-link" @click="router.push('/logs')">
              查看更多 <el-icon :size="11"><ArrowRight /></el-icon>
            </button>
          </div>
        </div>
        <div v-if="isMobile" class="log-mobile-list">
          <div v-if="filteredLogs.length === 0" class="empty-hint">
            暂无记录
          </div>
          <div
            v-for="log in filteredLogs"
            :key="log.id"
            class="log-mobile-card"
          >
            <div class="log-mobile-card__head">
              <span class="log-mobile-card__name">{{
                log.task_name || "未命名任务"
              }}</span>
              <span
                class="log-status-chip"
                :class="`is-${statusBadgeType(log.status)}`"
              >
                {{ statusBadgeText(log.status) }}
              </span>
            </div>
            <div class="log-mobile-card__meta">
              <span>{{ formatDateTime(log.created_at) }}</span>
              <span v-if="log.duration != null"
                >耗时 {{ formatDuration(log.duration) }}</span
              >
            </div>
          </div>
        </div>
        <table v-else class="log-table">
          <colgroup>
            <col class="log-table__col log-table__col--name" />
            <col class="log-table__col log-table__col--status" />
            <col class="log-table__col log-table__col--time" />
            <col class="log-table__col log-table__col--duration" />
            <col class="log-table__col log-table__col--trigger" />
            <col class="log-table__col log-table__col--env" />
            <col class="log-table__col log-table__col--actions" />
          </colgroup>
          <thead>
            <tr>
              <th>任务名称</th>
              <th class="col-center">状态</th>
              <th>执行时间</th>
              <th class="col-center">耗时</th>
              <th class="col-center">触发方式</th>
              <th class="col-center">环境</th>
              <th class="col-center">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="filteredLogs.length === 0">
              <td colspan="7" class="empty-cell">暂无记录</td>
            </tr>
            <!-- 这里刻意【不】给每一行做 stagger 入场，见 design-system.md「入场动画」一条：
                 行数一多逐行动画就会卡，而且这张表是轮询刷新的，每次刷新都会整表重放，
                 表现为规律性闪烁。整块入场由外层 .panel 的 dd-panel-in 负责，一次就够。 -->
            <tr v-for="log in filteredLogs" :key="log.id">
              <td>
                <span class="log-cell-name">{{
                  log.task_name || "未命名任务"
                }}</span>
              </td>
              <td class="col-center">
                <span
                  class="log-status-chip"
                  :class="`is-${statusBadgeType(log.status)}`"
                >
                  {{ statusBadgeText(log.status) }}
                </span>
              </td>
              <td>
                <span class="log-cell-time">{{
                  formatDateTime(log.created_at)
                }}</span>
              </td>
              <td class="col-center">
                <span class="log-cell-duration">{{
                  formatDuration(log.duration)
                }}</span>
              </td>
              <td class="col-center">
                <span class="log-cell-trigger">{{
                  triggerLabel(taskTypeOf(log))
                }}</span>
              </td>
              <td class="col-center">
                <span
                  class="env-chip"
                  :style="{
                    background: envBadgeColor(envLabel(log)).bg,
                    color: envBadgeColor(envLabel(log)).color,
                  }"
                  >{{ envLabel(log) }}</span
                >
              </td>
              <td class="col-center">
                <div class="log-cell-actions">
                  <button
                    class="icon-btn"
                    title="查看日志"
                    @click="viewLog(log)"
                  >
                    <el-icon :size="14"><View /></el-icon>
                  </button>
                  <button
                    class="icon-btn"
                    title="重新运行"
                    @click="rerunLog(log)"
                  >
                    <el-icon :size="14"><Refresh /></el-icon>
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- 系统资源 -->
      <div class="panel panel--resource">
        <div class="panel-header">
          <div class="panel-header__title">
            <el-icon
              class="panel-header__icon"
              :size="14"
              style="color: #10b981"
              ><Cpu
            /></el-icon>
            <span>系统资源</span>
            <span class="panel-header__hint">最近更新：{{ updatedHint }}</span>
          </div>
          <div v-if="canViewSystemDetails" class="panel-header__actions">
            <button class="text-link" @click="router.push('/admin/settings')">
              查看详情 <el-icon :size="11"><ArrowRight /></el-icon>
            </button>
          </div>
        </div>
        <div class="resource-list">
          <div v-for="r in resourceItems" :key="r.key" class="resource-row">
            <div
              class="resource-row__icon"
              :style="{ background: r.iconBg, color: r.iconColor }"
            >
              <el-icon :size="16"><component :is="r.icon" /></el-icon>
            </div>
            <div class="resource-row__body">
              <div class="resource-row__top">
                <span class="resource-row__label">{{ r.label }}</span>
                <span class="resource-row__detail">{{ r.detail }}</span>
                <span class="resource-row__pct"
                  >{{ r.percent.toFixed(1) }}%</span
                >
              </div>
              <div class="resource-bar">
                <div
                  class="resource-bar__fill"
                  :style="{
                    width: Math.min(r.percent, 100) + '%',
                    background: r.barColor,
                  }"
                ></div>
              </div>
            </div>
          </div>
          <div class="resource-row">
            <div
              class="resource-row__icon"
              style="background: rgba(245, 158, 11, 0.12); color: #f59e0b"
            >
              <el-icon :size="16"><Timer /></el-icon>
            </div>
            <div class="resource-row__body">
              <div class="resource-row__top">
                <span class="resource-row__label">面板运行</span>
                <span class="resource-row__detail uptime-detail">{{
                  sysInfo.uptime || "-"
                }}</span>
              </div>
              <div class="uptime-track">
                <span class="uptime-track__dot"></span>
                <span class="uptime-track__line"></span>
                <span class="uptime-track__text">自本次面板启动后持续运行</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped lang="scss">
.dashboard-page {
  display: flex;
  flex-direction: column;
  // 行间留白加大，体现"不塞满"的呼吸感
  gap: 20px;
}

// ============ 轻量问候条（坐在页面底色上，无彩色背景）============
.dash-welcome {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  // 与下方统计卡拉开少量间距即可，整体紧凑
  margin-bottom: 4px;
}

.dash-welcome__greet {
  display: flex;
  flex-direction: column;
  // 标题与元信息间 4px
  gap: 4px;
  min-width: 0;
}

.dash-welcome__title {
  margin: 0;
  font-size: 21px;
  font-weight: 700;
  line-height: 1.25;
  color: var(--el-text-color-primary);
}

.dash-welcome__meta {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  line-height: 1.4;
}

// 快捷操作按钮：复用页面令牌，明暗双主题自动适配
.dash-welcome__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.dash-pill {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 7px 14px;
  /* 注意：类名叫 pill，实际是问候条右侧的快捷操作按钮（可点击）→ control 档，
     不吃 pill 档，否则会和状态 chip 混成一类 */
  border-radius: var(--dd-radius-control);
  border: 1px solid var(--el-border-color-lighter);
  background: var(--el-bg-color);
  color: var(--el-text-color-regular);
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition:
    color var(--dd-motion-fast) var(--dd-ease-standard),
    border-color var(--dd-motion-fast) var(--dd-ease-standard);

  &:hover {
    // 只换边框与文字颜色，不做位移与缩放
    border-color: color-mix(
      in srgb,
      var(--el-color-primary) 45%,
      var(--el-border-color)
    );
    color: var(--el-color-primary);
  }
}

// ============ Stat Grid ============
.stat-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  // 间距加大，更舒展
  gap: 18px;
}

.stat-card {
  background: var(--el-bg-color);
  position: relative;
  // 扁平风格下靠 1px 边框与背景色分层，不再使用阴影与上浮
  border: 1px solid var(--el-border-color-lighter);
  // 统计卡片是容器类表面 →「卡片」角色令牌（issue #144 / v3.3.2 起与 global.scss 的 .dd-stat-card 同档）。
  // 走角色令牌而不是 surface 档：桌面 --dd-radius-card 恒等于 surface（10px，零变化），
  // ≤768 被重指到 --dd-radius-surface-mobile 自动抬到 20px，直角模式下两边都是 0。
  border-radius: var(--dd-radius-card);
  padding: 16px 18px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  cursor: pointer;
  transition: border-color var(--dd-motion-fast) var(--dd-ease-standard);

  &:hover {
    border-color: color-mix(in srgb, var(--el-color-primary) 20%, var(--el-border-color));
  }
}

.stat-card--cinematic {
  animation: dd-card-rise-in 360ms var(--dd-ease-emphasized) both;
}

.stat-card--cinematic:nth-child(1) { animation-delay: 30ms; }
.stat-card--cinematic:nth-child(2) { animation-delay: 70ms; }
.stat-card--cinematic:nth-child(3) { animation-delay: 110ms; }
.stat-card--cinematic:nth-child(4) { animation-delay: 150ms; }

.stat-card__main {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.stat-card__label {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  font-weight: 500;
}

.stat-card__value {
  font-size: 26px;
  font-weight: 700;
  line-height: 1.15;
  font-family:
    "Inter",
    var(--dd-font-ui),
    -apple-system,
    "PingFang SC",
    "Microsoft YaHei",
    sans-serif;
  font-variant-numeric: tabular-nums;
  -webkit-font-smoothing: antialiased;
  letter-spacing: -0.01em;
}

.stat-card__delta {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  flex-wrap: wrap;
}

.stat-card__delta-prefix {
  color: var(--el-text-color-placeholder);
}

.stat-card__delta-value {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  font-weight: 600;
  padding: 1px 6px;
  // 涨跌幅是卡片里的小标签，归控件类 → control 档（与 global.scss 的 .dd-stat-card__delta 同档）
  border-radius: var(--dd-radius-control);

  &.is-up {
    color: #10b981;
    background: rgba(16, 185, 129, 0.1);
  }

  &.is-down {
    color: #ef4444;
    background: rgba(239, 68, 68, 0.1);
  }

  &.is-flat {
    color: var(--el-text-color-secondary);
    background: var(--el-fill-color);
    padding: 1px 8px;
  }
}

.stat-card__icon {
  width: 44px;
  height: 44px;
  // 44×44 的图标色底属控件类表面 → control 档（不做成正圆，避免与头像混淆）
  border-radius: var(--dd-radius-control);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.icon-spin {
  animation: spin 2.4s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

// ============ 焦点行 Grid（趋势图大 + 执行统计占比条）============
.focus-grid {
  display: grid;
  grid-template-columns: 1.8fr 1fr;
  gap: 18px;
  align-items: stretch;
}

// ============ 底部行 Grid（任务表 + 系统资源）============
.bottom-grid {
  display: grid;
  grid-template-columns: 2fr 1fr;
  gap: 18px;
}

.panel {
  background: var(--el-bg-color);
  animation: dd-panel-rise-in 420ms var(--dd-ease-emphasized) both;
  // 面板与页面底色的分隔完全由 1px 边框承担，不再叠加阴影
  border: 1px solid var(--el-border-color-lighter);
  // 面板外壳是容器类表面 →「卡片」角色令牌（issue #144 / v3.3.2，与 .stat-card 同档：
  // 桌面仍是 10px，≤768 自动抬到 20px，直角模式为 0）；下面的 overflow:hidden 会把贴边内嵌的
  // .panel-header / 表格 / 移动端列表自动裁掉四角，所以那些子块刻意不写圆角
  border-radius: var(--dd-radius-card);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.panel-header {
  display: flex;
  position: relative;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 18px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.panel-header__title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  flex-wrap: wrap;
}

.panel-header__icon {
  flex-shrink: 0;
}

.panel-header__hint {
  font-size: 11px;
  font-weight: 400;
  color: var(--el-text-color-placeholder);
}

.panel-header__actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.text-link {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  border: none;
  background: transparent;
  cursor: pointer;
  color: var(--el-color-primary);
  font-size: 12px;
  padding: 4px 6px;
  // 文字链接按钮属控件类表面 → control 档
  border-radius: var(--dd-radius-control);
  transition: background 0.15s, border-color 0.18s ease;

  &:hover {
    background: var(--el-color-primary-light-9);
  }
}

.seg-btn-group {
  display: inline-flex;
  background: var(--el-fill-color-light);
  // 分段控件的灰底槽 → control 档（与槽内的 .seg-btn 同档，两者一致才不会露出内外错位的角）
  border-radius: var(--dd-radius-control);
  padding: 2px;
  gap: 2px;

  &--mini {
    margin-left: 8px;
  }
}

.seg-btn {
  border: none;
  background: transparent;
  padding: 4px 10px;
  font-size: 12px;
  // 分段项属控件类表面 → control 档
  border-radius: var(--dd-radius-control);
  cursor: pointer;
  color: var(--el-text-color-secondary);
  transition: background-color 0.18s, color 0.18s;

  &:hover {
    color: var(--el-text-color-primary);
  }

  // 选中态只靠底色与字色区分，不再用投影抬起
  &.is-active {
    background: var(--el-bg-color);
    color: var(--el-color-primary);
    font-weight: 600;
  }
}

// ============ Trend Chart ============
.trend-chart-shell {
  flex: 1;
  // 给趋势图足够高度，焦点行更舒展
  min-height: 320px;
  padding: 10px 12px 14px;
}

.trend-chart-placeholder {
  height: 300px;
  // 图表骨架屏是面板内的独立区块 → surface 档。
  // 面板内嵌块，刻意低父级一档：.panel 已抬到 card 档（移动端 20px），这里不跟（issue #144 / v3.3.2）。
  border-radius: var(--dd-radius-surface);
  padding: 18px;
  // 骨架屏用纯色底，不再用渐变
  background: var(--el-fill-color-lighter);
  display: flex;
  flex-direction: column;
  justify-content: flex-end;
  gap: 14px;
}

.placeholder-bar {
  height: 8px;
  // 骨架屏占位条属小型块 → control 档（8px 高时半径会被自动夹到 4px，呈圆头条）
  border-radius: var(--dd-radius-control);
  background: rgba(59, 130, 246, 0.15);
  animation: placeholderPulse 1.6s ease-in-out infinite;
}

.placeholder-bar--short {
  width: 65%;
  animation-delay: 0.12s;
}

.placeholder-legend {
  display: flex;
  gap: 10px;
}

.placeholder-legend span {
  width: 48px;
  height: 6px;
  // 骨架屏图例占位块，与 .placeholder-bar 同类 → control 档
  border-radius: var(--dd-radius-control);
  background: rgba(140, 140, 140, 0.12);
}

@keyframes placeholderPulse {
  0%,
  100% {
    opacity: 0.5;
  }
  50% {
    opacity: 1;
  }
}

// ============ Resource ============
.resource-list {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 16px 18px;
}

.resource-row {
  display: flex;
  // 资源列表的每一行是独立内容块（hover 换底色），归容器类表面 → surface 档。
  // 面板内嵌块，刻意低父级一档：.panel 已抬到 card 档（移动端 20px），这里不跟（issue #144 / v3.3.2）。
  // 父级 .resource-list 有 16px 18px 内边距，行不贴边，可以安全吃圆角
  border-radius: var(--dd-radius-surface);
  padding: 8px 10px;
  transition: background-color 0.18s ease;
  align-items: flex-start;
  gap: 12px;
}

.resource-row__icon {
  width: 36px;
  height: 36px;
  // 36×36 的图标色底属控件类表面 → control 档
  border-radius: var(--dd-radius-control);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.resource-row__body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.resource-row__top {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 13px;
  flex-wrap: wrap;
}

.resource-row__label {
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.resource-row__detail {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  flex: 1;
  display: inline-flex;
  align-items: center;
  gap: 2px;
}

.resource-row__pct {
  font-size: 13px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  font-family: "Inter", var(--dd-font-ui), sans-serif;
  font-variant-numeric: tabular-nums;
  -webkit-font-smoothing: antialiased;
}

.resource-bar {
  height: 6px;
  // 进度条轨道是天然胶囊 → pill 档
  border-radius: var(--dd-radius-pill);
  background: var(--el-fill-color);
  overflow: hidden;
}

// 进度条为纯色实心，去掉了原来的流光高亮层
.resource-bar__fill {
  height: 100%;
  // 与轨道同档（pill）：轨道虽有 overflow:hidden 兜住两端，但填充未满时右侧前沿在轨道中间，
  // 需要自己带圆头才和 el-progress 的 __inner 观感一致
  border-radius: var(--dd-radius-pill);
  transition: width 0.6s cubic-bezier(0.25, 0.46, 0.45, 0.94);
}

.uptime-detail {
  font-family: "Inter", var(--dd-font-ui), sans-serif;
  font-variant-numeric: tabular-nums;
  font-weight: 700;
  color: #f59e0b;
}

.uptime-track {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 6px;
  color: var(--el-text-color-placeholder);
  font-size: 11.5px;
}

// 白名单：形状承载语义 —— 8×8 的状态灯（面板持续运行中），与 global.scss 的 .pulse-dot、
// api-docs 的 .status-dot 同类，方化后与旁边文字糊成一小块色斑，认不出是状态灯。
// 两种 shape 模式下都固定圆形，不吃 --dd-radius-* 刻度。
.uptime-track__dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #f59e0b;
  flex-shrink: 0;
}

.uptime-track__line {
  height: 6px;
  flex: 1;
  // 状态灯后面的运行轨道线，与进度条同类 → pill 档
  border-radius: var(--dd-radius-pill);
  background: rgba(245, 158, 11, 0.28);
  overflow: hidden;
}

.uptime-track__text {
  flex-shrink: 0;
  white-space: nowrap;
}

// ============ Recent Logs Table ============
.log-table {
  width: 100%;
  table-layout: fixed;
  border-collapse: separate;
  border-spacing: 0;
  font-size: 13px;

  &__col--name {
    width: 19%;
  }
  &__col--status {
    width: 9%;
  }
  &__col--time {
    width: 18%;
  }
  &__col--duration {
    width: 10%;
  }
  &__col--trigger {
    width: 14%;
  }
  &__col--env {
    width: 16%;
  }
  &__col--actions {
    width: 14%;
  }

  thead {
    background: var(--el-fill-color-light);
  }

  th {
    text-align: left;
    font-weight: 600;
    font-size: 12px;
    color: var(--el-text-color-secondary);
    padding: 12px 16px;
    border-bottom: 1px solid var(--el-border-color-lighter);
    white-space: nowrap;
    line-height: 1.25;
  }

  tbody tr {
    transition: background 0.15s;

    // 行悬浮只换底色，不做位移与投影
    &:hover {
      background: color-mix(in srgb, var(--el-color-primary-light-9) 76%, white);
    }
  }
}

.log-table td {
  padding: 12px 16px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  color: var(--el-text-color-primary);
  vertical-align: middle;
  line-height: 1.35;
}

.log-table tbody tr:last-child td {
  border-bottom: none;
}

.log-table .col-center {
  text-align: center;
}

.empty-cell {
  text-align: center;
  padding: 28px;
  color: var(--el-text-color-placeholder);
  font-size: 13px;
}

.empty-hint {
  text-align: center;
  padding: 28px;
  color: var(--el-text-color-placeholder);
  font-size: 13px;
}

.log-cell-name {
  display: block;
  font-weight: 500;
  color: var(--el-text-color-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.log-cell-time,
.log-cell-duration {
  display: inline-block;
  min-width: 0;
  font-size: 12.5px;
  color: var(--el-text-color-secondary);
  font-family: "Inter", var(--dd-font-ui), sans-serif;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.log-cell-trigger {
  display: inline-block;
  max-width: 100%;
  font-size: 12.5px;
  color: var(--el-text-color-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.log-status-chip {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 44px;
  padding: 2px 10px;
  // 执行状态 chip 是天然胶囊 → pill 档（与 global.scss 的 .dd-status-chip 同档）
  border-radius: var(--dd-radius-pill);
  font-size: 11.5px;
  font-weight: 600;
  line-height: 1.4;

  &.is-success {
    background: rgba(16, 185, 129, 0.12);
    color: #10b981;
  }

  &.is-danger {
    background: rgba(239, 68, 68, 0.12);
    color: #ef4444;
  }

  &.is-primary {
    background: rgba(59, 130, 246, 0.12);
    color: #3b82f6;
  }

  &.is-warning {
    background: rgba(245, 158, 11, 0.12);
    color: #f59e0b;
  }
}

.env-chip {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  max-width: 100%;
  padding: 2px 8px;
  // 环境标记是标签而不是状态灯，归控件类 → control 档（与 global.scss 的 .dd-env-chip 同档）
  border-radius: var(--dd-radius-control);
  font-size: 11.5px;
  font-weight: 500;
  font-family: "Inter", var(--dd-font-ui), sans-serif;
  letter-spacing: 0.02em;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.log-cell-actions {
  display: inline-flex;
  // 操作按钮组的灰底槽 → control 档（与槽内的 .icon-btn 同档，两者一致才不会露出内外错位的角）
  border-radius: var(--dd-radius-control);
  padding: 2px;
  background: color-mix(in srgb, var(--el-fill-color-light) 82%, transparent);
  align-items: center;
  justify-content: center;
  gap: 2px;
}

.icon-btn {
  width: 26px;
  height: 26px;
  // 图标按钮属控件类表面 → control 档（与 global.scss 的 .dd-icon-btn 同档）
  border-radius: var(--dd-radius-control);
  border: none;
  background: transparent;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  color: var(--el-text-color-placeholder);
  transition: background-color 0.15s ease, color 0.15s ease;

  &:hover {
    background: var(--el-fill-color);
    color: var(--el-color-primary);
  }
}

.log-mobile-list {
  padding: 8px 14px 14px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.log-mobile-card {
  border: 1px solid var(--el-border-color-lighter);
  // 面板内嵌的小卡片，刻意低父级一档 → surface 档（issue #144 / v3.3.2：.panel 与 global.scss 的
  // .dd-mobile-card 都已改吃 card 档、移动端 20px，这里不跟，嵌套圆角本就该逐层递减）；
  // 父级 .log-mobile-list 有内边距，卡片不贴边，可以安全吃圆角
  border-radius: var(--dd-radius-surface);
  padding: 10px 12px;
  background: var(--el-fill-color-light);
}

.log-mobile-card__head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;
}

.log-mobile-card__name {
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.log-mobile-card__meta {
  display: flex;
  gap: 12px;
  font-size: 11.5px;
  color: var(--el-text-color-secondary);
}

// ============ Task Stats（总数 + 横向堆叠占比条 + 图例）============
.task-stats-body {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 14px 18px;
}

.task-stats__total {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.task-stats__total-label {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
}

.task-stats__total-value {
  font-size: 22px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  font-family: "Inter", var(--dd-font-ui), sans-serif;
  font-variant-numeric: tabular-nums;
  -webkit-font-smoothing: antialiased;
  letter-spacing: -0.01em;
}

.task-stats__bar {
  display: flex;
  width: 100%;
  height: 8px;
  // 横向堆叠占比条与进度条同类，是天然胶囊 → pill 档；
  // 下面的 overflow:hidden 会把内部 .task-stats__seg 各分段裁进这个圆头轨道，
  // 所以分段本身刻意不写圆角（贴边内嵌，写了反而露角）
  border-radius: var(--dd-radius-pill);
  // 总执行数为 0 时四段宽度都是 0，这里的底色就是空态表现
  background: var(--el-fill-color);
  overflow: hidden;
}

.task-stats__seg {
  height: 100%;
  min-width: 0;
  transition: width 0.4s var(--dd-ease-standard);

  &.is-success {
    background: #10b981;
  }

  &.is-failed {
    background: #ef4444;
  }

  &.is-running {
    background: #3b82f6;
  }

  &.is-aborted {
    background: #f59e0b;
  }
}

.task-stats__legend {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.task-stats__legend-item {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12.5px;
}

.task-stats__swatch {
  width: 10px;
  height: 10px;
  // 🔴 固定 2px，不吃令牌（v3.1.0 新立的「≤12px 小色标」判据）。
  //
  // 图例色块只做「颜色对应」用（对应上面那条堆叠占比条的四个分段），不是表达运行状态的
  // 状态灯，所以不进圆形白名单——这一层判断没变。但原来写「吃 control 档」是错的：
  // 10×10 的盒子上，control 的 6px 会触发 CSS 圆角等比收缩（6+6=12 > 10 ⇒ 全部夹到 5px），
  // 结果正好是一个【正圆】，跟上面 .uptime-track__dot 那类状态灯长得一模一样，
  // 「不进白名单以便区分形状」的意图在圆角模式下自动失效。
  // 写死一个小于半边长的值才稳得住方形轮廓。
  border-radius: 2px;
  flex-shrink: 0;

  &.is-success {
    background: #10b981;
  }

  &.is-failed {
    background: #ef4444;
  }

  &.is-running {
    background: #3b82f6;
  }

  &.is-aborted {
    background: #f59e0b;
  }
}

.task-stats__legend-label {
  flex: 1;
  color: var(--el-text-color-regular);
}

.task-stats__legend-value {
  font-weight: 700;
  color: var(--el-text-color-primary);
  font-family: "Inter", var(--dd-font-ui), sans-serif;
  font-variant-numeric: tabular-nums;
}

.task-stats__legend-pct {
  font-size: 11.5px;
  color: var(--el-text-color-placeholder);
}

.task-stats-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 18px;
  border-top: 1px solid var(--el-border-color-lighter);
  font-size: 12.5px;
}

.task-stats-footer__label {
  color: var(--el-text-color-secondary);
}

.task-stats-footer__value {
  font-weight: 700;
  color: var(--el-color-primary);
  font-family: "Inter", var(--dd-font-ui), sans-serif;
  font-variant-numeric: tabular-nums;
  font-size: 13.5px;
}

// ============ Responsive ============
@media (max-width: 1280px) {
  // 窄屏：焦点/底部行各自收敛为单列
  .focus-grid {
    grid-template-columns: 1.5fr 1fr;
  }
  .bottom-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 960px) {
  .stat-grid {
    grid-template-columns: repeat(2, 1fr);
  }
  .focus-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 768px) {
  // issue #144 / v3.3.2：卡片（.stat-card / .panel）在移动端抬到 card 档 20px 后，
  // 按钮跟着从 control 档 6px 抬到 surface 档（圆角模式 10px），免得圆角落差太大。
  // 不写死 10px：直角模式下 --dd-radius-surface 是 0，写死会把方形面板破掉。
  // --dd-radius-surface 在 ≤768 没有被重指，手机上仍解析为 10px，所以这里直接吃它即可。
  // 本页没有一个 el-button，全是原生 button，global.scss 那条移动端 `html .el-button` 规则够不着它们。
  // 槽与槽内项必须同值（.seg-btn-group/.seg-btn、.log-cell-actions/.icon-btn），否则会露出内外错位的角。
  .dash-pill,
  .text-link,
  .seg-btn-group,
  .seg-btn,
  .log-cell-actions,
  .icon-btn {
    border-radius: var(--dd-radius-surface);
  }

  // 窄屏：问候条竖排，actions 占满宽度、按钮换行不溢出
  .dash-welcome {
    flex-direction: column;
    align-items: flex-start;
  }
  .dash-welcome__title {
    font-size: 18px;
  }
  .dash-welcome__actions {
    width: 100%;
  }

  .stat-grid {
    gap: 12px;
  }
  .stat-card {
    padding: 14px;
  }
  .stat-card__value {
    font-size: 22px;
  }
  .stat-card__icon {
    width: 38px;
    height: 38px;
  }

  .panel-header {
    padding: 12px 12px;
    flex-wrap: wrap;
  }
  .panel-header__title {
    font-size: 13px;
    flex-wrap: wrap;
  }
  .seg-btn-group--mini {
    margin-left: 0;
    margin-top: 4px;
  }

  .resource-list {
    padding: 12px;
  }
  .resource-row__top {
    gap: 6px;
  }

  .task-stats-body {
    gap: 16px;
    padding: 16px;
  }
}


@keyframes dd-card-rise-in {
  from {
    opacity: 0;
    transform: translate3d(0, 18px, 0) scale3d(0.985, 0.985, 1);
  }
  to {
    opacity: 1;
    transform: translate3d(0, 0, 0) scale3d(1, 1, 1);
  }
}

@keyframes dd-panel-rise-in {
  from {
    opacity: 0;
    transform: translate3d(0, 16px, 0);
  }
  to {
    opacity: 1;
    transform: translate3d(0, 0, 0);
  }
}

// 页面自带的「减少动效」规则统一走这个包装（C8 动效偏好）：
// - 跟随系统：媒体查询里带 :root:not(.dd-motion-force)，个人设置选「始终开启」时不生效；
// - 个人设置选「减少动效」：html.dd-motion-off 下不看系统同样生效。
// 与 global.scss 末尾「减少动效」段同一口径；前缀包在 :where() 里，特异性与改动前的裸选择器相同，层叠结果不变。
@mixin dd-page-reduced-motion {
  @media (prefers-reduced-motion: reduce) {
    :where(:root:not(.dd-motion-force)) {
      @content;
    }
  }

  :where(html.dd-motion-off) {
    @content;
  }
}

@include dd-page-reduced-motion {
  .stat-card--cinematic,
  .panel {
    animation: none;
  }
}

</style>
