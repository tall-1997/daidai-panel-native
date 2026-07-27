<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { notificationApi } from '@/api/notification'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Bell, Plus, Refresh, Search } from '@element-plus/icons-vue'
import { useResponsive } from '@/composables/useResponsive'

const { isMobile, dialogFullscreen } = useResponsive()

const channels = ref<any[]>([])
const channelLoading = ref(false)
const channelTypes = ref<{ type: string; name: string }[]>([])

const showChannelDialog = ref(false)
const isCreateChannel = ref(true)
const channelForm = ref({ id: 0, name: '', type: 'webhook', config: '{}' })

// --- Search / filter state ---
const searchKeyword = ref('')
const filterType = ref('')
const filterStatus = ref('')
const channelPage = ref(1)
const channelPageSize = ref(10)

// --- Filtered channels ---
const filteredChannels = computed(() => {
  let list = channels.value
  if (searchKeyword.value) {
    const kw = searchKeyword.value.toLowerCase()
    list = list.filter(c => c.name?.toLowerCase().includes(kw) || c.type?.toLowerCase().includes(kw))
  }
  if (filterType.value) {
    list = list.filter(c => c.type === filterType.value)
  }
  if (filterStatus.value) {
    if (filterStatus.value === 'enabled') {
      list = list.filter(c => c.enabled)
    } else if (filterStatus.value === 'disabled') {
      list = list.filter(c => !c.enabled)
    }
  }
  return list
})

const pagedChannels = computed(() => {
  const start = (channelPage.value - 1) * channelPageSize.value
  return filteredChannels.value.slice(start, start + channelPageSize.value)
})

const filteredTotal = computed(() => filteredChannels.value.length)

function handleChannelSearch() {
  channelPage.value = 1
}

function resetFilters() {
  searchKeyword.value = ''
  filterType.value = ''
  filterStatus.value = ''
  channelPage.value = 1
}

// --- Channel type color / avatar helpers ---
const typeColorMap: Record<string, { bg: string; color: string; badge: string }> = {
  wecom: { bg: '#e8f5e9', color: '#4caf50', badge: 'success' },
  wecom_app: { bg: '#e8f5e9', color: '#4caf50', badge: 'success' },
  pushplus: { bg: '#e3f2fd', color: '#2196f3', badge: '' },
  feishu: { bg: '#eff6ff', color: '#2563eb', badge: '' },
  dingtalk: { bg: '#fff3e0', color: '#ff9800', badge: 'warning' },
  email: { bg: '#fce4ec', color: '#e91e63', badge: 'danger' },
  webhook: { bg: '#eff6ff', color: '#2563eb', badge: 'warning' },
  custom: { bg: '#ecfeff', color: '#0891b2', badge: 'warning' },
  telegram: { bg: '#e3f2fd', color: '#2196f3', badge: '' },
  bark: { bg: '#fff8e1', color: '#ffc107', badge: 'warning' },
  serverchan: { bg: '#e0f7fa', color: '#00bcd4', badge: '' },
  gotify: { bg: '#e8f5e9', color: '#4caf50', badge: 'success' },
  pushdeer: { bg: '#fff3e0', color: '#ff9800', badge: 'warning' },
  pushme: { bg: '#e3f2fd', color: '#2196f3', badge: '' },
  discord: { bg: '#e0f2fe', color: '#0284c7', badge: '' },
  slack: { bg: '#fce4ec', color: '#e91e63', badge: 'danger' },
  ntfy: { bg: '#e0f2f1', color: '#009688', badge: 'success' },
  wxpusher: { bg: '#e8f5e9', color: '#4caf50', badge: 'success' },
}

function getChannelAvatar(type: string, name: string) {
  const colors = typeColorMap[type] || { bg: '#f5f5f5', color: '#999' }
  const initial = (name || type || '?').charAt(0).toUpperCase()
  return { initial, bg: colors.bg, color: colors.color }
}

function getTypeBadgeType(type: string): string {
  return typeColorMap[type]?.badge || ''
}

const configFields = computed(() => {
  const t = channelForm.value.type
  const wecomMsgType = (configData.value.msg_type || 'text').trim() || 'text'
  const wecomAppMsgType = (configData.value.msg_type || 'text').trim() || 'text'
  switch (t) {
    case 'webhook': return [
      { key: 'url', label: 'Webhook URL', type: 'input', placeholder: 'https://example.com/webhook' },
    ]
    case 'email': return [
      { key: 'smtp_host', label: 'SMTP 主机', type: 'input', placeholder: 'smtp.qq.com' },
      { key: 'smtp_port', label: 'SMTP 端口', type: 'input', placeholder: '465' },
      { key: 'smtp_ssl', label: 'SSL 连接', type: 'select', placeholder: '自动：465 端口启用', options: [
        { label: '自动 (465 启用)', value: 'auto' },
        { label: '启用 SSL', value: 'true' },
        { label: '关闭 SSL', value: 'false' },
      ]},
      { key: 'smtp_user', label: '邮箱账号', type: 'input', placeholder: 'user@example.com' },
      { key: 'smtp_pass', label: '邮箱密码/授权码', type: 'password', placeholder: 'SMTP 授权码' },
      { key: 'to', label: '收件人', type: 'input', placeholder: '多个收件人用逗号分隔' },
      { key: 'from', label: '发件人 (可选)', type: 'input', placeholder: '留空则使用邮箱账号' },
    ]
    case 'telegram': return [
      { key: 'token', label: 'Bot Token', type: 'input', placeholder: '从 @BotFather 获取' },
      { key: 'chat_id', label: 'Chat ID', type: 'input', placeholder: '聊天/群组 ID' },
      { key: 'message_thread_id', label: 'Topic ID (可选)', type: 'input', placeholder: '群组话题 ID，留空则发到默认话题' },
      { key: 'api_host', label: 'API 地址 (可选)', type: 'input', placeholder: '自定义 API 地址，留空使用官方' },
      { key: 'proxy', label: '代理地址 (可选)', type: 'input', placeholder: 'http/socks5 代理地址' },
    ]
    case 'dingtalk': return [
      { key: 'webhook', label: 'Webhook URL', type: 'input', placeholder: 'https://oapi.dingtalk.com/robot/send?access_token=xxx' },
      { key: 'secret', label: '加签秘钥 (可选)', type: 'input', placeholder: '安全设置中的 SEC 开头的秘钥' },
      { key: 'msg_type', label: '消息类型', type: 'select', placeholder: '选择钉钉机器人消息类型', options: [
        { label: '文本', value: 'text' },
        { label: 'Markdown', value: 'markdown' },
      ]},
    ]
    case 'wecom': return [
      { key: 'webhook', label: 'Webhook URL', type: 'input', placeholder: 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx' },
      { key: 'msg_type', label: '消息类型', type: 'select', placeholder: '选择企业微信机器人消息类型', options: [
        { label: '文本', value: 'text' },
        { label: 'Markdown', value: 'markdown' },
        { label: 'Markdown V2', value: 'markdown_v2' },
        { label: '图片', value: 'image' },
        { label: '图文', value: 'news' },
        { label: '模版卡片', value: 'template_card' },
      ]},
      ...(wecomMsgType === 'text' ? [
        { key: 'content_template', label: '文本模板', type: 'textarea', placeholder: '支持 {{title}} 和 {{content}}，留空默认 {{title}}\\n{{content}}' },
        { key: 'mentioned_list', label: '提醒成员 (可选)', type: 'textarea', placeholder: '多个成员用逗号、分号或换行分隔，可填 @all' },
        { key: 'mentioned_mobile_list', label: '提醒手机号 (可选)', type: 'textarea', placeholder: '多个手机号用逗号、分号或换行分隔，可填 @all' },
      ] : []),
      ...((wecomMsgType === 'markdown' || wecomMsgType === 'markdown_v2') ? [
        { key: 'content_template', label: '内容模板', type: 'textarea', placeholder: '支持 {{title}} 和 {{content}} 占位符' },
      ] : []),
      ...(wecomMsgType === 'image' ? [
        { key: 'image_base64', label: '图片 Base64', type: 'textarea', placeholder: '填写图片的 Base64 内容' },
        { key: 'image_md5', label: '图片 MD5', type: 'input', placeholder: '填写图片内容对应的 MD5 值' },
      ] : []),
      ...(wecomMsgType === 'news' ? [
        { key: 'news_articles', label: '图文 Articles(JSON)', type: 'textarea', placeholder: '[{\"title\":\"{{title}}\",\"description\":\"{{content}}\",\"url\":\"https://example.com\",\"picurl\":\"https://example.com/demo.png\"}]' },
      ] : []),
      ...(wecomMsgType === 'template_card' ? [
        { key: 'template_card_payload', label: '卡片配置(JSON)', type: 'textarea', placeholder: '{\"card_type\":\"text_notice\",\"main_title\":{\"title\":\"{{title}}\",\"desc\":\"{{content}}\"}}' },
      ] : []),
    ]
    case 'wecom_app': return [
      { key: 'corp_id', label: '企业 ID', type: 'input', placeholder: '企业微信 CorpID' },
      { key: 'secret', label: '应用 Secret', type: 'password', placeholder: '应用 Secret' },
      { key: 'agent_id', label: 'Agent ID', type: 'input', placeholder: '应用 AgentId' },
      { key: 'base_url', label: '反代基础地址 (可选)', type: 'input', placeholder: '留空使用 https://qyapi.weixin.qq.com，也可填你的 Nginx 反代地址' },
      { key: 'to_user', label: '成员账号 (可选)', type: 'input', placeholder: '多个成员用 | 分隔，留空默认 @all' },
      { key: 'to_party', label: '部门 ID (可选)', type: 'input', placeholder: '多个部门用 | 分隔' },
      { key: 'to_tag', label: '标签 ID (可选)', type: 'input', placeholder: '多个标签用 | 分隔' },
      { key: 'msg_type', label: '消息类型', type: 'select', placeholder: '选择企业微信应用消息类型', options: [
        { label: '文本', value: 'text' },
        { label: 'Markdown', value: 'markdown' },
        { label: '图片', value: 'image' },
        { label: '文件', value: 'file' },
        { label: '视频', value: 'video' },
        { label: '图文', value: 'news' },
        { label: '图文消息 (mpnews)', value: 'mpnews' },
        { label: '模版卡片', value: 'template_card' },
      ]},
      ...((wecomAppMsgType === 'text' || wecomAppMsgType === 'markdown') ? [
        { key: 'content_template', label: '内容模板', type: 'textarea', placeholder: '支持 {{title}} 和 {{content}} 占位符' },
      ] : []),
      ...((wecomAppMsgType === 'image' || wecomAppMsgType === 'file' || wecomAppMsgType === 'video') ? [
        { key: 'media_id', label: 'Media ID', type: 'input', placeholder: '调用上传临时素材接口后得到的 media_id' },
      ] : []),
      ...(wecomAppMsgType === 'news' ? [
        { key: 'news_articles', label: '图文 Articles(JSON)', type: 'textarea', placeholder: '[{\"title\":\"{{title}}\",\"description\":\"{{content}}\",\"url\":\"https://example.com\",\"picurl\":\"https://example.com/demo.png\"}]' },
      ] : []),
      ...(wecomAppMsgType === 'mpnews' ? [
        { key: 'mpnews_articles', label: '图文消息 Articles(JSON)', type: 'textarea', placeholder: '[{\"title\":\"{{title}}\",\"thumb_media_id\":\"MEDIA_ID\",\"author\":\"Author\",\"content_source_url\":\"https://example.com\",\"content\":\"<p>{{content}}</p>\",\"digest\":\"Digest description\"}]' },
      ] : []),
      ...(wecomAppMsgType === 'template_card' ? [
        { key: 'template_card_payload', label: '卡片配置(JSON)', type: 'textarea', placeholder: '{\"card_type\":\"text_notice\",\"main_title\":{\"title\":\"{{title}}\",\"desc\":\"{{content}}\"}}' },
      ] : []),
      { key: 'safe', label: '保密消息', type: 'select', placeholder: '默认 0', options: [
        { label: '否 (0)', value: '0' },
        { label: '是 (1)', value: '1' },
        ...(wecomAppMsgType === 'mpnews' ? [{ label: '仅企业内分享 (2)', value: '2' }] : []),
      ]},
      { key: 'enable_id_trans', label: 'ID 转译', type: 'select', placeholder: '默认 0', options: [
        { label: '关闭 (0)', value: '0' },
        { label: '开启 (1)', value: '1' },
      ]},
      { key: 'enable_duplicate_check', label: '重复检查', type: 'select', placeholder: '默认 0', options: [
        { label: '关闭 (0)', value: '0' },
        { label: '开启 (1)', value: '1' },
      ]},
      { key: 'duplicate_check_interval', label: '去重间隔(秒)', type: 'input', placeholder: '默认 1800，最大 14400' },
    ]
    case 'bark': return [
      { key: 'key', label: 'Device Key', type: 'input', placeholder: '打开 Bark App 复制推送地址中的 Key，如 https://api.day.app/xxxxxx 中的 xxxxxx' },
      { key: 'server', label: '服务器 (可选)', type: 'input', placeholder: '默认 https://api.day.app' },
      { key: 'sound', label: '推送声音 (可选)', type: 'input', placeholder: '如 birdsong，留空使用默认' },
      { key: 'group', label: '推送分组 (可选)', type: 'input', placeholder: '消息分组名称' },
      { key: 'icon', label: '图标 URL (可选)', type: 'input', placeholder: 'https://example.com/icon.png' },
      { key: 'level', label: '时效性 (可选)', type: 'select', placeholder: '推送优先级', options: [
        { label: '默认 (active)', value: 'active' },
        { label: '时效性 (timeSensitive)', value: 'timeSensitive' },
        { label: '被动 (passive)', value: 'passive' },
      ]},
      { key: 'url', label: '跳转 URL (可选)', type: 'input', placeholder: '点击通知后跳转的链接' },
    ]
    case 'pushplus': return [
      { key: 'token', label: 'Token', type: 'input', placeholder: 'PushPlus 用户 Token' },
      { key: 'topic', label: '群组编码 (可选)', type: 'input', placeholder: '一对多推送时的群组编码' },
      { key: 'template', label: '模板 (可选)', type: 'select', placeholder: '消息模板', options: [
        { label: '默认 (html)', value: 'html' },
        { label: 'JSON', value: 'json' },
        { label: '纯文本', value: 'txt' },
        { label: 'Markdown', value: 'markdown' },
      ]},
    ]
    case 'serverchan': return [
      { key: 'key', label: 'SendKey', type: 'input', placeholder: 'Server酱的 SendKey (SCT...)' },
    ]
    case 'feishu': return [
      { key: 'webhook', label: 'Webhook URL', type: 'input', placeholder: 'https://open.feishu.cn/open-apis/bot/v2/hook/xxx' },
      { key: 'secret', label: '加签秘钥 (可选)', type: 'input', placeholder: '安全设置中的签名校验秘钥' },
    ]
    case 'gotify': return [
      { key: 'server', label: '服务器地址', type: 'input', placeholder: 'https://gotify.example.com' },
      { key: 'token', label: 'App Token', type: 'input', placeholder: 'Gotify 应用 Token' },
      { key: 'priority', label: '优先级 (可选)', type: 'input', placeholder: '0-10，默认 5' },
    ]
    case 'pushdeer': return [
      { key: 'key', label: 'PushKey', type: 'input', placeholder: 'PushDeer 的 PushKey' },
      { key: 'server', label: '服务器 (可选)', type: 'input', placeholder: '默认 https://api2.pushdeer.com' },
    ]
    case 'pushme': return [
      { key: 'key', label: 'PushMe Key', type: 'input', placeholder: 'PushMe 的 push_key' },
      { key: 'server', label: '接口地址 (可选)', type: 'input', placeholder: '默认 https://push.i-i.me' },
      { key: 'message_type', label: '消息类型 (可选)', type: 'input', placeholder: '按 PushMe 支持的 type 值填写' },
    ]
    case 'chanify': return [
      { key: 'token', label: 'Token', type: 'input', placeholder: 'Chanify 设备 Token' },
      { key: 'server', label: '服务器 (可选)', type: 'input', placeholder: '默认 https://api.chanify.net' },
    ]
    case 'igot': return [
      { key: 'key', label: 'Key', type: 'input', placeholder: 'iGot 推送 Key' },
    ]
    case 'qmsg': return [
      { key: 'key', label: 'Qmsg Key', type: 'input', placeholder: 'Qmsg 酱的 Key' },
      { key: 'mode', label: '发送模式', type: 'select', placeholder: '选择 send 或 group', options: [
        { label: '私聊/默认 (send)', value: 'send' },
        { label: '群发 (group)', value: 'group' },
      ]},
      { key: 'qq', label: 'QQ 号/群号 (可选)', type: 'input', placeholder: '留空则按 Qmsg 端默认配置发送' },
    ]
    case 'pushover': return [
      { key: 'token', label: 'API Token', type: 'input', placeholder: '应用 API Token' },
      { key: 'user', label: 'User Key', type: 'input', placeholder: '用户 Key' },
    ]
    case 'discord': return [
      { key: 'webhook', label: 'Webhook URL', type: 'input', placeholder: 'https://discord.com/api/webhooks/...' },
    ]
    case 'slack': return [
      { key: 'webhook', label: 'Webhook URL', type: 'input', placeholder: 'https://hooks.slack.com/services/...' },
    ]
    case 'ntfy': return [
      { key: 'topic', label: 'Topic', type: 'input', placeholder: '订阅主题名称' },
      { key: 'server', label: '服务器 (可选)', type: 'input', placeholder: '默认 https://ntfy.sh' },
      { key: 'token', label: 'Token (可选)', type: 'input', placeholder: '访问令牌，用于私有主题' },
      { key: 'priority', label: '优先级 (可选)', type: 'select', placeholder: '消息优先级', options: [
        { label: '最低 (1)', value: '1' },
        { label: '低 (2)', value: '2' },
        { label: '默认 (3)', value: '3' },
        { label: '高 (4)', value: '4' },
        { label: '紧急 (5)', value: '5' },
      ]},
    ]
    case 'wxpusher': return [
      { key: 'app_token', label: 'App Token', type: 'input', placeholder: 'WxPusher 的 appToken' },
      { key: 'uids', label: 'UID 列表 (可选)', type: 'textarea', placeholder: '多个 UID 可用分号、逗号或换行分隔' },
      { key: 'topic_ids', label: 'Topic ID 列表 (可选)', type: 'textarea', placeholder: '多个 Topic ID 可用分号、逗号或换行分隔' },
      { key: 'content_type', label: '内容类型 (可选)', type: 'select', placeholder: '默认文本消息', options: [
        { label: '文本 (1)', value: '1' },
        { label: 'HTML (2)', value: '2' },
        { label: 'Markdown (3)', value: '3' },
      ]},
      { key: 'url', label: '原文链接 (可选)', type: 'input', placeholder: '消息详情页跳转地址' },
      { key: 'verify_pay_type', label: '付费校验 (可选)', type: 'select', placeholder: '默认不校验', options: [
        { label: '不校验 (0)', value: '0' },
        { label: '仅付费用户 (1)', value: '1' },
        { label: '仅未订阅/已过期 (2)', value: '2' },
      ]},
      { key: 'server', label: '接口地址 (可选)', type: 'input', placeholder: '默认 https://wxpusher.zjiecode.com/api/send/message' },
    ]
    case 'custom': return [
      { key: 'url', label: 'URL', type: 'input', placeholder: 'https://example.com/api/notify' },
      { key: 'method', label: 'Method', type: 'select', placeholder: '请求方法', options: [
        { label: 'POST', value: 'POST' },
        { label: 'GET', value: 'GET' },
        { label: 'PUT', value: 'PUT' },
      ]},
      { key: 'content_type', label: 'Content-Type', type: 'input', placeholder: '默认 application/json' },
      { key: 'headers', label: 'Headers (JSON)', type: 'textarea', placeholder: '{"Authorization": "Bearer xxx"}' },
      { key: 'body', label: 'Body 模板', type: 'textarea', placeholder: '使用 {{title}} 和 {{content}} 作为占位符' },
    ]
    default: return [{ key: 'url', label: 'URL', type: 'input', placeholder: '' }]
  }
})

const configData = ref<Record<string, string>>({})

function syncConfigToForm() {
  channelForm.value.config = JSON.stringify(configData.value)
}

function syncFormToConfig() {
  try {
    configData.value = JSON.parse(channelForm.value.config)
  } catch {
    configData.value = {}
  }
}

async function loadChannels() {
  channelLoading.value = true
  try {
    const res = await notificationApi.list()
    channels.value = res.data || []
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.error || '加载通知渠道失败')
  } finally {
    channelLoading.value = false
  }
}

async function loadChannelTypes() {
  try {
    const res = await notificationApi.types()
    channelTypes.value = res.data || []
  } catch { /* ignore */ }
}

onMounted(() => {
  loadChannels()
  loadChannelTypes()
})

function applyChannelTypeDefaults() {
  // 钉钉新建渠道默认走 Markdown，与后端 msg_type 空值兜底保持一致
  if (channelForm.value.type === 'dingtalk') {
    configData.value.msg_type = 'markdown'
  }
}

function onChannelTypeChange() {
  configData.value = {}
  applyChannelTypeDefaults()
}

function openCreateChannel() {
  isCreateChannel.value = true
  channelForm.value = { id: 0, name: '', type: 'webhook', config: '{}' }
  configData.value = {}
  applyChannelTypeDefaults()
  showChannelDialog.value = true
}

function openEditChannel(row: any) {
  isCreateChannel.value = false
  channelForm.value = { id: row.id, name: row.name, type: row.type, config: row.config || '{}' }
  syncFormToConfig()
  showChannelDialog.value = true
}

// 配置中属于 JSON 结构的字段 key（需要 JSON.parse 校验）
const JSON_CONFIG_KEYS = new Set([
  'headers',
  'news_articles',
  'mpnews_articles',
  'template_card_payload',
])

// email 端口校验 key
const NUMERIC_CONFIG_KEYS = new Set(['smtp_port', 'port'])

function validateConfigFields(): string | null {
  for (const field of configFields.value) {
    const key = (field as any).key
    const val = (configData.value[key] ?? '').toString().trim()
    if (!val) continue
    if (JSON_CONFIG_KEYS.has(key)) {
      try {
        JSON.parse(val)
      } catch (e: any) {
        return `字段「${(field as any).label || key}」不是合法 JSON：${e?.message || ''}`
      }
    }
    if (NUMERIC_CONFIG_KEYS.has(key)) {
      const n = Number(val)
      if (!Number.isInteger(n) || n <= 0 || n > 65535) {
        return `字段「${(field as any).label || key}」应为 1-65535 的端口号`
      }
    }
  }
  return null
}

async function handleSaveChannel() {
  if (!channelForm.value.name.trim()) {
    ElMessage.warning('名称不能为空')
    return
  }
  const validationErr = validateConfigFields()
  if (validationErr) {
    ElMessage.warning(validationErr)
    return
  }
  syncConfigToForm()
  try {
    if (isCreateChannel.value) {
      await notificationApi.create(channelForm.value)
      ElMessage.success('创建成功')
    } else {
      await notificationApi.update(channelForm.value.id, channelForm.value)
      ElMessage.success('更新成功')
    }
    showChannelDialog.value = false
    loadChannels()
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.error || (isCreateChannel.value ? '创建失败' : '更新失败'))
  }
}

async function handleDeleteChannel(id: number) {
  try {
    await ElMessageBox.confirm('确定要删除该通知渠道吗？', '确认删除', { type: 'warning' })
    await notificationApi.delete(id)
    ElMessage.success('删除成功')
    loadChannels()
  } catch { /* cancelled */ }
}

async function handleToggleChannel(row: any) {
  try {
    const enabling = !row.enabled
    await ElMessageBox.confirm(
      enabling
        ? `确认启用通知渠道「${row.name}」吗？`
        : `确认禁用通知渠道「${row.name}」吗？禁用后将不再接收任务推送。`,
      enabling ? '启用确认' : '禁用确认',
      { type: enabling ? 'info' : 'warning' }
    )
    if (row.enabled) {
      await notificationApi.disable(row.id)
    } else {
      await notificationApi.enable(row.id)
    }
    ElMessage.success(row.enabled ? '已禁用' : '已启用')
    loadChannels()
  } catch (err: any) {
    if (err === 'cancel' || err?.toString?.() === 'cancel') return
    ElMessage.error(err?.response?.data?.error || '操作失败')
  }
}

async function handleTestChannel(id: number) {
  try {
    const res: any = await notificationApi.test(id)
    const detail = res?.message || res?.data?.message
    ElMessage.success(detail ? `测试通知发送成功：${detail}` : '测试通知发送成功')
  } catch (e: any) {
    const data = e?.response?.data
    const mainError = data?.error || '测试发送失败'
    const detail = data?.detail || data?.message || e?.message
    if (detail && detail !== mainError) {
      ElMessageBox.alert(String(detail), mainError, {
        confirmButtonText: '知道了',
        type: 'error',
      }).catch(() => {})
    } else {
      ElMessage.error(mainError)
    }
  } finally {
    await loadChannels()
  }
}

function getTypeName(type: string) {
  const found = channelTypes.value.find(t => t.type === type)
  return found?.name || type
}

function parseChannelConfig(configText: string): Record<string, unknown> {
  try {
    const parsed = JSON.parse(configText || '{}')
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {}
  } catch {
    return {}
  }
}

function extractDisplayHost(raw: unknown): string {
  const value = String(raw ?? '').trim()
  if (!value) return ''
  try {
    const parsed = new URL(value)
    return parsed.host || value
  } catch {
    return value.length > 28 ? `${value.slice(0, 28)}...` : value
  }
}

function splitConfigTargets(raw: unknown): string[] {
  return String(raw ?? '')
    .split(/[,\n;|\t ]+/)
    .map(item => item.trim())
    .filter(Boolean)
}

function countConfiguredConfigItems(config: Record<string, unknown>): number {
  return Object.values(config).filter((value) => {
    if (value === null || value === undefined) return false
    if (Array.isArray(value)) return value.length > 0
    return String(value).trim() !== ''
  }).length
}

function getChannelConfigSummary(row: any): string[] {
  const config = parseChannelConfig(row.config || '{}')
  const lines: string[] = []
  const configCount = countConfiguredConfigItems(config)

  switch (row.type) {
    case 'webhook':
    case 'custom':
      if (config.url) lines.push(`地址 ${extractDisplayHost(config.url)}`)
      break
    case 'dingtalk':
    case 'wecom':
    case 'feishu':
    case 'discord':
    case 'slack':
      if (config.webhook) lines.push(`地址 ${extractDisplayHost(config.webhook)}`)
      break
    case 'email':
      if (config.smtp_host) {
        const port = String(config.smtp_port ?? '').trim()
        lines.push(`SMTP ${String(config.smtp_host)}${port ? `:${port}` : ''}`)
      }
      {
        const sslMode = String(config.smtp_ssl ?? '').trim()
        const port = String(config.smtp_port ?? '').trim()
        if (sslMode === 'true' || ((!sslMode || sslMode === 'auto') && port === '465')) {
          lines.push('SSL 已启用')
        } else if (sslMode === 'false') {
          lines.push('SSL 已关闭')
        } else if (sslMode === 'auto') {
          lines.push('SSL 自动')
        }
      }
      if (splitConfigTargets(config.to).length > 0) lines.push(`收件人 ${splitConfigTargets(config.to).length} 个`)
      break
    case 'telegram':
      if (config.chat_id) lines.push(`Chat ${String(config.chat_id)}`)
      if (config.message_thread_id) lines.push(`Topic ${String(config.message_thread_id)}`)
      if (config.api_host) lines.push(`API ${extractDisplayHost(config.api_host)}`)
      break
    case 'gotify':
    case 'pushdeer':
    case 'pushme':
    case 'chanify':
    case 'ntfy':
      if (config.server) lines.push(`服务器 ${extractDisplayHost(config.server)}`)
      if (row.type === 'ntfy' && config.topic) lines.push(`主题 ${String(config.topic)}`)
      break
    case 'wxpusher': {
      const uidCount = splitConfigTargets(config.uids).length
      const topicCount = splitConfigTargets(config.topic_ids).length
      if (uidCount > 0) lines.push(`UID ${uidCount} 个`)
      if (topicCount > 0) lines.push(`Topic ${topicCount} 个`)
      break
    }
    case 'wecom_app':
      if (config.agent_id) lines.push(`Agent ${String(config.agent_id)}`)
      if (config.base_url) lines.push(`地址 ${extractDisplayHost(config.base_url)}`)
      break
  }

  if (lines.length === 0) {
    return configCount > 0 ? [`已配置 ${configCount} 项`] : ['未配置']
  }
  if (configCount > lines.length) {
    lines.push(`共 ${configCount} 项配置`)
  }
  return lines.slice(0, 2)
}

</script>

<template>
  <div class="notifications-page dd-fixed-page dd-page-hide-heading">
    <!-- Page Header -->
    <div class="page-header">
      <div>
        <h2 class="page-title-with-icon"><el-icon><Bell /></el-icon><span>通知渠道</span></h2>
        <p class="page-subtitle">配置任务执行结果的通知渠道</p>
      </div>
    </div>

        <!-- Toolbar -->
        <div class="toolbar">
          <div class="toolbar__left">
            <el-input
              v-model="searchKeyword"
              placeholder="搜索渠道名称或关键词..."
              clearable
              class="toolbar__search"
              @keyup.enter="handleChannelSearch"
              @clear="handleChannelSearch"
            >
              <template #prefix><el-icon><Search /></el-icon></template>
            </el-input>
            <el-select v-model="filterType" placeholder="类型" clearable class="toolbar__filter" @change="handleChannelSearch">
              <template #prefix>类型</template>
              <el-option label="全部" value="" />
              <el-option v-for="t in channelTypes" :key="t.type" :label="t.name" :value="t.type" />
            </el-select>
            <el-select v-model="filterStatus" placeholder="状态" clearable class="toolbar__filter" @change="handleChannelSearch">
              <template #prefix>状态</template>
              <el-option label="全部" value="" />
              <el-option label="启用" value="enabled" />
              <el-option label="禁用" value="disabled" />
            </el-select>
            <el-button @click="resetFilters">
              <el-icon><Refresh /></el-icon> 重置
            </el-button>
          </div>
          <div class="toolbar__right">
            <el-button type="primary" @click="openCreateChannel">
              <el-icon><Plus /></el-icon> 新建渠道
            </el-button>
          </div>
        </div>

        <!-- Mobile Cards -->
        <div v-if="isMobile" class="dd-mobile-list">
          <div
            v-for="row in pagedChannels"
            :key="row.id"
            class="dd-mobile-card"
          >
            <div class="dd-mobile-card__header">
              <div class="dd-mobile-card__title-wrap">
                <span class="dd-mobile-card__title">{{ row.name }}</span>
                <div class="dd-mobile-card__badges">
                  <el-tag size="small" :type="getTypeBadgeType(row.type)" effect="plain">{{ getTypeName(row.type) }}</el-tag>
                </div>
              </div>
              <el-switch :model-value="row.enabled" size="small" @change="handleToggleChannel(row)" />
            </div>
            <div class="dd-mobile-card__body">
              <div class="dd-mobile-card__grid">
                <div class="dd-mobile-card__field dd-mobile-card__field--full">
                  <span class="dd-mobile-card__label">配置概览</span>
                  <div class="dd-mobile-card__value config-summary">
                    <span v-for="item in getChannelConfigSummary(row)" :key="item" class="config-summary__line">{{ item }}</span>
                  </div>
                </div>
                <div class="dd-mobile-card__field">
                  <span class="dd-mobile-card__label">创建时间</span>
                  <span class="dd-mobile-card__value">{{ new Date(row.created_at).toLocaleString('zh-CN', { hour12: false }) }}</span>
                </div>
              </div>
              <div class="dd-mobile-card__actions notification-card__actions">
                <el-button size="small" type="success" plain @click="handleTestChannel(row.id)">测试</el-button>
                <el-button size="small" type="primary" plain @click="openEditChannel(row)">编辑</el-button>
                <el-button size="small" type="danger" plain @click="handleDeleteChannel(row.id)">删除</el-button>
              </div>
            </div>
          </div>
          <el-empty v-if="!channelLoading && pagedChannels.length === 0" description="暂无通知渠道" />
        </div>

        <!-- Desktop Table -->
        <div v-else class="table-card">
          <el-table
            :data="pagedChannels"
            v-loading="channelLoading"
            style="width: 100%"
            :header-cell-style="{ background: '#f8fafc', color: '#64748b', fontWeight: 600, fontSize: '13px' }"
          >
            <el-table-column prop="name" label="名称" min-width="180">
              <template #default="{ row }">
                <div class="channel-name-cell">
                  <div
                    class="channel-avatar"
                    :style="{ background: getChannelAvatar(row.type, row.name).bg, color: getChannelAvatar(row.type, row.name).color }"
                  >
                    {{ getChannelAvatar(row.type, row.name).initial }}
                  </div>
                  <div class="channel-name-info">
                    <span class="channel-name-text">{{ row.name }}</span>
                    <span class="channel-name-sub">{{ getTypeName(row.type) }}</span>
                  </div>
                </div>
              </template>
            </el-table-column>
            <el-table-column prop="type" label="类型" width="130">
              <template #default="{ row }">
                <el-tag size="small" :type="getTypeBadgeType(row.type)" effect="plain" round>{{ getTypeName(row.type) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="配置概览" min-width="180">
              <template #default="{ row }">
                <div class="config-summary">
                  <span v-for="item in getChannelConfigSummary(row)" :key="item" class="config-summary__line">{{ item }}</span>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="启用" width="80" align="center">
              <template #default="{ row }">
                <el-switch :model-value="row.enabled" size="small" @change="handleToggleChannel(row)" />
              </template>
            </el-table-column>
            <el-table-column label="最近测试" width="160">
              <template #default="{ row }">
                <div v-if="row.last_test_at" class="test-status">
                  <el-tag
                    size="small"
                    :type="row.last_test_status === 'success' ? 'success' : row.last_test_status === 'error' || row.last_test_status === 'failed' ? 'danger' : 'warning'"
                    effect="plain"
                    round
                  >
                    {{ row.last_test_status === 'success' ? '测试通过' : row.last_test_status === 'error' || row.last_test_status === 'failed' ? '测试未通过' : '未测试' }}
                  </el-tag>
                  <span class="time-text">{{ new Date(row.last_test_at).toLocaleDateString('zh-CN') }}</span>
                </div>
                <span v-else class="text-muted">未测试</span>
              </template>
            </el-table-column>
            <el-table-column prop="created_at" label="创建时间" width="170">
              <template #default="{ row }">
                <div class="time-cell">
                  <span class="time-text">{{ new Date(row.created_at).toLocaleString('zh-CN', { hour12: false }) }}</span>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="180" fixed="right" align="center">
              <template #default="{ row }">
                <div class="action-btns">
                  <el-button size="small" text type="success" @click="handleTestChannel(row.id)">测试</el-button>
                  <el-button size="small" text type="primary" @click="openEditChannel(row)">编辑</el-button>
                  <el-button size="small" text type="danger" @click="handleDeleteChannel(row.id)">删除</el-button>
                </div>
              </template>
            </el-table-column>
          </el-table>
        </div>

        <!-- Pagination -->
        <div class="pagination-bar">
          <span class="pagination-total">共 {{ filteredTotal }} 条</span>
          <el-pagination
            v-model:current-page="channelPage"
            v-model:page-size="channelPageSize"
            :total="filteredTotal"
            :page-sizes="[10, 20, 50]"
            layout="sizes, prev, pager, next"
          />
        </div>

    <!-- Channel Dialog -->
    <el-dialog v-model="showChannelDialog" :title="isCreateChannel ? '新建通知渠道' : '编辑通知渠道'" width="600px" :fullscreen="dialogFullscreen">
      <el-form :model="channelForm" :label-width="dialogFullscreen ? 'auto' : '130px'" :label-position="dialogFullscreen ? 'top' : 'right'">
        <el-form-item label="名称">
          <el-input v-model="channelForm.name" placeholder="渠道名称" />
        </el-form-item>
        <el-form-item label="类型">
          <el-select v-model="channelForm.type" style="width: 100%" @change="onChannelTypeChange">
            <el-option v-for="t in channelTypes" :key="t.type" :label="t.name" :value="t.type" />
          </el-select>
        </el-form-item>
        <el-divider content-position="left">配置</el-divider>
        <el-form-item v-for="field in configFields" :key="field.key" :label="field.label">
          <el-select
            v-if="field.type === 'select'"
            v-model="configData[field.key]"
            :placeholder="field.placeholder || field.label"
            clearable
            style="width: 100%"
          >
            <el-option v-for="opt in field.options" :key="opt.value" :label="opt.label" :value="opt.value" />
          </el-select>
          <el-input
            v-else-if="field.type === 'textarea'"
            v-model="configData[field.key]"
            type="textarea"
            :rows="3"
            :placeholder="field.placeholder || field.label"
          />
          <el-input
            v-else
            v-model="configData[field.key]"
            :type="field.type === 'password' ? 'password' : 'text'"
            :show-password="field.type === 'password'"
            :placeholder="field.placeholder || field.label"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showChannelDialog = false">取消</el-button>
        <el-button type="primary" @click="handleSaveChannel">{{ isCreateChannel ? '创建' : '保存' }}</el-button>
      </template>
    </el-dialog>

  </div>
</template>

<style scoped lang="scss">
.notifications-page {
  padding: 0;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 18px;
  gap: 16px;

  .page-title-with-icon { display: inline-flex; align-items: center; gap: 8px; }
  .page-title-with-icon :deep(.el-icon) { color: var(--el-color-primary); }

  h2 { margin: 0; font-size: 22px; font-weight: 700; color: var(--el-text-color-primary); line-height: 1.3; }
  .page-subtitle { font-size: 13px; color: var(--el-text-color-secondary); margin: 6px 0 0; line-height: 1.6; max-width: 720px; }
}

// 工具条：与定时任务页/订阅管理页统一（margin:14px 0、左区 gap 12、右区 gap 10）
.toolbar {
  display: flex; justify-content: space-between; align-items: center; margin: 14px 0; gap: 12px; flex-wrap: wrap;
  &__left { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; flex: 1; min-width: 0; }
  &__right { display: flex; align-items: center; gap: 10px; flex-shrink: 0; }
  &__search { width: 260px; }
  &__filter { width: 130px; }
}

// 表格卡：圆角/阴影/边框全部对齐卡片令牌（dd-fixed-page 下的 flex + 内部滚动由全局规则接管）
.table-card {
  background: var(--el-bg-color);
  border-radius: var(--dd-card-radius);
  box-shadow: var(--dd-shadow-card);
  border: 1px solid var(--el-border-color-lighter);
  overflow: hidden;
}

.channel-name-cell {
  display: flex;
  align-items: center;
  gap: 10px;
}

// 渠道头像：底色/文字色保留按类型的品牌识别色（来自模板内联 style），
// 这里只统一通用尺寸与轻边框，边框走令牌以适配明暗。
.channel-avatar {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 15px;
  font-weight: 700;
  flex-shrink: 0;
  letter-spacing: 0;
  border: 1px solid var(--el-border-color-lighter);
}

.channel-name-info {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
}

.channel-name-text {
  font-weight: 500;
  color: var(--el-text-color-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.channel-name-sub {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
}

.config-summary {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.config-summary__line {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.5;
}

.test-status {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.time-cell {
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.time-text {
  font-family: var(--dd-font-mono, monospace);
  font-size: 12px;
  color: var(--el-text-color-regular);
}

.creator-text {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
}

.text-muted { color: var(--el-text-color-placeholder); }

.action-btns {
  display: flex; align-items: center; justify-content: center; gap: 2px;
}

// 分页条：与定时任务页/订阅管理页一致的间距收敛
.pagination-bar {
  margin-top: 14px; display: flex; justify-content: space-between; align-items: center; padding: 0 4px;
}

.pagination-total {
  font-size: 13px; color: var(--el-text-color-secondary);
}

.notification-card__actions > * {
  flex: 1 1 calc(50% - 4px);
}

:deep(.el-table) {
  // 边框统一走令牌，明暗自动适配（原写死浅灰会在暗色串色）
  --el-table-border-color: var(--el-border-color-lighter);
  .el-table__header-wrapper th { border-bottom: 1px solid var(--el-border-color-light); }
  .el-table__row td { border-bottom: 1px solid var(--el-border-color-lighter); }
  .el-table__cell { padding: 12px 0; }
}

@media (max-width: 768px) {
  .page-header { flex-direction: column; gap: 10px; margin-bottom: 14px; h2 { font-size: 18px; } }
  .toolbar {
    flex-direction: column; align-items: stretch; gap: 10px;
    &__left { flex-direction: column; gap: 10px; }
    &__search { width: 100% !important; }
    &__filter { width: 100% !important; }
    &__right { justify-content: flex-end; }
  }
}

// ===== 入场动画 =====
// 与定时任务页/订阅管理页统一：只对卡片级容器（工具条 / 表格卡 / 移动列表）做克制的淡入上移 + 轻微错落；
// 不给表格每一行或每张移动卡做 stagger。时长走令牌，prefers-reduced-motion 时令牌自动降为 1ms 即等效关闭。
@keyframes dd-notify-rise-in {
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
  animation: dd-notify-rise-in var(--dd-motion-page) var(--dd-ease-decelerate) both;
}

// 轻微错落：工具条先入，表格卡/移动列表略晚
.table-card,
.dd-mobile-list {
  animation-delay: 60ms;
}
</style>
