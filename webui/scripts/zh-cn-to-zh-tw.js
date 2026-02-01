/**
 * Generate zh-TW.json from zh-CN.json using OpenCC (Simplified → Traditional).
 * Run from webui: node scripts/zh-cn-to-zh-tw.js
 * No API key required.
 */

import fs from 'fs'
import path from 'path'
import { fileURLToPath } from 'url'
import { Converter } from 'opencc-js'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const LOCALES_DIR = path.join(__dirname, '../src/i18n/locales')
const converter = Converter({ from: 'cn', to: 'tw' })

function convertValue(val) {
  if (typeof val === 'string') {
    return converter(val)
  }
  if (Array.isArray(val)) {
    return val.map(convertValue)
  }
  if (val !== null && typeof val === 'object') {
    const out = {}
    for (const [k, v] of Object.entries(val)) {
      out[k] = convertValue(v)
    }
    return out
  }
  return val
}

function main() {
  const srcPath = path.join(LOCALES_DIR, 'zh-CN.json')
  const outPath = path.join(LOCALES_DIR, 'zh-TW.json')
  const zhCN = JSON.parse(fs.readFileSync(srcPath, 'utf8'))
  const zhTW = convertValue(zhCN)
  fs.writeFileSync(outPath, JSON.stringify(zhTW, null, 2) + '\n', 'utf8')
  console.log('Written:', outPath)
}

main()
