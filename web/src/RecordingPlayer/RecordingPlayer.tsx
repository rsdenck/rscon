import {useEffect, useRef, useState} from 'react'

export interface RecordingPlayerProps {
  /** Duration of the recording in seconds */
  duration: number
  /** Callback when seek changes */
  onSeek?: (seconds: number) => void
}

export function RecordingPlayer({
  duration,
  onSeek,
}: RecordingPlayerProps) {
  const barRef = useRef<HTMLDivElement>(null)
  const [seek, setSeek] = useState(0)

  useEffect(() => {
    const updateSeek = () => {
      if (barRef.current) {
        const percent = seek / duration
        barRef.current.style.width = `${Math.min(percent * 100, 100)}%`
      }
    }
    updateSeek()
  }, [seek, duration])

  const handleClick = (e: React.MouseEvent<HTMLDivElement>) => {
    if (barRef.current && duration > 0) {
      const rect = barRef.current.getBoundingClientRect()
      const clickX = e.clientX - rect.left
      const percent = Math.max(0, Math.min(1, clickX / rect.width))
      const seconds = Math.round(percent * duration)
      setSeek(seconds)
      onSeek?.(seconds)
    }
  }

  return (
    <div className="relative w-full h-6 rounded bg-slate-800">
      <div
        ref={barRef}
        className="absolute inset-0 bg-emerald-600 rounded-full transition-colors duration-200"
        style={{width: seek / duration * 100 + '%'}}
        onClick={handleClick}
      />
    </div>
  )
}