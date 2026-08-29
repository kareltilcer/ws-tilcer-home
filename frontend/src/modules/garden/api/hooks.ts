import { useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { qk } from '@/api/keys'
import { apiErrorMessage } from '@/api/client'
import { cs } from '@/i18n/cs'

/** useInvalidateGarden refreshes the whole module plus the dashboard.
 *
 *  Every garden mutation goes through this rather than picking keys, because the
 *  three caches a plan change touches — the plan, its CHECK and the tasks — must
 *  move together. A stale check is worse than no check: it is a green tick that
 *  has stopped being true, and a per-key invalidation is exactly how one gets
 *  left behind. */
export function useInvalidateGarden(): () => void {
  const qc = useQueryClient()
  return () => {
    void qc.invalidateQueries({ queryKey: qk.gardenAll })
    void qc.invalidateQueries({ queryKey: qk.dashboard })
  }
}

/** toastGardenError is the shared onError handler for every garden mutation:
 *  the Czech sentence the server already wrote, or the generic title when the
 *  failure never reached the server.
 *
 *  It used to wrap a `gardenError()` of its own, which was the module's spelling
 *  of a ternary written out ~20 times across the app. That now lives once as
 *  `apiErrorMessage` beside ApiError itself; this stays because "toast it" is
 *  the part every garden mutation shares. */
export function toastGardenError(e: unknown): void {
  toast.error(apiErrorMessage(e, cs.common.errorTitle))
}
