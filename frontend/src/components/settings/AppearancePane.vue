<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { MONO } from '../../lib/layout'
import SettingsRow from './SettingsRow.vue'
import SettingsSection from './SettingsSection.vue'
import Segment from '../ui/Segment.vue'
import Select from '../ui/Select.vue'
import { Accent, MonoFont, Theme, UISize } from '../../../bindings/schemo/internal/domain'
import type { AppearancePatch } from '../../../bindings/schemo/internal/usecase/settings'
import { useSetting, useSettings } from '../../composables/useSettings'
import { useTheme } from '../../composables/useTheme'
import { usePlatform } from '../../composables/usePlatform'
import { useMessages, type MessageKey } from '../../i18n'

// How the window is drawn: the palette, how large it is and what the code is set in. The theme is
// the one part the window needs before it exists.
const { t } = useMessages()
const { loadSettings, setAppearance } = useSettings()
const { theme, setTheme } = useTheme()
const { isMac } = usePlatform()

const look = useSetting('appearance')

const themes = computed(() => [
  { value: Theme.ThemeLight, label: t('theme.light') },
  { value: Theme.ThemeDark, label: t('theme.dark') },
  { value: Theme.ThemeSystem, label: t('theme.system') },
])

// The families the palette already has: the choice moves the accent onto one and mixes nothing new.
const ACCENTS = [
  { value: Accent.AccentBlue, key: 'settings.accentBlue', color: 'var(--blue)' },
  { value: Accent.AccentPurple, key: 'settings.accentPurple', color: 'var(--purple)' },
  { value: Accent.AccentGreen, key: 'settings.accentGreen', color: 'var(--green)' },
  { value: Accent.AccentOrange, key: 'settings.accentOrange', color: 'var(--orange)' },
  { value: Accent.AccentRed, key: 'settings.accentRed', color: 'var(--red)' },
  // The grey is the app's own second grey — the one the accent itself is drawn in, so the swatch
  // and the window agree.
  { value: Accent.AccentGrey, key: 'settings.accentGrey', color: 'var(--text-secondary)' },
] as const satisfies readonly { value: Accent; key: MessageKey; color: string }[]

const accents = computed(() => ACCENTS.map((one) => ({ ...one, label: t(one.key) })))

const sizes = computed(() => [
  { value: UISize.SizeSmall, label: t('settings.sizeSmall') },
  { value: UISize.SizeMedium, label: t('settings.sizeMedium') },
  { value: UISize.SizeLarge, label: t('settings.sizeLarge') },
])

// The keys that walk the scale, said in the platform's own letters: the note is how somebody
// learns they exist.
const zoomNote = computed(() =>
  t('settings.uiSizeNote', { mod: isMac.value ? '⌘' : 'Ctrl' })
)

// The fonts' own names, spelled the same in every language — and one is whatever the machine
// has, which is the only one with a word of its own.
const fonts = computed(() => [
  { value: MonoFont.MonoJetBrains, label: 'JetBrains Mono' },
  { value: MonoFont.MonoCascadia, label: 'Cascadia Code' },
  { value: MonoFont.MonoFira, label: 'Fira Code' },
  { value: MonoFont.MonoSystem, label: t('settings.monoSystem') },
])

const monoSize = computed(() => look.value.monoSize)

function write(patch: AppearancePatch) {
  void setAppearance(patch)
}

function step(delta: number) {
  write({ monoSize: monoSize.value + delta })
}

onMounted(() => {
  void loadSettings()
})
</script>

<template>
  <div class="pane">
    <SettingsSection :label="t('settings.sec.theme')">
      <template #lead>
        <div class="themes">
          <button
            v-for="one in themes"
            :key="one.value"
            type="button"
            class="theme"
            :class="{ on: theme === one.value }"
            @click="setTheme(one.value)"
          >
            <span class="preview" :data-theme="one.value">
              <span class="preview-side"><i></i><i></i><i></i></span>
              <span class="preview-body"><i class="lead"></i><i></i><i></i></span>
            </span>
            <span class="pick">
              <span class="radio" :class="{ on: theme === one.value }"></span>
              <span class="pick-label">{{ one.label }}</span>
            </span>
          </button>
        </div>
      </template>

      <SettingsRow :title="t('settings.accent')" :note="t('settings.accentNote')">
        <span class="swatches">
          <button
            v-for="one in accents"
            :key="one.value"
            type="button"
            class="swatch"
            :class="{ on: look.accent === one.value }"
            :style="{ background: one.color, '--ring': one.color }"
            :title="one.label"
            :aria-label="one.label"
            @click="write({ accent: one.value })"
          ></button>
        </span>
      </SettingsRow>
    </SettingsSection>

    <SettingsSection :label="t('settings.sec.fonts')">
      <SettingsRow :title="t('settings.uiSize')" :note="zoomNote">
        <Segment
          size="sm"
          :grow="false"
          :options="sizes"
          :value="look.size"
          @pick="write({ size: $event as UISize })"
        />
      </SettingsRow>

      <SettingsRow :title="t('settings.monoFont')" :note="t('settings.monoFontNote')">
        <Select
          :model-value="look.monoFont"
          :options="fonts"
          @update:model-value="write({ monoFont: $event as MonoFont })"
        />
        <span class="stepper">
          <button type="button" :disabled="monoSize <= MONO.min" @click="step(-1)">−</button>
          <span class="digits">{{ monoSize }} px</span>
          <button type="button" :disabled="monoSize >= MONO.max" @click="step(1)">+</button>
        </span>
      </SettingsRow>

      <!-- The sample belongs to the row above it: it is what that row's two choices come to, and it is
           drawn just under them rather than a card of its own. -->
      <div class="sample">
        <span class="code">
          <span class="p">{</span> <span class="k">"status"</span><span class="p">: </span>
          <span class="s">"ok"</span><span class="p">,</span>
          <span class="k">"items"</span><span class="p">: </span>
          <span class="n">42</span> <span class="p">}</span>
        </span>
      </div>
    </SettingsSection>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.pane {
  @apply flex flex-col;
  gap: 1.625rem;
}

.themes {
  @apply grid gap-2.5;
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.theme {
  @apply flex flex-col gap-2.5 text-left cursor-pointer rounded-[0.625rem] bg-bg-panel;
  font: inherit;
  padding: 0.625rem;
  border: 1px solid var(--border);
}

.theme:hover {
  border-color: var(--border-strong);
}

.theme.on {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
}

.preview {
  @apply flex h-20 rounded-md overflow-hidden;
  border: 1px solid var(--border);
}

.preview-side {
  @apply flex flex-col flex-none;
  gap: 0.3125rem;
  width: calc(30% + 0.75rem);
  padding: 0.5rem 0.375rem;
}

.preview-body {
  @apply flex-1 flex flex-col;
  gap: 0.3125rem;
  padding: 0.5rem;
}

.preview i {
  @apply block rounded-[0.1875rem];
  height: 0.3125rem;
  background: var(--preview-bar);
}

.preview-side i:nth-child(2) {
  width: 70%;
}

.preview-side i:nth-child(3) {
  width: 80%;
}

.preview-body i.lead {
  height: 0.4375rem;
  width: 50%;
  background: var(--accent);
}

.preview-body i:nth-child(3) {
  width: 60%;
}

.preview[data-theme='light'] {
  --preview-bar: #e3e4e8;
  background: #ffffff;
}

.preview[data-theme='light'] .preview-side {
  background: #f4f4f7;
}

.preview[data-theme='dark'] {
  --preview-bar: #3a3c42;
  background: #1c1d21;
}

.preview[data-theme='dark'] .preview-side {
  background: #26272b;
}

.preview[data-theme='system'] {
  --preview-bar: #b8bac2;
  background: linear-gradient(90deg, #ffffff 50%, #1c1d21 50%) border-box;
}

.preview[data-theme='system'] .preview-side {
  background: linear-gradient(90deg, #f4f4f7 50%, #26272b 50%);
}

.pick {
  @apply flex items-center gap-2;
  padding: 0 0.125rem;
}

.radio {
  @apply flex-none rounded-full;
  width: 0.875rem;
  height: 0.875rem;
  box-sizing: border-box;
  border: 1.5px solid var(--border-strong);
  background: var(--bg-panel);
}

.radio.on {
  border: 4px solid var(--accent);
}

.pick-label {
  @apply text-13 text-text;
}

.swatches {
  @apply flex items-center gap-2;
}

.swatch {
  @apply flex-none rounded-full cursor-pointer;
  width: 1.375rem;
  height: 1.375rem;
  border: 0;
}

.swatch.on {
  box-shadow: 0 0 0 2px var(--bg-panel), 0 0 0 4px var(--ring);
}

.swatch:hover {
  filter: brightness(1.08);
}

.stepper {
  @apply inline-flex items-center h-7.5 flex-none rounded-[0.375rem];
  border: 1px solid var(--border-strong);
  background: var(--bg-inset);
}

.stepper button {
  @apply flex items-center justify-center cursor-pointer text-text-secondary;
  width: 1.75rem;
  height: 1.75rem;
  border: 0;
  background: transparent;
  font: inherit;
  font-size: var(--text-15);
}

.stepper button:hover:not(:disabled) {
  @apply text-text bg-bg-hover;
}

.stepper button:disabled {
  @apply opacity-40 cursor-default;
}

.stepper .digits {
  @apply text-13 text-center;
  min-width: 2.75rem;
  font-variant-numeric: tabular-nums;
}

.sample {
  @apply rounded-lg;
  margin: 0 1rem 0.75rem;
  padding: 0.625rem 0.75rem;
  background: var(--bg-hover);
}

.code {
  font-family: var(--mono);
  font-size: var(--mono-size);
  line-height: 1.6;
  color: var(--text);
}

.code .p {
  color: var(--text-tertiary);
}

.code .k {
  color: var(--text-secondary);
}

.code .s {
  color: var(--tok-str);
}

.code .n {
  color: var(--tok-num);
}
</style>
