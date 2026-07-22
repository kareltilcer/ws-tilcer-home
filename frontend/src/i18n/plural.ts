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
} satisfies Record<string, PluralForms>
