<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
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
let resizeHandler: (() => void) | null = null

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
      shadow: 'rgba(0,0,0,0.25)',
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
    shadow: 'rgba(0,0,0,0.08)',
  }
})

function renderChart() {
  if (!chartRef.value) return
  if (!chart) {
    chart = echarts.init(chartRef.value)
  }

  const c = colors.value

  chart.setOption({
    tooltip: {
      trigger: 'axis',
      backgroundColor: c.tooltipBg,
      borderColor: c.tooltipBorder,
      borderWidth: 1,
      textStyle: { color: c.tooltipText, fontSize: 12 },
      extraCssText: `border-radius: 8px; box-shadow: 0 2px 8px ${c.shadow};`,
    },
    legend: {
      data: ['执行总数', '成功', '失败', '终止'],
      icon: 'circle',
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
        // 主线更顺、symbol/线宽统一；面积渐变更明显，强调"执行总数"主线
        smooth: 0.5,
        showSymbol: false,
        symbol: 'circle',
        symbolSize: 6,
        lineStyle: { width: 3, color: '#409EFF' },
        itemStyle: { color: '#409EFF', borderWidth: 2, borderColor: c.pointBorder },
        // 不用 focus:'series'：带渐变面积时它会让 hover 每帧重绘整图，导致掉帧
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
        symbol: 'circle',
        symbolSize: 6,
        lineStyle: { width: 2.5, color: '#67C23A' },
        itemStyle: { color: '#67C23A', borderWidth: 2, borderColor: c.pointBorder },
        // 不用 focus:'series'：带渐变面积时它会让 hover 每帧重绘整图，导致掉帧
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
        symbol: 'circle',
        symbolSize: 6,
        lineStyle: { width: 2.5, color: '#F56C6C' },
        itemStyle: { color: '#F56C6C', borderWidth: 2, borderColor: c.pointBorder },
        // 不用 focus:'series'：带渐变面积时它会让 hover 每帧重绘整图，导致掉帧
      },
      {
        name: '终止',
        type: 'line',
        // Aborted 是用户主动终止的独立状态，不再混进成功或失败。
        data: props.stats.map((item) => item.aborted || 0),
        smooth: 0.5,
        showSymbol: false,
        symbol: 'circle',
        symbolSize: 6,
        lineStyle: { width: 2.5, color: '#E6A23C' },
        itemStyle: { color: '#E6A23C', borderWidth: 2, borderColor: c.pointBorder },
        // 不用 focus:'series'：保持和其它线一致，避免 hover 时整图频繁重绘。
      },
    ],
  })
}

watch(() => props.stats, renderChart, { deep: true })
watch(() => theme.isDark, renderChart)

onMounted(() => {
  renderChart()
  resizeHandler = () => {
    chart?.resize()
  }
  window.addEventListener('resize', resizeHandler)
})

onBeforeUnmount(() => {
  if (resizeHandler) {
    window.removeEventListener('resize', resizeHandler)
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
