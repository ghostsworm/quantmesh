/**
 * Translate webui i18n locale files using Gemini API with size-based chunking.
 * Splits en-US.json into chunks by character limit; large top-level keys are
 * split by nested keys. Each chunk is translated separately and results merged.
 * Requires GEMINI_API_KEY in env or .env.
 *
 * Usage:
 *   node scripts/translate-locales-chunked.js
 *   node scripts/translate-locales-chunked.js de-DE fr-FR
 */

import fs from 'fs'
import path from 'path'
import { fileURLToPath } from 'url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const LOCALES_DIR = path.join(__dirname, '../src/i18n/locales')
const SOURCE_FILE = 'en-US.json'
const SKIP_SOURCE = ['en-US', 'zh-CN']

// Keep each chunk under this size (chars) to avoid Gemini truncation
const MAX_CHUNK_CHARS = 7000
const DELAY_MS = 400
const MAX_RETRIES = 2

const LOCALE_NAMES = {
  'zh-TW': 'Traditional Chinese (繁體中文)',
  'ar-SA': 'Arabic',
  'de-DE': 'German',
  'es-ES': 'Spanish',
  'fr-FR': 'French',
  'hi-IN': 'Hindi',
  'id-ID': 'Indonesian',
  'it-IT': 'Italian',
  'ko-KR': 'Korean',
  'nl-NL': 'Dutch',
  'pt-BR': 'Portuguese (Brazil)',
  'ru-RU': 'Russian',
  'tr-TR': 'Turkish',
  'vi-VN': 'Vietnamese'
}

function loadEnv() {
  const candidates = [
    path.resolve(__dirname, '../../..'),
    path.resolve(__dirname, '..')
  ]
  for (const root of candidates) {
    const envPath = path.join(root, '.env')
    if (fs.existsSync(envPath)) {
      const content = fs.readFileSync(envPath, 'utf8')
      for (const line of content.split('\n')) {
        const m = line.match(/^\s*GEMINI_API_KEY\s*=\s*(.+)/)
        if (m) process.env.GEMINI_API_KEY = m[1].trim().replace(/^["']|["']$/g, '')
      }
      if (process.env.GEMINI_API_KEY) break
    }
  }
}

/**
 * Build list of chunks: each chunk is { key, payload } where payload is
 * a JSON-serializable object to send to Gemini (e.g. { common: {...} }).
 * Large top-level values are split by nested keys.
 */
function buildChunks(source) {
  const chunks = []
  for (const topKey of Object.keys(source)) {
    const value = source[topKey]
    const single = { [topKey]: value }
    const singleStr = JSON.stringify(single)
    if (singleStr.length <= MAX_CHUNK_CHARS) {
      chunks.push({ key: topKey, payload: single })
      continue
    }
    // Value too large: split by nested keys (only for plain objects)
    if (typeof value !== 'object' || value === null || Array.isArray(value)) {
      chunks.push({ key: topKey, payload: single })
      continue
    }
    const nestedKeys = Object.keys(value)
    let group = []
    let groupSize = 0
    const groupOverhead = JSON.stringify({ [topKey]: {} }).length
    for (const nk of nestedKeys) {
      const itemStr = JSON.stringify({ [nk]: value[nk] })
      const added = (group.length ? 2 : 0) + itemStr.length
      if (groupSize + added > MAX_CHUNK_CHARS - groupOverhead - 20 && group.length > 0) {
        const subset = {}
        for (const k of group) subset[k] = value[k]
        chunks.push({ key: topKey, payload: { [topKey]: subset } })
        group = []
        groupSize = 0
      }
      group.push(nk)
      groupSize += added
    }
    if (group.length > 0) {
      const subset = {}
      for (const k of group) subset[k] = value[k]
      chunks.push({ key: topKey, payload: { [topKey]: subset } })
    }
  }
  return chunks
}

function deepMerge(target, source) {
  for (const key of Object.keys(source)) {
    if (
      source[key] &&
      typeof source[key] === 'object' &&
      !Array.isArray(source[key]) &&
      target[key] &&
      typeof target[key] === 'object' &&
      !Array.isArray(target[key])
    ) {
      deepMerge(target[key], source[key])
    } else {
      target[key] = source[key]
    }
  }
}

async function translateChunk(apiKey, chunkJson, targetLocale, chunkLabel) {
  const langName = LOCALE_NAMES[targetLocale] || targetLocale
  const prompt = `You are a precise translator. Translate ONLY the string values in the following JSON to ${langName}. Keep every key unchanged. Preserve nested structure and placeholders like {{.Version}}, {{count}}, {{exchange}}, {{symbol}}, {{path}}, {{days}}, {{total}}, {{showing}}, {{current}}, {{year}}, {{month}}, {{open}}, {{close}}, {{input}}, {{output}}, {{minutes}}, {{seconds}}, {{line}}, {{date}}, {{healthy}}, {{module}}. Return only valid JSON, no explanation, no markdown.

JSON:
${chunkJson}`

  const url = `https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent?key=${apiKey}`
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      contents: [{ role: 'user', parts: [{ text: prompt }] }],
      generationConfig: {
        temperature: 0.2,
        maxOutputTokens: 8192,
        responseMimeType: 'application/json'
      }
    })
  })

  if (!res.ok) {
    const err = await res.text()
    throw new Error(`API ${res.status}: ${err}`)
  }

  const data = await res.json()
  const text = data?.candidates?.[0]?.content?.parts?.[0]?.text
  if (!text) throw new Error('No text in response')
  let cleaned = text.trim()
  if (cleaned.startsWith('```')) cleaned = cleaned.replace(/^```\w*\n?|\n?```$/g, '')
  try {
    return JSON.parse(cleaned)
  } catch (e) {
    throw new Error(`Invalid JSON (${chunkLabel}): ${e.message}`)
  }
}

async function sleep(ms) {
  return new Promise((r) => setTimeout(r, ms))
}

async function main() {
  loadEnv()
  const apiKey = process.env.GEMINI_API_KEY
  if (!apiKey) {
    console.error('GEMINI_API_KEY is required. Set it in env or .env.')
    process.exit(1)
  }

  const sourcePath = path.join(LOCALES_DIR, SOURCE_FILE)
  const source = JSON.parse(fs.readFileSync(sourcePath, 'utf8'))
  const chunks = buildChunks(source)

  const requested = process.argv.slice(2).filter(Boolean)
  const locales = requested.length > 0
    ? requested
    : fs.readdirSync(LOCALES_DIR)
        .filter((f) => f.endsWith('.json'))
        .map((f) => path.basename(f, '.json'))
        .filter((code) => !SKIP_SOURCE.includes(code))

  console.log('Chunks:', chunks.length, '| Locales:', locales.join(', '))

  for (const locale of locales) {
    const outPath = path.join(LOCALES_DIR, `${locale}.json`)
    if (!fs.existsSync(outPath)) {
      console.warn('Skip (no file):', locale)
      continue
    }
    const result = {}
    console.log('\nTranslating', locale, '...')
    for (let i = 0; i < chunks.length; i++) {
      const { key, payload } = chunks[i]
      const chunkStr = JSON.stringify(payload)
      const label = `${key}#${i + 1}/${chunks.length}`
      let lastErr
      for (let retry = 0; retry <= MAX_RETRIES; retry++) {
        try {
          const translated = await translateChunk(apiKey, chunkStr, locale, label)
          if (translated[key] === undefined) throw new Error('Missing key in response')
          if (result[key]) deepMerge(result[key], translated[key])
          else result[key] = translated[key]
          process.stdout.write('.')
          break
        } catch (e) {
          lastErr = e
          if (retry < MAX_RETRIES) await sleep(1000 * (retry + 1))
        }
      }
      if (lastErr && !result[key]) {
        console.error('\nFailed', locale, label, lastErr.message, '- using source for', key)
        if (source[key] !== undefined) {
          if (result[key]) deepMerge(result[key], source[key])
          else result[key] = JSON.parse(JSON.stringify(source[key]))
        }
      }
      await sleep(DELAY_MS)
    }
    const expectedKeys = Object.keys(source).length
    const gotKeys = Object.keys(result).length
    if (gotKeys >= expectedKeys) {
      fs.writeFileSync(outPath, JSON.stringify(result, null, 2) + '\n', 'utf8')
      console.log('\nWritten:', outPath)
    } else {
      console.log('\nSkipped', locale, `(got ${gotKeys}/${expectedKeys} keys)`)
    }
  }
  console.log('\nDone.')
}

main().catch((e) => {
  console.error(e)
  process.exit(1)
})
