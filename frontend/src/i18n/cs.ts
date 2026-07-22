// Centralized Czech UI copy (PRD D20: Czech-only, no i18n framework, strings in
// one place). Add module strings here as screens are built.

export const cs = {
  app: {
    name: 'home',
    redirecting: 'Přesměrování na auth.tilcer.cz…',
  },
  nav: {
    nastenka: 'Nástěnka',
    ukoly: 'Úkoly',
    okno: 'Okno do budoucnosti',
    oknoShort: 'Okno',
    log: 'Log',
  },
  common: {
    loading: 'Načítání…',
    errorTitle: 'Něco se pokazilo',
    retry: 'Zkusit znovu',
    readOnly: 'Jen pro čtení',
    accessDenied: 'Přístup odepřen',
    accessDeniedDetail: 'Tato sekce je jen pro administrátory.',
    empty: 'Nic tu zatím není',
  },
  dashboard: {
    title: 'Nástěnka',
    subtitle: 'Co teď potřebuje tvou pozornost',
    remindersHeading: 'Události',
    tasksHeading: 'Úkoly',
    emptyTitle: 'Teď tě nic nečeká',
    emptyBody: 'Žádné aktivní připomínky ani úkoly v „Právě dělám".',
  },
} as const
