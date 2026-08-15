// Ambient types for the JSON data files shipped by `unicode-emoji-json`. The
// package has no bundled `.d.ts` for these subpath imports, and letting TS infer
// the type of the ~400 KB literal would bloat type-checking — so we declare the
// shape we actually consume by hand. Vite's JSON plugin turns the import into a
// module at build time regardless of these ambient types.
declare module 'unicode-emoji-json/data-by-group.json' {
  interface RawEmoji {
    emoji: string
    name: string
    slug: string
    skin_tone_support: boolean
    unicode_version: string
    emoji_version: string
  }
  interface RawEmojiGroup {
    name: string
    slug: string
    emojis: RawEmoji[]
  }
  const groups: RawEmojiGroup[]
  export default groups
}
