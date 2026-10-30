import assert from 'node:assert/strict'
import test from 'node:test'
import { filterQueues, filterUnhealthyQueues, queueVHosts } from './queueFilters.mjs'

test('keeps warning, down, stale, and unknown queues for the overview', () => {
  const queues = [
    { name: 'healthy', status: 'healthy', is_stale: false },
    { name: 'warning', status: 'warning', is_stale: false },
    { name: 'down', status: 'down', is_stale: false },
    { name: 'stale', status: 'healthy', is_stale: true },
    { name: 'unknown', status: 'unknown', is_stale: false },
  ]

  assert.deepEqual(
    filterUnhealthyQueues(queues).map((queue) => queue.name),
    ['warning', 'down', 'stale', 'unknown'],
  )
  assert.equal(queues.length, 5)
})

test('returns no attention queues when all queues are healthy and current', () => {
  assert.deepEqual(
    filterUnhealthyQueues([{ status: 'healthy', is_stale: false }]),
    [],
  )
})

test('filters queues by critical, warning, healthy, stale, and unknown status', () => {
  const queues = [
    { name: 'healthy', status: 'healthy', is_stale: false, node: 'rabbit@a', vhost: '/' },
    { name: 'warning', status: 'warning', is_stale: false, node: 'rabbit@a', vhost: '/' },
    { name: 'down', status: 'down', is_stale: false, node: 'rabbit@b', vhost: 'telemetry' },
    { name: 'stale', status: 'healthy', is_stale: true, node: 'rabbit@b', vhost: 'telemetry' },
    { name: 'unknown', status: 'unknown', is_stale: false, node: '', vhost: '' },
  ]

  assert.deepEqual(filterQueues(queues, { status: 'critical' }).map((queue) => queue.name), ['down'])
  assert.deepEqual(filterQueues(queues, { status: 'warning' }).map((queue) => queue.name), ['warning'])
  assert.deepEqual(filterQueues(queues, { status: 'healthy' }).map((queue) => queue.name), ['healthy'])
  assert.deepEqual(filterQueues(queues, { status: 'stale' }).map((queue) => queue.name), ['stale'])
  assert.deepEqual(filterQueues(queues, { status: 'unknown' }).map((queue) => queue.name), ['unknown'])
  assert.deepEqual(
    filterQueues(queues, { status: 'critical', vhost: 'telemetry' }).map((queue) => queue.name),
    ['down'],
  )
})

test('filters by vhost and allows selecting unknown vhosts', () => {
  const queues = [
    { name: 'one', status: 'healthy', is_stale: false, node: 'rabbit@a', vhost: '/' },
    { name: 'two', status: 'healthy', is_stale: false, node: 'rabbit@b', vhost: 'telemetry' },
    { name: 'three', status: 'unknown', is_stale: false, node: 'rabbit@c', vhost: '' },
  ]

  assert.deepEqual(filterQueues(queues, { vhost: 'telemetry' }).map((queue) => queue.name), ['two'])
  assert.deepEqual(filterQueues(queues, { vhost: 'rabbit@b' }), [])
  assert.deepEqual(filterQueues(queues, { vhost: '__unknown__' }).map((queue) => queue.name), ['three'])
  assert.deepEqual(queueVHosts(queues), ['/', 'telemetry'])
})

test('searches queue names case-insensitively without matching host or vhost', () => {
  const queues = [
    { name: 'telemetry_cache', status: 'healthy', is_stale: false, node: 'rabbit@dev05', vhost: '/' },
    { name: 'orders', status: 'healthy', is_stale: false, node: 'rabbit@telemetry', vhost: 'telemetry' },
  ]

  assert.deepEqual(filterQueues(queues, { name: 'TELEMETRY' }).map((queue) => queue.name), ['telemetry_cache'])
  assert.deepEqual(filterQueues(queues, { name: 'rabbit@dev05' }), [])
  assert.deepEqual(filterQueues(queues, { name: '  orders  ' }).map((queue) => queue.name), ['orders'])
})
