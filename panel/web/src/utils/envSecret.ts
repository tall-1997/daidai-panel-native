/**
 * 环境变量敏感值的识别与遮蔽（issue #127）。
 *
 * 只管「页面上怎么显示」，不改任何接口返回：/envs 同时挂着 Open API，App 的编辑、复制都依赖明文，
 * 所以遮蔽绝不能下沉到服务端响应里。复制、编辑弹窗、导出仍然是明文，这是刻意的。
 *
 * 🔴 Go 侧有一份同样的规则：内置 MCP（server/mcptools，issue #128 的 list_envs 工具）按本文件实现。
 *    关键词清单、切段口径、遮罩格式、下面两组测试向量，两边必须逐字一致。
 *    改了这里任何一条，必须同步改 Go 那一份和它的测试；反过来也一样。
 *    两边对不上不会报错，只会出现「网页上遮着的值，AI 通过 MCP 一问就是明文」（或者反过来）。
 *
 * ── 识别规则 isSensitiveEnvName(name) ──
 * 先整体转大写，再按 `_` 切段：
 *   · 长词按【子串】匹配，出现在名字任何位置都算：
 *       TOKEN SECRET PASSWORD PASSWD COOKIE CREDENTIAL WSKEY PRIVATE APIKEY ACCESSKEY
 *   · 短词按【整段】匹配，必须恰好是 `_` 切出来的一整段：
 *       KEY PWD PASS AUTH SK AK SID CK WSCK
 *   短词不做子串是刻意的：KEY 做子串会误伤 KEYWORD / MONKEY，PASS 会误伤 BYPASS / PASSPORT，
 *   AUTH 会误伤 AUTHOR，CK 会误伤 QUICK / BLOCK / CHECK。
 *   关键词都不含 `_`，长词在整串上找子串和逐段找子串结果完全相同，所以实现里直接在整串上找。
 *
 * 必须命中（16 条）：
 *   JD_COOKIE、S3_SECRET_ACCESS_KEY、SMTP_PASSWORD、API_TOKEN、APP_SECRET、DB_PASSWORD、
 *   OPENAI_API_KEY、APIKEY、BARK_KEY、PUSH_KEY、JD_CK、WSKEY、TG_BOT_TOKEN、X_AUTH、DB_PWD、MAIL_PASS
 * 必须不命中（7 条）：
 *   SEARCH_KEYWORD、AUTHOR_NAME、BYPASS_PROXY、MONKEY_X、QUICK_MODE、KEYWORDS、PASSPORT_URL
 *
 * 已知漏判（规则本身就这样，不是 bug）：名字里没有任何关键词的凭据，比如驼峰写法的 barkKey
 * 转大写后是 BARKKEY，不是一整段 KEY。所以页面另外提供了「全部遮蔽」这一档兜底，见 envs/index.vue。
 *
 * ── 遮罩格式 maskEnvValue(value) ──
 *   长度 > 8：前 3 个字符 + `******` + 后 3 个字符，例：abcdefghijk → abc******ijk
 *   长度 ≤ 8：一律显示 `********`（空串也一样；页面对空值直接显示「-」，不会调到这里）
 *   中间的星号是定长的，不随原值变长变短，看显示宽度猜不出原值有多长。
 *   长度按 Unicode 码点算（Array.from）。按 UTF-16 码元切会把 emoji 劈成半个代理对；
 *   Go 侧要按 rune（[]rune）切，才和这里口径一致。
 */

/** 长词：在整个名字里找子串 */
const SUBSTRING_KEYWORDS: readonly string[] = [
  'TOKEN',
  'SECRET',
  'PASSWORD',
  'PASSWD',
  'COOKIE',
  'CREDENTIAL',
  'WSKEY',
  'PRIVATE',
  'APIKEY',
  'ACCESSKEY'
]

/** 短词：只在恰好是 `_` 切出来的一整段时才算 */
const SEGMENT_KEYWORDS: ReadonlySet<string> = new Set(['KEY', 'PWD', 'PASS', 'AUTH', 'SK', 'AK', 'SID', 'CK', 'WSCK'])

/** 超过这个长度才露出头尾，否则整个遮住 */
const MASK_REVEAL_MIN_EXCLUSIVE = 8
/** 头尾各露几个字符 */
const MASK_KEEP_CHARS = 3
const MASK_MIDDLE = '******'
const MASK_FULL = '********'

/** 页面级遮蔽模式：自动（按变量名识别）/ 全部遮蔽 / 全部明文 */
export type EnvValueMaskMode = 'auto' | 'all' | 'none'

/** 变量名是否像凭据（规则与测试向量见文件头注释） */
export function isSensitiveEnvName(name: string): boolean {
  const upper = String(name ?? '').toUpperCase()
  if (!upper) return false
  if (SUBSTRING_KEYWORDS.some((keyword) => upper.includes(keyword))) return true
  return upper.split('_').some((segment) => SEGMENT_KEYWORDS.has(segment))
}

/** 把值换成定长遮罩（格式见文件头注释） */
export function maskEnvValue(value: string): string {
  const chars = Array.from(String(value ?? ''))
  if (chars.length <= MASK_REVEAL_MIN_EXCLUSIVE) return MASK_FULL
  return chars.slice(0, MASK_KEEP_CHARS).join('') + MASK_MIDDLE + chars.slice(-MASK_KEEP_CHARS).join('')
}

/** 当前模式下这个变量的值要不要遮（还没算上用户用小眼睛临时揭示的那几行，那部分由页面自己管） */
export function shouldMaskEnvValue(name: string, mode: EnvValueMaskMode): boolean {
  if (mode === 'none') return false
  if (mode === 'all') return true
  return isSensitiveEnvName(name)
}
