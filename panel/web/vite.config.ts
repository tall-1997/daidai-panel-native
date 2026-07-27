import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import type { Plugin, ResolvedConfig } from 'vite'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'

const localMonacoSourceDir = path.resolve(process.cwd(), 'node_modules/monaco-editor/min')

function normalizeBase(base: string) {
  return base === '/' ? '' : base.replace(/\/$/, '')
}

function getContentType(filePath: string) {
  switch (path.extname(filePath)) {
    case '.css':
      return 'text/css; charset=utf-8'
    case '.js':
      return 'application/javascript; charset=utf-8'
    case '.json':
    case '.map':
      return 'application/json; charset=utf-8'
    case '.svg':
      return 'image/svg+xml'
    case '.ttf':
      return 'font/ttf'
    default:
      return 'application/octet-stream'
  }
}

function localMonacoAssetsPlugin(): Plugin {
  let resolvedConfig: ResolvedConfig

  return {
    name: 'local-monaco-assets',
    apply: 'serve',
    configResolved(config) {
      resolvedConfig = config
    },
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const requestUrl = req.url?.split('?')[0] || ''
        const prefix = `${normalizeBase(resolvedConfig.base)}/monaco/`
        if (!requestUrl.startsWith(prefix)) {
          next()
          return
        }

        const relativePath = requestUrl.slice(prefix.length)
        const filePath = path.resolve(localMonacoSourceDir, relativePath)
        if (!filePath.startsWith(localMonacoSourceDir) || !fs.existsSync(filePath) || fs.statSync(filePath).isDirectory()) {
          next()
          return
        }

        res.setHeader('Content-Type', getContentType(filePath))
        fs.createReadStream(filePath).pipe(res)
      })
    }
  }
}

export default defineConfig({
  plugins: [
    vue(),
    localMonacoAssetsPlugin(),
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
        manualChunks(id) {
          if (id.includes('node_modules/@monaco-editor/loader')) return 'monaco-loader'
          if (id.includes('node_modules/@monaco-editor')) return 'monaco-loader'
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
})
