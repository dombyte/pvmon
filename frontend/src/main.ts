import '@fontsource-variable/inter'
import { createPinia } from 'pinia'
import { createApp, watch } from 'vue'
import App from './App.vue'
import { createAppI18n } from './i18n'
import { LiveClient, liveURL } from './live/client'
import { liveKey } from './live/useSubscription'
import { createAppRouter } from './router'
import { usePreferencesStore } from './stores/preferences'
import { useSessionStore } from './stores/session'
import { useSiteStore } from './stores/site'
import { preventZoom } from './layout/noZoom'
import { registerWorker } from './pwa/update'
import { useVersionStore } from './stores/version'
import './styles/main.css'

const app = createApp(App)
const pinia = createPinia()
app.use(pinia)

const prefs = usePreferencesStore()
const i18n = createAppI18n(prefs.language)
watch(
  () => prefs.language,
  (lang) => {
    i18n.global.locale.value = lang
  },
)

const site = useSiteStore()
const live = new LiveClient({
  url: liveURL(window.location),
  onDeviceState: (device, state) => site.setDeviceState(device, state),
  onSiteChanged: () => void site.load(),
})

app.use(i18n).use(createAppRouter()).provide(liveKey, live)
app.mount('#app')

void site.load()
live.start()

// Logging in or out changes what may be read (auth.public_read: false): reload the model
// and reconnect the live data at once instead of after their backoff.
watch(
  () => useSessionStore().user?.id,
  () => {
    void site.load()
    live.stop()
    live.start()
  },
)
useVersionStore().start()

registerWorker()

preventZoom(document)
