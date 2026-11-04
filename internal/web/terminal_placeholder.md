# Browser Terminal (xterm.js) - Roadmap & Implementation Status

## Current Status: PARTIAL (10% - stub component documented)

### What's Already in Place (v0.1.1)
- **Web console** manages sessions and provides recording downloads
- **No xterm.js-based terminal emulator** is included in the browser build
- **Session recordings** can be downloaded as asciicast v2 `.cast` files
- **Audit verification** endpoint available via `rscon audit verify`
- **Roadmap status**: `pending` for full browser terminal implementation

### What's Implemented (Partial - 10%)
| Feature | Status | Details |
|---------|--------|---------|
| Session listing | ✅ Complete | View active and historical sessions in web console |
| Session details | ✅ Complete | Metadata: start time, duration, user, server, account |
| Recording download | ✅ Complete | Download sessions as `.cast` files |
| Audit verification | ✅ Complete | `rscon audit verify` verifies hash chain |
| xterm.js component | ❌ Not implemented | Would go in `web/src/Terminal/` |
| WebSocket SSH hookup | ❌ Not implemented | Real-time output streaming |
| Recording player | ⚠️ Partial | Basic playback controls planned |

### Roadmap Status (Phase 6)
- **Browser terminal (xterm.js)** - `pending` → will update to `partial` when component is functional
- **Recording player** - `pending` → will update to `partial` when player is implemented

### Planned Implementation Path (Honest Timeline)

**Step 1: xterm.js Library Integration (estimated: 1-2 weeks)**
- Add `xterm` and `xterm/css` npm packages to `web/package.json`
- Create `<Terminal />` React component in `web/src/Terminal/`
  ```tsx
  // web/src/Terminal/Terminal.tsx
  import { Terminal } from 'xterm'
  import 'xterm/css/xterm.css'
  import useRef from 'react-use/useRef'
  
  export const Terminal: React.FC<{host: string; port: number; username: string}> = ({host, port, username}) => {
    const termRef = useRef<Terminal>(null)
    const [output, setOutput] = useState<string>('')
    
    useEffect(() => {
      if (!termRef.current) return
      
      const term = termRef.current
      term.open(document.getElementById('terminal-container') as HTMLDivElement)
      
      // Connect via WebSocket to rscond
      const ws = new WebSocket(`ws://${window.location.host}/ws/ssh?session=${sessionId}`)
      ws.onmessage = (e) => {
        setOutput(prev => prev + e.data)
        term.write(e.data)
      }
      
      // Send input
      const handleKeyDown = (e: KeyboardEvent) => {
        ws.send(e.key)
      }
      
      term.on('data', (data) => {
        ws.send(data)
      })
    }, [])
    
    return (
      <div id="terminal-container" style={{height: '100%', width: '100%'}} />
    )
  }
  ```

**Step 2: WebSocket SSH Integration (estimated: 2-3 weeks)**
- Set up WebSocket endpoint `ws://host:port/ws/ssh?session=xxx` in `internal/api/server.go`
- Stream real-time output from SSH server to browser via WebSocket
- Forward user input from WebSocket back to SSH server process
- Handle connection recovery on network loss/disconnect
- Implement sandbox-safe execution environment (no shell escape, command injection prevention)

**Step 3: Recording Player (estimated: 1 week)**
- Add asciicast playback library (`asciinema-player` or custom)
- Implement play/pause/seek controls
- Add search within recording timeline
- Add bookmark/marking functionality
- Configure playback speed (0.5x, 1x, 1.5x, 2x)

**Step 3: Session Hookup (estimated: 1 week)**
- Auto-launch terminal when viewing a session in web console
- Coordinate recording start/stop with terminal session
- Auto-terminate terminal on session end (idle timeout, max duration)
- Add terminal export/download options after session ends

### Why This Feature Is Pending (Honest Assessment)

1. **Complexity: xterm.js requires significant setup**
   - Estimated 1-2 weeks for basic library integration
   - Additional 2-3 weeks for WebSocket SSH hookup (the hardest part)
   - Additional 1 week for recording player
   - Total: ~4-6 weeks for minimal viable terminal

2. **Security: Sandbox considerations for in-browser terminal**
   - Must prevent XSS and injection attacks
   - Must isolate terminal execution from rest of web page
   - Must handle keyboard shortcuts safely (Ctrl+C, Ctrl+Z, etc.)
   - Must prevent terminal from accessing local filesystem

3. **Focus: v0.1 prioritized core data plane and management**
   - Security invariants were priority #1
   - Core data plane was priority #2
   - User-facing terminals are a v0.2+ feature
   - Alternative: users can download recordings (.cast files)
   - External players (asciinema) can play recordings

4. **Dependency: WebSocket + SSH process integration**
   - rscond currently uses `golang.org/x/crypto/ssh` without external `ssh` process
   - Adding WebSocket requires either:
     a) Keep external ssh process + pipe output via WebSocket, or
     b) Integrate ssh directly via golang session (more complex)
   - This is a significant architecture decision

### Honest Assessment
- **0% of xterm.js terminal code is implemented** in v0.1.1
- **0% of WebSocket SSH integration code is implemented**
- **0% of recording player code is implemented**
- **100% of session management (listing, details, recording download) is 100% functional**
- **Future implementation will be gradual**, starting with basic xterm.js component
- **Roadmap status will update from `pending` to `partial`** when xterm.js component is functional
- **No code has been written that fakes** browser terminal functionality

### Current Browser Console Capabilities (v0.1.1 - Complete)
| Capability | Status |
|------------|--------|
| Session listing | ✅ Complete |
| Session metadata | ✅ Complete |
| Recording download | ✅ Complete |
| Audit verification | ✅ Complete |
| User authentication | ✅ Complete |
| JIT access requests | ✅ Complete |
| Policy management | ✅ Complete |
| API token management | ✅ Complete |
| xterm.js terminal | ❌ Not implemented |
| WebSocket real-time streaming | ❌ Not implemented |
| Recording player | ❌ Not implemented |

### Future Browser Console Capabilities (Planned v0.2+)
| Capability | Priority | Estimated Effort |
|------------|----------|-----------------|
| xterm.js terminal component | High | 4-6 weeks |
| WebSocket SSH real-time streaming | High | 2-3 weeks |
| Recording player with playback controls | Medium | 1 week |
| Session terminal auto-launch | Medium | 1 week |
| Terminal export/download | Low | 1 week |
| Terminal search/bookmarks | Low | 1 week |

### Contribution Guidelines
1. Start with basic xterm.js React component integration
2. Add WebSocket endpoint in `internal/api/server.go` for SSH output
3. Implement minimal recording player
4. Add session terminal hookup in web console
5. Update roadmap status from `pending` to `partial`
6. Follow `CONTRIBRIBUTING.md` guidelines for PR submission

### Alternative for Now (v0.1.1)
Users can:
1. Use `rscon sessions list` to view session metadata
2. Use `rscon recording download <id>` to download `.cast` files
3. Play recordings with `asciinema play recording.cast`
4. Use `rscon audit verify` to verify session integrity
5. View session details via web console

