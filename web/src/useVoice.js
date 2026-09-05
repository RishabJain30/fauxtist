import { useEffect, useRef, useState, useCallback } from 'react'
import { T } from './protocol.js'

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

  const sendState = useCallback((m, sp) => send(T.VoiceState, { muted: m, speaking: sp }), [send])

  const closePeer = useCallback((peerId) => {
    const pc = pcs.current.get(peerId)
    if (pc) { pc.close(); pcs.current.delete(peerId) }
    const el = audios.current.get(peerId)
    if (el) { el.srcObject = null; audios.current.delete(peerId) }
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
          break
        default:
          break
      }
    })
    return off
  }, [subscribe, meId, callPeer, closePeer, onSignal])

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
    // Best-effort: ask for the current ICE configuration before any peer
    // connection is created (that happens later, once VoiceJoin's response
    // enumerates peers to call) — the mic prompt below gives the round
    // trip time to land. If it doesn't, or the server has no TURN
    // configured, iceConfig.current is still a valid STUN-only fallback.
    send(T.IceConfigRequest, {})
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
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
  }, [send, sendState, startSpeakingDetection])

  const toggleMute = useCallback(() => {
    if (!activeRef.current) return
    const m = !mutedRef.current
    mutedRef.current = m
    setMuted(m)
    localStream.current?.getAudioTracks().forEach((t) => (t.enabled = !m))
    sendState(m, m ? false : speakingRef.current)
  }, [sendState])

  useEffect(() => {
    return () => {
      if (speakTimer.current) clearInterval(speakTimer.current)
      if (audioCtx.current) audioCtx.current.close().catch(() => {})
      pcs.current.forEach((pc) => pc.close())
      pcs.current.clear()
      audios.current.forEach((el) => { el.srcObject = null })
      audios.current.clear()
      localStream.current?.getTracks().forEach((t) => t.stop())
      if (activeRef.current) send(T.VoiceLeave, {})
    }
  }, [send])

  return { active, muted, speaking, error, enable, toggleMute }
}
