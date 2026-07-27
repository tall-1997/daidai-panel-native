export interface ApiParam {
  name: string
  type: string
  required?: boolean
  description: string
  example?: string
}

export interface ApiEndpoint {
  id: string
  method: 'GET' | 'POST' | 'PUT' | 'DELETE'
  path: string
  title: string
  description: string
  auth?: 'jwt' | 'open_api' | 'none'
  pathParams?: ApiParam[]
  queryParams?: ApiParam[]
  bodyParams?: ApiParam[]
  helperExamples?: Record<string, string>
  responseExample?: string
  responseFields?: ApiParam[]
}

export interface ApiCategory {
  key: string
  label: string
  endpoints: ApiEndpoint[]
}

export const apiCategories: ApiCategory[] = [
  {
    key: 'plugin-guide',
    label: '插件对接指南',
    endpoints: [
      {
        id: 'plugin-overview',
        method: 'GET',
        path: '/docs/plugin-overview',
        title: '对接概述',
        description: '呆呆面板通过 Open API 应用机制对接第三方插件，实现环境变量的自动管理。首先在面板「Open API」页面创建应用，获取 App Key 和 App Secret。配置格式：Host丨AppKey丨AppSecret（使用中文竖线 丨 分隔），示例：http://127.0.0.1:5173丨a1b2c3d4...丨e5f6g7h8...。对接流程：1. 使用 App Key + App Secret 获取 Token → 2. 使用 Token 操作环境变量（增删改查）→ 3. Token 过期（24小时）后重新获取。',
        auth: 'none',
        responseExample: JSON.stringify({
          '配置格式': 'Host丨AppKey丨AppSecret',
          '配置示例': 'http://127.0.0.1:5173丨a1b2c3d4e5f6...丨e5f6g7h8i9j0...',
          '认证方式': 'Open API Token (Bearer)',
          '接口前缀': '/api/',
          'Token有效期': '24 小时，过期后重新调用 token 接口获取',
          '对接流程': [
            '1. 在面板「Open API」页面创建应用，获取 App Key 和 App Secret',
            '2. POST /api/open-api/token 获取 access_token',
            '3. GET /api/envs 查询环境变量',
            '4. POST /api/envs 添加环境变量',
            '5. PUT /api/envs/:id 更新环境变量',
            '6. DELETE /api/envs/:id 删除环境变量',
          ],
        }, null, 2),
        responseFields: [
          { name: 'Host', type: 'string', description: '呆呆面板地址，如 http://127.0.0.1:5173' },
          { name: 'AppKey', type: 'string', description: '应用的 App Key（在面板 Open API 页面创建应用后获取）' },
          { name: 'AppSecret', type: 'string', description: '应用的 App Secret（创建应用时生成）' },
          { name: 'Token', type: 'string', description: '通过 App Key + Secret 获取的 JWT Token，有效期 24 小时' },
        ],
      },
      {
        id: 'plugin-login',
        method: 'POST',
        path: '/api/open-api/token',
        title: '获取 Open API Token',
        description: '插件对接的第一步：使用在面板「Open API」页面创建的应用 App Key 和 App Secret 获取 access_token。Token 有效期 24 小时，过期后需重新调用此接口获取新 Token（无 refresh_token 机制）。',
        auth: 'none',
        bodyParams: [
          { name: 'app_key', type: 'string', required: true, description: '应用 App Key', example: 'a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4' },
          { name: 'app_secret', type: 'string', required: true, description: '应用 App Secret', example: 'e5f6g7h8i9j0e5f6g7h8i9j0e5f6g7h8i9j0e5f6g7h8i9j0e5f6g7h8i9j0k1l2' },
        ],
        responseExample: JSON.stringify({
          data: {
            access_token: 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...',
            token_type: 'Bearer',
            expires_in: 86400,
          },
        }, null, 2),
        responseFields: [
          { name: 'data.access_token', type: 'string', description: 'JWT 访问令牌，有效期 24 小时，需携带在请求头 Authorization: Bearer {token}' },
          { name: 'data.token_type', type: 'string', description: '令牌类型，固定为 Bearer' },
          { name: 'data.expires_in', type: 'integer', description: '令牌有效期（秒），固定为 86400（24小时）' },
        ],
      },
      {
        id: 'plugin-find-env',
        method: 'GET',
        path: '/api/envs',
        title: '查找环境变量',
        description: '通过关键字搜索环境变量，用于判断变量是否已存在。插件通常通过 keyword 参数按变量名搜索，然后遍历结果匹配 remarks 中的用户标识来定位具体变量。',
        auth: 'jwt',
        queryParams: [
          { name: 'keyword', type: 'string', description: '搜索关键字（按变量名或备注模糊匹配）', example: 'sfsyUrl' },
          { name: 'page', type: 'integer', description: '页码，默认 1', example: '1' },
          { name: 'page_size', type: 'integer', description: '每页数量，建议设为 100 以获取完整列表', example: '100' },
        ],
        responseExample: JSON.stringify({
          data: [{
            id: 1,
            name: 'sfsyUrl',
            value: 'sessionId=xxx&_login_mobile_=xxx',
            remarks: '顺丰:账号1丨用户:123丨手机:138****1234丨到期:2026-12-31',
            created_at: '2026-03-13T10:00:00',
            updated_at: '2026-03-13T10:00:00',
          }],
          total: 1,
          page: 1,
          page_size: 100,
        }, null, 2),
        responseFields: [
          { name: 'data', type: 'array', description: '环境变量列表' },
          { name: 'data[].id', type: 'integer', description: '变量 ID，更新和删除时需要' },
          { name: 'data[].name', type: 'string', description: '变量名' },
          { name: 'data[].value', type: 'string', description: '变量值' },
          { name: 'data[].remarks', type: 'string', description: '备注，可存储用户标识等信息' },
          { name: 'total', type: 'integer', description: '总数' },
        ],
      },
      {
        id: 'plugin-add-env',
        method: 'POST',
        path: '/api/envs',
        title: '添加环境变量',
        description: '当查找不到已有变量时，调用此接口添加新的环境变量。注意：呆呆面板不需要对 value 进行 URL 编码，直接传递原始值即可。',
        auth: 'jwt',
        bodyParams: [
          { name: 'name', type: 'string', required: true, description: '变量名', example: 'sfsyUrl' },
          { name: 'value', type: 'string', required: true, description: '变量值（无需 URL 编码）', example: 'sessionId=xxx&_login_mobile_=xxx' },
          { name: 'remarks', type: 'string', description: '备注，建议存储用户标识信息', example: '顺丰:账号1丨用户:123丨手机:138****1234丨到期:2026-12-31' },
          { name: 'group', type: 'string', description: '分组名称', example: '顺丰' },
        ],
        responseExample: JSON.stringify({
          message: '创建成功',
          data: { id: 1, name: 'sfsyUrl', value: 'sessionId=xxx&_login_mobile_=xxx' },
        }, null, 2),
        responseFields: [
          { name: 'message', type: 'string', description: '操作结果消息' },
          { name: 'data', type: 'object', description: '创建的环境变量信息' },
          { name: 'data.id', type: 'integer', description: '新创建的变量 ID' },
        ],
      },
      {
        id: 'plugin-update-env',
        method: 'PUT',
        path: '/api/envs/:id',
        title: '更新环境变量',
        description: '当查找到已有变量时，调用此接口更新变量值。注意：ID 通过 URL 路径传递，不是放在请求体中。',
        auth: 'jwt',
        pathParams: [
          { name: 'id', type: 'integer', required: true, description: '环境变量 ID（通过查找接口获取）' },
        ],
        bodyParams: [
          { name: 'name', type: 'string', description: '变量名', example: 'sfsyUrl' },
          { name: 'value', type: 'string', description: '新的变量值', example: 'sessionId=new_value&_login_mobile_=new_mobile' },
          { name: 'remarks', type: 'string', description: '备注', example: '顺丰:账号1丨用户:123丨手机:138****1234丨到期:2026-12-31' },
          { name: 'group', type: 'string', description: '分组名称', example: '顺丰' },
        ],
        responseExample: JSON.stringify({ message: '更新成功' }, null, 2),
      },
      {
        id: 'plugin-delete-env',
        method: 'DELETE',
        path: '/api/envs/:id',
        title: '删除环境变量',
        description: '删除指定的环境变量。注意：呆呆面板通过路径参数传递 ID，不是请求体数组。',
        auth: 'jwt',
        pathParams: [
          { name: 'id', type: 'integer', required: true, description: '环境变量 ID' },
        ],
        responseExample: JSON.stringify({ message: '删除成功' }, null, 2),
      },
      {
        id: 'plugin-python-example',
        method: 'POST',
        path: '/api/open-api/token',
        title: 'Python 完整示例',
        description: '以下是完整的 Python 类封装 DaiDaiPanel，可直接在插件中使用。通过 Open API 的 App Key 和 App Secret 认证，支持查找/添加/更新/删除环境变量，以及智能的添加或更新逻辑。使用方法：panel = DaiDaiPanel(host, app_key, app_secret)，然后调用 panel.add_or_update_env(name, value, remarks, keyword) 即可自动判断添加或更新。',
        auth: 'none',
        bodyParams: [
          { name: 'host', type: 'string', required: true, description: '面板地址', example: 'http://127.0.0.1:5173' },
          { name: 'app_key', type: 'string', required: true, description: '应用 App Key', example: 'a1b2c3d4e5f6...' },
          { name: 'app_secret', type: 'string', required: true, description: '应用 App Secret', example: 'e5f6g7h8i9j0...' },
        ],
        responseExample: `# DaiDaiPanel 完整示例代码
import requests
from typing import Optional, Dict

class DaiDaiPanel:
    def __init__(self, host: str, app_key: str, app_secret: str):
        self.host = host.rstrip('/')
        self.app_key = app_key
        self.app_secret = app_secret
        self.access_token = None
        self._get_token()

    def _get_token(self):
        url = f'{self.host}/api/open-api/token'
        data = {"app_key": self.app_key, "app_secret": self.app_secret}
        res = requests.post(url, json=data, timeout=10)
        res.raise_for_status()
        result = res.json()
        self.access_token = result['data']['access_token']

    def _headers(self) -> Dict[str, str]:
        return {
            "Authorization": f"Bearer {self.access_token}",
            "Content-Type": "application/json"
        }

    def _request(self, method, url, **kwargs):
        res = getattr(requests, method)(url, headers=self._headers(), timeout=10, **kwargs)
        if res.status_code == 401:
            self._get_token()
            res = getattr(requests, method)(url, headers=self._headers(), timeout=10, **kwargs)
        return res

    def find_env(self, name: str, keyword: str = "") -> Optional[int]:
        url = f"{self.host}/api/envs?keyword={name}&page_size=100"
        res = self._request("get", url)
        for env in res.json().get('data', []):
            if env['name'] == name:
                if not keyword or keyword in env.get('remarks', ''):
                    return env['id']
        return None

    def add_env(self, name: str, value: str, remarks: str = "") -> bool:
        url = f"{self.host}/api/envs"
        data = {"name": name, "value": value, "remarks": remarks}
        res = self._request("post", url, json=data)
        return res.status_code == 200

    def update_env(self, env_id: int, name: str, value: str, remarks: str = "") -> bool:
        url = f"{self.host}/api/envs/{env_id}"
        data = {"name": name, "value": value, "remarks": remarks}
        res = self._request("put", url, json=data)
        return res.status_code == 200

    def delete_env(self, env_id: int) -> bool:
        url = f"{self.host}/api/envs/{env_id}"
        res = self._request("delete", url)
        return res.status_code == 200

    def add_or_update_env(self, name, value, remarks="", keyword=""):
        env_id = self.find_env(name, keyword)
        if env_id:
            return self.update_env(env_id, name, value, remarks)
        return self.add_env(name, value, remarks)

# 使用示例
panel = DaiDaiPanel(
    "http://127.0.0.1:5173",
    "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
    "e5f6g7h8i9j0e5f6g7h8i9j0e5f6g7h8i9j0e5f6g7h8i9j0e5f6g7h8i9j0k1l2"
)
panel.add_or_update_env(
    name="sfsyUrl",
    value="sessionId=xxx&_login_mobile_=xxx",
    remarks="顺丰:账号1丨手机:138****1234丨到期:2026-12-31",
    keyword="账号1"
)`,
        responseFields: [
          { name: 'DaiDaiPanel(host, key, secret)', type: 'class', description: '初始化并通过 Open API 获取 Token' },
          { name: '_get_token()', type: 'method', description: '获取/刷新 Open API Token' },
          { name: '_request(method, url, ...)', type: 'method', description: '自动处理 401 重新获取 Token 的请求封装' },
          { name: 'find_env(name, keyword)', type: 'method', description: '查找环境变量，返回 ID 或 None' },
          { name: 'add_env(name, value, remarks)', type: 'method', description: '添加环境变量' },
          { name: 'update_env(id, name, value, remarks)', type: 'method', description: '更新环境变量' },
          { name: 'delete_env(id)', type: 'method', description: '删除环境变量' },
          { name: 'add_or_update_env(name, value, ...)', type: 'method', description: '智能添加或更新（推荐使用）' },
        ],
      },
      {
        id: 'plugin-notes',
        method: 'GET',
        path: '/docs/plugin-notes',
        title: '注意事项与常见问题',
        description: 'Token 管理：access_token 有效期 24 小时，过期后重新调用 POST /api/open-api/token 获取新 Token（无 refresh_token 机制）。收到 401 错误即表示 Token 过期，需重新获取。错误格式：{"error": "错误信息"} + HTTP 状态码（400/401/403/404/500）。最佳实践：1. 不需要对 value 进行 URL 编码 2. 建议设置 10 秒超时 3. 建议实现 401 自动重新获取 Token 4. 在 remarks 中存储用户标识便于查找 5. 查询时设置较大的 page_size（如 100）减少请求次数。',
        auth: 'none',
        responseExample: JSON.stringify({
          '差异对比': {
            '认证方式': '青龙: Client ID + Secret → 呆呆: App Key + App Secret',
            'Token获取': '青龙: GET /open/auth/token → 呆呆: POST /api/open-api/token',
            'Token有效期': '青龙: 不固定 → 呆呆: 24 小时',
            '添加变量': '青龙: POST /open/envs (数组) → 呆呆: POST /api/envs (对象)',
            '更新变量': '青龙: PUT /open/envs (body带id) → 呆呆: PUT /api/envs/{id} (路径参数)',
            '删除变量': '青龙: DELETE /open/envs (数组) → 呆呆: DELETE /api/envs/{id} (路径参数)',
            'Value编码': '青龙: 需要 URL 编码 → 呆呆: 不需要编码',
          },
          '错误响应格式': { error: '错误信息' },
          'HTTP状态码': { '200': '成功', '400': '参数错误', '401': 'Token 过期或无效', '403': '应用已禁用', '404': '不存在', '500': '服务器错误' },
        }, null, 2),
        responseFields: [
          { name: 'Token 过期', type: 'tip', description: '收到 401 时重新调用 POST /api/open-api/token 获取新 Token' },
          { name: '应用禁用', type: 'tip', description: '收到 403 表示应用已被禁用，需在面板 Open API 页面重新启用' },
          { name: 'Value 编码', type: 'tip', description: '不需要 URL 编码，直接传原始值' },
          { name: '批量操作', type: 'tip', description: '暂不支持批量，需循环调用单个接口' },
          { name: '超时设置', type: 'tip', description: '建议所有请求设置 10 秒超时' },
          { name: '用户标识', type: 'tip', description: '在 remarks 中存储用户标识，查询时通过 keyword 过滤' },
        ],
      },
    ],
  },
  {
    key: 'auth',
    label: '认证',
    endpoints: [
      {
        id: 'auth-login',
        method: 'POST',
        path: '/api/auth/login',
        title: '用户登录',
        description: '使用用户名和密码登录，获取 JWT Token',
        auth: 'none',
        bodyParams: [
          { name: 'username', type: 'string', required: true, description: '用户名', example: 'admin' },
          { name: 'password', type: 'string', required: true, description: '密码', example: 'admin123' },
        ],
        responseExample: JSON.stringify({
          access_token: 'eyJhbGciOi...',
          refresh_token: 'eyJhbGciOi...',
          user: { id: 1, username: 'admin', role: 'admin' },
        }, null, 2),
        responseFields: [
          { name: 'access_token', type: 'string', description: 'JWT 访问令牌' },
          { name: 'refresh_token', type: 'string', description: 'JWT 刷新令牌' },
          { name: 'user', type: 'object', description: '用户信息' },
        ],
      },
      {
        id: 'auth-refresh',
        method: 'POST',
        path: '/api/auth/refresh',
        title: '刷新令牌',
        description: '使用 refresh_token 获取新的 access_token',
        auth: 'jwt',
        responseExample: JSON.stringify({ access_token: 'eyJhbGciOi...' }, null, 2),
      },
      {
        id: 'auth-me',
        method: 'GET',
        path: '/api/auth/user',
        title: '获取当前用户信息',
        description: '获取当前登录用户的详细信息',
        auth: 'jwt',
        responseExample: JSON.stringify({
          user: { id: 1, username: 'admin', role: 'admin', enabled: true, created_at: '2026-01-01T00:00:00' },
        }, null, 2),
      },
      {
        id: 'auth-password',
        method: 'PUT',
        path: '/api/auth/password',
        title: '修改密码',
        description: '修改当前用户密码',
        auth: 'jwt',
        bodyParams: [
          { name: 'old_password', type: 'string', required: true, description: '当前密码' },
          { name: 'new_password', type: 'string', required: true, description: '新密码（至少8位）' },
        ],
        responseExample: JSON.stringify({ message: '密码修改成功' }, null, 2),
      },
    ],
  },
  {
    key: 'tasks',
    label: '定时任务',
    endpoints: [
      {
        id: 'tasks-list',
        method: 'GET',
        path: '/api/tasks',
        title: '获取任务列表',
        description: '获取所有定时任务，支持关键字搜索和分页',
        auth: 'jwt',
        queryParams: [
          { name: 'keyword', type: 'string', description: '搜索关键字' },
          { name: 'page', type: 'integer', description: '页码，默认 1', example: '1' },
          { name: 'page_size', type: 'integer', description: '每页数量，默认 20', example: '20' },
        ],
        responseExample: JSON.stringify({
          data: [{
            id: 1,
            name: '签到任务',
            command: 'task sign.py',
            cron_expression: '0 9 * * *',
            status: 1,
            last_run_status: 0,
            last_run_at: '2026-03-10T09:00:00',
            notify_on_abort: false,
            notification_channel_id: 3,
            notification_channel_name: 'Telegram 主通道',
          }],
          total: 1, page: 1, page_size: 20,
        }, null, 2),
      },
      {
        id: 'tasks-create',
        method: 'POST',
        path: '/api/tasks',
        title: '创建任务',
        description: '创建新的定时任务',
        auth: 'jwt',
        bodyParams: [
          { name: 'name', type: 'string', required: true, description: '任务名称', example: '签到任务' },
          { name: 'command', type: 'string', required: true, description: '执行命令', example: 'task sign.py' },
          { name: 'task_type', type: 'string', description: '任务类型：cron / manual / startup', example: 'cron' },
          { name: 'cron_expression', type: 'string', description: 'Cron 表达式；task_type=cron 时必填', example: '0 9 * * *' },
          { name: 'timeout', type: 'integer', description: '超时时间（秒），0 表示永不超时', example: '0' },
          { name: 'max_retries', type: 'integer', description: '最大重试次数', example: '0' },
          { name: 'retry_interval', type: 'integer', description: '重试间隔（秒）', example: '5' },
          { name: 'notify_on_failure', type: 'boolean', description: '失败时通知', example: 'true' },
          { name: 'notify_on_success', type: 'boolean', description: '成功时通知', example: 'false' },
          { name: 'notify_on_abort', type: 'boolean', description: '主动终止时通知', example: 'false' },
          { name: 'notification_channel_id', type: 'integer', description: '指定通知渠道 ID，留空则发送到全部启用渠道', example: '3' },
        ],
        responseExample: JSON.stringify({ message: '创建成功', data: { id: 1, name: '签到任务' } }, null, 2),
      },
      {
        id: 'tasks-update',
        method: 'PUT',
        path: '/api/tasks/:id',
        title: '更新任务',
        description: '更新指定任务的配置',
        auth: 'jwt',
        pathParams: [{ name: 'id', type: 'integer', required: true, description: '任务 ID' }],
        bodyParams: [
          { name: 'name', type: 'string', description: '任务名称' },
          { name: 'command', type: 'string', description: '执行命令' },
          { name: 'task_type', type: 'string', description: '任务类型：cron / manual / startup' },
          { name: 'cron_expression', type: 'string', description: 'Cron 表达式' },
          { name: 'timeout', type: 'integer', description: '超时时间（秒），0 表示永不超时' },
          { name: 'max_retries', type: 'integer', description: '最大重试次数' },
          { name: 'retry_interval', type: 'integer', description: '重试间隔（秒）' },
          { name: 'notify_on_failure', type: 'boolean', description: '失败时通知' },
          { name: 'notify_on_success', type: 'boolean', description: '成功时通知' },
          { name: 'notify_on_abort', type: 'boolean', description: '主动终止时通知' },
          { name: 'notification_channel_id', type: 'integer', description: '指定通知渠道 ID，设为 null 则恢复全部启用渠道' },
        ],
        responseExample: JSON.stringify({ message: '更新成功' }, null, 2),
      },
      {
        id: 'tasks-delete',
        method: 'DELETE',
        path: '/api/tasks/:id',
        title: '删除任务',
        description: '删除指定任务',
        auth: 'jwt',
        pathParams: [{ name: 'id', type: 'integer', required: true, description: '任务 ID' }],
        responseExample: JSON.stringify({ message: '删除成功' }, null, 2),
      },
      {
        id: 'tasks-run',
        method: 'PUT',
        path: '/api/tasks/:id/run',
        title: '立即执行任务',
        description: '立即触发执行指定任务',
        auth: 'jwt',
        pathParams: [{ name: 'id', type: 'integer', required: true, description: '任务 ID' }],
        responseExample: JSON.stringify({ message: '任务已开始执行' }, null, 2),
      },
      {
        id: 'tasks-stop',
        method: 'PUT',
        path: '/api/tasks/:id/stop',
        title: '停止任务',
        description: '停止正在执行的任务',
        auth: 'jwt',
        pathParams: [{ name: 'id', type: 'integer', required: true, description: '任务 ID' }],
        responseExample: JSON.stringify({ message: '任务已停止' }, null, 2),
      },
    ],
  },
  {
    key: 'logs',
    label: '执行日志',
    endpoints: [
      {
        id: 'logs-list',
        method: 'GET',
        path: '/api/logs',
        title: '获取日志列表',
        description: '获取任务执行日志，支持按状态过滤和分页',
        auth: 'jwt',
        queryParams: [
          { name: 'status', type: 'integer', description: '状态过滤：0=成功, 1=失败, 2=运行中, 3=已终止' },
          { name: 'page', type: 'integer', description: '页码', example: '1' },
          { name: 'page_size', type: 'integer', description: '每页数量', example: '20' },
        ],
        responseExample: JSON.stringify({
          data: [{ id: 1, task_id: 1, task_name: '签到任务', status: 0, content: '执行成功', duration: 2.5, started_at: '2026-03-10T09:00:00' }],
          total: 1,
        }, null, 2),
      },
      {
        id: 'logs-detail',
        method: 'GET',
        path: '/api/logs/:id',
        title: '获取日志详情',
        description: '获取单条日志的详细内容',
        auth: 'jwt',
        pathParams: [{ name: 'id', type: 'integer', required: true, description: '日志 ID' }],
        responseExample: JSON.stringify({
          id: 1, task_id: 1, task_name: '签到任务', status: 0, content: '签到成功\n获得 10 积分', duration: 2.5, started_at: '2026-03-10T09:00:00', ended_at: '2026-03-10T09:00:02',
        }, null, 2),
      },
      {
        id: 'logs-delete',
        method: 'DELETE',
        path: '/api/logs/:id',
        title: '删除日志',
        description: '删除指定日志记录',
        auth: 'jwt',
        pathParams: [{ name: 'id', type: 'integer', required: true, description: '日志 ID' }],
        responseExample: JSON.stringify({ message: '删除成功' }, null, 2),
      },
    ],
  },
  {
    key: 'scripts',
    label: '脚本管理',
    endpoints: [
      {
        id: 'scripts-list',
        method: 'GET',
        path: '/api/scripts',
        title: '获取脚本列表',
        description: '获取所有脚本文件列表',
        auth: 'jwt',
        responseExample: JSON.stringify({ data: [{ path: 'sign.py', size: 1024, modified: '2026-03-10T00:00:00' }] }, null, 2),
      },
      {
        id: 'scripts-content',
        method: 'GET',
        path: '/api/scripts/content?path=:path',
        title: '获取脚本内容',
        description: '获取指定脚本的文件内容',
        auth: 'jwt',
        queryParams: [{ name: 'path', type: 'string', required: true, description: '脚本路径', example: 'sign.py' }],
        responseExample: JSON.stringify({ data: { path: 'sign.py', content: 'print("hello")', binary: false, is_binary: false } }, null, 2),
      },
      {
        id: 'scripts-save',
        method: 'PUT',
        path: '/api/scripts/content',
        title: '保存脚本内容',
        description: '创建或更新脚本文件',
        auth: 'jwt',
        bodyParams: [
          { name: 'path', type: 'string', required: true, description: '脚本路径' },
          { name: 'content', type: 'string', required: true, description: '脚本内容' },
        ],
        responseExample: JSON.stringify({ message: '保存成功' }, null, 2),
      },
      {
        id: 'scripts-delete',
        method: 'DELETE',
        path: '/api/scripts/:path',
        title: '删除脚本',
        description: '删除指定脚本文件',
        auth: 'jwt',
        pathParams: [{ name: 'path', type: 'string', required: true, description: '脚本路径' }],
        responseExample: JSON.stringify({ message: '删除成功' }, null, 2),
      },
      {
        id: 'scripts-upload',
        method: 'POST',
        path: '/api/scripts/upload',
        title: '上传脚本',
        description: '上传脚本文件，支持 .py/.js/.sh/.ts',
        auth: 'jwt',
        bodyParams: [{ name: 'file', type: 'file', required: true, description: '脚本文件（multipart/form-data）' }],
        responseExample: JSON.stringify({ message: '上传成功', data: { path: 'sign.py' } }, null, 2),
      },
    ],
  },
  {
    key: 'envs',
    label: '环境变量',
    endpoints: [
      {
        id: 'envs-list',
        method: 'GET',
        path: '/api/envs',
        title: '获取所有环境变量',
        description: '获取环境变量列表，支持搜索和分页；带 all=1 时一次返回全部（硬上限 5000）',
        auth: 'jwt',
        queryParams: [
          { name: 'keyword', type: 'string', description: '搜索关键字' },
          { name: 'page', type: 'integer', description: '页码', example: '1' },
          { name: 'page_size', type: 'integer', description: '每页数量', example: '20' },
          { name: 'all', type: 'integer', description: '设为 1 时一次返回全部（最多 5000 条）', example: '1' },
        ],
        responseExample: JSON.stringify({ data: [{ id: 1, name: 'MY_TOKEN', value: 'abc123', enabled: true, remarks: '签到Token' }], total: 1 }, null, 2),
        responseFields: [
          { name: 'id', type: 'integer', description: '变量 ID' },
          { name: 'name', type: 'string', description: '变量名' },
          { name: 'value', type: 'string', description: '变量值' },
          { name: 'enabled', type: 'boolean', description: '是否启用' },
          { name: 'remarks', type: 'string', description: '备注' },
        ],
      },
      {
        id: 'envs-get',
        method: 'GET',
        path: '/api/envs/:id',
        title: '按 ID 获取环境变量',
        description: '根据变量 ID 直接返回单条环境变量详情，避免按 keyword 反复查询匹配。',
        auth: 'jwt',
        pathParams: [{ name: 'id', type: 'integer', required: true, description: '变量 ID' }],
        responseExample: JSON.stringify({ data: { id: 1, name: 'MY_TOKEN', value: 'abc123', enabled: true, remarks: '签到Token', group: '京东' } }, null, 2),
      },
      {
        id: 'envs-create',
        method: 'POST',
        path: '/api/envs',
        title: '创建环境变量',
        description: '创建新的环境变量',
        auth: 'jwt',
        bodyParams: [
          { name: 'name', type: 'string', required: true, description: '变量名', example: 'MY_TOKEN' },
          { name: 'value', type: 'string', required: true, description: '变量值', example: 'abc123' },
          { name: 'remarks', type: 'string', description: '备注' },
          { name: 'group', type: 'string', description: '分组名称' },
        ],
        responseExample: JSON.stringify({ message: '创建成功', data: { id: 1 } }, null, 2),
      },
      {
        id: 'envs-update',
        method: 'PUT',
        path: '/api/envs/:id',
        title: '更新环境变量',
        description: '更新指定环境变量',
        auth: 'jwt',
        pathParams: [{ name: 'id', type: 'integer', required: true, description: '变量 ID' }],
        bodyParams: [
          { name: 'name', type: 'string', description: '变量名' },
          { name: 'value', type: 'string', description: '变量值' },
          { name: 'remarks', type: 'string', description: '备注' },
          { name: 'group', type: 'string', description: '分组名称' },
        ],
        responseExample: JSON.stringify({ message: '更新成功' }, null, 2),
      },
      {
        id: 'envs-delete',
        method: 'DELETE',
        path: '/api/envs/:id',
        title: '删除环境变量',
        description: '删除指定环境变量',
        auth: 'jwt',
        pathParams: [{ name: 'id', type: 'integer', required: true, description: '变量 ID' }],
        responseExample: JSON.stringify({ message: '删除成功' }, null, 2),
      },
    ],
  },
  {
    key: 'subscriptions',
    label: '订阅管理',
    endpoints: [
      {
        id: 'subs-list',
        method: 'GET',
        path: '/api/subscriptions',
        title: '获取订阅列表',
        description: '获取所有仓库订阅',
        auth: 'jwt',
        responseExample: JSON.stringify({ data: [{ id: 1, name: '示例仓库', url: 'https://github.com/user/repo.git', branch: 'main', schedule: '0 0 * * *', enabled: true }] }, null, 2),
      },
      {
        id: 'subs-create',
        method: 'POST',
        path: '/api/subscriptions',
        title: '创建订阅',
        description: '创建新的仓库订阅',
        auth: 'jwt',
        bodyParams: [
          { name: 'name', type: 'string', required: true, description: '订阅名称' },
          { name: 'url', type: 'string', required: true, description: '仓库 URL（HTTP/HTTPS）' },
          { name: 'branch', type: 'string', description: '分支，默认 main', example: 'main' },
          { name: 'schedule', type: 'string', description: 'Cron 表达式', example: '0 0 * * *' },
          { name: 'whitelist', type: 'string', description: '白名单 glob（逗号分隔）' },
          { name: 'blacklist', type: 'string', description: '黑名单 glob（逗号分隔）' },
          { name: 'target_dir', type: 'string', description: '存放子目录' },
        ],
        responseExample: JSON.stringify({ message: '创建成功', data: { id: 1 } }, null, 2),
      },
      {
        id: 'subs-update',
        method: 'PUT',
        path: '/api/subscriptions/:id',
        title: '更新订阅',
        description: '更新指定订阅配置',
        auth: 'jwt',
        pathParams: [{ name: 'id', type: 'integer', required: true, description: '订阅 ID' }],
        responseExample: JSON.stringify({ message: '更新成功' }, null, 2),
      },
      {
        id: 'subs-delete',
        method: 'DELETE',
        path: '/api/subscriptions/:id',
        title: '删除订阅',
        description: '删除指定订阅',
        auth: 'jwt',
        pathParams: [{ name: 'id', type: 'integer', required: true, description: '订阅 ID' }],
        responseExample: JSON.stringify({ message: '删除成功' }, null, 2),
      },
      {
        id: 'subs-pull',
        method: 'PUT',
        path: '/api/subscriptions/:id/pull',
        title: '手动拉取订阅',
        description: '立即拉取指定订阅的脚本。拉取时是否覆盖本地修改由订阅设置中的「覆盖拉取」开关控制，本接口不再接收临时覆盖模式参数。',
        auth: 'jwt',
        pathParams: [{ name: 'id', type: 'integer', required: true, description: '订阅 ID' }],
        responseExample: JSON.stringify({ message: '拉取成功，拉取 5 个文件，新增 3 个任务' }, null, 2),
      },
    ],
  },
  {
    key: 'notifications',
    label: '通知渠道',
    endpoints: [
      {
        id: 'notify-list',
        method: 'GET',
        path: '/api/notifications',
        title: '获取通知渠道列表',
        description: '获取所有通知渠道配置',
        auth: 'jwt',
        responseExample: JSON.stringify({ data: [{ id: 1, name: '钉钉通知', type: 'dingtalk', config: { token: '***' }, enabled: true }] }, null, 2),
      },
      {
        id: 'notify-create',
        method: 'POST',
        path: '/api/notifications',
        title: '创建通知渠道',
        description: '创建新的通知渠道，支持：webhook / email / telegram(支持 api_host、proxy 单独代理) / dingtalk / wecom(企业微信机器人，支持 text/markdown/markdown_v2/image/news/template_card) / wecom_app(企业微信应用，支持 text/markdown/image/file/video/news/template_card，并支持 base_url 反代基础地址) / bark / pushplus / serverchan / feishu / gotify / pushdeer / pushme / chanify / igot / qmsg / pushover / discord / slack / ntfy / wxpusher(WxPusher / ClawBot(iLink)，支持 url、verify_pay_type) / custom',
        auth: 'jwt',
        bodyParams: [
          { name: 'name', type: 'string', required: true, description: '渠道名称' },
          { name: 'type', type: 'string', required: true, description: '渠道类型', example: 'dingtalk' },
          { name: 'config', type: 'object', required: true, description: '渠道配置（各类型字段不同，例如 email 可填 smtp_ssl，telegram 可填 proxy，wecom_app 可填 base_url，wxpusher 可填 url / verify_pay_type）' },
        ],
        responseExample: JSON.stringify({ message: '创建成功', data: { id: 1 } }, null, 2),
      },
      {
        id: 'notify-update',
        method: 'PUT',
        path: '/api/notifications/:id',
        title: '更新通知渠道',
        description: '更新指定通知渠道',
        auth: 'jwt',
        pathParams: [{ name: 'id', type: 'integer', required: true, description: '渠道 ID' }],
        responseExample: JSON.stringify({ message: '更新成功' }, null, 2),
      },
      {
        id: 'notify-delete',
        method: 'DELETE',
        path: '/api/notifications/:id',
        title: '删除通知渠道',
        description: '删除指定通知渠道',
        auth: 'jwt',
        pathParams: [{ name: 'id', type: 'integer', required: true, description: '渠道 ID' }],
        responseExample: JSON.stringify({ message: '删除成功' }, null, 2),
      },
      {
        id: 'notify-test',
        method: 'POST',
        path: '/api/notifications/:id/test',
        title: '测试发送',
        description: '向指定渠道发送测试通知',
        auth: 'jwt',
        pathParams: [{ name: 'id', type: 'integer', required: true, description: '渠道 ID' }],
        responseExample: JSON.stringify({ message: '测试通知发送成功' }, null, 2),
      },
      {
        id: 'notify-send',
        method: 'POST',
        path: '/api/notifications/send',
        title: '脚本发送通知',
        description: '供脚本或外部程序主动调用系统通知配置进行推送。支持普通用户 JWT，也支持带 notifications scope 的 Open API Bearer Token。未指定 channel_id / channel_ids 时，会发送到全部已启用通知渠道；任务自带默认通知渠道时，helper 会优先落到该渠道，传 ignore_default_config=true 可跳过默认渠道。面板运行脚本时会统一在脚本根目录提供 notify.py 和 sendNotify.js，不再向每个脚本子目录复制，可直接按青龙风格先收集 notifyStr 再发送。',
        auth: 'jwt',
        bodyParams: [
          { name: 'title', type: 'string', required: true, description: '通知标题', example: '签到脚本通知' },
          { name: 'content', type: 'string', required: true, description: '通知正文，通常传 notifyStr.join(\'\\n\') 或 "\\n".join(notify_lines)', example: '签到成功\\n账号: user01\\n积分: +20' },
          { name: 'channel_id', type: 'integer', description: '单个通知渠道 ID，可选', example: '3' },
          { name: 'channel_ids', type: 'array', description: '多个通知渠道 ID，可选', example: '[3,5]' },
          { name: 'context', type: 'object', description: '额外模板变量，可选；供 content_template 使用', example: '{"task_name":"签到脚本","status":"success"}' },
        ],
        helperExamples: {
          JavaScript: `const { sendNotify } = require('./sendNotify')

async function main() {
  const name = '京东签到'
  const notifyStr = []
  notifyStr.push('签到成功')
  notifyStr.push('账号: user01')
  notifyStr.push('积分: +20')
  await sendNotify(name, notifyStr.join('\\n'))

  // 如需忽略任务默认通知渠道：
  // await sendNotify(name, notifyStr.join('\\n'), { channel_ids: [1, 2], ignore_default_config: true })
}

main().catch(console.error)`,
          Python: `from notify import send

def main():
    name = '京东签到'
    notify_lines = []
    notify_lines.append('签到成功')
    notify_lines.append('账号: user01')
    notify_lines.append('积分: +20')
    send(name, '\\n'.join(notify_lines))

    # 如需忽略任务默认通知渠道：
    # send(name, '\\n'.join(notify_lines), ignore_default_config=True, channel_ids=[1, 2])

if __name__ == '__main__':
    main()`,
        },
        responseExample: JSON.stringify({
          message: '通知发送完成，成功 1 个渠道',
          data: {
            sent_count: 1,
            failed_count: 0,
            channel_names: ['Telegram 主通道'],
            errors: [],
            requested_ids: [3],
            used_all: false,
            content_length: 18,
          },
        }, null, 2),
      },
    ],
  },
  {
    key: 'deps',
    label: '依赖管理',
    endpoints: [
      {
        id: 'deps-list',
        method: 'GET',
        path: '/api/deps',
        title: '获取依赖列表',
        description: '按类型获取依赖列表',
        auth: 'jwt',
        queryParams: [
          { name: 'type', type: 'string', required: true, description: '依赖类型：nodejs / python / linux', example: 'python' },
        ],
        responseExample: JSON.stringify({ data: [{ id: 1, type: 'python', name: 'requests', status: 'installed' }], total: 1 }, null, 2),
      },
      {
        id: 'deps-create',
        method: 'POST',
        path: '/api/deps',
        title: '安装依赖',
        description: '按类型批量提交依赖安装任务',
        auth: 'jwt',
        bodyParams: [
          { name: 'type', type: 'string', required: true, description: '依赖类型', example: 'python' },
          { name: 'names', type: 'array', required: true, description: '依赖名称数组', example: '["requests"]' },
        ],
        responseExample: JSON.stringify({ message: '已提交 1 个依赖安装', data: [{ id: 1, type: 'python', name: 'requests', status: 'installing' }] }, null, 2),
      },
      {
        id: 'deps-delete',
        method: 'DELETE',
        path: '/api/deps/:id',
        title: '卸载依赖',
        description: '卸载指定依赖，支持 force 强制卸载',
        auth: 'jwt',
        pathParams: [{ name: 'id', type: 'integer', required: true, description: '依赖 ID' }],
        queryParams: [{ name: 'force', type: 'boolean', description: '是否强制卸载', example: 'true' }],
        responseExample: JSON.stringify({ message: '卸载中' }, null, 2),
      },
      {
        id: 'deps-status',
        method: 'GET',
        path: '/api/deps/:id/status',
        title: '获取依赖状态',
        description: '获取单个依赖的状态和日志',
        auth: 'jwt',
        pathParams: [{ name: 'id', type: 'integer', required: true, description: '依赖 ID' }],
        responseExample: JSON.stringify({ data: { id: 1, type: 'python', name: 'requests', status: 'installed', log: 'install ok' } }, null, 2),
      },
      {
        id: 'deps-reinstall',
        method: 'PUT',
        path: '/api/deps/:id/reinstall',
        title: '重新安装依赖',
        description: '重新安装指定依赖',
        auth: 'jwt',
        pathParams: [{ name: 'id', type: 'integer', required: true, description: '依赖 ID' }],
        responseExample: JSON.stringify({ message: '重新安装中' }, null, 2),
      },
    ],
  },
  {
    key: 'users',
    label: '用户管理',
    endpoints: [
      {
        id: 'users-list',
        method: 'GET',
        path: '/api/users',
        title: '获取用户列表',
        description: '获取所有用户（仅管理员）',
        auth: 'jwt',
        responseExample: JSON.stringify({ data: [{ id: 1, username: 'admin', role: 'admin', enabled: true, last_login_at: '2026-03-10T00:00:00' }] }, null, 2),
      },
      {
        id: 'users-create',
        method: 'POST',
        path: '/api/users',
        title: '创建用户',
        description: '创建新用户（仅管理员）',
        auth: 'jwt',
        bodyParams: [
          { name: 'username', type: 'string', required: true, description: '用户名' },
          { name: 'password', type: 'string', required: true, description: '密码（至少8位）' },
          { name: 'role', type: 'string', required: true, description: '角色：admin / operator / viewer' },
        ],
        responseExample: JSON.stringify({ message: '创建成功', data: { id: 2 } }, null, 2),
      },
      {
        id: 'users-update',
        method: 'PUT',
        path: '/api/users/:id',
        title: '更新用户',
        description: '更新用户信息（仅管理员）',
        auth: 'jwt',
        pathParams: [{ name: 'id', type: 'integer', required: true, description: '用户 ID' }],
        bodyParams: [
          { name: 'role', type: 'string', description: '角色' },
          { name: 'enabled', type: 'boolean', description: '是否启用' },
          { name: 'password', type: 'string', description: '新密码' },
        ],
        responseExample: JSON.stringify({ message: '更新成功' }, null, 2),
      },
      {
        id: 'users-delete',
        method: 'DELETE',
        path: '/api/users/:id',
        title: '删除用户',
        description: '删除指定用户（仅管理员，不能删除自己）',
        auth: 'jwt',
        pathParams: [{ name: 'id', type: 'integer', required: true, description: '用户 ID' }],
        responseExample: JSON.stringify({ message: '删除成功' }, null, 2),
      },
    ],
  },
  {
    key: 'open',
    label: '开放 API',
    endpoints: [
      {
        id: 'open-apps-list',
        method: 'GET',
        path: '/api/open-api/apps',
        title: '获取应用列表',
        description: '获取所有开放 API 应用',
        auth: 'jwt',
        responseExample: JSON.stringify({ data: [{ id: 1, name: '自动化工具', client_id: 'cid_xxx', client_secret: 'cs_xxx', enabled: true, scopes: 'tasks,envs,notifications' }] }, null, 2),
      },
      {
        id: 'open-token',
        method: 'POST',
        path: '/api/open-api/token',
        title: '获取开放 API Token',
        description: '使用 App Key 和 App Secret 获取访问令牌，有效期 24 小时',
        auth: 'none',
        bodyParams: [
          { name: 'app_key', type: 'string', required: true, description: 'App Key' },
          { name: 'app_secret', type: 'string', required: true, description: 'App Secret' },
        ],
        responseExample: JSON.stringify({ data: { access_token: 'eyJhbGciOi...', token_type: 'Bearer', expires_in: 86400 } }, null, 2),
      },
    ],
  },
  {
    key: 'config',
    label: '系统配置',
    endpoints: [
      {
        id: 'config-get',
        method: 'GET',
        path: '/api/configs',
        title: '获取全部系统配置',
        description: '获取所有系统配置项及其值',
        auth: 'jwt',
        responseExample: JSON.stringify({ data: { auto_add_cron: { key: 'auto_add_cron', value: 'true', description: '订阅拉取时自动创建任务' }, proxy_url: { key: 'proxy_url', value: '', description: '代理地址' } } }, null, 2),
      },
      {
        id: 'config-update',
        method: 'PUT',
        path: '/api/configs/batch',
        title: '批量更新配置',
        description: '批量更新系统配置（仅管理员）',
        auth: 'jwt',
        bodyParams: [
          { name: 'configs', type: 'object', required: true, description: '配置键值对', example: '{ "proxy_url": "http://127.0.0.1:7890" }' },
        ],
        responseExample: JSON.stringify({ message: '配置已保存' }, null, 2),
      },
    ],
  },
  {
    key: 'system',
    label: '系统信息',
    endpoints: [
      {
        id: 'system-info',
        method: 'GET',
        path: '/api/system/info',
        title: '获取系统信息',
        description: '获取服务器系统信息（CPU/内存/磁盘/机器码等）',
        auth: 'jwt',
        responseExample: JSON.stringify({
          data: { hostname: 'server', machine_code: 'A1B2C3D4-E5F6-4789-ABCD-1234567890AB', os: 'linux', arch: 'amd64', go_version: 'go1.22.0', num_cpu: 4, cpu_usage: 25.0, memory_total: 8589934592, memory_used: 4294967296, memory_usage: 50.0, disk_total: 107374182400, disk_used: 53687091200, disk_usage: 50.0 },
        }, null, 2),
      },
      {
        id: 'system-machine-code',
        method: 'GET',
        path: '/api/system/machine-code',
        title: '获取面板机器码',
        description: '直接读取面板首次启动生成的机器码（UUID v4 大写格式）。不再需要手工查 SQL，OpenAPI 应用具备 system 权限即可调用。',
        auth: 'jwt',
        responseExample: JSON.stringify({ data: { machine_code: 'A1B2C3D4-E5F6-4789-ABCD-1234567890AB' } }, null, 2),
      },
      {
        id: 'system-stats',
        method: 'GET',
        path: '/api/system/stats',
        title: '获取面板统计',
        description: '获取任务、日志、脚本的统计数据',
        auth: 'jwt',
        responseExample: JSON.stringify({
          data: { tasks: { total: 10, enabled: 8, disabled: 2, running: 1 }, logs: { total: 105, success: 90, failed: 10, aborted: 5, success_rate: 90.0 }, scripts: { total: 15 } },
        }, null, 2),
      },
    ],
  },
  {
    key: 'backup',
    label: '系统备份',
    endpoints: [
      {
        id: 'backup-create',
        method: 'POST',
        path: '/api/system/backup',
        title: '一键创建备份',
        description: '创建数据库与脚本数据备份；支持管理员账号 Token 或 OpenAPI 应用（需 backup 权限）调用。',
        auth: 'jwt',
        bodyParams: [
          { name: 'name', type: 'string', required: false, example: 'auto-backup', description: '备份文件名前缀，可省略由系统自动生成' },
          { name: 'password', type: 'string', required: false, example: '', description: '可选加密密码，留空则导出明文 .json 文件' },
          { name: 'selection', type: 'object', required: false, example: '{}', description: '可选模块选择，留空导出全部内容' },
        ],
        responseExample: JSON.stringify({ data: { path: 'data/backups/daidai-backup-20260101-120000.json' }, message: '备份成功' }, null, 2),
      },
      {
        id: 'backup-list',
        method: 'GET',
        path: '/api/system/backups',
        title: '获取备份列表',
        description: '列出当前面板已生成的全部备份文件',
        auth: 'jwt',
        responseExample: JSON.stringify({ data: [{ filename: 'daidai-backup-20260101.json', size: 102400, modified_at: '2026-01-01T12:00:00Z' }] }, null, 2),
      },
      {
        id: 'backup-delete',
        method: 'DELETE',
        path: '/api/system/backup',
        title: '删除备份文件',
        description: '按文件名删除一个备份文件（filename 通过 query 传递）',
        auth: 'jwt',
        responseExample: JSON.stringify({ message: '删除成功' }, null, 2),
      },
    ],
  },
]

export function generateCodeExamples(endpoint: ApiEndpoint): Record<string, string> {
  const { method, path, bodyParams, auth } = endpoint
  const url = `${getApiBaseOrigin()}${path}`
  const hasBody = bodyParams && bodyParams.length > 0 && method !== 'GET'

  const bodyObj: Record<string, any> = {}
  if (hasBody) {
    bodyParams!.forEach(p => {
      if (p.example) {
        if (p.type === 'integer') {
          bodyObj[p.name] = Number(p.example)
        } else if (p.type === 'boolean') {
          bodyObj[p.name] = p.example === 'true'
        } else if (p.type === 'array' || p.type === 'object') {
          try {
            bodyObj[p.name] = JSON.parse(p.example)
          } catch {
            bodyObj[p.name] = p.example
          }
        } else {
          bodyObj[p.name] = p.example
        }
      } else {
        bodyObj[p.name] = p.type === 'integer' ? 0 : p.type === 'boolean' ? false : p.type === 'array' ? [] : p.type === 'object' ? {} : ''
      }
    })
  }
  const bodyJson = JSON.stringify(bodyObj, null, 2)

  const authHeader = auth === 'jwt' ? `-H "Authorization: Bearer <TOKEN>"` : ''

  let shell = `curl -X ${method} "${url}"`
  if (authHeader) shell += ` \\\n  ${authHeader}`
  if (hasBody) shell += ` \\\n  -H "Content-Type: application/json" \\\n  -d '${bodyJson}'`

  let js = `const res = await fetch("${url}", {\n  method: "${method}",\n  headers: {\n    "Content-Type": "application/json",`
  if (auth === 'jwt') js += `\n    "Authorization": "Bearer <TOKEN>",`
  js += `\n  },`
  if (hasBody) js += `\n  body: JSON.stringify(${bodyJson}),`
  js += `\n})\nconst data = await res.json()\nconsole.log(data)`

  let python = `import requests\n\n`
  if (auth === 'jwt') python += `headers = {"Authorization": "Bearer <TOKEN>"}\n`
  if (hasBody) {
    python += `data = ${bodyJson}\n`
    python += `res = requests.${method.toLowerCase()}("${url}"${auth === 'jwt' ? ', headers=headers' : ''}, json=data)\n`
  } else {
    python += `res = requests.${method.toLowerCase()}("${url}"${auth === 'jwt' ? ', headers=headers' : ''})\n`
  }
  python += `print(res.json())`

  return { Shell: shell, JavaScript: js, Python: python }
}

export function getApiBaseOrigin(): string {
  if (typeof window !== 'undefined' && window.location?.origin) {
    return window.location.origin
  }
  return 'http://localhost:5701'
}
