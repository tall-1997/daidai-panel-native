import fs from 'node:fs'
import path from 'node:path'

// GitHub Pages 没有 docker/nginx.conf 里 `try_files $uri /index.html` 的等价物：
// 访客直接打开深链（如 /daidai-panel/tasks）或在深链上刷新时，Pages 找不到同名文件，
// 会去取站点根的 404.html。这份 404.html 必须就是 SPA 的入口页，前端路由才能接管。
//
// 为什么不是把 404.html 放进 web/public/：
//   public/ 下的文件是【原样拷贝】的，不经过 Vite 的 HTML 处理。
//   index.html 里的 `<script type="module" src="/src/main.ts">` 只有构建时才会被改写成
//   带 hash 的产物路径，直接拷一份源码 index.html 到 public/ 会在线上请求 /src/main.ts 而 404，
//   整个演示站白屏。所以必须在构建【之后】复制产物 index.html。
//
// 顺带：复制产物而不是维护第二份 HTML，也避免了两份 HTML 随时间漂移。
const distDir = path.resolve(process.cwd(), 'dist')
const indexFile = path.join(distDir, 'index.html')
const fallbackFile = path.join(distDir, '404.html')

if (!fs.existsSync(indexFile)) {
  console.error('[copy-spa-fallback] 找不到 dist/index.html，请先执行 vite build')
  process.exit(1)
}

fs.copyFileSync(indexFile, fallbackFile)
console.log('[copy-spa-fallback] copied dist/index.html -> dist/404.html')
