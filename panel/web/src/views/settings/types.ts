export type CaptchaFailMode = 'open' | 'strict'

export interface SettingsConfigForm {
  max_concurrent_tasks: number
  log_retention_days: number
  max_log_content_size: number
  random_delay: string
  random_delay_extensions: string
  auto_install_deps: boolean
  auto_add_cron: boolean
  auto_del_cron: boolean
  default_cron_rule: string
  repo_file_extensions: string
  cpu_warn: number
  memory_warn: number
  disk_warn: number
  notify_on_resource_warn: boolean
  notify_panel_label: string
  notify_on_login: boolean
  proxy_url: string
  update_image_mirror: string
  binary_update_proxy: string
  auto_update_enabled: boolean
  trusted_proxy_cidrs: string
  captcha_enabled: boolean
  captcha_id: string
  captcha_key: string
  captcha_fail_mode: CaptchaFailMode | string
  panel_title: string
  timezone: string
  panel_icon: string
  editor_background_color: string
  log_background_color: string
  log_background_image: string
  backup_schedule_enabled: boolean
  backup_schedule_frequency: 'daily' | 'weekly' | 'monthly' | string
  backup_schedule_time: string
  backup_schedule_weekday: string
  backup_schedule_monthday: number
  backup_schedule_name: string
  backup_schedule_password: string
  backup_schedule_selection: string
  max_web_sessions: number
  max_app_sessions: number
}
