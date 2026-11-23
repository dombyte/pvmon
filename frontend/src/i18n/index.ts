import { createI18n } from 'vue-i18n'
import type { Lang } from '@/format'
import de from './de.json'
import en from './en.json'

export const languages: readonly Lang[] = ['de', 'en']
const fallbackLang: Lang = 'en'

export function isLang(s: string | null | undefined): s is Lang {
  return s === 'de' || s === 'en'
}

/** Picks the first supported language of the browser's preference list, else English. */
export function detectLang(preferred: readonly string[]): Lang {
  for (const tag of preferred) {
    const base = tag.toLowerCase().split('-')[0]
    if (isLang(base)) return base
  }
  return fallbackLang
}

export function createAppI18n(lang: Lang) {
  return createI18n({
    legacy: false,
    locale: lang,
    fallbackLocale: fallbackLang,
    messages: { de, en },
  })
}
