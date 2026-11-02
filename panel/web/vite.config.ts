import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import type { Plugin } from 'vite'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'

// 发布版的值，真正生效的那份写在 web/index.html 里；这里留一份只为报错时提示。
const RELEASE_ROBOTS = 'noindex, nofollow, noarchive, nosnippet'
const DEMO_ROBOTS = 'index, follow'

/**
 * robots meta 条件化：只有 Demo 构建改成可索引，其余一切场景保持 noindex。
 *
 * 方向是刻意选的——【安全值是 index.html 里的静态默认值，Demo 才是显式改写】，而不是反过来：
 *   - dev server、发布版构建、以及「这个插件哪天被误删」的情况，拿到的都还是 noindex；
 *   - 自建面板不该被搜索引擎收录，这条对真实用户是保护，必须 fail-safe。
 *
 * 因此没有用 Vite 原生的 `%VITE_ROBOTS%` 占位 + web/.env 提供默认值：那是 fail-open 的。
 * 一旦 web/.env 被删、变量名拼错或 envPrefix 改动，Vite 对未定义的 `%VITE_XXX%`
 * 只 warn 不 fail，会把字面量原样留在产物里 —— 等于**静默丢掉 noindex**，
 * 而且丢的是发布版（真实用户），不是 Demo。用 transformIndexHtml 则最坏情况只是
 * 「Demo 没被收录」，代价方向正确。
 *
 * 另外 404.html 不用单独处理：scripts/copy-spa-fallback.mjs 是在 vite build【之后】
 * 复制产物 dist/index.html 的，改写结果自动跟着走。
 */
function robotsMetaPlugin(isDemoBuild: boolean): Plugin {
  return {
    name: 'daidai-robots-meta',
    transformIndexHtml: {
      // post：等 Vite 注入完脚本/样式再改，避免与其它 HTML 处理抢同一段文本。
      order: 'post',
      handler(html) {
        if (!isDemoBuild) return html

        // 用正则而不是全等字符串匹配：不假设 Vite 的 HTML 处理会原样保留
        // 属性顺序、引号风格和自闭合斜杠。只认「name=robots 的 meta」，
        // 然后整条替换掉，避免因为空格差异而静默不生效。
        const metaRe = /<meta\b[^>]*\bname=["']robots["'][^>]*>/i
        if (!metaRe.test(html)) {
          // 这里必须 throw 而不是 warn。静默失配的后果是「Demo 站带着 noindex 上线」，
          // 而让 Demo 能被收录正是这段代码存在的唯一理由，失效了却不报错等于白写。
          throw new Error(
            '[daidai-robots-meta] 在 index.html 中找不到 name="robots" 的 meta。\n' +
              `发布版应当保持 content="${RELEASE_ROBOTS}"；删掉这一行会让 Demo 构建失去可索引改写。`
          )
        }

        return html.replace(metaRe, `<meta name="robots" content="${DEMO_ROBOTS}" />`)
      }
    }
  }
}

/**
 * 自托管字体的落盘校验。
 *
 * public/ 是【原样拷贝】的，Vite 根本不解析 public/fonts/fonts.css，
 * 所以 woff2 缺失时构建会安安静静地通过，直到线上 12 个字体请求全部 404、
 * 界面退回系统字体 —— 而这正是本次改动要消灭的那种「静默降级」。
 *
 * 于是把它变成构建期硬失败。这与仓库既有的约定一致：demo fixture 也是
 * 「生成产物进版本库、缺文件就让构建失败」，而不是靠人记得。
 */
function selfHostedFontsPlugin(): Plugin {
  return {
    name: 'daidai-selfhosted-fonts',
    // 只管构建。dev 时字体缺失最多是本地预览字形不对，不值得把 `npm run dev` 也堵死。
    apply: 'build',
    buildStart() {
      const fontsDir = path.resolve(process.cwd(), 'public/fonts')
      const cssFile = path.join(fontsDir, 'fonts.css')
      if (!fs.existsSync(cssFile)) {
        throw new Error('[daidai-selfhosted-fonts] 缺少 web/public/fonts/fonts.css')
      }

      const css = fs.readFileSync(cssFile, 'utf8')
      const referenced = [...css.matchAll(/url\(["']?\.\/([^"')]+\.woff2)["']?\)/g)].map((m) => m[1])
      const missing = [...new Set(referenced)].filter(
        (name) => !fs.existsSync(path.join(fontsDir, name))
      )

      if (missing.length > 0) {
        throw new Error(
          `[daidai-selfhosted-fonts] fonts.css 引用的 ${missing.length} 个 woff2 不在磁盘上：\n` +
            `  ${missing.join('\n  ')}\n` +
            '这些是进版本库的产物，请先跑一次：node scripts/fetch-fonts.mjs'
        )
      }
    }
  }
}

/**
 * 前端包自己的版本号，供 define 写成构建期常量 __DD_WEB_VERSION__（契约 C10，类型声明在 src/shims.d.ts）。
 *
 * 用途只有一个：MainLayout 拿后端版本号和它比，不一致说明浏览器跑的是缓存里的旧前端壳，自动刷新一次
 * （utils/chunkReload.ts）。侧栏徽标取的是后端版本，旧壳也显示新版本号，在此之前前端没有任何办法发现
 * 「壳比后端旧」（issue #126）。发版时 scripts/release-preflight.ps1 会校验 web/package.json 与
 * server/handler/version.go 版本号一致，这是版本自检不误刷的前提。
 * 路径按本文件定位，不依赖 cwd。读不到版本号时给空串：版本自检把空串当成「无从比较」直接跳过，
 * 失效方向是「不刷」而不是「乱刷」。
 */
function readWebPackageVersion(): string {
  const packageJsonPath = fileURLToPath(new URL('./package.json', import.meta.url))
  const pkg = JSON.parse(fs.readFileSync(packageJsonPath, 'utf8')) as { version?: unknown }
  return typeof pkg.version === 'string' ? pkg.version : ''
}

export default defineConfig(({ mode }) => {
  // 只有 `vite build --mode demo`（npm run build:demo）会加载 web/.env.demo，
  // 从而拿到 VITE_DEMO=1；发布版构建走默认的 production 模式，读不到这个变量。
  const isDemoBuild = loadEnv(mode, process.cwd(), 'VITE_').VITE_DEMO === '1'

  return {
    // 本机版（Android）构建 base：APP 内嵌 WebView 走 /local-ui/ 前缀，
    // 与本地 main.ts request.ts 的会话桥接联动。
    base: process.env.VITE_LOCAL_WEB_BUILD === 'true' ? '/local-ui/' : '/',
    define: {
      // 硬约束：发布版产物必须 0 字节 demo 代码。
      //
      // 这条 define 的作用是把 `import.meta.env.VITE_DEMO` 变成【确定的字符串字面量】。
      // 不加它的话，未定义该变量时 Vite 只会把整个 `import.meta.env` 替换成对象字面量，
      // 表达式退化成 `{...}.VITE_DEMO === '1'`——这种形式压缩器不一定会常量折叠，
      // 一旦折叠不掉，main.ts 里的 `import('./demo')` 就会被当成活代码，
      // demo 层连同 fixture 会被打进真实用户拿到的产物里（最坏情况：真实面板的请求被 mock 顶替）。
      //
      // 折叠成 '' 之后，`'' === '1'` 恒假，rollup 会把整段分支与对应 chunk 一起剔除。
      'import.meta.env.VITE_DEMO': JSON.stringify(isDemoBuild ? '1' : ''),
      // 前端包版本号（契约 C10），见上面 readWebPackageVersion 的注释
      __DD_WEB_VERSION__: JSON.stringify(readWebPackageVersion())
    },
    plugins: [
      vue(),
      robotsMetaPlugin(isDemoBuild),
      selfHostedFontsPlugin(),
      Components({
        dts: false,
        resolvers: [
          ElementPlusResolver({
            importStyle: 'css'
          })
        ]
      })
    ],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url))
      }
    },
    build: {
      emptyOutDir: true,
      rollupOptions: {
        output: {
          manualChunks(id: string) {
            // Vite 的 preload helper 是虚拟模块（\0vite/preload-helper）。不显式指派的话，
            // Rollup 会把它折进某个 manual chunk；一旦折进 monaco，入口就变成【静态】import monaco chunk，
            // index.html 里随之冒出 <link rel="modulepreload" .../assets/monaco-*.js> 外加 monaco 的
            // <link rel="stylesheet">，懒加载当场失效——而且构建成功、页面能用，纯静默劣化。
            // 钉到 app-core 上：它必然是首屏 chunk（main.ts 顶层就 import vue/router/pinia）。
            // 🔴 这条不是冗余，删掉 Monaco 会立刻回到首屏。checks.yml 里有一条反向断言守着它。
            if (
              id.includes('vite/preload-helper') ||
              id.includes('vite/modulepreload-polyfill')
            ) {
              return 'app-core'
            }

            // Monaco 单独成块，且**只能**由 utils/monacoEngine.ts 动态 import 进来（见那个文件的说明）。
            // 必须排在下面那条 node_modules 兜底【之前】，否则整个 Monaco 会被吞进首屏的 vendor chunk
            // （vendor 在 index.html 里是 modulepreload 的）。
            if (id.includes('node_modules/monaco-editor')) {
              return 'monaco'
            }

            // CodeMirror 全家桶（含 @lezer 的各语言语法）单独成块：
            // 它只有编辑器相关的四个页面用得上，不该压进首屏的 vendor 里。
            // 只匹配 @codemirror/* 与 @lezer/*：本仓从来没有 import 过那个叫 codemirror 的
            // meta 包（依赖里也已经移除），再留一条 'node_modules/codemirror/' 判断是恒假的死代码。
            if (
              id.includes('node_modules/@codemirror') ||
              id.includes('node_modules/@lezer')
            ) return 'codemirror'
            if (id.includes('node_modules/echarts')) return 'echarts'
            if (id.includes('node_modules/zrender')) return 'zrender'
            if (id.includes('node_modules/qrcode')) return 'qrcode'
            if (id.includes('node_modules/sortablejs')) return 'sortablejs'
            if (id.includes('node_modules/element-plus')) return undefined
            if (
              id.includes('node_modules/vue') ||
              id.includes('node_modules/@vue') ||
              id.includes('vue-router') ||
              id.includes('pinia') ||
              id.includes('axios')
            ) return 'app-core'
            if (id.includes('node_modules')) return 'vendor'
            return undefined
          }
        }
      }
    },
    server: {
      port: 5173,
      proxy: {
        '/api': {
          target: 'http://localhost:5701',
          changeOrigin: true
        }
      }
    }
  }
})
