<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { Window } from '@wailsio/runtime'
import { clamp } from '@vueuse/core'
import CaptionClose from '../components/ui/CaptionClose.vue'
import { useAppInfo } from '../composables/useAppInfo'
import { usePlatform } from '../composables/usePlatform'
import { t } from '../i18n'

const { customTitlebar } = usePlatform()
const { name: appName, versionLabel } = useAppInfo()

const year = new Date().getFullYear()

const R = 372.8
const TOP = 151.6
const BOTTOM = 897.2
const DISC_SCALE = 0.68
const TURNS = 2
const TRAIL = 14
const SPLIT_START = 0.7
const SPLIT_END = 1.15
const ANIMATION_END = 1.15

const DISC_A = `M0 ${-R}A${R} ${R} 0 0 0 0 ${R}Z`
const DISC_B = `M0 ${-R}A${R} ${R} 0 0 1 0 ${R}Z`

type Disc = {
  opacity: number
  a: string
  b: string
}

const opacity = ref(0)
const scale = ref(0.94)
const discs = ref<Disc[]>([])

let raf = 0

function easeOut(value: number, power = 3) {
  return 1 - Math.pow(1 - value, power)
}

function progress(time: number, from: number, to: number) {
  return easeOut(clamp((time - from) / (to - from), 0, 1))
}

function rotation(time: number) {
  return 360 * TURNS * progress(time, 0, 0.85)
}

function renderMark(time: number) {
  const intro = progress(time, 0, 0.4)
  const split = clamp((time - SPLIT_START) / (SPLIT_END - SPLIT_START), 0, 1)

  opacity.value = intro
  scale.value = 0.94 + 0.06 * intro

  // The spin settles the short way: near the split the remainder is wrapped into (-180, 180] —
  // without it a remainder of ~356° unwinds almost a full turn and the settle reads as a jerk.
  const currentRotation = rotation(time)
  const raw = currentRotation % 360
  const angle = (raw > 180 ? raw - 360 : raw) * (1 - split)

  const previousRotation = rotation(Math.max(0, time - 1 / 30))
  const arc = Math.min((currentRotation - previousRotation) * 1.6, 200)
  const hasTrail = arc > 1

  const discScale = DISC_SCALE + (1 - DISC_SCALE) * split
  const top = 512 + (TOP - 512) * split
  const bottom = 512 + (BOTTOM - 512) * split

  discs.value = Array.from({ length: TRAIL }, (_, i) => {
    if (i > 0 && !hasTrail) {
      return { opacity: 0, a: '', b: '' }
    }

    const trailAngle = angle - (hasTrail ? arc * i / (TRAIL - 1) : 0)

    const transform = (y: number) =>
        `translate(512 ${y}) rotate(${trailAngle}) scale(${discScale})`

    return {
      opacity: 1 / (i + 1),
      a: transform(top),
      b: transform(bottom),
    }
  })
}

function startAnimation() {
  renderMark(0)

  if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
    renderMark(ANIMATION_END)
    return
  }

  const start = performance.now()

  function frame(now: number) {
    const time = Math.min((now - start) / 1000, ANIMATION_END)

    renderMark(time)

    if (time < ANIMATION_END) {
      raf = requestAnimationFrame(frame)
    }
  }

  raf = requestAnimationFrame(frame)
}

onMounted(() => {
  void Window.SetTitle(t('about.title'))
  startAnimation()
})

onBeforeUnmount(() => {
  cancelAnimationFrame(raf)
})
</script>

<template>
  <div class="about-window">
    <header v-if="customTitlebar" class="about-bar">
      <span class="about-bar-title">{{ t('about.title') }}</span>

      <CaptionClose @close="Window.Close()" />
    </header>

    <div v-else class="about-bar-mac"></div>

    <div class="about-body">
      <svg
          class="about-icon"
          viewBox="0 0 1024 1024"
          aria-hidden="true"
          :style="{ opacity, transform: `scale(${scale})` }"
      >
        <defs>
          <linearGradient id="about-logo-g" x1="0" y1="0" x2="1" y2="1">
            <stop offset="0" stop-color="#6a6cff" />
            <stop offset="1" stop-color="#3a2bd1" />
          </linearGradient>

          <clipPath id="about-logo-c">
            <rect width="1024" height="1024" rx="230" />
          </clipPath>

          <filter id="about-logo-s" x="-10%" y="-10%" width="120%" height="125%">
            <feDropShadow
                dx="0"
                dy="12"
                stdDeviation="14"
                flood-color="#000"
                flood-opacity="0.28"
            />
          </filter>
        </defs>

        <g filter="url(#about-logo-s)">
          <rect width="1024" height="1024" rx="230" fill="url(#about-logo-g)" />

          <g clip-path="url(#about-logo-c)">
            <g v-for="(disc, i) in discs" :key="i" :opacity="disc.opacity">
              <path :d="DISC_A" fill="#fff" :transform="disc.a" />
              <path :d="DISC_B" fill="#ffb08a" :transform="disc.b" />
            </g>
          </g>
        </g>
      </svg>

      <div class="about-name">{{ appName }}</div>
      <div class="about-version">
        {{ t('about.version', { version: versionLabel || '…' }) }}
      </div>

      <div class="about-divider"></div>

      <div class="about-copy">
        {{ t('about.copyright', { year, name: appName || 'Schemo' }) }}<br />
        {{ t('about.rights') }}
      </div>
    </div>
  </div>
</template>

<style scoped>
@reference "../style.css";

.about-window {
  @apply h-full flex flex-col bg-bg-panel text-text select-none;
}

.about-bar-mac {
  @apply flex-none;
  height: 3.125rem;
  --wails-draggable: drag;
}

.about-bar {
  @apply relative flex-none h-13 flex items-center justify-start px-3;
  border-bottom: 1px solid var(--glass-chrome-border);
  background: var(--glass-chrome);
  backdrop-filter: var(--blur-chrome);
  --wails-draggable: drag;
}

.about-bar-title {
  @apply text-13 font-medium text-text-secondary;
}

.about-bar .cap-btn {
  @apply absolute right-0;
}

.about-body {
  @apply flex-1 flex flex-col items-center justify-center px-7.5 pt-5.5 pb-9 text-center gap-0.5;
}

.about-icon {
  @apply w-26 h-26 mb-4;
  filter: drop-shadow(0 0.75rem 1.25rem rgba(106, 108, 255, 0.4));
}

.about-name {
  @apply text-19 font-bold tracking-[-0.015em];
}

.about-version {
  @apply font-mono text-12 text-text-tertiary tabular-nums mb-4.5;
}

.about-divider {
  @apply w-full h-px mt-4 mb-3.5;
  background: var(--border);
}

.about-copy {
  @apply text-12 leading-[1.6] text-text-tertiary;
}
</style>