import loader, { type Monaco } from '@monaco-editor/loader'

const MONACO_CDN_VS = 'https://cdn.jsdelivr.net/npm/monaco-editor@0.55.1/min/vs'
const LOCAL_MONACO_VS = 'monaco/vs'
const LOCAL_MONACO_REQUIRED_FILES = [
  'loader.js',
  'editor/editor.main.js',
  'editor/editor.main.css',
  'language/css/monaco.contribution.js',
  'language/html/monaco.contribution.js',
  'language/json/monaco.contribution.js',
  'language/typescript/monaco.contribution.js',
]

type MonacoSource = 'local' | 'cdn'

export interface MonacoLoadResult {
  monaco: Monaco
  source: MonacoSource
}

let monacoPromise: Promise<MonacoLoadResult> | null = null

function getLocalMonacoWorkerUrl() {
  return `${import.meta.env.BASE_URL}${LOCAL_MONACO_VS}/loader.js`
}

function getLocalMonacoAssetUrl(relativePath: string) {
  return `${import.meta.env.BASE_URL}${LOCAL_MONACO_VS}/${relativePath}`
}

async function checkMonacoAssetExists(relativePath: string) {
  const assetUrl = getLocalMonacoAssetUrl(relativePath)

  try {
    const headResponse = await fetch(assetUrl, { method: 'HEAD', cache: 'no-store' })
    if (headResponse.ok) {
      return true
    }
    if (headResponse.status !== 405) {
      return false
    }
  } catch {
    return false
  }

  try {
    const getResponse = await fetch(assetUrl, { method: 'GET', cache: 'no-store' })
    getResponse.body?.cancel?.()
    return getResponse.ok
  } catch {
    return false
  }
}

async function canUseLocalMonaco() {
  // 不能只看 loader.js。
  // v2.2.19 的故障就是 loader.js 还在，但 editor/main、worker、basic-languages 等已被裁掉，
  // 导致前端误判“本地 Monaco 可用”，真正初始化时才崩。
  for (const relativePath of LOCAL_MONACO_REQUIRED_FILES) {
    const exists = await checkMonacoAssetExists(relativePath)
    if (!exists) {
      return false
    }
  }
  return true
}

async function loadLocalMonaco(): Promise<MonacoLoadResult> {
  loader.config({
    paths: {
      vs: `${import.meta.env.BASE_URL}${LOCAL_MONACO_VS}`
    }
  })

  const monaco = await loader.init()
  return { monaco, source: 'local' }
}

async function loadCdnMonaco(): Promise<MonacoLoadResult> {
  loader.config({
    paths: {
      vs: MONACO_CDN_VS
    }
  })

  const monaco = await loader.init()
  return { monaco, source: 'cdn' }
}

export async function loadMonacoEditor(): Promise<MonacoLoadResult> {
  if (!monacoPromise) {
    monacoPromise = (async () => {
      if (await canUseLocalMonaco()) {
        try {
          return await loadLocalMonaco()
        } catch (error) {
          console.warn('本地 Monaco 资源加载失败，已回退到 CDN。', error)
        }
      }

      return loadCdnMonaco()
    })().catch((error) => {
      monacoPromise = null
      throw error
    })
  }

  return monacoPromise
}
