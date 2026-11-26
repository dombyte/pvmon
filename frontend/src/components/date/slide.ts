import { ref, watch } from 'vue'

/** The animation class of a calendar page: it follows the direction of the last turn. */
export function useSlide(page: () => string) {
  const slide = ref('')
  watch(page, (now, before) => (slide.value = now > before ? 'slide-next' : 'slide-prev'))
  return slide
}
