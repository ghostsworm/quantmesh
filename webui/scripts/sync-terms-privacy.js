/**
 * Sync expanded Terms of Service and Privacy Policy sections from en-US to other locale files.
 * Keeps each locale's terms.title, lastUpdated, backToLogin, backToApp and only replaces sections.
 */
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const localesDir = path.join(__dirname, '../src/i18n/locales');
const enPath = path.join(localesDir, 'en-US.json');

const otherLocales = [
  'fr-FR.json', 'es-ES.json', 'ko-KR.json', 'pt-BR.json', 'it-IT.json',
  'nl-NL.json', 'ru-RU.json', 'ar-SA.json', 'tr-TR.json', 'vi-VN.json',
  'id-ID.json', 'hi-IN.json'
];

const en = JSON.parse(fs.readFileSync(enPath, 'utf8'));
const termsSections = en.terms.sections;
const privacySections = en.privacy.sections;

for (const file of otherLocales) {
  const filePath = path.join(localesDir, file);
  const data = JSON.parse(fs.readFileSync(filePath, 'utf8'));
  if (data.terms && data.terms.sections) {
    data.terms.sections = JSON.parse(JSON.stringify(termsSections));
  }
  if (data.privacy && data.privacy.sections) {
    data.privacy.sections = JSON.parse(JSON.stringify(privacySections));
  }
  fs.writeFileSync(filePath, JSON.stringify(data, null, 2) + '\n', 'utf8');
  console.log('Updated', file);
}

console.log('Done. Terms and privacy sections synced from en-US to', otherLocales.length, 'locales.');
