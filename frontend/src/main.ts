import '@wailsio/runtime'

import App from './App.vue'
import './style.css'
import { mountWindow } from './boot'

void mountWindow(App)
