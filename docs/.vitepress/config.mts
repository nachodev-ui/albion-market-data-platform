import { defineConfig } from 'vitepress'

export default defineConfig({
  lang: 'es-CL',
  title: 'Albion Market Data Platform',
  description: 'Documentación operativa del receiver local de Albion Market Data Platform.',
  base: '/albion-market-data-platform/',
  cleanUrls: true,
  lastUpdated: true,
  sitemap: {
    hostname: 'https://nachodev-ui.github.io/albion-market-data-platform/'
  },
  head: [
    ['meta', { name: 'theme-color', content: '#16a34a' }]
  ],
  themeConfig: {
    siteTitle: 'Albion Data Platform',
    nav: [
      { text: 'Guía', link: '/guide/' },
      { text: 'Operación', link: '/operations/' },
      { text: 'Recuperación', link: '/recovery/' },
      { text: 'Releases', link: '/release/' },
      { text: 'Seguridad', link: '/security/' },
      { text: 'Testing', link: '/testing/' }
    ],
    sidebar: [
      {
        text: 'Guía',
        items: [
          { text: 'Inicio', link: '/guide/' },
          { text: 'Instalación inicial', link: '/guide/installation' },
          { text: 'Configuración', link: '/guide/configuration' },
          { text: 'Albion Data Client', link: '/guide/albion-data-client' },
          { text: 'API central', link: '/guide/central-api' }
        ]
      },
      {
        text: 'Operación',
        items: [
          { text: 'Operación diaria', link: '/operations/' },
          { text: 'Inicio y detención', link: '/operations/start-stop' },
          { text: 'Diagnóstico', link: '/operations/diagnostics' },
          { text: 'Observabilidad', link: '/OBSERVABILIDAD' },
          { text: 'Runbook degradado', link: '/RUNBOOK_RECEIVER_DEGRADED' }
        ]
      },
      {
        text: 'Datos y recuperación',
        items: [
          { text: 'Backup y restauración', link: '/recovery/backup-restore' },
          { text: 'Reconstrucción', link: '/recovery/rebuild' },
          { text: 'Outbox y dead-letter', link: '/OUTBOX_Y_BACKFILL' },
          { text: 'Limpieza segura', link: '/recovery/cleanup' }
        ]
      },
      {
        text: 'Releases',
        items: [
          { text: 'Política de releases', link: '/release/' },
          { text: 'Actualizar versión', link: '/release/update' },
          { text: 'Rollback', link: '/release/rollback' },
          { text: 'Verificación', link: '/release/verification' },
          { text: 'Distribución Windows', link: '/DISTRIBUCION_WINDOWS' }
        ]
      },
      {
        text: 'Seguridad',
        items: [
          { text: 'Secretos y token', link: '/SEGURIDAD_SECRETOS' },
          { text: 'Vulnerabilidades', link: '/security/vulnerabilities' }
        ]
      },
      {
        text: 'Testing',
        items: [
          { text: 'Cierre de proyecto', link: '/testing/' },
          { text: 'E2E tres proyectos', link: '/E2E_TRES_PROYECTOS' }
        ]
      }
    ],
    search: { provider: 'local' },
    socialLinks: [
      { icon: 'github', link: 'https://github.com/nachodev-ui/albion-market-data-platform' }
    ],
    editLink: {
      pattern: 'https://github.com/nachodev-ui/albion-market-data-platform/edit/develop/docs/:path',
      text: 'Editar esta página en GitHub'
    },
    lastUpdated: { text: 'Última actualización' },
    outline: { level: [2, 3], label: 'En esta página' },
    docFooter: { prev: 'Anterior', next: 'Siguiente' },
    returnToTopLabel: 'Volver arriba',
    sidebarMenuLabel: 'Menú',
    darkModeSwitchLabel: 'Apariencia',
    footer: {
      message: 'Documentación mantenida como código junto al receiver local.',
      copyright: 'Albion Market Data Platform'
    }
  }
})
