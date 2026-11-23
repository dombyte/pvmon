import vueI18n from '@intlify/eslint-plugin-vue-i18n'
import vue from 'eslint-plugin-vue'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  { ignores: ['dist', 'node_modules', 'public', 'test-results', 'playwright-report'] },
  ...tseslint.configs.recommended,
  ...vue.configs['flat/recommended'],
  ...vueI18n.configs.recommended,
  {
    files: ['**/*.vue'],
    languageOptions: { parserOptions: { parser: tseslint.parser } },
  },
  {
    settings: {
      'vue-i18n': {
        localeDir: './src/i18n/*.json',
        messageSyntaxVersion: '^11.0.0',
      },
    },
    rules: {
      '@intlify/vue-i18n/no-missing-keys': 'error',
      '@intlify/vue-i18n/no-unused-keys': [
        'error',
        { extensions: ['.ts', '.vue'], ignores: ['/^(errors|nav|theme|language|pages|roles|energy|status\\.level)\\./'] },
      ],
      '@intlify/vue-i18n/no-missing-keys-in-other-locales': 'error',
      '@intlify/vue-i18n/no-raw-text': 'off',
      '@intlify/vue-i18n/no-v-html': 'error',
      'vue/multi-word-component-names': 'off',
      'vue/max-attributes-per-line': 'off',
      'vue/singleline-html-element-content-newline': 'off',
      'vue/html-self-closing': 'off',
      'no-restricted-properties': [
        'error',
        { property: 'toFixed', message: 'Use the format module (spec 11.10).' },
        { property: 'toLocaleString', message: 'Use the format module (spec 11.10).' },
      ],
    },
  },
  {
    files: ['src/format/**', '**/*.test.ts'],
    rules: { 'no-restricted-properties': 'off' },
  },
)
