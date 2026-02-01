/**
 * Apply a flat key-value patch to a locale JSON. Patch keys use dot path, e.g. "common.save".
 * Usage: node scripts/apply-locale-patch.js <locale> [patch.json]
 * Example: node scripts/apply-locale-patch.js fr-FR scripts/locale-patches/fr-FR.json
 * If patch.json omitted, reads scripts/locale-patches/<locale>.json
 */

import fs from 'fs'
import path from 'path'
import { fileURLToPath } from 'url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const LOCALES_DIR = path.join(__dirname, '../src/i18n/locales')

function setByPath(obj, pathStr, value) {
  const parts = pathStr.split('.')
  let cur = obj
  for (let i = 0; i < parts.length - 1; i++) {
    const k = parts[i]
    if (!(k in cur)) cur[k] = {}
    cur = cur[k]
  }
  cur[parts[parts.length - 1]] = value
}

const locale = process.argv[2]
if (!locale) {
  console.error('Usage: node apply-locale-patch.js <locale> [patch.json]')
  process.exit(1)
}

const patchPath = process.argv[3] || path.join(__dirname, 'locale-patches', `${locale}.json`)
const localePath = path.join(LOCALES_DIR, `${locale}.json`)

if (!fs.existsSync(patchPath)) {
  console.error('Patch file not found:', patchPath)
  process.exit(1)
}
if (!fs.existsSync(localePath)) {
  console.error('Locale file not found:', localePath)
  process.exit(1)
}

const patch = JSON.parse(fs.readFileSync(patchPath, 'utf8'))
const localeObj = JSON.parse(fs.readFileSync(localePath, 'utf8'))

for (const [key, value] of Object.entries(patch)) {
  setByPath(localeObj, key, value)
}

fs.writeFileSync(localePath, JSON.stringify(localeObj, null, 2) + '\n', 'utf8')
console.log('Applied', Object.keys(patch).length, 'translations to', localePath)
