/**
 * Merge missing i18n keys from en-US.json into other locale files.
 * Missing keys get the English value as fallback so the UI never shows raw key names.
 * Run from repo root: node webui/scripts/merge-locales.js
 */

import fs from 'fs'
import path from 'path'
import { fileURLToPath } from 'url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const LOCALES_DIR = path.join(__dirname, '../src/i18n/locales')
const SOURCE_FILE = 'en-US.json'
const SKIP_FILES = ['en-US.json', 'zh-CN.json'] // zh-CN is complete; en-US is source

function deepMergeMissing(source, target) {
  if (source === null || typeof source !== 'object' || Array.isArray(source)) {
    return
  }
  for (const key of Object.keys(source)) {
    const s = source[key]
    const t = target[key]
    if (t === undefined) {
      if (typeof s === 'object' && s !== null && !Array.isArray(s)) {
        target[key] = {}
        deepMergeMissing(s, target[key])
      } else {
        target[key] = JSON.parse(JSON.stringify(s))
      }
    } else if (typeof s === 'object' && s !== null && !Array.isArray(s) && typeof t === 'object' && t !== null && !Array.isArray(t)) {
      deepMergeMissing(s, t)
    }
  }
}

function main() {
  const sourcePath = path.join(LOCALES_DIR, SOURCE_FILE)
  const source = JSON.parse(fs.readFileSync(sourcePath, 'utf8'))

  const files = fs.readdirSync(LOCALES_DIR).filter((f) => f.endsWith('.json') && !SKIP_FILES.includes(f))
  let merged = 0
  for (const file of files) {
    const targetPath = path.join(LOCALES_DIR, file)
    const target = JSON.parse(fs.readFileSync(targetPath, 'utf8'))
    const before = JSON.stringify(target).length
    deepMergeMissing(source, target)
    fs.writeFileSync(targetPath, JSON.stringify(target, null, 2) + '\n', 'utf8')
    const after = JSON.stringify(target).length
    if (after > before) merged++
    console.log('Merged:', file)
  }
  console.log('Done. Updated', merged, 'locale file(s).')
}

main()
