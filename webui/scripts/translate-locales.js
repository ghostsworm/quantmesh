/**
 * Translate webui i18n locale files using Gemini API.
 * Source: en-US.json. Target locales get full translation of values (keys unchanged).
 * Requires GEMINI_API_KEY in env or .env at project root.
 *
 * Usage:
 *   GEMINI_API_KEY=your_key node webui/scripts/translate-locales.js
 *   GEMINI_API_KEY=your_key node webui/scripts/translate-locales.js zh-TW fr-FR   # only these locales
 */

import fs from 'fs'
import path from 'path'
import { fileURLToPath } from 'url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const LOCALES_DIR = path.join(__dirname, '../src/i18n/locales')
const SOURCE_FILE = 'en-US.json'
const SKIP_SOURCE = ['en-US', 'zh-CN']

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
    path.resolve(__dirname, '../../..'), // repo root
    path.resolve(__dirname, '..')        // webui
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

async function translateWithGemini(apiKey, sourceJson, targetLocale) {
  const langName = LOCALE_NAMES[targetLocale] || targetLocale
  const prompt = `You are a precise translator. Translate ONLY the string values in the following JSON to ${langName}. Keep every key unchanged. Preserve nested structure and placeholders like {{.Version}}, {{count}}, etc. Return only the JSON object, no explanation, no markdown code fence.

JSON:
${sourceJson}`

  const url = `https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent?key=${apiKey}`
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      contents: [{ role: 'user', parts: [{ text: prompt }] }],
      generationConfig: {
        temperature: 0.2,
        maxOutputTokens: 65536,
        responseMimeType: 'application/json'
      }
    })
  })

  if (!res.ok) {
    const err = await res.text()
    throw new Error(`Gemini API error ${res.status}: ${err}`)
  }

  const data = await res.json()
  const text = data?.candidates?.[0]?.content?.parts?.[0]?.text
  if (!text) throw new Error('No text in Gemini response')
  let cleaned = text.trim()
  if (cleaned.startsWith('```')) cleaned = cleaned.replace(/^```\w*\n?|\n?```$/g, '')
  return JSON.parse(cleaned)
}

async function main() {
  loadEnv()
  const apiKey = process.env.GEMINI_API_KEY
  if (!apiKey) {
    console.error('GEMINI_API_KEY is required. Set it in env or .env at project root / webui.')
    process.exit(1)
  }

  const sourcePath = path.join(LOCALES_DIR, SOURCE_FILE)
  const source = JSON.parse(fs.readFileSync(sourcePath, 'utf8'))
  const sourceStr = JSON.stringify(source)

  const requested = process.argv.slice(2).filter(Boolean)
  const locales = requested.length > 0
    ? requested
    : fs.readdirSync(LOCALES_DIR)
        .filter((f) => f.endsWith('.json'))
        .map((f) => path.basename(f, '.json'))
        .filter((code) => !SKIP_SOURCE.includes(code))

  if (sourceStr.length > 90000) {
    console.warn('Source JSON is large; consider splitting or using a model with larger context.')
  }

  for (const locale of locales) {
    const outPath = path.join(LOCALES_DIR, `${locale}.json`)
    if (!fs.existsSync(outPath)) {
      console.warn('Skip (no file):', locale)
      continue
    }
    try {
      console.log('Translating', locale, '...')
      const translated = await translateWithGemini(apiKey, sourceStr, locale)
      fs.writeFileSync(outPath, JSON.stringify(translated, null, 2) + '\n', 'utf8')
      console.log('Written:', outPath)
    } catch (e) {
      console.error('Failed', locale, e.message)
    }
  }
  console.log('Done.')
}

main().catch((e) => {
  console.error(e)
  process.exit(1)
})
