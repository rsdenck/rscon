# Browser Terminal (xterm.js) - Pending Implementation

## Status: PENDING (not yet implemented)

This feature is documented as pending in the roadmap and will be implemented in a future release.

### Current State
- **No browser terminal** is included in RSCON v0.1
- The web console manages sessions and provides recording downloads, but **does not include** an xterm.js-based terminal emulator
- Session recordings can be downloaded as asciicast v2 files, but there is no in-browser player
- All browser terminal support is listed as `pending` in `docs/development/roadmap.md`

### Current Web Console Features (related to sessions)
- **Session listing**: View active and historical sessions
- **Session details**: View session metadata (start time, duration, user, server, account)
- **Recording download**: Download session recordings as `.cast` files
- **Audit verification**: Verify the hash chain of session events

### Planned Implementation (future release)
When browser terminal support is added, the following will be implemented:

1. **xterm.js Integration** (`web/src/`):
   - xterm.js library integration
   - Terminal resize handling
   - Keyboard and mouse event passthrough
   - Theme support (light/dark)

2. **Terminal Component** (`web/src/Terminal/`):
   - `<Terminal />` React component
   - Autoscroll toggle
   - Fit to screen toggle
   - Copy to clipboard

3. **Recording Player** (`web/src/RecordingPlayer/`):
   - Playback of asciicast v2 files
   - Playback speed control
   - Search within recording
   - Bookmark markings

4. **Session Hookup** (`web/src/sessions/`):
   - Auto-launch terminal when viewing a session
   - Terminal auto-termination on session end
   - Recording start/stop coordination

5. **WebSocket Integration**:
   - Real-time output streaming
   - Input forwarding from browser to SSH server
   - Connection recovery on network loss

### Roadmap Status
- **Current phase**: Pending (0% complete)
- **Estimated effort**: 3-4 sprints (library integration + UI + hookup)
- **Dependencies**: xterm.js library, React integration, WebSocket handling
- **Will update to `partial` status** when terminal component is functional

### References
- See `CONTRIBUTING.md` for contribution guidelines
- Track progress in `docs/development/roadmap.md`
- See `RELEASE_NOTES.md` for version planning
- xterm.js: https://github.com/xtermjs/xterm.js
- asciicast player: https://asciinema.org player library

### Why This Feature Is Pending (Honest Assessment)
1. **Complexity**: xterm.js requires significant setup, CSS, and event handling
2. **Security**: Sandbox considerations for in-browser terminal execution
3. **Focus**: v0.1 prioritizes core data plane and management features over user-facing terminals
4. **Alternative**: Users can download recordings (.cast files) and play them with external players

### Future Considerations
- After v0.1, a browser terminal could be added as a separate feature
- Could be integrated with the recording download workflow
- Could support SSH over WebSocket (wssh) for true remote terminal access
- Could support copy/paste between local and remote sessions
