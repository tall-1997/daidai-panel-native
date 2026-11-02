/*
 * 一次性抓取 web/public/fonts/ 下的 woff2 二进制，产物【提交进版本库】。
 *
 * 为什么是「跑一次然后进库」而不是构建期下载：
 *   checks.yml / release.yml / deploy-demo.yml 三条流水线都只跑 `npm ci` + `vite build`。
 *   把字体挪到构建期联网，等于给每次发版加一个 fonts.gstatic.com 的单点依赖 ——
 *   这恰好是本次改动想消灭的那类依赖，只是从运行期挪到了构建期。
 *   同理，也不引入 @fontsource 之类的 npm 包：那会把字体版本绑到 lockfile 上，
 *   离线/内网克隆仓库后仍然要能直接构建。
 *
 * 用法（仓库根或 web/ 下都行）：
 *   node web/scripts/fetch-fonts.mjs
 *
 * 期望产出的文件名不是写死在本脚本里的，而是从 web/public/fonts/fonts.css 的
 * url("./xxx.woff2") 反解出来的 —— fonts.css 与磁盘上的文件因此不可能漂移：
 * 改了 fonts.css 忘了重跑脚本，或者反过来，都会在这里直接报错。
 */
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const scriptDir = path.dirname(fileURLToPath(import.meta.url))
const webDir = path.resolve(scriptDir, '..')
const fontsDir = path.join(webDir, 'public', 'fonts')
const fontsCssFile = path.join(fontsDir, 'fonts.css')

// 只要这两个子集。中文走系统字体，cyrillic/greek/vietnamese 用不上。
const WANTED_SUBSETS = new Set(['latin', 'latin-ext'])

// family 在 Google Fonts 上的名字 -> 落盘文件名前缀（要与 fonts.css 对得上）。
const FAMILIES = [
  { googleName: 'Inter', slug: 'inter', weights: [400, 500, 600, 700] },
  { googleName: 'JetBrains Mono', slug: 'jetbrains-mono', weights: [400, 500] }
]

// 不带这个 UA，Google 会回 ttf/eot 之类的老格式，拿不到 woff2。
const CHROME_UA =
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36'

/** 从 fonts.css 里反解出所有 url("./xxx.woff2") 的文件名。 */
function readExpectedFileNames() {
  if (!fs.existsSync(fontsCssFile)) {
    throw new Error(`找不到 ${fontsCssFile}`)
  }
  const css = fs.readFileSync(fontsCssFile, 'utf8')
  const names = new Set()
  for (const m of css.matchAll(/url\(["']?\.\/([^"')]+\.woff2)["']?\)/g)) {
    names.add(m[1])
  }
  if (names.size === 0) {
    throw new Error('fonts.css 里没解析出任何 .woff2 引用，格式可能被改动过')
  }
  return names
}

async function fetchFamilyCss({ googleName, weights }) {
  const family = `${googleName.replace(/ /g, '+')}:wght@${weights.join(';')}`
  const url = `https://fonts.googleapis.com/css2?family=${family}&display=swap`
  const res = await fetch(url, { headers: { 'User-Agent': CHROME_UA } })
  if (!res.ok) {
    throw new Error(`拉取 ${googleName} 的 CSS 失败：HTTP ${res.status} ${url}`)
  }
  return res.text()
}

/**
 * 解析 Google 的 CSS。它在每个 @font-face 前面都有一行 `/* subset *\/` 注释，
 * 这是拿到子集名最可靠的途径（比反查 unicode-range 稳）。
 */
function parseFaces(css) {
  const faces = []
  const blockRe = /\/\*\s*([a-z0-9-]+)\s*\*\/\s*@font-face\s*\{([\s\S]*?)\}/g
  for (const m of css.matchAll(blockRe)) {
    const subset = m[1]
    const body = m[2]
    const weight = body.match(/font-weight:\s*([^;]+);/)?.[1]?.trim()
    const src = body.match(/src:\s*url\((https:\/\/[^)]+\.woff2)\)/)?.[1]
    if (!weight || !src) continue
    faces.push({ subset, weight, src })
  }
  return faces
}

async function download(url) {
  const res = await fetch(url, { headers: { 'User-Agent': CHROME_UA } })
  if (!res.ok) throw new Error(`下载失败：HTTP ${res.status} ${url}`)
  return Buffer.from(await res.arrayBuffer())
}

async function main() {
  const expected = readExpectedFileNames()
  fs.mkdirSync(fontsDir, { recursive: true })

  const written = new Map()

  for (const family of FAMILIES) {
    const css = await fetchFamilyCss(family)
    const faces = parseFaces(css)
    if (faces.length === 0) {
      throw new Error(`没能从 ${family.googleName} 的 CSS 里解析出 @font-face，Google 的返回格式可能变了`)
    }

    for (const face of faces) {
      if (!WANTED_SUBSETS.has(face.subset)) continue

      // 变量字体在某些请求形态下会返回 `font-weight: 100 900` 这样的区间，
      // 那种情况下没法按离散字重落盘，直接报错比默默产出错文件好。
      if (!/^\d+$/.test(face.weight)) {
        throw new Error(
          `${family.googleName} 返回了字重区间 "${face.weight}"（变量字体形态），` +
            '本脚本按离散字重落盘，请检查 css2 请求参数。'
        )
      }

      const fileName = `${family.slug}-${face.subset}-${face.weight}.woff2`
      if (!expected.has(fileName)) continue

      const buf = await download(face.src)
      fs.writeFileSync(path.join(fontsDir, fileName), buf)
      written.set(fileName, buf.length)
    }
  }

  const missing = [...expected].filter((name) => !written.has(name))
  if (missing.length > 0) {
    throw new Error(
      `fonts.css 引用了以下文件，但这轮没能抓到：\n  ${missing.join('\n  ')}\n` +
        '要么 fonts.css 里的文件名写错了，要么 Google 那边的子集/字重供给变了。'
    )
  }

  let total = 0
  for (const [name, size] of [...written].sort()) {
    total += size
    console.log(`[fetch-fonts] ${name.padEnd(34)} ${(size / 1024).toFixed(1)} KB`)
  }
  console.log(`[fetch-fonts] 共 ${written.size} 个文件，合计 ${(total / 1024).toFixed(1)} KB`)
}

main().catch((err) => {
  console.error(`[fetch-fonts] ${err.message}`)
  process.exit(1)
})
