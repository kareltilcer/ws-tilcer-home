// Czech has three plural forms (PRD D20): 1 / 2–4 / 5+. Every count label in the
// UI runs through czPlural. Ported verbatim from design/Home.dc.html.

/** A [one, few, many] triple, e.g. ['úkol', 'úkoly', 'úkolů']. */
export type PluralForms = readonly [one: string, few: string, many: string]

/** czPlural picks the Czech plural form for n. */
export function czPlural(n: number, forms: PluralForms): string {
  if (n === 1) return forms[0]
  if (n >= 2 && n <= 4) return forms[1]
  return forms[2]
}

/** count renders "n <form>" with the correct plural, e.g. count(5, TASKS) → "5 úkolů". */
export function count(n: number, forms: PluralForms): string {
  return `${n} ${czPlural(n, forms)}`
}

// Shared plural triples used across screens.
export const PLURAL = {
  tasks: ['úkol', 'úkoly', 'úkolů'],
  cards: ['karta', 'karty', 'karet'],
  reminders: ['připomínka', 'připomínky', 'připomínek'],
  events: ['událost', 'události', 'událostí'],
  fields: ['pole', 'pole', 'polí'],
  results: ['výsledek', 'výsledky', 'výsledků'],
  days: ['den', 'dny', 'dní'],
  notes: ['poznámka', 'poznámky', 'poznámek'],
  folders: ['složka', 'složky', 'složek'],
  documents: ['dokument', 'dokumenty', 'dokumentů'],
  files: ['soubor', 'soubory', 'souborů'],
  // v5 — Administrace + oznámení.
  recipients: ['příjemce', 'příjemci', 'příjemců'],
  people: ['člověk', 'lidé', 'lidí'],
  devices: ['zařízení', 'zařízení', 'zařízení'],
  notifications: ['oznámení', 'oznámení', 'oznámení'],
  rules: ['pravidlo', 'pravidla', 'pravidel'],
  summaries: ['souhrn', 'souhrny', 'souhrnů'],
  seconds: ['sekunda', 'sekundy', 'sekund'],
  // v6 — Finance counts months, everywhere from the list header to the missing-
  // months prompt.
  months: ['měsíc', 'měsíce', 'měsíců'],
  // v7 — Zahrada counts beds, work and warnings on nearly every screen. Note
  // that `warnings` and `works` are invariant in Czech, which is not a mistake:
  // "2 varování" and "5 varování" really are the same word.
  beds: ['záhon', 'záhony', 'záhonů'],
  works: ['práce', 'práce', 'prací'],
  warnings: ['varování', 'varování', 'varování'],
  crops: ['plodina', 'plodiny', 'plodin'],
  varieties: ['odrůda', 'odrůdy', 'odrůd'],
  years: ['rok', 'roky', 'let'],
  plantings: ['výsadba', 'výsadby', 'výsadeb'],
  // v8 — Elektřina. The nudge line ("poslední odečet před 47 dny") and the
  // prediction basis ("z průměru za posledních 122 dní") both count days, which
  // `days` above already covers; these two are the module's own nouns.
  readings: ['odečet', 'odečty', 'odečtů'],
  tariffs: ['ceník', 'ceníky', 'ceníků'],
} satisfies Record<string, PluralForms>
