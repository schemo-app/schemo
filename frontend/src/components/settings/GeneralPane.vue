<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import SettingsRow from './SettingsRow.vue'
import SettingsSection from './SettingsSection.vue'
import Button from '../ui/Button.vue'
import IconButton from '../ui/IconButton.vue'
import Switch from '../ui/Switch.vue'
import Icon from '../ui/Icon.vue'
import Segment from '../ui/Segment.vue'
import Select from '../ui/Select.vue'
import { Workspaces } from '../../../bindings/schemo/internal/workspace'
import { Indent } from '../../../bindings/schemo/internal/domain'
import { useSetting, useSettings, type SettingsPatch } from '../../composables/useSettings'
import { useLocale, type Language } from '../../composables/useLocale'
import { useSheetNotice } from '../../composables/useSheetNotice'
import { languages, t } from '../../i18n'
import { workspaceName } from '../../stores/workspaces'

// What the app does when it starts, what the viewer does with a long line, which language the
// window is written in, what it accepts from a server, and where the data lives.
const { loadSettings, save, addRootCertificate } = useSettings()
const { language, setLanguage } = useLocale()
const { withNotice } = useSheetNotice()

const languageOptions = computed(() => [
  { value: 'system', label: t('settings.systemLanguage') },
  ...Object.entries(languages).map(([value, label]) => ({ value, label })),
])

const indents = computed(() => [
  { value: Indent.IndentTwo, label: t('settings.indentTwo') },
  { value: Indent.IndentFour, label: t('settings.indentFour') },
  { value: Indent.IndentTab, label: t('settings.indentTab') },
])

// The stored choice until the window has read it: the same defaults Go answers with, so a switch
// shows something true rather than nothing.
const wrap = useSetting('wrapLines')
const numbers = useSetting('lineNumbers')
const indent = useSetting('indent')
const reopen = useSetting('reopenWorkspace')
const restore = useSetting('restoreTabs')

// What the app accepts from a server. Both switches are written the moment they move: one that
// took effect on the next launch would lie, and a refusal is drawn rather than swallowed.
const certificates = useSetting('certificates')
const verify = computed(() => certificates.value.verify)
const trustLocalhost = computed(() => certificates.value.trustLocalhost)
const roots = computed(() => certificates.value.roots ?? [])

// The one note in this window that is a warning rather than an explanation.
const verifyNote = computed(() =>
  verify.value ? t('settings.certificates.verifyOn') : t('settings.certificates.verifyOff')
)

// Whole sentences in the catalogue rather than glued here, as the capture rules' two are.
const rootsNote = computed(() =>
  roots.value.length === 0
    ? t('settings.certificates.rootsNote')
    : t('settings.certificates.rootsAdded', { n: roots.value.length })
)

function writeCertificates(patch: SettingsPatch['certificates']) {
  void withNotice(() => save({ certificates: patch }))
}

// A refusal is said rather than swallowed: a trust meant to be added and missing is worst of both.
function addRoot() {
  void withNotice(() => addRootCertificate(t('settings.certificates.roots')))
}

function removeRoot(name: string) {
  void withNotice(() => save({ certificates: { roots: roots.value.filter((root) => root.name !== name) } }))
}

// Named rather than described: the answer to "the last workspace" is a name, and the default workspace
// has none of its own — the interface's word is what it gets, as in the title bar.
const lastName = ref('')
const lastWorkspace = computed(() => lastName.value || t('workspaces.personal'))

onMounted(async () => {
  void loadSettings()
  // Cosmetic to the setting it stands beside: a failed call leaves the name empty and the sentence
  // falls back to the interface's own word for the workspace.
  try {
    const state = await Workspaces.Snapshot()
    const active = state.workspaces?.find((workspace) => workspace.id === state.activeId) ?? state.workspaces?.[0]
    if (active) lastName.value = workspaceName(active)
  } catch {
    // Nothing to do: the sentence falls back to the interface's own word for the workspace.
  }
})
</script>

<template>
  <div class="pane">
    <SettingsSection :label="t('settings.sec.startup')">
      <SettingsRow :title="t('settings.reopenWorkspace')" :note="t('settings.reopenNote', { name: lastWorkspace })">
        <Switch
          :model-value="reopen"
          :aria-label="t('settings.reopenWorkspace')"
          @update:model-value="save({ reopenWorkspace: !!$event })"
        />
      </SettingsRow>
      <!-- Written the moment it moves: with it off the strip stops being kept, and the launch after
           starts clean rather than restoring a session nobody asked to keep. -->
      <SettingsRow :title="t('settings.restoreTabs')" :note="t('settings.restoreTabsNote')">
        <Switch
          :model-value="restore"
          :aria-label="t('settings.restoreTabs')"
          @update:model-value="save({ restoreTabs: !!$event })"
        />
      </SettingsRow>
    </SettingsSection>

    <SettingsSection :label="t('settings.sec.editor')">
      <SettingsRow :title="t('settings.wrapLines')" :note="t('settings.wrapLinesNote')">
        <Switch
          :model-value="wrap"
          :aria-label="t('settings.wrapLines')"
          @update:model-value="save({ wrapLines: !!$event })"
        />
      </SettingsRow>
      <SettingsRow :title="t('settings.lineNumbers')">
        <Switch
          :model-value="numbers"
          :aria-label="t('settings.lineNumbers')"
          @update:model-value="save({ lineNumbers: !!$event })"
        />
      </SettingsRow>
      <SettingsRow :title="t('settings.indent')">
        <Segment
          size="sm"
          :grow="false"
          :options="indents"
          :value="indent"
          @pick="save({ indent: $event as Indent })"
        />
      </SettingsRow>
      <SettingsRow :title="t('settings.interfaceLanguage')">
        <Select :model-value="language" :options="languageOptions" @update:model-value="setLanguage($event as Language)" />
      </SettingsRow>
    </SettingsSection>

    <SettingsSection :label="t('settings.sec.certificates')" :note="t('settings.certificatesNote')">
      <SettingsRow
        :title="t('settings.certificates.verify')"
        :note="verifyNote"
        :alarm="!verify"
      >
        <Switch
          :model-value="verify"
          :aria-label="t('settings.certificates.verify')"
          @update:model-value="writeCertificates({ verify: !!$event })"
        />
      </SettingsRow>
      <SettingsRow
        :title="t('settings.certificates.trustLocal')"
        :note="t('settings.certificates.trustLocalNote')"
      >
        <Switch
          :model-value="trustLocalhost"
          :disabled="!verify"
          :aria-label="t('settings.certificates.trustLocal')"
          @update:model-value="writeCertificates({ trustLocalhost: !!$event })"
        />
      </SettingsRow>
      <SettingsRow :title="t('settings.certificates.roots')" :note="rootsNote">
        <Button size="field" @click="addRoot">
          {{ t('settings.certificates.addRoot') }}
        </Button>
      </SettingsRow>
      <!-- One row per authority that was added, each with the way to stop trusting it: a list that
           could only grow would be a trust nobody can take back. -->
      <SettingsRow v-for="root in roots" :key="root.name" :title="root.name">
        <IconButton
          :hint="t('settings.certificates.removeRoot', { name: root.name })"
          @click="removeRoot(root.name)"
        >
          <Icon name="Trash2" :size="14" />
        </IconButton>
      </SettingsRow>
    </SettingsSection>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.pane {
  @apply flex flex-col;
  gap: 1.625rem;
}
</style>
