import DefaultTheme from 'vitepress/theme'
import { theme, useOpenapi } from 'vitepress-openapi/client'
import 'vitepress-openapi/dist/style.css'
import { data as spec } from '../spec.data.mjs'

export default {
  extends: DefaultTheme,
  enhanceApp(context) {
    useOpenapi({
      spec,
      config: {
        // The site cannot send requests to a pvmon instance. Swagger UI in pvmon can.
        // One column, because two columns cut off the code samples.
        operation: { hiddenSlots: ['playground', 'branding'], cols: 1 },
        codeSamples: {
          availableLanguages: [
            { lang: 'curl', label: 'cURL', target: 'shell', client: 'curl', highlighter: 'bash' },
          ],
        },
      },
    })
    theme.enhanceApp(context)
  },
}
