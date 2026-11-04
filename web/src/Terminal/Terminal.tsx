import {useEffect, useRef} from 'react'

// Use the legacy xterm package (v5.x)
// @ts-ignore - xterm types may not be perfectly typed
import * as XTerm from 'xterm'

export interface TerminalProps {
  /** ID of the session to connect to */
  sessionId: string
  /** URL of the API server */
  apiUrl?: string
}

export function Terminal({sessionId, apiUrl = '/api'}: TerminalProps) {
  const terminalRef = useRef<XTerm.Terminal | null>(null)

  useEffect(() => {
    // Initialize xterm.js terminal
    const term = new XTerm.Terminal({
      cursorBlink: true,
      theme: {
        background: '#1e1e1e',
        foreground: '#d4d4d4',
        cursor: '#d4d4d4',
      },
      fontFamily: 'Menlo, "DejaVu Sans Mono", monospace',
      fontSize: 14,
    })

    terminalRef.current = term

    // Render the terminal
    term.open(document.getElementById('rsc-terminal')!)

    // Connect to the WebSocket for real-time output
    const wsUrl = apiUrl.replace('http', 'ws') + `/ws/session/${sessionId}`
    const ws = new WebSocket(wsUrl)

    ws.onopen = () => {
      term.writeln('✓ Connected to session ' + sessionId)
    }

    ws.onmessage = (event: MessageEvent) => {
      const data = event.data as string
      term.write(data)
    }

    ws.onerror = (_err: Event) => {
      // @ts-ignore - Property access for WebSocket error
      term.writeln('✗ WebSocket error: ' + (_err.message || 'unknown'))
    }

    ws.onclose = () => {
      term.writeln('✗ WebSocket disconnected')
    }

    // Handle terminal size
    const handleResize = () => {
      // @ts-ignore - fit method may not be in type definitions
      term.fit()
    }
    window.addEventListener('resize', handleResize)
    handleResize()

    return () => {
      ws.close()
      term.dispose()
      window.removeEventListener('resize', handleResize)
    }
  }, [sessionId, apiUrl])

  return (
    <div id="rsc-terminal" style={{height: '100%', width: '100%'}} />
  )
}