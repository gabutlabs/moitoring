import assert from 'node:assert/strict'
import test from 'node:test'
import { dashboardViewByKey, dashboardViews } from './dashboardViews.mjs'

test('each sidebar view has a complete empty state and a unique key', () => {
  const keys = dashboardViews.map((view) => view.key)

  assert.equal(new Set(keys).size, keys.length)
  assert.deepEqual(keys, ['overview', 'services', 'queues'])

  for (const view of dashboardViews) {
    assert.ok(view.label)
    assert.ok(view.title)
    assert.ok(view.description)
    assert.ok(view.eyebrow)
    assert.ok(view.emptyTitle)
    assert.ok(view.emptyDescription)
    assert.ok(view.emptyDetails)
    assert.equal(dashboardViewByKey(view.key), view)
  }
})

test('an unknown view falls back to overview', () => {
  assert.equal(dashboardViewByKey('missing').key, 'overview')
})
