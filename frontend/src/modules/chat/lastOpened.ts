import type { Conversation } from './api/types'

/**
 * Which conversation `/chat` should open (v10.1, D269).
 *
 * ⚠ THE EMPTY SECOND PANE WAS THE BUG, and it only exists on the desktop. At ≥1024
 * the list and the thread are both on screen (D262), so arriving at `/chat` put a
 * member in front of a permanent half-page of instructions telling them to click
 * something — every single time they opened the module. Below 1024 the list IS the
 * screen and there is no empty pane to fill, which is why the caller gates this on
 * the viewport and not on the data.
 *
 * ⚠ AND WHY IT MUST BE GATED THERE. Redirecting `/chat` → `/chat/{id}` on a phone
 * would make the conversation list unreachable: the thread's back arrow goes to
 * `/chat`, which would redirect straight back into the thread. A member could not
 * get to their own list of rooms. The desktop has no such loop because the list
 * never leaves the screen.
 *
 * ⚠ WHAT IS REMEMBERED IS AN ID, NOT CONTENT. Chat is excluded from the PWA
 * persister because message bodies and other members' names on a shared laptop's
 * disk are worth less than offline reading (leak row 20) — and that argument is
 * about CONTENT. A UUID says nothing to somebody reading the disk: not the room's
 * name, not who is in it, not a word anybody wrote. It is keyed per user anyway, so
 * two people sharing the kitchen laptop do not inherit each other's last room.
 */

/** Where one member's last-opened room is remembered. */
function storageKey(userID: string): string {
  return `home.chat.lastOpened.${userID}`
}

/**
 * rememberLastOpened records the room a member is looking at.
 *
 * ⚠ EVERY FAILURE IS SWALLOWED. localStorage throws in a private window, when site
 * data is blocked, and when the quota is full — and none of those is a reason for a
 * thread to stop rendering. What is lost is the convenience, which is what it is.
 */
export function rememberLastOpened(userID: string, conversationID: string): void {
  if (!userID || !conversationID) return
  try {
    localStorage.setItem(storageKey(userID), conversationID)
  } catch {
    // Deliberately empty — see above.
  }
}

/** readLastOpened is the remembered id, or '' when there is none to be had. */
export function readLastOpened(userID: string): string {
  if (!userID) return ''
  try {
    return localStorage.getItem(storageKey(userID)) ?? ''
  } catch {
    return ''
  }
}

/**
 * pickConversationToOpen chooses which room `/chat` lands on.
 *
 * The order is Karel's, and each step answers a different question:
 *
 *  1. **The remembered room, IF IT IS STILL IN THE LIST.** A room can be left,
 *     trashed or purged between two visits, and a stored id that is followed
 *     blindly is a navigation straight into a 404 — the one screen this whole
 *     mechanism exists to avoid landing on.
 *  2. **Všichni.** The household room is the one conversation every member is in by
 *     construction (D258), so it is the only defensible answer when nothing is
 *     remembered.
 *  3. **The first row**, which is the most recently active one — the list is
 *     ordered by `updated_at` descending.
 *
 * ⚠ "A MEMBER WITH ONE ROOM ALWAYS LANDS IN IT" FALLS OUT OF THAT ORDER RATHER THAN
 * BRANCHING ON IT. Their single conversation is either Všichni, which step 2 picks,
 * or the only row, which step 3 does. A `length === 1` case would be a fourth rule
 * that can only ever agree with the three above it — and a rule that cannot
 * disagree is a rule that can drift out of agreement unnoticed.
 *
 * ⚠ IT RETURNS '' RATHER THAN GUESSING WHEN THE LIST IS EMPTY. A member in no
 * conversation at all should see the empty state, which says what the module is;
 * navigating them somewhere would be navigating them nowhere.
 *
 * Pure, and exported, so the order is asserted by a test rather than by reading it.
 */
export function pickConversationToOpen(
  conversations: readonly Conversation[],
  remembered: string,
): string {
  if (conversations.length === 0) return ''
  if (remembered && conversations.some((c) => c.id === remembered)) return remembered
  const everyone = conversations.find((c) => c.kind === 'default')
  if (everyone) return everyone.id
  return conversations[0].id
}
