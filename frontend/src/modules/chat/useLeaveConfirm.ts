import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'

/**
 * D244's confirmation: leaving the clean-up page while a threshold is still
 * exceeded asks first.
 *
 * ⚠ IT IS A CONFIRM, NEVER A BLOCK. Whoever answers "leave" leaves. Nothing here
 * may ever produce a page somebody cannot get out of, which is why the fallback for
 * every case this hook does not cover is *let the navigation happen*.
 *
 * ⚠ AND `beforeunload` ALONE WOULD NOT DO, which is the whole reason this file
 * exists. Most exits from a chat sub-page are CLIENT-SIDE route changes — tapping
 * another tab in the bottom bar, the side nav, the back link in the header — and
 * none of them ever reaches `beforeunload`. Guarding only the unload would have
 * produced a confirmation that fires when somebody closes the browser and stays
 * silent when they walk away from the screen, which is the case the decision is
 * about.
 *
 * ⚠ SO WHY NOT react-router's `useBlocker`. Because it is DATA-ROUTER ONLY, and
 * this app mounts `<BrowserRouter>` (App.tsx). `unstable_usePrompt` was written
 * here first and it threw *"useBlocker must be used within a data router"* on
 * mount — the clean-up page rendered a blank screen and nothing else. It was found
 * by opening the page, which is exactly where §V9-12 says these live. Migrating the
 * whole route tree to `createBrowserRouter` to gain one confirmation is a change to
 * every module in the app; intercepting the click is a change to this one.
 *
 * ⚠ WHAT IT DOES NOT COVER, stated rather than hidden: the BROWSER'S OWN BACK
 * BUTTON. By the time `popstate` fires the entry is already gone, and the only way
 * back is to push a sentinel and re-push it on cancel — a pattern that traps people
 * when it goes wrong, which is the one outcome "a confirm, never a block" rules out.
 * Back leaves without asking. The figure is still on the storage picture and in
 * Administrace, so nothing is lost by it.
 */
export function useLeaveConfirm(active: boolean, message: string): void {
  const navigate = useNavigate()

  useEffect(() => {
    if (!active) return

    // The secondary guard: a tab close or a reload. The browser shows its own
    // wording, not ours — every engine ignores the string — so this exists to make
    // the pause happen at all, and the specific sentence lives on the click path.
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', onBeforeUnload)

    // The primary guard, in the CAPTURE phase so it runs before react-router's own
    // link handler consumes the click.
    const onClick = (e: MouseEvent) => {
      // ⚠ EVERY ONE OF THESE EXITS IS "let the browser do what it was going to".
      // A modified click opens a new tab and never leaves this page; a non-primary
      // button is not a navigation; a default already prevented is somebody else's
      // handler that has already decided.
      if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) {
        return
      }
      const anchor = (e.target as HTMLElement | null)?.closest?.('a')
      const href = anchor?.getAttribute('href')
      if (!anchor || !href) return
      // An external link, a download, or one aimed at another tab leaves this page
      // standing — there is nothing to confirm.
      if (anchor.target && anchor.target !== '_self') return
      if (anchor.hasAttribute('download')) return
      if (!href.startsWith('/')) return
      // Staying on this page is not leaving it.
      if (href === window.location.pathname) return

      e.preventDefault()
      e.stopPropagation()
      // eslint-disable-next-line no-alert -- the platform confirm is the one dialog
      // that survives the navigation it is interrupting; a React modal here would
      // have to re-drive the navigate itself anyway, and this is the same control
      // every other "are you sure" in the browser uses.
      if (window.confirm(message)) navigate(href)
    }
    document.addEventListener('click', onClick, true)

    return () => {
      window.removeEventListener('beforeunload', onBeforeUnload)
      document.removeEventListener('click', onClick, true)
    }
  }, [active, message, navigate])
}
