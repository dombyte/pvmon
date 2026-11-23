import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { readStored, writeStored } from '@/composables/storage'
import type { Lang } from '@/format'
import { detectLang, isLang } from '@/i18n'

export type ThemeChoice = 'system' | 'light' | 'dark'

const themeKey = 'pvmon.theme'
const langKey = 'pvmon.lang'

function isTheme(s: string | null): s is ThemeChoice {
  return s === 'system' || s === 'light' || s === 'dark'
}

function initialLang(): Lang {
  const stored = readStored(langKey)
  if (isLang(stored)) return stored
  return detectLang(navigator.languages?.length ? navigator.languages : [navigator.language])
}

/** Applies the choice to <html>: "system" removes the override so the OS decides. */
function applyTheme(choice: ThemeChoice): void {
  const root = document.documentElement
  if (choice === 'system') root.removeAttribute('data-theme')
  else root.setAttribute('data-theme', choice)
}

export const usePreferencesStore = defineStore('preferences', () => {
  const storedTheme = readStored(themeKey)
  const theme = ref<ThemeChoice>(isTheme(storedTheme) ? storedTheme : 'system')
  const language = ref<Lang>(initialLang())

  watch(
    theme,
    (v) => {
      writeStored(themeKey, v)
      applyTheme(v)
    },
    { immediate: true },
  )
  watch(language, (v) => {
    writeStored(langKey, v)
    document.documentElement.lang = v
  }, { immediate: true })

  return { theme, language }
})
