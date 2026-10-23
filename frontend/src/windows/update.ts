import '@wailsio/runtime'

import UpdateWindow from './UpdateWindow.vue'
import '../style.css'
import { mountWindow } from '../boot'

void mountWindow(UpdateWindow)
