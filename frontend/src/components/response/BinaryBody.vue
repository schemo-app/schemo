<script setup lang="ts">
import { computed, ref } from 'vue'
import { useAsyncState, useObjectUrl } from '@vueuse/core'
import { History } from '../../../bindings/schemo/internal/request'
import { formatBytes, formatNumber, t } from '../../i18n'
import type { RecordView } from '../../lib/requestRecord'
import Segment from '../ui/Segment.vue'
import ResponseBodyBar from './ResponseBodyBar.vue'

const { record } = defineProps<{ record: RecordView }>()

const mediaFailed = ref(false)
const view = ref('preview')
const zoom = ref('fit')
const dimensions = ref('')
const duration = ref('')

const { state: preview, error } = useAsyncState(async () => {
  const { mediaURL, ...details } = await History.ResponsePreview(record.id)
  const blob = mediaURL ? await fetch(mediaURL).then((response) => response.blob()).catch(() => undefined) : undefined
  return { ...details, hasMedia: Boolean(mediaURL), blob }
}, undefined, { onError: () => {} })
const source = useObjectUrl(() => preview.value?.blob)

const contentType = computed(() => preview.value?.contentType || record.contentType?.split(';')[0].trim())
const image = computed(() => contentType.value?.startsWith('image/'))
const checks = computed(() => ['image/png', 'image/webp', 'image/gif'].includes(contentType.value ?? ''))
const viewOptions = computed(() => [
  { value: 'preview', label: t('response.binary.preview') },
  { value: 'hex', label: t('response.binary.hex') },
])
const zoomOptions = computed(() => [
  { value: 'fit', label: t('response.binary.fit') },
  { value: 'real', label: t('response.binary.originalSize') },
])

const size = computed(() => formatBytes(record.responseBytes ?? 0, 1))
const info = computed(() => {
  const type = contentType.value ?? ''
  const format = preview.value?.hasMedia ? type.split('/')[1].toUpperCase() : type
  return [format, dimensions.value, duration.value, size.value].filter(Boolean).join(' · ')
})

const hexNote = computed(() => t('response.binary.hexPrefix', {
  count: formatNumber(preview.value?.hexBytes ?? 0),
  size: size.value,
}))

function measureImage(event: Event) {
  const image = event.currentTarget as HTMLImageElement
  dimensions.value = `${image.naturalWidth} × ${image.naturalHeight}`
}

function measureVideo(event: Event) {
  const video = event.currentTarget as HTMLVideoElement
  if (video.videoWidth && video.videoHeight) {
    dimensions.value = `${video.videoWidth} × ${video.videoHeight}`
  }
  if (Number.isFinite(video.duration)) {
    const seconds = Math.floor(video.duration)
    duration.value = `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`
  }
}

</script>

<template>
  <ResponseBodyBar :info>
    <Segment v-if="preview?.hasMedia" :options="viewOptions" :value="view" size="xs" :grow="false" @pick="view = $event" />
    <template #actions>
      <Segment
        v-if="source && image && view === 'preview' && !mediaFailed"
        :options="zoomOptions"
        :value="zoom"
        size="xs"
        :grow="false"
        @pick="zoom = $event"
      />
    </template>
  </ResponseBodyBar>

  <p v-if="error" class="notice">{{ t('response.binary.loadFailed') }}</p>
  <template v-else-if="preview">
    <div v-if="view === 'hex' || !preview.hasMedia" class="hex-view">
      <pre>{{ preview.hex }}</pre>
      <p class="hex-note">{{ hexNote }}</p>
    </div>
    <p v-else-if="mediaFailed || !preview.blob" class="notice">{{ t('response.binary.previewFailed') }}</p>
    <div v-else-if="source && image" class="image-stage" :class="{ 'original-size': zoom === 'real' }">
      <button
        type="button"
        class="image-zoom"
        :title="t(zoom === 'fit' ? 'response.binary.originalSize' : 'response.binary.fit')"
        @click="zoom = zoom === 'fit' ? 'real' : 'fit'"
      >
        <img
          :src="source"
          :alt="t('response.binary.image')"
          :class="{ checks }"
          @load="measureImage"
          @error="mediaFailed = true"
        />
      </button>
    </div>
    <div v-else-if="source" class="video-stage">
      <video
        :src="source"
        :aria-label="t('response.binary.video')"
        controls
        controlslist="nodownload"
        playsinline
        preload="metadata"
        @loadedmetadata="measureVideo"
        @durationchange="measureVideo"
        @error="mediaFailed = true"
      />
    </div>
  </template>
</template>

<style scoped>
@reference "../../style.css";

.image-stage {
  @apply flex flex-1 min-h-0 min-w-0 items-center justify-center overflow-auto;
  padding: 0.5rem 1.25rem 1.5rem;
  container-type: size;
}

.image-stage.original-size {
  @apply items-start justify-start;
}

.image-zoom {
  @apply block flex-none border-0 bg-transparent p-0 cursor-zoom-in;
  border-radius: 0.375rem;
  box-shadow: 0 0 0 1px var(--line2);
}

.image-zoom img {
  @apply block rounded-md;
  max-width: 100cqw;
  max-height: 100cqh;
}

.original-size .image-zoom {
  @apply cursor-zoom-out;
}

.original-size img {
  max-width: none;
  max-height: none;
}

.checks {
  background: repeating-conic-gradient(var(--chip) 0 25%, transparent 0 50%) 0 0 / 1rem 1rem;
}

.video-stage {
  @apply flex-1 min-h-0 overflow-auto;
  padding: 0.5rem 1.25rem 1.5rem;
}

.video-stage video {
  @apply block w-full mx-auto rounded-lg object-contain;
  max-width: 40rem;
  aspect-ratio: 16 / 9;
  background: #0b0b0d;
  box-shadow: 0 0 0 1px var(--line2);
}

.hex-view {
  @apply flex-1 min-h-0 overflow-auto;
  padding: 0.25rem 1.25rem 1.25rem;
}

.hex-view pre {
  width: max-content;
  margin: 0;
  font: var(--text-12)/1.3125rem var(--mono);
  user-select: text;
}

.hex-note,
.notice {
  @apply text-text-tertiary;
  font-size: var(--text-12);
}

.hex-note {
  margin: 0.625rem 0 0;
}

.notice {
  margin: 0.5rem 1.25rem;
}
</style>
