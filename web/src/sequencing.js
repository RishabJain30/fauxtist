import { T, SEQUENCED_TYPES } from './protocol.js'

// decideSequence is a pure function deciding what to do with one incoming
// envelope given the highest sequenced revision already applied locally.
// It never touches state itself — useRoomSocket acts on the verdict.
//
// Verdicts:
//   'apply'            - safe to apply now (unsequenced message, the very
//                         first sequenced message seen, or exactly the
//                         next expected revision)
//   'apply-snapshot'    - a state_snapshot that is not older than what's
//                         already applied: always safe to apply, since a
//                         snapshot is a full authoritative replace, not an
//                         incremental delta
//   'stale-snapshot'    - a state_snapshot strictly older than what's
//                         already applied: must be dropped, or it would
//                         overwrite newer client state with stale data
//   'duplicate-or-old'  - an exact repeat, or an incremental event at or
//                         behind the already-applied revision: drop it
//   'gap'               - the next incremental event skips one or more
//                         revisions: do not apply it, request a resync
export function decideSequence(lastAppliedSeq, envelope) {
  if (!SEQUENCED_TYPES.has(envelope.type)) return 'apply'

  const seq = envelope.seq
  if (typeof seq !== 'number') return 'apply' // defensive: treat as unsequenced rather than crash on it

  if (envelope.type === T.StateSnapshot) {
    return lastAppliedSeq != null && seq < lastAppliedSeq ? 'stale-snapshot' : 'apply-snapshot'
  }

  if (lastAppliedSeq == null) return 'apply' // haven't got a snapshot yet; don't wedge on it, just apply
  if (seq <= lastAppliedSeq) return 'duplicate-or-old'
  if (seq === lastAppliedSeq + 1) return 'apply'
  return 'gap'
}
