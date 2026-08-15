import rawGroups from 'unicode-emoji-json/data-by-group.json'
import { MAX_ICON_RUNES } from '@/lib/foldericon'

// Full emoji dataset (~1900 glyphs) sourced from `unicode-emoji-json`. This module
// is only ever pulled in by the lazily-loaded EmojiPickerPanel, so the ~400 KB of
// JSON stays out of the main bundle and off the critical path — it downloads once,
// the first time someone opens the "browse all" picker.

// Re-exported for the picker and its tests. MAX_ICON_RUNES mirrors the backend's
// `foldericon.maxRunes`: an icon is capped at 8 code points server-side, so we never
// offer a glyph the backend would mangle. (Today no base emoji exceeds this; the
// guard keeps FE and BE in lock-step if the dataset ever gains a longer sequence.)
export { MAX_ICON_RUNES }

export interface EmojiItem {
  /** The glyph itself — this is what gets stored as the folder icon. */
  char: string
  /** English display name, used as the button's title/aria-label. */
  name: string
  /** Pre-computed, diacritics-folded haystack for search (see `deburr`). */
  search: string
}

export interface EmojiCategory {
  key: string
  /** Czech section heading. */
  label: string
  /** Representative glyph shown in the category quick-nav. */
  icon: string
  items: EmojiItem[]
}

// Per-group Czech labels + a representative nav glyph, keyed by the English group
// name that `unicode-emoji-json` uses. Order here is the order shown in the picker.
const GROUP_META: Record<string, { label: string; icon: string }> = {
  'Smileys & Emotion': { label: 'Smajlíci a emoce', icon: '😀' },
  'People & Body': { label: 'Lidé a tělo', icon: '👋' },
  'Animals & Nature': { label: 'Zvířata a příroda', icon: '🐻' },
  'Food & Drink': { label: 'Jídlo a pití', icon: '🍔' },
  'Travel & Places': { label: 'Cestování a místa', icon: '🚗' },
  Activities: { label: 'Aktivity', icon: '⚽' },
  Objects: { label: 'Předměty', icon: '💡' },
  Symbols: { label: 'Symboly', icon: '❤️' },
  Flags: { label: 'Vlajky', icon: '🏳️' },
}

// Curated Czech search aliases. The dataset's names/slugs are English only, so a
// Czech user searching "srdce" or "auto" would otherwise come up empty. This map
// boosts the common, folder-icon-shaped searches (home, money, car, health,
// garden, documents, food, pets…) — it is intentionally not exhaustive; English
// name search still covers the long tail. Keys are emoji, values are space-
// separated ASCII keywords (no diacritics — search folds them anyway). Matching
// is variation-selector-insensitive, so "❤" and "❤️" both attach here.
const CZECH_ALIASES: Record<string, string> = {
  // Faces & emotion
  '😀': 'smajlik usmev stastny radost', '😄': 'smich stastny', '😍': 'zamilovany laska oci',
  '😂': 'smich slzy', '🙂': 'usmev', '😉': 'mrknuti', '😎': 'bryle cool', '😢': 'plac smutek',
  '😭': 'plac breceni', '😡': 'vztek zlost', '😱': 'strach lek', '🤔': 'premysleni',
  '😴': 'spanek unava', '🤒': 'nemoc nemocny teplota', '🥳': 'oslava party', '😇': 'andel',
  // Hearts & feelings
  '❤️': 'srdce laska', '🧡': 'srdce oranzove', '💛': 'srdce zlute', '💚': 'srdce zelene',
  '💙': 'srdce modre', '💜': 'srdce fialove', '🖤': 'srdce cerne', '🤍': 'srdce bile',
  '💔': 'zlomene srdce', '💕': 'srdce laska', '💯': 'sto procent',
  // Nature & weather
  '⭐': 'hvezda', '🌟': 'hvezda zare', '✨': 'jiskry trpytky', '🔥': 'ohen', '💧': 'kapka voda',
  '⚡': 'blesk elektrina energie', '☀️': 'slunce', '🌙': 'mesic noc', '☁️': 'oblak mrak',
  '🌈': 'duha', '❄️': 'snih vlocka zima', '🌊': 'vlna more', '🌍': 'zemekoule svet planeta',
  // Symbols
  '✅': 'hotovo splneno fajfka ok', '☑️': 'zaskrtnuto', '❌': 'krizek chyba ne', '⚠️': 'varovani pozor',
  '❓': 'otaznik', '❗': 'vykricnik', '♻️': 'recyklace', '🔔': 'zvonek upozorneni',
  '🔕': 'ztlumeno ticho', '📍': 'poloha pin mapa', '🚩': 'vlajka prapor', '🏷️': 'cenovka stitek',
  // Home, work & finance
  '🏠': 'dum domov byt bydleni', '🏡': 'dum domov zahrada', '🏢': 'budova kancelar firma',
  '🏦': 'banka', '💰': 'penize finance pytel', '💵': 'penize dolar bankovky', '💶': 'penize euro',
  '💳': 'karta platba', '🧾': 'uctenka faktura', '📊': 'graf statistika sloupce',
  '📈': 'graf rust', '📉': 'graf pokles', '💼': 'kufrik prace aktovka', '🗓️': 'kalendar planovac',
  '📅': 'kalendar datum', '📆': 'kalendar', '⏰': 'budik cas hodiny', '⏱️': 'stopky',
  // Documents & writing
  '📁': 'slozka', '📂': 'slozka otevrena', '🗂️': 'slozky rejstrik poradac', '📄': 'dokument papir list',
  '📃': 'dokument stranka', '📑': 'zalozky', '📝': 'poznamka psani tuzka', '📋': 'schranka seznam',
  '📚': 'knihy', '📖': 'kniha cteni', '📘': 'kniha modra', '✏️': 'tuzka', '🖊️': 'pero',
  '✂️': 'nuzky', '📌': 'pripinacek', '📎': 'sponka', '🗑️': 'kos smazat odpadky',
  '🔑': 'klic', '🔒': 'zamek zamceno', '🔓': 'odemceno',
  // Tools & settings
  '🔧': 'klic naradi oprava', '🔨': 'kladivo', '🛠️': 'naradi', '⚙️': 'nastaveni ozubene kolo',
  '🧰': 'brasna naradi', '🪛': 'sroubovak', '🔩': 'sroub matice', '🧲': 'magnet',
  // Tech & communication
  '📱': 'telefon mobil', '💻': 'notebook pocitac laptop', '🖥️': 'pocitac monitor',
  '⌨️': 'klavesnice', '🖱️': 'mys', '🖨️': 'tiskarna', '📷': 'foto fotoaparat', '📸': 'foto blesk',
  '🎥': 'kamera film', '📹': 'kamera video', '🎧': 'sluchatka', '🎤': 'mikrofon',
  '📺': 'televize', '📻': 'radio', '☎️': 'telefon', '📞': 'telefon hovor',
  '✉️': 'obalka email posta dopis', '📧': 'email', '📮': 'schranka posta', '💾': 'disketa ulozit',
  // Food & drink
  '🍎': 'jablko ovoce', '🍌': 'banan', '🍓': 'jahoda', '🍇': 'hrozny vino', '🍊': 'pomeranc',
  '🍔': 'hamburger jidlo', '🍕': 'pizza', '🍟': 'hranolky', '🌭': 'parek hotdog',
  '🍞': 'chleb pecivo', '🧀': 'syr', '🥚': 'vejce', '🍗': 'kure maso', '🥗': 'salat',
  '🍲': 'polevka jidlo hrnec', '🍜': 'polevka nudle', '🍰': 'dort kolac', '🎂': 'dort narozeniny',
  '🍫': 'cokolada', '🍬': 'bonbon sladkosti', '🍦': 'zmrzlina', '☕': 'kava', '🍵': 'caj',
  '🍺': 'pivo', '🍷': 'vino', '🥤': 'napoj', '🧃': 'dzus', '💊': 'lek prasek tabletka', '🧂': 'sul',
  // Animals & plants
  '🐶': 'pes stene', '🐱': 'kocka', '🐭': 'mys', '🐹': 'krecek', '🐰': 'kralik zajic',
  '🦊': 'liska', '🐻': 'medved', '🐼': 'panda', '🐨': 'koala', '🦁': 'lev', '🐮': 'krava',
  '🐷': 'prase', '🐔': 'slepice kure', '🐧': 'tucnak', '🐦': 'ptak', '🐝': 'vcela',
  '🦋': 'motyl', '🐟': 'ryba', '🐢': 'zelva', '🐍': 'had', '🐴': 'kun', '🐾': 'tlapky zvire',
  '🌳': 'strom', '🌲': 'strom jehlicnan', '🌴': 'palma', '🌵': 'kaktus', '🌷': 'tulipan kvetina',
  '🌹': 'ruze kvetina', '🌻': 'slunecnice', '🌼': 'kvetina sedmikraska', '🍀': 'ctyrlistek stesti',
  '🍁': 'list javor podzim', '🌱': 'sazenice rostlina', '🌿': 'bylina listi',
  // Travel & vehicles
  '🚗': 'auto vuz', '🚙': 'auto suv', '🚕': 'taxi', '🚌': 'autobus', '🚑': 'sanitka',
  '🚒': 'hasici auto', '🚓': 'policie auto', '🏎️': 'zavodni auto', '🚲': 'kolo bicykl',
  '🛵': 'skutr moped', '🏍️': 'motorka', '✈️': 'letadlo cestovani let', '🚂': 'vlak lokomotiva',
  '🚀': 'raketa', '⛵': 'plachetnice lod', '🚢': 'lod', '🚁': 'vrtulnik', '🗺️': 'mapa',
  '🧭': 'kompas', '⛰️': 'hora', '🏔️': 'hora snih', '🏖️': 'plaz dovolena', '🏕️': 'kemp stan',
  '⛽': 'benzin cerpaci stanice palivo', '🚦': 'semafor', '🅿️': 'parkovani',
  // Activities, sport & fun
  '⚽': 'fotbal mic', '🏀': 'basketbal', '🏈': 'americky fotbal', '⚾': 'baseball', '🎾': 'tenis',
  '🏐': 'volejbal', '🏓': 'stolni tenis pingpong', '🎱': 'kulecnik', '🏊': 'plavani',
  '🚴': 'cyklistika kolo', '🏃': 'beh', '⛷️': 'lyzovani', '🏂': 'snowboard', '🎿': 'lyze',
  '🥇': 'medaile zlato prvni', '🏆': 'pohar trofej vyhra', '🎯': 'terc cil', '🎲': 'kostka hra',
  '🎮': 'hry ovladac', '🎸': 'kytara', '🎹': 'klavir piano', '🎺': 'trumpeta', '🥁': 'bubny',
  '🎨': 'malovani umeni paleta', '🎬': 'film klapka', '🎵': 'hudba nota', '🎶': 'hudba noty',
  '🎁': 'darek', '🎉': 'oslava party konfety', '🎈': 'balonek', '🎃': 'dyne halloween',
  '🎄': 'vanoce stromek', '🧩': 'puzzle skladacka',
  // Health & people
  '🩺': 'lekar stetoskop zdravi', '💉': 'injekce ockovani', '🩹': 'naplast', '🦷': 'zub',
  '🧠': 'mozek', '🏥': 'nemocnice', '👶': 'miminko dite', '🧑': 'clovek osoba', '👨': 'muz',
  '👩': 'zena', '👴': 'dedecek senior', '👵': 'babicka', '👪': 'rodina', '🤝': 'ruce dohoda',
  '👍': 'palec nahoru souhlas dobre', '👎': 'palec dolu', '👏': 'potlesk', '🙏': 'prosba diky modlitba',
  '💪': 'sila biceps sval',
  // Household & shopping
  '💡': 'zarovka napad svetlo', '🔦': 'baterka svetlo', '🕯️': 'svicka', '🧯': 'hasici pristroj',
  '🧹': 'kostene uklid', '🧺': 'kos pradlo', '🧻': 'papir toaletni', '🚿': 'sprcha',
  '🛁': 'vana koupel', '🚽': 'zachod wc toaleta', '🪑': 'zidle', '🛏️': 'postel',
  '🛋️': 'gauc pohovka', '🚪': 'dvere', '🪟': 'okno', '🧳': 'kufr zavazadlo', '📦': 'balik krabice',
  '🛒': 'nakup kosik vozik', '🛍️': 'nakup tasky', '💎': 'diamant sperk',
  // Clothing
  '👗': 'saty obleceni', '👕': 'tricko', '👖': 'kalhoty dziny', '👟': 'bota tenisky',
  '👠': 'podpatky bota', '🧥': 'kabat bunda', '🧦': 'ponozky', '🧤': 'rukavice', '🎓': 'promoce skola',
  // Search
  '🔍': 'lupa hledat', '🔎': 'lupa hledat',
}

// Fold diacritics + lowercase so search is accent-insensitive both ways: the
// Czech aliases are written without accents, and a query like "kůň" folds to
// "kun" to match them (and "Café" folds to "cafe" to match English names).
export const deburr = (s: string): string =>
  s
    .normalize('NFD')
    .replace(/[\u0300-\u036f]/g, '') // strip combining diacritical marks
    .toLowerCase()

// Variation-selector-insensitive key so alias entries attach whether the dataset
// stores the glyph with or without a trailing U+FE0F.
const aliasKey = (emoji: string): string => emoji.replace(/\uFE0F/g, '')
const ALIAS_BY_KEY = new Map<string, string>()
for (const [emoji, words] of Object.entries(CZECH_ALIASES)) ALIAS_BY_KEY.set(aliasKey(emoji), words)

// Build the grouped + flat structures once, at module load.
export const EMOJI_CATEGORIES: EmojiCategory[] = []
export const ALL_EMOJI: EmojiItem[] = []

for (const group of rawGroups) {
  const meta = GROUP_META[group.name]
  if (!meta) continue // ignore any future group we have no label for
  const items: EmojiItem[] = []
  for (const e of group.emojis) {
    if ([...e.emoji].length > MAX_ICON_RUNES) continue // guard (see MAX_ICON_RUNES)
    const czech = ALIAS_BY_KEY.get(aliasKey(e.emoji)) ?? ''
    const item: EmojiItem = {
      char: e.emoji,
      name: e.name,
      // name + slug-as-words + Czech aliases, all folded for accent-free matching.
      search: deburr(`${e.name} ${e.slug.replace(/_/g, ' ')} ${czech}`),
    }
    items.push(item)
    ALL_EMOJI.push(item)
  }
  EMOJI_CATEGORIES.push({ key: group.slug, label: meta.label, icon: meta.icon, items })
}

// searchEmoji — every whitespace-separated token in the (folded) query must appear
// somewhere in an emoji's haystack (AND semantics), so "cerne srdce" narrows to
// black hearts rather than every heart plus everything black.
export function searchEmoji(query: string): EmojiItem[] {
  const tokens = deburr(query).split(/\s+/).filter(Boolean)
  // Copy, never hand back the live ALL_EMOJI array: a caller that sorts or splices
  // the result must not mutate the shared dataset for the rest of the session.
  if (tokens.length === 0) return [...ALL_EMOJI]
  return ALL_EMOJI.filter((item) => tokens.every((t) => item.search.includes(t)))
}
