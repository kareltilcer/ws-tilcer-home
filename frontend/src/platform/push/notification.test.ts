import { describe, expect, it } from 'vitest'
import { notificationFor } from './notification'

// The bug these cover: chat pushes carried a per-conversation collapse tag and
// nothing else, so every message after the first REPLACED a notification already
// in the shade — and a same-tag replacement is silent unless renotify says
// otherwise. The room stayed at one entry, which was the point, and stopped
// making any sound, which was not.

describe('notificationFor', () => {
  it('re-alerts a collapsed notification when the envelope asks for it', () => {
    const { options } = notificationFor({ title: 'Domácnost · Karel', tag: 'chat:c1', renotify: true })
    expect(options.tag).toBe('chat:c1')
    expect(options.renotify).toBe(true)
  })

  it('leaves a collapsing notification silent by default', () => {
    // A module that collapses in order to update quietly — an unread count, a
    // progress total — must not start buzzing because it set a tag.
    const { options } = notificationFor({ title: 'Souhrn', tag: 'summary:morning' })
    expect(options.renotify).toBe(false)
  })

  // ⚠ showNotification THROWS a TypeError on renotify with an empty tag, and a
  // push handler that throws shows NOTHING — the silent delivery this whole
  // change exists to fix, made unconditional.
  it('never sets renotify without a tag to hang it on', () => {
    for (const tag of [undefined, '']) {
      const { options } = notificationFor({ title: 'T', tag, renotify: true })
      expect(options.renotify, `tag ${JSON.stringify(tag)}`).toBe(false)
    }
  })

  it('falls back to a titled notification rather than an empty one', () => {
    expect(notificationFor({}).title).toBe('Home')
    expect(notificationFor({ body: 'B' }).options.body).toBe('B')
  })

  it('routes a click through data.url, defaulting to the app root', () => {
    const { options } = notificationFor({ module: 'chat', type: 'chat', url: '/chat/c1' })
    expect(options.data).toMatchObject({ url: '/chat/c1', module: 'chat', type: 'chat' })
    expect(notificationFor({}).options.data).toMatchObject({ url: '/' })
  })

  it('carries the envelope’s extra routing hints into data', () => {
    const { options } = notificationFor({ url: '/chat/c1', data: { conversation_id: 'c1', message_id: 'm7' } })
    expect(options.data).toMatchObject({ url: '/chat/c1', conversation_id: 'c1', message_id: 'm7' })
  })
})
