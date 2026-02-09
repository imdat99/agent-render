import { state } from './state.js';

export function initTerminal() {
    state.terminal = new Terminal({
        theme: {
            background: '#000000',
            foreground: '#cbd5e1', // slate-300
            cursor: '#818cf8', // indigo-400
            cursorAccent: '#000000',
            selection: 'rgba(129, 140, 248, 0.2)',
            black: '#000000',
            red: '#ef4444',
            green: '#22c55e',
            yellow: '#eab308',
            blue: '#6366f1',
            magenta: '#a855f7',
            cyan: '#06b6d4',
            white: '#f8fafc',
            brightBlack: '#475569',
            brightRed: '#f87171',
            brightGreen: '#4ade80',
            brightYellow: '#facc15',
            brightBlue: '#818cf8',
            brightMagenta: '#c084fc',
            brightCyan: '#67e8f9',
            brightWhite: '#ffffff',
        },
        fontFamily: "'JetBrains Mono', 'Fira Code', 'Menlo', 'Courier New', monospace",
        fontSize: 13,
        fontWeight: '400',
        lineHeight: 1.5,
        cursorBlink: true,
        cursorStyle: 'bar',
        cursorWidth: 2,
        scrollback: 50000, // Large scrollback for logs
        disableStdin: true, // Read-only for now
    });

    state.fitAddon = new FitAddon.FitAddon();
    state.terminal.loadAddon(state.fitAddon);
    state.terminal.open(document.getElementById('terminal'));

    // Initial banner
    writeTerminalBanner();

    // Auto-resize
    const resizeObserver = new ResizeObserver(() => {
        try { state.fitAddon.fit(); } catch (e) { }
    });
    resizeObserver.observe(document.getElementById('terminal'));
    window.addEventListener('resize', () => state.fitAddon.fit());
    setTimeout(() => state.fitAddon.fit(), 100);
}

function writeTerminalBanner() {
    state.terminal.clear();
    state.terminal.writeln('\x1b[90m [SYSTEM] Ready. Waiting for job selection...\x1b[0m');
}

export function writeLogLine(line) {
    if (!line) return;

    // Heuristic color coding
    if (line.match(/error|fail|exception|fatal/i)) {
        state.terminal.writeln(`\x1b[31m${line}\x1b[0m`);
    } else if (line.match(/warn/i)) {
        state.terminal.writeln(`\x1b[33m${line}\x1b[0m`);
    } else if (line.match(/success|completed|done/i)) {
        state.terminal.writeln(`\x1b[32m${line}\x1b[0m`);
    } else if (line.match(/info/i)) {
        state.terminal.writeln(`\x1b[34m${line}\x1b[0m`);
    } else {
        state.terminal.writeln(line);
    }
}

export function clearTerminal() {
    state.terminal.clear();
}
