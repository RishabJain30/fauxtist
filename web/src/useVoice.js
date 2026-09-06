import { useEffect, useRef, useState, useCallback } from 'react'
import { T } from './protocol.js'
import { STATE_SNAPSHOT_RECEIVED } from './reducer.js'

// STUN_ONLY_ICE is the fallback used until (or unless) the server answers
// an ice_config_request with something better, and whenever it can't —
// TURN is an enhancement for restrictive NATs, never a requirement: voice
// stays best-effort on STUN alone either way.
const STUN_ONLY_ICE = { iceServers: [{ urls: 'stun:stun.l.google.com:19302' }] }

export function useVoice({ meId, send, subscribe }) {
  const [active, setActive] = useState(false)
  const [muted, setMuted] = useState(true)
  const [speaking, setSpeaking] = useState(false)
  const [error, setError] = useState(null)

  const localStream = useRef(null)
  const pcs = useRef(new Map()) // peerId -> RTCPeerConnection
  const audios = useRef(new Map()) // peerId -> HTMLAudioElement
  const activeRef = useRef(false)
  const mutedRef = useRef(true)
  const speakingRef = useRef(false)
  const audioCtx = useRef(null)
  const speakTimer = useRef(null)
  const iceConfig = useRef(STUN_ONLY_ICE)
  // The generation carried on the most recently observed state_snapshot —
  // null until the first one arrives. A later snapshot carrying a
  // different generation means the underlying WebSocket was replaced
  // (roomConnection.js reconnected), not just refreshed via a same-socket
  // resync: the server has forgotten this seat's voice presence (see
  // processLeave in internal/room/join.go) and every peer already tore
  // down its RTCPeerConnection to us, so voice needs an explicit rejoin.
  const lastSnapshotGeneration = useRef(null)
  // Resolvers waiting on the next ice_config response (see enable()), so a
  // peer connection is never created against a still-in-flight TURN
  // credential request: without this, mic-permission latency could beat
  // the ice_config round trip, locking every peer created in that window
  // onto STUN-only forever (iceConfig.current changing later never
  // touches already-constructed RTCPeerConnection objects).
  const iceConfigWaiters = useRef([])

  const waitForIceConfig = useCallback((timeoutMs = 1500) => {
    return new Promise((resolve) => {
      const timer = setTimeout(resolve, timeoutMs)
      iceConfigWaiters.current.push(() => { clearTimeout(timer); resolve() })
    })
  }, [])

  const sendState = useCallback((m, sp) => send(T.VoiceState, { muted: m, speaking: sp }), [send])

  const closePeer = useCallback((peerId) => {
    const pc = pcs.current.get(peerId)
    if (pc) { pc.close(); pcs.current.delete(peerId) }
    const el = audios.current.get(peerId)
    if (el) { el.srcObject = null; audios.current.delete(peerId) }
  }, [])

  const closeAllPeers = useCallback(() => {
    pcs.current.forEach((pc) => pc.close())
    pcs.current.clear()
    audios.current.forEach((el) => { el.srcObject = null })
    audios.current.clear()
  }, [])

  const makePeer = useCallback((peerId) => {
    const existing = pcs.current.get(peerId)
    if (existing) return existing
    const pc = new RTCPeerConnection(iceConfig.current)
    pcs.current.set(peerId, pc)
    if (localStream.current) {
      localStream.current.getTracks().forEach((t) => pc.addTrack(t, localStream.current))
    }
    pc.onicecandidate = (e) => {
      if (e.candidate) send(T.VoiceSignal, { to: peerId, kind: 'ice', payload: e.candidate })
    }
    pc.ontrack = (e) => {
      let el = audios.current.get(peerId)
      if (!el) { el = new Audio(); el.autoplay = true; audios.current.set(peerId, el) }
      el.srcObject = e.streams[0]
      el.play().catch(() => {})
    }
    return pc
  }, [send])

  const callPeer = useCallback(async (peerId) => {
    const pc = makePeer(peerId)
    try {
      const offer = await pc.createOffer()
      await pc.setLocalDescription(offer)
      send(T.VoiceSignal, { to: peerId, kind: 'offer', payload: pc.localDescription })
    } catch { /* ignore */ }
  }, [makePeer, send])

  const onSignal = useCallback(async (from, kind, payload) => {
    const pc = makePeer(from)
    try {
      if (kind === 'offer') {
        await pc.setRemoteDescription(payload)
        const answer = await pc.createAnswer()
        await pc.setLocalDescription(answer)
        send(T.VoiceSignal, { to: from, kind: 'answer', payload: pc.localDescription })
      } else if (kind === 'answer') {
        await pc.setRemoteDescription(payload)
      } else if (kind === 'ice') {
        await pc.addIceCandidate(payload)
      }
    } catch { /* ignore malformed/late signals */ }
  }, [makePeer, send])

  useEffect(() => {
    const off = subscribe((msg) => {
      const p = msg.payload || {}
      switch (msg.type) {
        case T.VoicePeers:
          (p.ids || []).forEach((id) => { if (meId < id) callPeer(id) })
          break
        case T.VoicePeerJoined:
          if (p.id !== meId && meId < p.id) callPeer(p.id)
          break
        case T.VoicePeerLeft:
          closePeer(p.id)
          break
        case T.VoiceSignal:
          if (p.from) onSignal(p.from, p.kind, p.payload)
          break
        case T.IceConfig:
          if (Array.isArray(p.iceServers) && p.iceServers.length > 0) {
            iceConfig.current = { iceServers: p.iceServers }
          }
          iceConfigWaiters.current.forEach((resolve) => resolve())
          iceConfigWaiters.current = []
          break
        case STATE_SNAPSHOT_RECEIVED: {
          const seen = lastSnapshotGeneration.current
          const reconnected = seen != null && msg.generation !== seen
          lastSnapshotGeneration.current = msg.generation
          if (reconnected && activeRef.current) {
            // Every peer already closed their RTCPeerConnection to us the
            // moment our old socket disconnected (they got voice_peer_left)
            // — our own stale, now one-sided connections must go too, or
            // makePeer would keep handing out dead objects nothing will
            // ever renegotiate.
            closeAllPeers()
            send(T.VoiceJoin, {})
          }
          break
        }
        default:
          break
      }
    })
    return off
  }, [subscribe, meId, callPeer, closePeer, onSignal, closeAllPeers, send])

  const startSpeakingDetection = useCallback((stream) => {
    const Ctx = window.AudioContext || window.webkitAudioContext
    const ctx = new Ctx()
    audioCtx.current = ctx
    const analyser = ctx.createAnalyser()
    analyser.fftSize = 512
    ctx.createMediaStreamSource(stream).connect(analyser)
    const data = new Uint8Array(analyser.frequencyBinCount)
    speakTimer.current = setInterval(() => {
      analyser.getByteTimeDomainData(data)
      let sum = 0
      for (let i = 0; i < data.length; i++) { const v = (data[i] - 128) / 128; sum += v * v }
      const rms = Math.sqrt(sum / data.length)
      const sp = !mutedRef.current && rms > 0.05
      if (sp !== speakingRef.current) {
        speakingRef.current = sp
        setSpeaking(sp)
        sendState(mutedRef.current, sp)
      }
    }, 250)
  }, [sendState])

  const enable = useCallback(async () => {
    if (activeRef.current) return
    // Ask for the current ICE configuration up front, in parallel with the
    // mic prompt below, but do not send voice_join — and so never create a
    // peer connection — until it has either arrived or timed out. Racing
    // ahead on mic-permission latency alone would risk locking every peer
    // created in that window onto STUN-only forever, since iceConfig.current
    // changing later never touches an already-constructed RTCPeerConnection.
    send(T.IceConfigRequest, {})
    const iceReady = waitForIceConfig()
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
      await iceReady
      stream.getAudioTracks().forEach((t) => (t.enabled = false))
      localStream.current = stream
      activeRef.current = true
      setActive(true)
      startSpeakingDetection(stream)
      send(T.VoiceJoin, {})
      sendState(true, false)
    } catch {
      setError('Mic unavailable — you can still play')
    }
  }, [send, sendState, startSpeakingDetection, waitForIceConfig])

  const setMicMuted = useCallback((m) => {
    if (!activeRef.current) return
    mutedRef.current = m
    setMuted(m)
    localStream.current?.getAudioTracks().forEach((t) => (t.enabled = !m))
    sendState(m, m ? false : speakingRef.current)
  }, [sendState])

  const toggleMute = useCallback(() => {
    setMicMuted(!mutedRef.current)
  }, [setMicMuted])

  useEffect(() => {
    return () => {
      if (speakTimer.current) clearInterval(speakTimer.current)
      if (audioCtx.current) audioCtx.current.close().catch(() => {})
      closeAllPeers()
      localStream.current?.getTracks().forEach((t) => t.stop())
      if (activeRef.current) send(T.VoiceLeave, {})
    }
  }, [send, closeAllPeers])

  return { active, muted, speaking, error, enable, toggleMute, setMicMuted }
}
