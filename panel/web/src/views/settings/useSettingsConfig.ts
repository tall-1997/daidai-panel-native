import { ref } from 'vue'
import { configApi } from '@/api/system'
import { ElMessage } from 'element-plus'
import { applyPanelAppearance } from '@/utils/panelAppearance'
import type { SettingsConfigForm } from './types'

const logBackgroundImageMaxBytes = 10 * 1024 * 1024
const logBackgroundImageTargetDataUrlBytes = 1600 * 1024
const logBackgroundImageMaxDimensions = [1920, 1600, 1280, 1024, 900, 768, 640]
const logBackgroundImageQualities = [0.82, 0.72, 0.62, 0.52, 0.42, 0.34, 0.28]

function getTextBytes(value: string) {
  return new Blob([value]).size
}

function readFileAsDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result || ''))
    reader.onerror = () => reject(reader.error || new Error('read failed'))
    reader.readAsDataURL(file)
  })
}

function loadImage(dataUrl: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image()
    image.onload = () => resolve(image)
    image.onerror = () => reject(new Error('image load failed'))
    image.src = dataUrl
  })
}

function canvasToDataURL(canvas: HTMLCanvasElement, mimeType: string, quality: number) {
  const dataUrl = canvas.toDataURL(mimeType, quality)
  if (mimeType === 'image/webp' && !dataUrl.startsWith('data:image/webp')) {
    return canvas.toDataURL('image/jpeg', quality)
  }
  return dataUrl
}

async function compressLogBackgroundImage(file: File) {
  const originalDataUrl = await readFileAsDataURL(file)
  if (getTextBytes(originalDataUrl) <= logBackgroundImageTargetDataUrlBytes) {
    return { dataUrl: originalDataUrl, compressed: false }
  }

  const image = await loadImage(originalDataUrl)
  const sourceWidth = image.naturalWidth || image.width
  const sourceHeight = image.naturalHeight || image.height
  if (!sourceWidth || !sourceHeight) {
    throw new Error('invalid image')
  }

  for (const maxDimension of logBackgroundImageMaxDimensions) {
    const scale = Math.min(1, maxDimension / Math.max(sourceWidth, sourceHeight))
    const width = Math.max(1, Math.round(sourceWidth * scale))
    const height = Math.max(1, Math.round(sourceHeight * scale))
    const canvas = document.createElement('canvas')
    canvas.width = width
    canvas.height = height

    const context = canvas.getContext('2d', { alpha: true })
    if (!context) {
      throw new Error('canvas unsupported')
    }
    context.clearRect(0, 0, width, height)
    context.drawImage(image, 0, 0, width, height)

    for (const quality of logBackgroundImageQualities) {
      const dataUrl = canvasToDataURL(canvas, 'image/webp', quality)
      const size = getTextBytes(dataUrl)
      if (size <= logBackgroundImageTargetDataUrlBytes) {
        return { dataUrl, compressed: true }
      }
    }
  }

  throw new Error('compressed image too large')
}

export function useSettingsConfig() {
  // 此开关原为功能上线门控，当前已全量启用（保留常量以兼容消费方）
  const captchaFeatureImplemented = true
  const configsLoading = ref(false)
  const configsSaving = ref(false)

  const configForm = ref<SettingsConfigForm>({
    max_concurrent_tasks: 5,
    log_retention_days: 7,
    max_log_content_size: 102400000,
    random_delay: '',
    random_delay_extensions: '',
    auto_install_deps: true,
    auto_add_cron: true,
    auto_del_cron: true,
    default_cron_rule: '',
    repo_file_extensions: '',
    cpu_warn: 80,
    memory_warn: 80,
    disk_warn: 90,
    notify_on_resource_warn: false,
    notify_panel_label: '',
    notify_on_login: false,
    proxy_url: '',
    update_image_mirror: '',
    binary_update_proxy: '',
    auto_update_enabled: false,
    trusted_proxy_cidrs: '',
    captcha_enabled: false,
    captcha_id: '',
    captcha_key: '',
    captcha_fail_mode: 'open',
    panel_title: '',
    timezone: 'Asia/Shanghai',
    panel_icon: '',
    editor_background_color: '',
    log_background_color: '',
    log_background_image: '',
    backup_schedule_enabled: false,
    backup_schedule_frequency: 'daily',
    backup_schedule_time: '03:00',
    backup_schedule_weekday: '1',
    backup_schedule_monthday: 1,
    backup_schedule_name: '',
    backup_schedule_password: '',
    backup_schedule_selection: 'configs,tasks,subscriptions,env_vars,logs,scripts,dependencies,task_views',
    max_web_sessions: 1,
    max_app_sessions: 1
  })

  function readConfigString(cfgs: Record<string, any>, key: string, fallback = ''): string {
    const entry = cfgs[key]
    const raw = entry?.value ?? entry?.default_value ?? fallback
    if (raw === null || raw === undefined) return fallback
    return String(raw)
  }

  function readConfigNumber(cfgs: Record<string, any>, key: string, fallback: number): number {
    const raw = readConfigString(cfgs, key, String(fallback))
    const parsed = Number(raw)
    return Number.isFinite(parsed) ? parsed : fallback
  }

  function readConfigBool(cfgs: Record<string, any>, key: string, fallback: boolean): boolean {
    const raw = readConfigString(cfgs, key, fallback ? 'true' : 'false').trim().toLowerCase()
    if (['true', '1', 'yes', 'on'].includes(raw)) return true
    if (['false', '0', 'no', 'off'].includes(raw)) return false
    return fallback
  }

  async function loadSystemConfigs() {
    configsLoading.value = true
    try {
      const res = await configApi.list()
      const cfgs = res.data || {}

      configForm.value = {
        max_concurrent_tasks: readConfigNumber(cfgs, 'max_concurrent_tasks', 5),
        log_retention_days: readConfigNumber(cfgs, 'log_retention_days', 7),
        max_log_content_size: readConfigNumber(cfgs, 'max_log_content_size', 102400000),
        random_delay: readConfigString(cfgs, 'random_delay', ''),
        random_delay_extensions: readConfigString(cfgs, 'random_delay_extensions', ''),
        auto_install_deps: readConfigBool(cfgs, 'auto_install_deps', true),
        auto_add_cron: readConfigBool(cfgs, 'auto_add_cron', true),
        auto_del_cron: readConfigBool(cfgs, 'auto_del_cron', true),
        default_cron_rule: readConfigString(cfgs, 'default_cron_rule', ''),
        repo_file_extensions: readConfigString(cfgs, 'repo_file_extensions', ''),
        cpu_warn: readConfigNumber(cfgs, 'cpu_warn', 80),
        memory_warn: readConfigNumber(cfgs, 'memory_warn', 80),
        disk_warn: readConfigNumber(cfgs, 'disk_warn', 90),
        notify_on_resource_warn: readConfigBool(cfgs, 'notify_on_resource_warn', false),
        notify_panel_label: readConfigString(cfgs, 'notify_panel_label', ''),
        notify_on_login: readConfigBool(cfgs, 'notify_on_login', false),
        proxy_url: readConfigString(cfgs, 'proxy_url', ''),
        update_image_mirror: readConfigString(cfgs, 'update_image_mirror', ''),
        binary_update_proxy: readConfigString(cfgs, 'binary_update_proxy', ''),
        auto_update_enabled: readConfigBool(cfgs, 'auto_update_enabled', false),
        trusted_proxy_cidrs: readConfigString(cfgs, 'trusted_proxy_cidrs', ''),
        captcha_enabled: readConfigBool(cfgs, 'captcha_enabled', false),
        captcha_id: readConfigString(cfgs, 'captcha_id', ''),
        captcha_key: readConfigString(cfgs, 'captcha_key', ''),
        captcha_fail_mode: readConfigString(cfgs, 'captcha_fail_mode', 'open'),
        panel_title: readConfigString(cfgs, 'panel_title', ''),
        timezone: readConfigString(cfgs, 'timezone', 'Asia/Shanghai'),
        panel_icon: readConfigString(cfgs, 'panel_icon', ''),
        editor_background_color: readConfigString(cfgs, 'editor_background_color', ''),
        log_background_color: readConfigString(cfgs, 'log_background_color', ''),
        log_background_image: readConfigString(cfgs, 'log_background_image', ''),
        backup_schedule_enabled: readConfigBool(cfgs, 'backup_schedule_enabled', false),
        backup_schedule_frequency: readConfigString(cfgs, 'backup_schedule_frequency', 'daily'),
        backup_schedule_time: readConfigString(cfgs, 'backup_schedule_time', '03:00'),
        backup_schedule_weekday: readConfigString(cfgs, 'backup_schedule_weekday', '1'),
        backup_schedule_monthday: readConfigNumber(cfgs, 'backup_schedule_monthday', 1),
        backup_schedule_name: readConfigString(cfgs, 'backup_schedule_name', ''),
        backup_schedule_password: readConfigString(cfgs, 'backup_schedule_password', ''),
        backup_schedule_selection: readConfigString(cfgs, 'backup_schedule_selection', 'configs,tasks,subscriptions,env_vars,logs,scripts,dependencies,task_views'),
        max_web_sessions: readConfigNumber(cfgs, 'max_web_sessions', 1),
        max_app_sessions: readConfigNumber(cfgs, 'max_app_sessions', 1)
      }
      applyPanelAppearance(configForm.value)
    } catch (err: any) {
      ElMessage.error(err?.response?.data?.error || '加载配置失败')
    } finally {
      configsLoading.value = false
    }
  }

  async function saveConfigKeys(keys: string[]) {
    configsSaving.value = true
    try {
      const configs: Record<string, string> = {}
      for (const key of keys) {
        const val = (configForm.value as any)[key]
        configs[key] = typeof val === 'boolean' ? (val ? 'true' : 'false') : String(val ?? '')
      }
      await configApi.batchSet(configs)
      applyPanelAppearance(configForm.value)
      ElMessage.success('配置已保存')
    } catch (err: any) {
      ElMessage.error(err?.response?.data?.error || '保存失败')
      // 保存失败后从后端重新拉取该组的真实值，避免陈旧本地状态被下一次保存再次带出
      void loadSystemConfigs()
    } finally {
      configsSaving.value = false
    }
  }

  function handleSaveSystemConfig() {
    void saveConfigKeys([
      'panel_title', 'timezone', 'panel_icon', 'editor_background_color', 'log_background_color', 'log_background_image'
    ])
  }

  function handleSaveAlertConfig() {
    void saveConfigKeys([
      'cpu_warn', 'memory_warn', 'disk_warn', 'notify_on_resource_warn', 'notify_panel_label', 'notify_on_login'
    ])
  }

  function handleIconUpload(file: File) {
    if (!file.name.endsWith('.svg')) {
      ElMessage.warning('仅支持 SVG 格式图标')
      return false
    }
    if (file.size > 100 * 1024) {
      ElMessage.warning('图标文件不能超过 100KB')
      return false
    }
    const reader = new FileReader()
    reader.onload = (e) => {
      configForm.value.panel_icon = e.target?.result as string
    }
    reader.readAsDataURL(file)
    return false
  }

  function handleLogBackgroundUpload(file: File) {
    if (!file.type.startsWith('image/')) {
      ElMessage.warning('仅支持图片格式背景')
      return false
    }
    if (file.size > logBackgroundImageMaxBytes) {
      ElMessage.warning('背景图片不能超过 10MB')
      return false
    }

    void (async () => {
      try {
        const result = await compressLogBackgroundImage(file)
        configForm.value.log_background_image = result.dataUrl
        applyPanelAppearance(configForm.value)
        if (result.compressed) {
          ElMessage.success('背景图片已自动压缩，请保存配置后生效')
        }
      } catch {
        ElMessage.error('背景图片压缩失败，请换一张尺寸更小的图片')
      }
    })()
    return false
  }

  function previewPanelAppearance() {
    applyPanelAppearance(configForm.value)
  }

  function handleSaveTaskConfig() {
    void saveConfigKeys([
      'max_concurrent_tasks', 'log_retention_days',
      'max_log_content_size', 'random_delay', 'random_delay_extensions', 'auto_install_deps'
    ])
  }

  function handleSaveProxy() {
    void saveConfigKeys(['proxy_url', 'update_image_mirror', 'binary_update_proxy', 'auto_update_enabled', 'trusted_proxy_cidrs'])
  }

  function handleSaveCaptcha() {
    void saveConfigKeys(['captcha_enabled', 'captcha_id', 'captcha_key', 'captcha_fail_mode'])
  }

  function handleSaveSessionConfig() {
    void saveConfigKeys(['max_web_sessions', 'max_app_sessions'])
  }

  function handleSaveBackupSchedule(selectionCSV?: string) {
    const normalizedSelection = selectionCSV?.trim() || configForm.value.backup_schedule_selection
    configForm.value.backup_schedule_selection = normalizedSelection
    void saveConfigKeys([
      'backup_schedule_enabled',
      'backup_schedule_frequency',
      'backup_schedule_time',
      'backup_schedule_weekday',
      'backup_schedule_monthday',
      'backup_schedule_name',
      'backup_schedule_password',
      'backup_schedule_selection'
    ])
  }

  return {
    captchaFeatureImplemented,
    configsLoading,
    configsSaving,
    configForm,
    loadSystemConfigs,
    handleSaveSystemConfig,
    handleSaveAlertConfig,
    handleIconUpload,
    handleLogBackgroundUpload,
    previewPanelAppearance,
    handleSaveTaskConfig,
    handleSaveProxy,
    handleSaveCaptcha,
    handleSaveSessionConfig,
    handleSaveBackupSchedule
  }
}
