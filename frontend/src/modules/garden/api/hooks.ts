import { useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { qk } from '@/api/keys'
import { ApiError } from '@/api/client'
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

/** gardenError turns an API failure into the Czech sentence the server already
 *  wrote. The backend's 422s name the field and say what a valid value looks
 *  like, so surfacing `detail` is strictly better than a generic message. */
export function gardenError(e: unknown): string {
  return e instanceof ApiError ? (e.detail ?? cs.common.errorTitle) : cs.common.errorTitle
}

/** toastGardenError is the shared onError handler. */
export function toastGardenError(e: unknown): void {
  toast.error(gardenError(e))
}
