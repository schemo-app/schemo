import '@wailsio/runtime'

import SettingsWindow from './SettingsWindow.vue'
import '../style.css'
import { mountWindow } from '../boot'

void mountWindow(SettingsWindow)
