// Offline reads (PRD D71/D73, HANDOFF-7 §12).
//
// The last-synced boards, cards, events, notes and document METADATA render
// offline because TanStack Query's cache is persisted to IndexedDB and rehydrated
// on boot. That is the whole offline story in v5: reads work, writes fail closed,
// there is no queue and no sync.
//
// The security-critical part is the KEY. The cache is namespaced by user id and
// purged on logout, because home is a shared household device story: a laptop in
// the kitchen gets used by more than one person, and a cache that outlived a
// logout would show the next person the previous one's data.

import { createAsyncStoragePersister } from '@tanstack/query-async-storage-persister'
import { persistQueryClient } from '@tanstack/react-query-persist-client'
import { del, get, keys, set } from 'idb-keyval'
import type { QueryClient } from '@tanstack/react-query'
import { qk } from '@/api/keys'

const STORE_PREFIX = 'home-query-cache'
/** Bump when a cached shape changes incompatibly; old buckets are discarded. */
const CACHE_BUSTER = 'v5'
const MAX_AGE = 7 * 24 * 60 * 60 * 1000 // a week of staleness is plenty for reads

function keyFor(userID: string): string {
  return `${STORE_PREFIX}:${userID}`
}

/** isPrivateItemsKey matches every filter combination of the purge listing, through
 *  the key factory rather than a literal — see shouldDehydrateQuery for why it must
 *  never reach disk. */
function isPrivateItemsKey(queryKey: readonly unknown[]): boolean {
  return qk.adminPrivateItemsAll.every((segment, i) => queryKey[i] === segment)
}

let activeUser: string | null = null
let unsubscribe: (() => void) | null = null

/** startPersisting begins persisting the cache for one user. Calling it with a
 *  different user first clears the previous user's bucket. */
export async function startPersisting(client: QueryClient, userID: string): Promise<void> {
  if (activeUser === userID) return
  if (activeUser && activeUser !== userID) {
    await stopPersisting(client)
  }
  activeUser = userID
  // Drop every OTHER member's bucket. stopPersisting above only covers a handover
  // this tab watched happen; the common case is nobody watching — a session that
  // ended by closing the tab, or one that expired while it was closed. Without
  // this sweep that cache stays on a shared laptop forever, which is precisely
  // what "must not outlive its session" rules out.
  await sweepOtherUsers(userID)

  const storageKey = keyFor(userID)
  const persister = createAsyncStoragePersister({
    storage: {
      getItem: async (key) => (await get<string>(key)) ?? null,
      // Writes are THROTTLED, so one scheduled while this user was signed in can
      // execute after they logged out — and it would dehydrate whatever the
      // shared QueryClient holds by then, which on a household laptop is the
      // next person's data, into this user's bucket. Dropping a write whose user
      // is no longer active is what closes that window.
      setItem: async (key, value) => {
        if (activeUser !== userID) return
        await set(key, value)
      },
      removeItem: async (key) => del(key),
    },
    key: storageKey,
  })

  // persistQueryClient returns [unsubscribe, restorePromise]. KEEPING the
  // unsubscribe is what makes stopPersisting real: a discarded one leaves the
  // previous user's subscriber attached to the same QueryClient, and it would
  // then write the NEXT user's queries into the previous user's bucket.
  const [detach, restored] = persistQueryClient({
    queryClient: client,
    persister,
    maxAge: MAX_AGE,
    buster: CACHE_BUSTER,
    dehydrateOptions: {
      shouldDehydrateQuery: (query) => {
        const key = String(query.queryKey[0] ?? '')
        // Never persist auth/session state or push registration: both are
        // online-only by design, and a stale "you are logged in" is a bug.
        if (key === 'session' || key === 'auth' || key === 'push') return false
        // ⚠ NOR the purge listing (v9, D198). Opening Soukromé položky writes
        // `admin.private_items.view` — the only READ in Home that is audited — and
        // PrivateItemsTab suppresses every automatic refetch precisely so that "a
        // page load is a person choosing to look". Persisting it defeats both: the
        // list rehydrates from IndexedDB days later with no request and therefore
        // no audit event, and other members' private item ids sit on a shared
        // laptop's disk between sessions. It is the one query whose value is
        // strictly worse offline than absent.
        if (isPrivateItemsKey(query.queryKey)) return false
        return query.state.status === 'success'
      },
    },
  })
  unsubscribe = detach
  void restored
}

/** stopPersisting is called on logout: it detaches the subscriber AND deletes the
 *  bucket, so nothing survives into the next session. */
export async function stopPersisting(client: QueryClient): Promise<void> {
  unsubscribe?.()
  unsubscribe = null
  const previous = activeUser
  activeUser = null
  client.clear()
  if (previous) {
    await del(keyFor(previous))
  }
}

/** sweepOtherUsers deletes every persisted bucket that is not the signed-in
 *  member's. It is the belt-and-braces path for a handover no tab observed. */
async function sweepOtherUsers(userID: string): Promise<void> {
  const mine = keyFor(userID)
  try {
    for (const key of await keys()) {
      if (typeof key !== 'string' || key === mine) continue
      if (key.startsWith(`${STORE_PREFIX}:`)) await del(key)
    }
  } catch {
    /* IndexedDB unavailable (private mode, quota) — persistence is best-effort */
  }
}
