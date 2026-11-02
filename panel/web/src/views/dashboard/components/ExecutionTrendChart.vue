<script setup lang="ts">
import { computed, onActivated, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as echarts from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import { useThemeStore } from '@/stores/theme'

const props = defineProps<{
  stats: Array<{
    date?: string
    success?: number
    failed?: number
    aborted?: number
  }>
}>()

echarts.use([LineChart, GridComponent, TooltipComponent, LegendComponent, CanvasRenderer])

const chartRef = ref<HTMLElement>()
let chart: echarts.ECharts | null = null

// 尺寸跟随改用 ResizeObserver 盯容器本身，不再听 window 的 resize：
//   1. 侧栏收起 / 展开是 .layout-aside 的宽度过渡，不触发 window resize，原来图表宽度不跟着变；
//   2. 仪表板被 keep-alive 缓存时 DOM 被移进脱离文档的容器，这时窗口一变（移动端地址栏伸缩就会），
//      zrender 量到 0×0、把画布设成 0 尺寸；回到仪表板后没人再 resize，图表一直空白。
let resizeObserver: ResizeObserver | null = null
let resizeFrame = 0

const theme = useThemeStore()

const colors = computed(() => {
  if (theme.isDark) {
    return {
      tooltipBg: '#1e293b',
      tooltipBorder: '#334155',
      tooltipText: '#e2e8f0',
      axisLine: '#334155',
      splitLine: '#1e293b',
      labelColor: '#94a3b8',
      pointBorder: '#1e293b',
    }
  }
  return {
    tooltipBg: '#fff',
    tooltipBorder: '#f0f0f0',
    tooltipText: '#333',
    axisLine: '#f0f0f0',
    splitLine: '#f5f5f5',
    labelColor: '#8c8c8c',
    pointBorder: '#fff',
  }
})

/**
 * 把容器尺寸同步给图表。
 *
 * rAF 合并：侧栏宽度过渡期间 ResizeObserver 会连续回调，一帧只 resize 一次。
 * 两种情况直接跳过：
 * - 量出来是 0（被 keep-alive 摘出文档、或祖先 display:none）：这时 resize 会把画布缩成 0×0；
 * - 尺寸与图表当前尺寸相同：observe() 之后 ResizeObserver 会立刻回调一次，这时首次加载的
 *   入场动画还在跑，resize 会以 0 时长重绘、把那段动画直接跳到终点。
 */
function scheduleResize() {
  if (resizeFrame) return
  resizeFrame = requestAnimationFrame(() => {
    resizeFrame = 0
    const el = chartRef.value
    if (!chart || !el) return
    const width = el.clientWidth
    const height = el.clientHeight
    if (width === 0 || height === 0) return
    if (width === chart.getWidth() && height === chart.getHeight()) return
    chart.resize()
  })
}

function renderChart() {
  if (!chartRef.value) return
  if (!chart) {
    chart = echarts.init(chartRef.value)
  }

  const c = colors.value

  chart.setOption({
    tooltip: {
      trigger: 'axis',
      // hover 掉帧的根因是 ECharts 默认的三层交互动画叠在一起（#132），这里逐层关掉：
      //   ① transitionDuration 默认 0.4s：提示框位置走 CSS transform 过渡，开了过渡后位置更新
      //      还会被 50ms 节流 —— 表现为拖影、跳格。设成 0 后提示框硬跟随，节流也一并取消。
      //   ② axisPointer.animation 默认按 auto 处理：类目轴带宽 >15px 时指示线移动带 200ms 动画。
      //   ③ axisPointer.triggerEmphasis 默认 true：每跨一个类目就对 4 条系列 highlight / downplay；
      //      折线不走 hover 层，于是每一帧都整张 canvas 重绘（含两块渐变面积）。
      // 首次加载的入场动画是 series 自己的 animationDuration，不受这里影响。
      transitionDuration: 0,
      axisPointer: { type: 'line', animation: false, triggerEmphasis: false },
      backgroundColor: c.tooltipBg,
      borderColor: c.tooltipBorder,
      borderWidth: 1,
      textStyle: { color: c.tooltipText, fontSize: 12 },
      // 提示框与全局风格一致：无投影，仅靠 1px 边框与背景色区分层次。
      // 圆角吃 surface 档（tooltip 是浮层容器）；ECharts 把 tooltip 挂在 <body> 下，
      // CSS 变量沿 :root 继承下来，所以切换外观开关后无需重新 setOption 就会自动重算。
      extraCssText: 'border-radius: var(--dd-radius-surface); box-shadow: none;',
    },
    legend: {
      data: ['执行总数', '成功', '失败', '终止'],
      // 图例色标用方块，与页面其余色标形状统一
      icon: 'rect',
      itemWidth: 8,
      textStyle: { fontSize: 12, color: c.labelColor },
      top: 0,
    },
    // 这里改用 outerBounds 语义，避免新版 ECharts 对 containLabel 给出兼容性警告。
    grid: {
      left: '3%',
      right: '4%',
      bottom: '3%',
      top: 40,
      outerBoundsMode: 'same',
      outerBoundsContain: 'axisLabel',
    },
    xAxis: {
      type: 'category',
      data: props.stats.map((item) => item.date),
      axisLine: { lineStyle: { color: c.axisLine } },
      axisTick: { show: false },
      axisLabel: { color: c.labelColor, fontSize: 11 },
    },
    yAxis: {
      type: 'value',
      minInterval: 1,
      axisLine: { lineStyle: { color: c.axisLine } },
      splitLine: { lineStyle: { color: c.splitLine } },
      axisLabel: { color: c.labelColor, fontSize: 11 },
    },
    series: [
      {
        name: '执行总数',
        type: 'line',
        data: props.stats.map(
          (item) => (item.success || 0) + (item.failed || 0) + (item.aborted || 0),
        ),
        // 主线更顺、线宽统一；面积渐变保留（属于数据表达），但不画圆形数据点标记
        smooth: 0.5,
        showSymbol: false,
        symbol: 'none',
        lineStyle: { width: 3, color: '#409EFF' },
        itemStyle: { color: '#409EFF', borderWidth: 2, borderColor: c.pointBorder },
        // 关掉高亮态兜底：symbol 本来就是 none，高亮不带来任何可见信息，只会触发整图重绘。
        // 也不要改用 focus:'series'：带渐变面积时它会让 hover 每帧重绘整图，同样掉帧。
        emphasis: { disabled: true },
        areaStyle: {
          color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
            { offset: 0, color: 'rgba(64,158,255,0.32)' },
            { offset: 1, color: 'rgba(64,158,255,0)' },
          ])
        },
      },
      {
        name: '成功',
        type: 'line',
        data: props.stats.map((item) => item.success || 0),
        smooth: 0.5,
        showSymbol: false,
        symbol: 'none',
        lineStyle: { width: 2.5, color: '#67C23A' },
        itemStyle: { color: '#67C23A', borderWidth: 2, borderColor: c.pointBorder },
        // 同第一条：关掉高亮态，不用 focus:'series'
        emphasis: { disabled: true },
        areaStyle: {
          color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
            { offset: 0, color: 'rgba(103,194,58,0.14)' },
            { offset: 1, color: 'rgba(103,194,58,0)' },
          ])
        },
      },
      {
        name: '失败',
        type: 'line',
        data: props.stats.map((item) => item.failed || 0),
        smooth: 0.5,
        showSymbol: false,
        symbol: 'none',
        lineStyle: { width: 2.5, color: '#F56C6C' },
        itemStyle: { color: '#F56C6C', borderWidth: 2, borderColor: c.pointBorder },
        // 同第一条：关掉高亮态，不用 focus:'series'
        emphasis: { disabled: true },
      },
      {
        name: '终止',
        type: 'line',
        // Aborted 是用户主动终止的独立状态，不再混进成功或失败。
        data: props.stats.map((item) => item.aborted || 0),
        smooth: 0.5,
        showSymbol: false,
        symbol: 'none',
        lineStyle: { width: 2.5, color: '#E6A23C' },
        itemStyle: { color: '#E6A23C', borderWidth: 2, borderColor: c.pointBorder },
        // 同第一条：关掉高亮态，不用 focus:'series'
        emphasis: { disabled: true },
      },
    ],
  })
}

watch(() => props.stats, renderChart, { deep: true })
watch(() => theme.isDark, renderChart)

onMounted(() => {
  renderChart()
  if (chartRef.value) {
    resizeObserver = new ResizeObserver(() => scheduleResize())
    resizeObserver.observe(chartRef.value)
  }
})

// keep-alive 再次激活时补一次尺寸校正。DOM 插回文档时 ResizeObserver 通常也会回调，这里是兜底；
// 尺寸没变时 scheduleResize 会直接跳过，重复触发没有代价。
onActivated(() => {
  scheduleResize()
})

onBeforeUnmount(() => {
  // keep-alive 缓存期间组件不会卸载，observer 只能在这里断开
  resizeObserver?.disconnect()
  resizeObserver = null
  if (resizeFrame) {
    cancelAnimationFrame(resizeFrame)
    resizeFrame = 0
  }
  chart?.dispose()
  chart = null
})
</script>

<template>
  <div ref="chartRef" class="trend-chart"></div>
</template>

<style scoped>
.trend-chart {
  height: 300px;
}
</style>
