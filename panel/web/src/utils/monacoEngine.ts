/**
 * Monaco Editor 引擎的**唯一**导入边界。
 *
 * 定位：CodeMirror 6 仍是默认引擎与移动端唯一引擎（理由见 components/CodeEditor.vue 顶部：
 * Monaco 是自绘编辑器，手机上「长按出系统菜单 / 双击出选择手柄 / 按住拖动光标」三条做不到）。
 * Monaco 在这一版是**桌面端可选的第二引擎**，给需要多光标、代码折叠、格式化那套重型能力的用户。
 *
 * ⚠️ 全仓只有这一个文件可以出现 `from 'monaco-editor/...'`。其它任何地方要用 Monaco，
 * 一律写 `await import('@/utils/monacoEngine')`。理由是硬性的：
 * Monaco 压缩后仍有 MB 级，而绝大多数用户不会切过去，**不切的人必须一个字节都不下载**。
 * 只要有任意一处对本模块（或对 monaco-editor 本身）写了静态 import，Rollup 就会把 monaco chunk
 * 提升成入口的静态依赖，首屏多出一条 modulepreload —— 构建全绿、页面能用，纯静默劣化。
 * vite.config.ts 的 manualChunks 里有两条规则专门守这件事，别删。
 *
 * ⚠️ 也**不要**把 v3.2.0 删掉的那套 AMD 管线加回来（utils/monaco.ts、copy-monaco-assets.mjs、
 * @monaco-editor/loader、vite-plugin-monaco-editor、CDN 兜底、server/main.go 的静态白名单开洞）。
 * 这里走的是纯 ESM + Vite 打包：产物全部落在 dist/assets/ 下，已被现有白名单覆盖，
 * 「本地资源残缺 → 回退 CDN → 超时 → 提示重试」那一整套故障模式在这个方案里不存在。
 */

// 入口刻意用 editor.api 而不是 editor.main：后者会把 Monaco 全部 contrib 与全部 80+ 语言
// 一股脑打进来（体积翻几倍）。editor.api 是裸核心，功能按下面两组 import 显式点单。
//
// ⚠️ 路径写成 `monaco-editor/editor/editor.api`，**不是** `monaco-editor/esm/vs/editor/editor.api`。
// 0.56 的 package.json exports 是 `{"./*.js": "./esm/vs/*.js", "./*": "./esm/vs/*.js"}`，
// 老写法会被解析成 `esm/vs/esm/vs/...` 直接 404。网上抄来的老教程全是老写法，别照抄。
import * as monaco from 'monaco-editor/editor/editor.api'

// ---------------------------------------------------------------------------
// 功能贡献（contrib）。editor.api 是裸核心，下面每一条都是「少了就静默丢一个功能」：
// 不报错、不警告，只是那个快捷键 / 那个菜单项 / 那个小部件不存在了。
// 顺序无关，按「基础操作 → 查找折叠 → 编辑增强 → 智能提示 → 差异编辑器」分组排列。
// ---------------------------------------------------------------------------
// 光标移动、选区、翻页、删除等核心命令；少了它编辑器几乎不能用
import 'monaco-editor/editor/browser/coreCommands.js'
// Ctrl+F / Ctrl+H 查找替换控件
import 'monaco-editor/editor/contrib/find/browser/findController.js'
// 行号旁的代码折叠箭头
import 'monaco-editor/editor/contrib/folding/browser/folding.js'
// 括号配对高亮
import 'monaco-editor/editor/contrib/bracketMatching/browser/bracketMatching.js'
// Ctrl+/ 行注释、Shift+Alt+A 块注释
import 'monaco-editor/editor/contrib/comment/browser/comment.js'
// 光标停在标识符上时高亮同名出现处
import 'monaco-editor/editor/contrib/wordHighlighter/browser/wordHighlighter.js'
// Alt+点击 / Ctrl+D 多光标
import 'monaco-editor/editor/contrib/multicursor/browser/multicursor.js'
// 右键上下文菜单本体（少了它右键退回浏览器原生菜单）
import 'monaco-editor/editor/contrib/contextmenu/browser/contextmenu.js'
// 复制/剪切/粘贴命令，上下文菜单里的那三项也靠它
import 'monaco-editor/editor/contrib/clipboard/browser/clipboard.js'
// 整行操作：Alt+↑↓ 移动行、Shift+Alt+↓ 复制行、Ctrl+Shift+K 删除行
import 'monaco-editor/editor/contrib/linesOperations/browser/linesOperations.js'
// Tab / Shift+Tab 缩进与「重新缩进选中行」
import 'monaco-editor/editor/contrib/indentation/browser/indentation.js'
// 自动补全下拉（配置文件页的 JSON 语言服务要靠它把补全项显示出来）
import 'monaco-editor/editor/contrib/suggest/browser/suggestController.js'
// 悬浮提示（JSON 语言服务的错误信息、schema 说明都走这里）
import 'monaco-editor/editor/contrib/hover/browser/hoverContribution.js'
// Ctrl+←→ 按词移动、Ctrl+Backspace 按词删除
import 'monaco-editor/editor/contrib/wordOperations/browser/wordOperations.js'
// 撤销时一并还原光标位置，而不是只还原文本
import 'monaco-editor/editor/contrib/cursorUndo/browser/cursorUndo.js'
// Shift+Alt+F 格式化（有语言服务的语言才有效，比如 JSON）
import 'monaco-editor/editor/contrib/format/browser/formatActions.js'
// 差异编辑器（版本对比页要用）。少了它 createDiffEditor 出来的是个没有折叠/导航的空壳
import 'monaco-editor/editor/browser/widget/diffEditor/diffEditor.contribution.js'

// ---------------------------------------------------------------------------
// 语言。这一份清单与 utils/codeEditor.ts 的 resolveCodeEditorLanguage / resolveMonacoLanguage
// 覆盖的语言集**必须一致**：少注册一个的表现是「同一个文件换个引擎高亮就没了」，不报错。
// ---------------------------------------------------------------------------
import 'monaco-editor/languages/definitions/javascript/register.js'
import 'monaco-editor/languages/definitions/typescript/register.js'
import 'monaco-editor/languages/definitions/python/register.js'
// 语言 id 是 'shell'（不是 bash），扩展名 .sh/.bash 都归它
import 'monaco-editor/languages/definitions/shell/register.js'
import 'monaco-editor/languages/definitions/go/register.js'
import 'monaco-editor/languages/definitions/yaml/register.js'
import 'monaco-editor/languages/definitions/markdown/register.js'
import 'monaco-editor/languages/definitions/html/register.js'
import 'monaco-editor/languages/definitions/css/register.js'
import 'monaco-editor/languages/definitions/xml/register.js'
// JSON 是唯一的例外：0.56 的 languages/definitions/ 下**没有** json 这个 Monarch 语法目录，
// 只有 languages/features/json 这套完整语言服务（语法 + 校验 + 补全 + 格式化，跑在 worker 里）。
// 配置文件页正是编 JSON 的地方，这份额外开销值得给。
import 'monaco-editor/languages/features/json/register.js'

// ?worker 后缀让 Vite 把 worker 连同它自己的整个模块图打成独立产物，落在 dist/assets/ 下。
// 这两个 import 是「值」导入（拿到的是构造函数），不能省成副作用导入。
import EditorWorker from 'monaco-editor/editor/editor.worker?worker'
import JsonWorker from 'monaco-editor/language/json/json.worker?worker'

/**
 * Monaco 的 worker 工厂。
 *
 * 必须显式给，不能指望 0.56 的内置回退——两条回退路径在 Vite 下都是坏的：
 *
 * 1. 编辑器 worker 回退到 `new URL('../../common/services/editorWebWorkerMain.js', import.meta.url)`
 *    （esm/vs/editor/browser/services/editorWorkerService.js），JSON worker 回退到
 *    `new URL('json.worker.js', import.meta.url)`（languages/features/json/workerManager.js）。
 *    这两条指向的都是 node_modules 里的裸文件：Vite 会把它们当**静态资源**原样拷出去，
 *    不打包它们自己的相对依赖，运行时 worker 一加载就 404 一片。`?worker` 才会打整个模块图。
 *
 * 2. 编辑器 worker 那条回退还会再套一层 `URL.createObjectURL(new Blob(...))` 引导脚本
 *    （platform/webWorker/browser/webWorkerServiceImpl.js 的 getWorkerBootstrapUrl）。
 *    server/middleware/security.go 的 CSP 是 `worker-src 'self' blob:`，blob 恰好放行，
 *    但这条只在内置服务端直发 HTML 时生效——Docker 部署走 nginx 根本不发 CSP，
 *    也就是说任何 worker 来源上的问题在 Docker 上**测不出来**，只有 Magisk / Windows
 *    内嵌部署的用户会中招。显式给 getWorker 之后两个 worker 都是同源 dist/assets/ 下的
 *    普通脚本，`'self'` 直接覆盖，整件事不用再赌。
 *
 * label 取值来自 Monaco 内部：编辑器 worker 是 'editorWorkerService'
 * （editorWorkerService.js 的 workerDescriptor），JSON 语言服务传的是语言 id 'json'
 * （languages/features/json/workerManager.js）。这里只认 'json'，其余一律给通用编辑器 worker
 * ——将来加别的语言服务（比如 css/html）时记得在这里补一条，漏了的表现是那门语言的
 * 校验/补全静默不工作。
 */
globalThis.MonacoEnvironment = {
  getWorker(_workerId: string, label: string) {
    if (label === 'json') {
      return new JsonWorker()
    }
    return new EditorWorker()
  }
}

/**
 * Monaco 的 API 对象类型，给需要标注 monaco 实例的调用点用。
 *
 * ⚠️ 引用它必须写成 `import type { MonacoApi } from '@/utils/monacoEngine'`——
 * `import type` 会被完全擦掉，不产生运行时导入；漏掉 type 关键字就等于给本模块加了一条
 * 静态 import，monaco chunk 当场被提升进首屏。
 * 正因为这条约束太容易破，utils/codeEditor.ts 那边宁可自己写结构化最小类型也不引它。
 */
export type MonacoApi = typeof monaco

export { monaco }
