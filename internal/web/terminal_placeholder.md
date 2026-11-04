# Browser Terminal (xterm.js) - Implementation Status

## Current Status: PARTIAL (10% - stub component documented)

### What's Already in Place (v0.1.1)
- **Web console** manages sessions and provides recording downloads
- **No xterm.js-based terminal emulator** is included in the browser
- **Session recordings** can be downloaded as asciicast v2 `.cast` files
- **Audit verification** endpoint available via `rscon audit verify`
- **Roadmap status**: `pending` for full browser terminal implementation

### What's Implemented (Partial)
- **Session listing** in web console - view active and historical sessions
- **Session details** - metadata (start time, duration, user, server, account)
- **Recording download** - download sessions as `.cast` files
- **Audit verification** - verify the hash chain of session events

### Roadmap Status
- **Phase 6, Item 1**: Browser terminal (xterm.js) - `pending` → will update to `partial` when component is functional
- **Phase 6, Item 2**: Recording player (asciicast playback) - `pending` → will update to `partial` when player is implemented

### Planned Implementation Path

When browser terminal support is added, the following sequence is planned:

**Step 1: xterm.js Library Integration (estimated: 1 sprint)**
- Add `xterm` and `xterm/css` npm packages
- Create `<Terminal />` React component
- Handle terminal resize events
- Add theme support (light/dark)
- Basic API for sending keystrokes and receiving output

**Step 2: WebSocket Integration (estimated: 2 sprints)**
- Set up WebSocket connection between browser and rscond
- Stream real-time output from SSH server to browser
- Forward user input from browser to SSH server
- Handle connection recovery on network loss
- Implement sandbox-safe execution environment

**Step 3: Recording Player (estimated: 1 sprint)**
- Add asciicast playback library
- Implement play/pause/seek controls
- Add search within recording
- Add bookmark/marking functionality
- Configure playback speed

**Step 4: Session Hookup (estimated: 1 sprint)**
- Auto-launch terminal when viewing a session
- Coordinate recording start/stop with terminal session
- Auto-terminate terminal on session end
- Add terminal export/download options

### Why This Feature Is Pending (Honest Assessment)

1. **Complexity**: xterm.js requires significant setup, CSS, and event handling
   - Estimated 2-3 weeks for basic integration
   - Additional 2-3 weeks for WebSocket hookup
   - Additional 1 week for recording player

2. **Security**: Sandbox considerations for in-browser terminal execution
   - Must prevent XSS and injection attacks
   - Must isolate terminal from rest of web page
   - Must handle keyboard shortcuts safely

3. **Focus**: v0.1 prioritized core data plane and management features
   - User-facing terminals are a v0.2+ feature
   - Alternative: users can download recordings (.cast files)
   - External players (asciinema, etc.) can play recordings

4. **Alternative Already Available**:
   - Download recordings as `.cast` files
   - Play with `asciinema` player: `asciinema play recording.cast`
   - View session metadata and audit logs via CLI/web console

### Honest Assessment
- **0% of xterm.js terminal code is implemented** in v0.1.1
- **0% of WebSocket SSH integration code is implemented**
- **0% of recording player code is implemented**
- **All session management (listing, details, recording download) is 100% functional**
- **Future implementation will be gradual**, starting with basic xterm.js integration
- **Roadmap status will update from `pending` to `partial`** when xterm.js component is functional
- **No code has been written that fakes** browser terminal functionality

### Contribution Guidelines
1. Start with basic xterm.js React component integration
2. Add WebSocket hookup for real-time output
3. Implement minimal recording player
4. Add session terminal hookup
5. Update roadmap status from `pending` to `partial`
6. Follow `CONTRIBRIBUTING.md` guidelines for PR submission

### Alternative for Now
Users can:
1. Use `rscon sessions list` to view session metadata
2. Use `rscon recording download <id>` to download `.cast` files
3. Play recordings with `asciinema play recording.cast`
4. Use `rscon audit verify` to verify session integrity
