/**
 * qs builds the query-string suffix for a request URL — `?a=1&b=2`, or `''` when
 * nothing survives.
 *
 * ⚠ IT DROPS `undefined` AND NOTHING ELSE. There were four of these, and the four
 * disagreed about what an empty value means: garden dropped `null` and `''` too,
 * electricity dropped every falsy value (so a real `0` or `false` would have
 * vanished), chat dropped `''`. That is the shape of a bug that only shows up on
 * one filter, in one module — a cleared search box sending `?q=` to a server that
 * treats an empty term differently from an absent one, or the reverse.
 *
 * So the rule here is the narrow one: absent means absent, and everything else is
 * a value the caller meant to send. `null` is not even in `QsValue`, because
 * `String(null)` is the string `"null"` and no endpoint wants that — a caller
 * holding a nullable value passes `?? undefined`. Every call site in all four
 * modules already did; the audit that established it is in the commit that
 * introduced this file.
 *
 * The mapped-type constraint rather than `Record<string, QsValue>` is what lets
 * the typed filter interfaces (garden's `PlantFilters` and friends) be passed
 * straight in: an interface has no implicit index signature, so it is not
 * assignable to a Record, and adding one to each of them would be several
 * declarations serving one helper. It is written homomorphically — `{ [K in keyof
 * T]: QsValue }` — because that preserves each key's OPTIONALITY, which
 * `Record<keyof T, QsValue>` does not: a Record makes every key required, so every
 * filter interface would fail on its first `?` field. Each value's type is still
 * checked, which the `object` this replaced in garden did not do.
 */
export type QsValue = string | number | boolean | string[] | undefined

export function qs<T extends { [K in keyof T]: QsValue }>(params: T): string {
  const sp = new URLSearchParams()
  for (const [k, v] of Object.entries(params) as [string, QsValue][]) {
    if (v === undefined) continue
    // append, not set: an array is the one value that maps to repeated keys.
    if (Array.isArray(v)) v.forEach((x) => sp.append(k, x))
    else sp.append(k, String(v))
  }
  const s = sp.toString()
  return s ? `?${s}` : ''
}
