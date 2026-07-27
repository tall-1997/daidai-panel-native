import http from 'node:http'

const tasksMock = [
  { id: 1, name: '每日数据备份', command: 'script/backup.sh', task_type: 'cron', cron_expression: '0 2 * * *', cron_expressions: ['0 2 * * *'], status: 1, is_pinned: true, last_run_at: '2024-06-21T02:00:00Z', next_run_at: '2024-06-22T02:00:00Z', last_run_status: 0, last_running_time: 12.3, labels: ['备份'] },
  { id: 2, name: '清理临时文件', command: 'rm -rf /tmp/cache/*', task_type: 'cron', cron_expression: '0 3 * * *', cron_expressions: ['0 3 * * *'], status: 1, is_pinned: false, last_run_at: '2024-06-21T03:00:00Z', next_run_at: '2024-06-22T03:00:00Z', last_run_status: 0, last_running_time: 2.1, labels: [] },
  { id: 3, name: '同步配置文件', command: 'script/sync-config.sh', task_type: 'cron', cron_expression: '*/30 * * * *', cron_expressions: ['*/30 * * * *'], status: 2, is_pinned: false, last_run_at: '2024-06-21T10:30:00Z', next_run_at: '2024-06-21T11:00:00Z', last_run_status: 1, last_running_time: 5.4, labels: ['配置'] },
  { id: 4, name: '检查SSL证书', command: 'script/check-ssl.sh', task_type: 'cron', cron_expression: '0 7 * * *', cron_expressions: ['0 7 * * *'], status: 1, is_pinned: false, last_run_at: '2024-06-21T07:00:00Z', next_run_at: '2024-06-22T07:00:00Z', last_run_status: 0, last_running_time: 1.2, labels: ['监控'] },
  { id: 5, name: '更新IP数据库', command: 'script/update-ipdb.js', task_type: 'cron', cron_expression: '0 23 * * 0', cron_expressions: ['0 23 * * 0'], status: 1, is_pinned: false, last_run_at: '2024-06-20T23:00:00Z', next_run_at: '2024-06-27T23:00:00Z', last_run_status: 0, last_running_time: 8.7, labels: [] },
  { id: 6, name: '数据库优化', command: 'script/db-optimize.sh', task_type: 'cron', cron_expression: '0 4 * * 0', cron_expressions: ['0 4 * * 0'], status: 0, is_pinned: false, last_run_at: '2024-06-16T04:00:00Z', next_run_at: null, last_run_status: 0, last_running_time: 45.2, labels: ['数据库'] },
  { id: 7, name: '发送日报', command: 'python3 /opt/scripts/daily_report.py', task_type: 'cron', cron_expression: '0 9 * * 1-5', cron_expressions: ['0 9 * * 1-5'], status: 1, is_pinned: false, last_run_at: '2024-06-21T09:00:00Z', next_run_at: '2024-06-24T09:00:00Z', last_run_status: 0, last_running_time: 3.8, labels: ['通知'] },
  { id: 8, name: '系统健康检查', command: 'script/health-check.sh', task_type: 'cron', cron_expression: '*/5 * * * *', cron_expressions: ['*/5 * * * *'], status: 2, is_pinned: true, last_run_at: '2024-06-21T10:35:00Z', next_run_at: '2024-06-21T10:40:00Z', last_run_status: 0, last_running_time: 0.8, labels: ['监控'] },
]

const mockData = {
  '/api/auth/user': { user: { id: 1, username: 'linzzxxxx', role: 'admin', enabled: true, avatar_url: '', last_login_at: null, created_at: '2024-01-01', updated_at: '2024-01-01' } },
  '/api/auth/refresh': { access_token: 'fake-refreshed-token' },
  '/api/system/version': { data: { version: '2.2.3' } },
  '/api/system/dashboard': { data: { task_count: 1248, running_tasks: 36, today_logs: 266, success_logs: 263, daily_stats: [
    { date: '2024-06-15', total: 180, success: 172, failed: 8 },
    { date: '2024-06-16', total: 195, success: 188, failed: 7 },
    { date: '2024-06-17', total: 210, success: 201, failed: 9 },
    { date: '2024-06-18', total: 188, success: 182, failed: 6 },
    { date: '2024-06-19', total: 230, success: 220, failed: 10 },
    { date: '2024-06-20', total: 245, success: 238, failed: 7 },
    { date: '2024-06-21', total: 266, success: 263, failed: 3 },
  ], recent_logs: [
    { id: 1, task_name: '每日数据备份', status: 0, duration: 12.3, created_at: '2024-06-21T10:24:18Z' },
    { id: 2, task_name: '清理临时文件', status: 0, duration: 2.1, created_at: '2024-06-21T09:15:00Z' },
    { id: 3, task_name: '同步配置文件', status: 1, duration: 5.4, created_at: '2024-06-21T08:30:00Z' },
    { id: 4, task_name: '检查SSL证书', status: 0, duration: 1.2, created_at: '2024-06-21T07:00:00Z' },
    { id: 5, task_name: '更新IP数据库', status: 0, duration: 8.7, created_at: '2024-06-20T23:00:00Z' },
  ] } },
  '/api/system/info': { data: { os: 'linux', arch: 'amd64', cpu_usage: 12.5, memory_usage: 45.2, disk_usage: 32.1, num_cpu: 4, goroutines: 28, uptime: '3d 12h 45m', memory_used: 1073741824, memory_total: 2147483648, disk_used: 10737418240, disk_total: 32212254720, go_version: '1.21.5' } },
  '/api/system/panel-settings': { data: { panel_title: '呆呆面板', panel_icon: '' } },
  '/api/tasks': { data: tasksMock, total: tasksMock.length },
  '/api/tasks/views': [],
  '/api/tasks/notification-channels': { data: [] },
}

const server = http.createServer((req, res) => {
  res.setHeader('Access-Control-Allow-Origin', '*')
  res.setHeader('Access-Control-Allow-Methods', 'GET, POST, PUT, DELETE, OPTIONS')
  res.setHeader('Access-Control-Allow-Headers', 'Content-Type, Authorization, X-Client-Type, X-Client-App')
  res.setHeader('Content-Type', 'application/json')

  if (req.method === 'OPTIONS') { res.writeHead(204); res.end(); return }

  const url = req.url?.split('?')[0] || ''
  const data = mockData[url]

  res.writeHead(200)
  res.end(JSON.stringify(data || {}))
})

server.listen(5701, () => console.log('Mock API on :5701'))
