<script setup lang="ts">
import { Bell, Document } from '@element-plus/icons-vue'
import type { SettingsConfigForm } from '../types'

defineProps<{
  configsLoading: boolean
  configsSaving: boolean
  form: SettingsConfigForm
  onSave: () => void
}>()
</script>

<template>
  <el-card shadow="never" v-loading="configsLoading">
    <template #header>
      <div class="card-header">
        <span class="card-title"><el-icon><Bell /></el-icon> 告警通知</span>
        <el-button type="primary" :loading="configsSaving" @click="onSave">
          <el-icon><Document /></el-icon>保存配置
        </el-button>
      </div>
    </template>

    <div class="config-section">
      <h4 class="section-title">资源告警</h4>
      <el-row :gutter="16">
        <el-col :xs="24" :md="8">
          <div class="form-field">
            <label>CPU 阈值 (%)</label>
            <el-input v-model.number="form.cpu_warn" />
          </div>
        </el-col>
        <el-col :xs="24" :md="8">
          <div class="form-field">
            <label>内存阈值 (%)</label>
            <el-input v-model.number="form.memory_warn" />
          </div>
        </el-col>
        <el-col :xs="24" :md="8">
          <div class="form-field">
            <label>磁盘阈值 (%)</label>
            <el-input v-model.number="form.disk_warn" />
          </div>
        </el-col>
      </el-row>
      <div class="switch-row">
        <div class="switch-item">
          <span class="switch-label">资源超限发送通知</span>
          <el-switch v-model="form.notify_on_resource_warn" inline-prompt active-text="开" inactive-text="关" />
        </div>
      </div>
      <span class="form-hint">开启后，资源使用率超过上方阈值时将向所有已启用的通知渠道发送通知</span>
    </div>

    <div class="config-section">
      <h4 class="section-title">通知标识</h4>
      <div class="form-field">
        <label>通知面板名称</label>
        <el-input v-model="form.notify_panel_label" placeholder="留空不附带，例如：家里NAS" maxlength="32" show-word-limit clearable />
      </div>
      <span class="form-hint">填写后所有通知标题统一加上「【面板名】」前缀，方便多个面板共用同一通知渠道时区分来源；留空则不附带</span>
    </div>

    <div class="config-section">
      <h4 class="section-title">登录通知</h4>
      <div class="switch-row">
        <div class="switch-item">
          <span class="switch-label">登录成功发送通知</span>
          <el-switch v-model="form.notify_on_login" inline-prompt active-text="开" inactive-text="关" />
        </div>
      </div>
      <span class="form-hint">开启后，每次登录成功将向所有已启用的通知渠道发送通知</span>
    </div>
  </el-card>
</template>

<style scoped lang="scss">
@use './config-card-shared.scss' as *;
</style>
