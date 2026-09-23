import { onMounted, onUnmounted, ref, watch } from 'vue'

type ColorMode = 'system' | 'light' | 'dark'

const preference = ref<ColorMode>(
  (localStorage.getItem('color-mode') as ColorMode) || 'system',
)

function isNightTime(date = new Date()) {
  const hour = date.getHours()
  return hour >= 19 || hour < 7
}

function applyMode(mode: ColorMode) {
  const media = window.matchMedia('(prefers-color-scheme: dark)')
  const isDark = mode === 'dark' || (mode === 'system' && (media.matches || isNightTime()))
  document.documentElement.classList.toggle('dark', isDark)
}

applyMode(preference.value)

watch(preference, (val) => {
  localStorage.setItem('color-mode', val)
  applyMode(val)
})

export function useColorMode() {
  let mediaQuery: MediaQueryList | null = null
  let timeTimer: ReturnType<typeof window.setInterval> | null = null

  function onSystemChange() {
    if (preference.value === 'system') applyMode('system')
  }

  onMounted(() => {
    mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
    mediaQuery.addEventListener('change', onSystemChange)
    timeTimer = window.setInterval(onSystemChange, 60 * 1000)
  })

  onUnmounted(() => {
    mediaQuery?.removeEventListener('change', onSystemChange)
    if (timeTimer) window.clearInterval(timeTimer)
  })

  return { preference }
}
