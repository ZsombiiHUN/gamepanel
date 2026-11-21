import DefaultTheme from 'vitepress/theme'
import type { Theme } from 'vitepress'
import '@fontsource-variable/geist'
import '@fontsource-variable/geist-mono'
import './custom.css'
import Home from './Home.vue'
import SystemMap from './SystemMap.vue'
import PageActions from './PageActions.vue'
import ApiReference from './ApiReference.vue'
import Shot from './Shot.vue'

export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    app.component('Home', Home)
    app.component('SystemMap', SystemMap)
    app.component('PageActions', PageActions)
    app.component('ApiReference', ApiReference)
    app.component('Shot', Shot)
  },
} satisfies Theme
