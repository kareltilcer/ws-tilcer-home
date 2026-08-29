// MAX_INLINE_IMAGE_DATA_LEN mirrors the backend's maxInlineImageDataLen: the longest a
// `data:image/…` URI may be before the server rejects the whole body (422). Anything at
// or under it the server stores verbatim, so it can legitimately already be sitting in a
// note's stored body_md.
//
// It lives in its own module because BOTH editing surfaces must agree on it: the Markdown
// tab's paste handler (NoteView) uploads only URIs OVER it, and the Vizuální editor's
// upload plugin (MilkdownEditor) rewrites only image nodes OVER it. If the two disagreed,
// the stricter surface would upload and rewrite an inline image the other one — and the
// server — is happy to keep, so merely SWITCHING TABS on an untouched note would rewrite
// its body and autosave the rewrite.
export const MAX_INLINE_IMAGE_DATA_LEN = 4096
