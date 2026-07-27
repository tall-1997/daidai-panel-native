import fs from 'node:fs'
import path from 'node:path'

const distDir = path.resolve(process.cwd(), 'dist')

function removeEntry(targetPath) {
  const stat = fs.lstatSync(targetPath)

  if (stat.isDirectory()) {
    for (const entry of fs.readdirSync(targetPath)) {
      removeEntry(path.join(targetPath, entry))
    }
    fs.rmdirSync(targetPath)
    return
  }

  fs.unlinkSync(targetPath)
}

if (fs.existsSync(distDir)) {
  for (const entry of fs.readdirSync(distDir)) {
    removeEntry(path.join(distDir, entry))
  }
}
