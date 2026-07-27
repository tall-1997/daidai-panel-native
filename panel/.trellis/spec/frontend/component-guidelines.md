# 组件规范

> 适用于 `views/*/*.vue`、`components/*.vue`、`layouts/*.vue`。

---

## 总原则

- 优先让组件**一眼能看懂**。
- 组件拆分以“职责边界明确”为前提，不以“文件越小越好”为目标。
- 如果一段逻辑只服务于当前组件，并不会复用，优先留在当前组件内。
- 复杂交互、边界分支、兼容逻辑建议补中文注释。

---

## 组件拆分边界

### 适合拆出去的情况

- 某块 UI 在多个页面/多个弹窗里复用。
- 当前页面太长，某个局部区域已经有独立职责。
- 某块区域本身就有清晰输入输出，比如表单、详情面板、日志查看器。

### 不适合拆出去的情况

- 只是为了“每个文件行数少一点”。
- 逻辑高度依赖父组件内部状态，拆出去反而传一堆 props 和 events。
- 只会出现一次、且本身并不复杂。

---

## 文件内部结构建议

Vue 单文件组件通常按下面顺序组织：

1. `template`
2. `script setup`
3. `style`

在 `script setup` 内部建议顺序：

1. import
2. 类型定义
3. props / emits
4. 响应式状态
5. 计算属性 / 侦听
6. 事件函数 / 业务函数
7. 生命周期

---

## props 和 emits

- props 要尽量语义清楚，别用模糊命名。
- 如果组件只是局部页面组件，也不要为了抽象强行设计很复杂的 props API。
- 对外事件名尽量直接体现动作，例如“保存”“关闭”“刷新”。
- 对业务对象较复杂的场景，优先传明确结构对象，不要传大量分散字段。

---

## 样式方式

- 当前项目以 `scss/css` 为主，样式应尽量贴近组件或页面目录。
- 页面专属样式优先就近放置。
- 多个设置卡片共用样式时，可以像现有项目一样提取共享 scss 文件，但前提是确实存在复用。
- 不要为了统一视觉把页面样式过度抽象成很难追踪的公共类名。

---

## 交互与可读性

- 表单、弹窗、抽屉、表格操作应保持用户路径清晰。
- 错误提示、确认弹窗、空状态文案要直接明确，不要写得太“技术化”。
- 当交互状态较多时，建议用清晰的状态变量名，不要混成一个难懂的大对象。

---

## 常见错误

- 一个页面拆出过多薄组件，导致阅读要来回跳文件。
- 把页面局部逻辑硬做成“通用组件”，结果 props/emits 变得复杂。
- 没有中文注释，导致状态切换或边界逻辑难理解。
- 同一类弹窗、卡片、表单在不同地方写出完全不同的交互风格。

## Scenario: 日志查看器中的 `\r` 单行覆盖刷新

### 1. Scope / Trigger
- Trigger: 修改任务日志查看器、执行日志详情、日志文件预览这类终端风格日志组件时必须看本节。
- 原因: 任务脚本常输出进度条，裸 `\r` 不是“换行”，而是“把光标移回当前行开头并覆盖原内容”。如果日志组件在每个流式分片到达时都强行追加新行，进度条就会刷屏。

### 2. Signatures
- 任务实时日志组件: `web/src/views/tasks/components/LogViewer.vue`
- 执行日志详情页: `web/src/views/logs/index.vue`
- 日志文件预览: `web/src/views/tasks/components/LogFileBrowser.vue`

### 3. Contracts
- 渲染规则必须区分三类边界:
  - `\n`: 真正落一新行
  - `\r\n`: 真正落一新行
  - 裸 `\r`: 清空当前行并等待后续字符覆盖
- 流式日志组件不能在“每个数据分片结束”时默认把当前 tail 强制 push 成一整行。
- 只有确认遇到真实换行，或者该分片本身没有覆盖语义时，才允许落行为历史行。

### 4. Validation & Error Matrix
- 纯文本日志 -> 展示行为与原来一致
- `下载中 10%\r下载中 20%\r下载中 30%\n` -> 最终只保留一条当前进度行
- 如果在 `requestAnimationFrame` flush、buffer flush 或 computed 渲染里把每个 chunk 直接 `join('\\n')` -> 进度条刷屏，属于错误实现

### 5. Good/Base/Bad Cases
- Good: 实时任务日志、执行日志详情、日志文件详情对同一份包含裸 `\r` 的内容展示结果一致
- Base: 没有 `\r` 的普通多行日志，仍按原有 `pre-wrap` 展示
- Bad: 任务页能单行刷新，但“执行日志”页和“日志文件”页又恢复成多行刷屏

### 6. Tests Required
- 前端验证: `cd web && npm run build`
- 手工回归点:
  - 任务页 `LogViewer` 中查看运行中的进度条脚本
  - 执行日志页打开同一任务的日志详情
  - 日志文件弹窗查看对应落盘文件

### 7. Wrong vs Correct
#### Wrong
```ts
detailContent.value += sseBuffer.join('\n') + '\n'
```

```ts
if (commitBoundary) {
  pushLogLine()
}
```

#### Correct
```ts
detailContent.value = mergeTerminalText(detailContent.value, chunk)
```

```ts
if (commitBoundary && !endedWithLineBreak && !sawCarriageReturn) {
  pushLogLine()
}
```

## Scenario: Monaco 本地静态资源与加载探测

### 1. Scope / Trigger
- Trigger: 修改 `web/scripts/copy-monaco-assets.mjs`、`web/src/utils/monaco.ts`、`MonacoEditor.vue`、`MonacoDiffEditor.vue` 时必须看本节。
- 原因: Monaco 是运行时动态加载资源，不是普通的“构建期 import 即可”。如果只保留 `loader.js`、却删掉 `editor/`、`language/`、`assets/` 等目录，构建仍然会成功，但浏览器里编辑器会直接初始化失败。

### 2. Signatures
- 资源复制脚本: `web/scripts/copy-monaco-assets.mjs`
- 本地资源探测: `web/src/utils/monaco.ts`
- 本地资源根路径: `${import.meta.env.BASE_URL}monaco/vs`

### 3. Contracts
- `copy-monaco-assets.mjs` 不能再按“带 hash 的具体文件名白名单”删 Monaco 资源。
- 本地资源探测不能只检查 `loader.js` 是否存在，至少要检查稳定关键入口:
  - `loader.js`
  - `editor/editor.main.js`
  - `editor/editor.main.css`
  - `language/css/monaco.contribution.js`
  - `language/html/monaco.contribution.js`
  - `language/json/monaco.contribution.js`
  - `language/typescript/monaco.contribution.js`
- 当本地资源不完整时，允许回退 CDN；但如果本地资源完整，应优先使用本地，避免用户网络无法访问 CDN 时编辑器直接挂掉。

### 4. Validation & Error Matrix
- `loader.js` 存在，但 `editor/editor.main.js` 或 `language/*` 缺失 -> 视为本地资源不可用
- 构建通过，但浏览器里出现“编辑器加载失败，请检查网络或稍后重试” -> 优先检查 `dist/monaco/vs` 完整性，而不是先怀疑用户网络
- 本地资源完整 + CDN 不可达 -> 编辑器仍应能正常加载

### 5. Good/Base/Bad Cases
- Good: `dist/monaco/vs` 包含 `editor/`、`language/`、`assets/`、`basic-languages/`，用户离线或访问不了 CDN 也能打开编辑器
- Base: 本地资源缺失时回退 CDN，至少不误判“本地可用”
- Bad: 只探测 `loader.js`，或者继续按 hash 白名单裁剪 `vs` 目录

### 6. Tests Required
- 前端验证: `cd web && npm run build`
- 构建后检查:
  - `web/dist/monaco/vs/editor` 存在
  - `web/dist/monaco/vs/language` 存在
  - `web/dist/monaco/vs/assets` 存在
- 手工回归点:
  - 脚本编辑页能正常打开 Monaco 编辑器
  - 断网或阻断 CDN 时，本地编辑器仍能加载

### 7. Wrong vs Correct
#### Wrong
```js
const allowedTopLevelVsFiles = new Set([
  'editor.worker-abc123.js',
  'ts.worker-def456.js',
])
```

```ts
return `${import.meta.env.BASE_URL}monaco/vs/loader.js`
```

#### Correct
```js
copyDirectory(sourceDir, targetDir)
```

```ts
const LOCAL_MONACO_REQUIRED_FILES = [
  'loader.js',
  'editor/editor.main.js',
  'editor/editor.main.css',
  'language/css/monaco.contribution.js',
]
```
