<script setup lang="ts">
import { Document, Setting, Upload } from '@element-plus/icons-vue'
import type { SettingsConfigForm } from '../types'

const timezoneOptions = [
  { value: 'Asia/Shanghai', label: '中国时间 Asia/Shanghai' },
  { value: 'UTC', label: 'UTC 标准时间' },
  { value: 'Asia/Tokyo', label: '日本时间 Asia/Tokyo' },
  { value: 'Asia/Hong_Kong', label: '香港时间 Asia/Hong_Kong' },
  { value: 'Asia/Singapore', label: '新加坡时间 Asia/Singapore' },
  { value: 'America/New_York', label: '纽约时间 America/New_York' },
  { value: 'Europe/London', label: '伦敦时间 Europe/London' }
]

defineProps<{
  configsLoading: boolean
  configsSaving: boolean
  form: SettingsConfigForm
  onSave: () => void
  onIconUpload: (file: File) => boolean
  onLogBackgroundUpload: (file: File) => boolean
  onAppearancePreview: () => void
}>()
</script>

<template>
  <el-card shadow="never" v-loading="configsLoading">
    <template #header>
      <div class="card-header">
        <span class="card-title"><el-icon><Setting /></el-icon> 面板外观</span>
        <el-button type="primary" :loading="configsSaving" @click="onSave">
          <el-icon><Document /></el-icon>保存配置
        </el-button>
      </div>
    </template>

    <div class="config-section">
      <h4 class="section-title">面板设置</h4>
      <div class="form-field">
        <label>面板标题</label>
        <el-input v-model="form.panel_title" placeholder="呆呆面板" />
        <span class="form-hint">自定义面板的站点标题，留空使用默认值"呆呆面板"</span>
      </div>
      <div class="form-field">
        <label>面板时区</label>
        <el-select
          v-model="form.timezone"
          class="timezone-select"
          filterable
          allow-create
          default-first-option
          placeholder="Asia/Shanghai"
        >
          <el-option
            v-for="item in timezoneOptions"
            :key="item.value"
            :label="item.label"
            :value="item.value"
          />
        </el-select>
        <span class="form-hint">影响面板日志、任务日期判断和脚本运行时 TZ；Linux 二进制包建议保持 Asia/Shanghai</span>
      </div>
      <div class="form-field">
        <label>面板图标 (SVG)</label>
        <div class="icon-upload-row">
          <el-upload
            :show-file-list="false"
            :before-upload="onIconUpload"
            accept=".svg"
          >
            <el-button size="small"><el-icon><Upload /></el-icon>上传 SVG 图标</el-button>
          </el-upload>
          <div v-if="form.panel_icon" class="icon-preview">
            <img :src="form.panel_icon" alt="icon" class="icon-preview__image" />
            <el-button size="small" text type="danger" @click="form.panel_icon = ''">移除</el-button>
          </div>
        </div>
        <span class="form-hint">上传 SVG 格式图标自定义面板图标，留空使用默认图标</span>
      </div>
      <div class="form-field">
        <label>编辑器背景颜色</label>
        <div class="log-bg-controls">
          <el-color-picker v-model="form.editor_background_color" @change="onAppearancePreview" />
          <el-input v-model="form.editor_background_color" placeholder="留空使用默认编辑器背景" @change="onAppearancePreview" />
        </div>
        <span class="form-hint">统一应用到脚本只读预览和在线编辑器，留空保持默认深色背景</span>
      </div>
      <div class="form-field">
        <label>日志背景颜色</label>
        <div class="log-bg-controls">
          <el-color-picker v-model="form.log_background_color" show-alpha @change="onAppearancePreview" />
          <el-input v-model="form.log_background_color" placeholder="留空跟随当前主题" @change="onAppearancePreview" />
        </div>
        <span class="form-hint">统一应用到任务日志和执行日志查看器，留空时浅色模式为浅底深字，深色模式为深底浅字</span>
      </div>
      <div class="form-field">
        <label>日志背景图片</label>
        <div class="log-bg-upload">
          <el-upload
            :show-file-list="false"
            :before-upload="onLogBackgroundUpload"
            accept="image/*"
          >
            <el-button size="small"><el-icon><Upload /></el-icon>上传背景图</el-button>
          </el-upload>
          <el-button
            v-if="form.log_background_image"
            size="small"
            text
            type="danger"
            @click="form.log_background_image = ''; onAppearancePreview()"
          >
            移除背景图
          </el-button>
        </div>
        <div
          class="log-bg-preview dd-log-surface"
          :style="{
            backgroundColor: form.log_background_color || undefined,
            backgroundImage: form.log_background_image
              ? `url('${form.log_background_image}'), radial-gradient(circle at top right, color-mix(in srgb, var(--dd-log-text-color) 10%, transparent), transparent 24%), linear-gradient(155deg, color-mix(in srgb, var(--dd-log-bg-color) 96%, white), color-mix(in srgb, var(--dd-log-bg-color) 88%, var(--dd-log-text-color) 8%))`
              : undefined
          }"
        >
          <div class="log-bg-preview__content">任务输出预览：日志背景将应用到所有日志查看器</div>
        </div>
        <span class="form-hint">支持常见图片格式，单张最大 10MB；较大的图片会自动压缩后保存</span>
      </div>
    </div>
  </el-card>
</template>

<style scoped lang="scss">
@use './config-card-shared.scss' as *;

.log-bg-controls {
  display: flex;
  align-items: center;
  gap: 12px;
}

.icon-upload-row {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.icon-preview {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.icon-preview__image {
  width: 32px;
  height: 32px;
}

.timezone-select {
  width: 100%;
  max-width: 360px;
}

.log-bg-upload {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}

.log-bg-preview {
  padding: 18px;
  min-height: 92px;
  overflow: hidden;
}

.log-bg-preview__content {
  font-family: var(--dd-font-mono);
  font-size: 13px;
  line-height: 1.7;
  white-space: pre-wrap;
}

@media (max-width: 768px) {
  .log-bg-controls {
    flex-direction: column;
    align-items: stretch;
  }

  .log-bg-upload {
    flex-wrap: wrap;
  }
}
</style>
