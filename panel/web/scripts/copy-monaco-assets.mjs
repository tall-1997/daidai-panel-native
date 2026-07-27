import fs from 'node:fs'
import path from 'node:path'

const sourceDir = path.resolve(process.cwd(), 'node_modules/monaco-editor/min')
const targetDir = path.resolve(process.cwd(), 'dist/monaco')
const monacoVsDir = path.join(sourceDir, 'vs')
const monacoTargetVsDir = path.join(targetDir, 'vs')

function copyDirectory(source, target) {
  fs.mkdirSync(target, { recursive: true })

  for (const entry of fs.readdirSync(source, { withFileTypes: true })) {
    const sourcePath = path.join(source, entry.name)
    const targetPath = path.join(target, entry.name)

    if (entry.isDirectory()) {
      copyDirectory(sourcePath, targetPath)
      continue
    }

    fs.copyFileSync(sourcePath, targetPath)
  }
}

copyDirectory(sourceDir, targetDir)
// 这里不再用带 hash 的文件名白名单去裁 Monaco 资源。
// 原因：Monaco 每次升级或内部构建调整后，hash 文件名都会变化，
// 白名单很容易把 editor / assets / basic-languages / worker 等运行时必需文件误删，
// 最终表现为“构建成功，但浏览器里编辑器加载失败”。
//
// 当前策略改成直接保留完整的 min 目录，优先保证运行时稳定。
// 未来如果还要继续瘦身，必须基于目录级依赖关系做验证后再裁，不能继续按 hash 白名单赌文件名。
console.log('[copy-monaco-assets] copied monaco-editor/min -> dist/monaco')
