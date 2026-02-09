// Premium Dashboard Logic
const API_URL = '';
const WS_URL = `ws://${window.location.host}/api/ws`;

// State Management
const state = {
    jobs: [],
    agents: [],
    currentJobId: null,
    terminal: null,
    fitAddon: null,
    ws: null,
    reconnectAttempts: 0,
    maxReconnectAttempts: 10
};

// --- Terminal Styling & Config ---
function initTerminal() {
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
    const t = state.terminal;
    t.clear();
    t.writeln('\x1b[1;34m  ____  _      ____  _      ____                 _\x1b[0m');
    t.writeln('\x1b[1;34m |  _ \\(_) ___|  _ \\(_) ___|  _ \\ ___ _ __   __| | ___ _ __\x1b[0m');
    t.writeln('\x1b[1;34m | |_) | |/ __| |_) | |/ __| |_) / _ \\ \'_ \\ / _` |/ _ \\ \'__|\x1b[0m');
    t.writeln('\x1b[1;34m |  __/| | (__|  __/| | (__|  _ <  __/ | | | (_| |  __/ |\x1b[0m');
    t.writeln('\x1b[1;34m |_|   |_|\\___|_|   |_|\\___|_| \\_\\___|_| |_|\\__,_|\\___|_|\x1b[0m \x1b[90mv0.2\x1b[0m');
    t.writeln('');
    t.writeln('\x1b[90m [SYSTEM] Ready. Waiting for job selection...\x1b[0m');
}

// --- WebSocket Connection ---
function connectWS() {
    const indicator = document.getElementById('ws-indicator');
    const statusText = document.getElementById('ws-status-text');
    const dot1 = indicator.querySelector('div span:nth-child(1)');
    const dot2 = indicator.querySelector('div span:nth-child(2)');

    // Set connecting state
    statusText.textContent = "Connecting...";
    statusText.className = "text-yellow-400";
    dot2.className = "relative inline-flex rounded-full h-2 w-2 bg-yellow-500";
    indicator.className = "flex items-center gap-2 px-3 py-1.5 rounded-full bg-yellow-900/10 border border-yellow-700/30 text-xs font-medium transition-colors";

    state.ws = new WebSocket(WS_URL);

    state.ws.onopen = () => {
        console.log('WS Connected');
        state.reconnectAttempts = 0;

        // Connected visuals
        statusText.textContent = "Live";
        statusText.className = "text-emerald-400";
        dot1.className = "animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75";
        dot2.className = "relative inline-flex rounded-full h-2 w-2 bg-emerald-500";
        indicator.className = "flex items-center gap-2 px-3 py-1.5 rounded-full bg-emerald-900/10 border border-emerald-700/30 text-xs font-medium transition-colors shadow-[0_0_10px_rgba(16,185,129,0.2)]";

        showToast("System Connected", "Real-time sync enabled", "success");
    };

    state.ws.onclose = () => {
        console.log('WS Disconnected');
        statusText.textContent = "Offline";
        statusText.className = "text-red-400";
        dot1.className = "hidden";
        dot2.className = "relative inline-flex rounded-full h-2 w-2 bg-red-500";
        indicator.className = "flex items-center gap-2 px-3 py-1.5 rounded-full bg-red-900/10 border border-red-700/30 text-xs font-medium transition-colors";

        // Reconnect logic
        if (state.reconnectAttempts < state.maxReconnectAttempts) {
            const delay = Math.min(1000 * Math.pow(2, state.reconnectAttempts), 10000);
            state.reconnectAttempts++;
            setTimeout(connectWS, delay);
        }
    };

    state.ws.onmessage = (event) => {
        try {
            const msg = JSON.parse(event.data);
            handleWSMessage(msg);
        } catch (e) {
            console.error('WS Parse Error', e);
        }
    };
}

function handleWSMessage(msg) {
    // Log Streaming
    if (msg.type === 'log') {
        if (state.currentJobId && msg.payload.job_id === state.currentJobId) {
            writeLogLine(msg.payload.line);
        }
    }
    // Data Updates
    else if (['job_created', 'job_update', 'job_cancelled', 'job_retried'].includes(msg.type)) {
        fetchJobs();
        if (msg.type === 'job_created') showToast('New Job', `Job ${msg.payload.id.substring(0, 8)} queued`, 'info');
    }
    else if (msg.type === 'agent_update') {
        fetchAgents();
    }
}

function writeLogLine(line) {
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

// --- API Interactions ---
async function fetchAgents() {
    try {
        const res = await fetch(`${API_URL}/api/agents`);
        const data = await res.json();
        // The API returns active agents now with the new backend
        state.agents = data || [];

        // Count active agents (still doing a client-side check just in case backend returns all)
        const activeCount = state.agents.length;
        document.getElementById('agent-count').textContent = activeCount;

        renderAgents();
    } catch (e) {
        console.error("Fetch Agents Error", e);
    }
}

async function fetchJobs() {
    try {
        const res = await fetch(`${API_URL}/api/jobs?limit=20`);
        const data = await res.json();
        state.jobs = data.jobs || data || [];
        renderJobs();
    } catch (e) {
        console.error("Fetch Jobs Error", e);
    }
}

async function createJob(e) {
    e.preventDefault();
    const btn = e.target.querySelector('button[type="submit"]');
    const originalText = btn.innerHTML;

    // Loading state
    btn.disabled = true;
    btn.innerHTML = `<svg class="animate-spin h-4 w-4 text-white" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg> Deploying...`;

    const image = document.getElementById('image').value;
    const command = document.getElementById('command').value;

    try {
        const res = await fetch(`${API_URL}/api/jobs`, {
            method: 'POST',
            body: JSON.stringify({ image, command, env: {} }),
            headers: { 'Content-Type': 'application/json' }
        });

        if (res.ok) {
            const job = await res.json();
            showToast('Job Dispatched', `ID: ${job.id.substring(0, 8)}`, 'success');
            document.getElementById('new-job-modal').classList.add('hidden');
            e.target.reset();
            fetchJobs();
            selectJob(job.id);
        } else {
            throw new Error(await res.text());
        }
    } catch (err) {
        alert('Failed: ' + err.message);
    } finally {
        btn.disabled = false;
        btn.innerHTML = originalText;
    }
}

// --- Rendering ---
function renderAgents() {
    const container = document.getElementById('agents-list');

    if (state.agents.length === 0) {
        container.innerHTML = `<div class="text-xs text-slate-500 text-center py-2 flex flex-col items-center gap-2">
            <span class="opacity-50 text-xl">😴</span>
            Waiting for agents...
        </div>`;
        return;
    }

    container.innerHTML = state.agents.map(agent => {
        const isOnline = true; // Since we're only getting active agents now
        return `
        <div class="group flex items-center justify-between p-2 rounded-lg hover:bg-white/5 transition-colors cursor-default border border-transparent hover:border-slate-800">
            <div class="flex items-center gap-3">
                <div class="w-8 h-8 rounded-md bg-slate-800 flex items-center justify-center font-mono text-xs text-indigo-400 border border-slate-700">
                    <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M5 12h14M12 5l7 7-7 7" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>
                </div>
                <div>
                    <div class="text-xs font-semibold text-slate-300 break-all line-clamp-1 w-32 pb-0.5">
                        ${agent.name || 'Worker Node'}
                    </div>
                    <div class="flex items-center gap-2">
                        <span class="text-[10px] px-1.5 py-0.5 rounded bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 font-medium">Online</span>
                        <span class="text-[10px] text-slate-500">${agent.capacity} cores</span>
                    </div>
                </div>
            </div>
        </div>`;
    }).join('');
}

function renderJobs() {
    const list = document.getElementById('jobs-list');

    if (state.jobs.length === 0) {
        list.innerHTML = `<div class="text-center py-10">
            <div class="text-slate-600 text-sm">No active jobs in queue</div>
            <button onclick="document.getElementById('new-job-modal').classList.remove('hidden')" class="mt-2 text-indigo-400 text-xs hover:underline">Create your first job</button>
        </div>`;
        return;
    }

    list.innerHTML = state.jobs.map(job => {
        const isSelected = state.currentJobId === job.id;
        const statusColors = {
            'pending': 'bg-yellow-500/20 text-yellow-400 border-yellow-500/20',
            'running': 'bg-blue-500/20 text-blue-400 border-blue-500/20 animate-pulse',
            'success': 'bg-emerald-500/20 text-emerald-400 border-emerald-500/20',
            'failure': 'bg-red-500/20 text-red-400 border-red-500/20',
            'cancelled': 'bg-slate-500/20 text-slate-400 border-slate-500/20'
        };
        const statusClass = statusColors[job.status] || statusColors['pending'];
        const activeClass = isSelected
            ? 'bg-indigo-500/10 border-indigo-500/50 shadow-[0_0_15px_rgba(99,102,241,0.1)]'
            : 'bg-slate-900/40 border-slate-800/50 hover:border-slate-700 hover:bg-slate-800/40';

        return `
        <div onclick="selectJob('${job.id}')" 
             class="${activeClass} p-3 rounded-lg border transition-all cursor-pointer group relative overflow-hidden">
            
             ${isSelected ? '<div class="absolute left-0 top-0 bottom-0 w-1 bg-indigo-500"></div>' : ''}
            
            <div class="flex items-center justify-between mb-2 pl-2">
                <span class="font-mono text-[10px] text-slate-500 uppercase tracking-wider">
                    ${job.id.substring(0, 8)}...
                </span>
                <span class="text-[10px] px-2 py-0.5 rounded border ${statusClass} font-medium uppercase tracking-tight">
                    ${job.status}
                </span>
            </div>
            
            <div class="flex items-center gap-3 pl-2">
                <div class="w-8 h-8 shrink-0 rounded bg-slate-950 border border-slate-800 flex items-center justify-center text-lg">
                    ${getJobIcon(job.status)}
                </div>
                <div class="min-w-0">
                    <div class="text-xs text-slate-300 truncate font-medium">${job.config.image || 'unknown-image'}</div>
                    <div class="text-[10px] text-slate-500 truncate mt-0.5 font-mono">${new Date(job.created_at).toLocaleTimeString()}</div>
                </div>
            </div>
        </div>`;
    }).join('');
}

function getJobIcon(status) {
    if (status === 'running') return '⚙️';
    if (status === 'success') return '✅';
    if (status === 'failure') return '❌';
    if (status === 'cancelled') return '🚫';
    return '⏳';
}

async function selectJob(id) {
    if (state.currentJobId === id) return;

    state.currentJobId = id;
    renderJobs(); // Update selection visuals

    const job = state.jobs.find(j => j.id === id);

    // Update Header
    document.getElementById('empty-state').classList.add('hidden');
    document.getElementById('job-header').classList.remove('hidden');
    document.getElementById('current-job-id').textContent = id;
    document.getElementById('current-job-image').textContent = job ? job.config.image : 'Loading...';
    document.getElementById('current-job-status').textContent = job ? job.status : 'FETCHING';

    // Clear Terminal
    state.terminal.clear();
    state.terminal.writeln(`\x1b[90m> Loading context for job ${id}...\x1b[0m`);

    // Fetch History
    try {
        const res = await fetch(`${API_URL}/api/jobs/${id}/logs`);
        if (res.ok) {
            const logs = await res.text();
            state.terminal.clear();
            if (logs.trim()) {
                const lines = logs.split('\n');
                lines.forEach(writeLogLine);
                writeLogLine('\x1b[90m--- End of historical logs ---\x1b[0m');
            } else {
                state.terminal.writeln('\x1b[33mNo logs available yet.\x1b[0m');
            }
        }
    } catch (e) {
        state.terminal.writeln('\x1b[31mError fetching logs.\x1b[0m');
    }
}

// --- Utils ---
function showToast(title, message, type = 'info') {
    const toast = document.getElementById('toast');
    const icon = document.getElementById('toast-icon');
    const titleEl = document.getElementById('toast-title');
    const msgEl = document.getElementById('toast-message');

    // Reset classes
    toast.className = `fixed bottom-6 right-6 z-[100] transform transition-all duration-300 pointer-events-none`;

    // Icons
    icon.innerHTML = type === 'success' ? '✅' : type === 'error' ? '❌' : 'ℹ️';

    titleEl.textContent = title;
    msgEl.textContent = message;

    // Animate In
    setTimeout(() => {
        toast.classList.remove('translate-y-20', 'opacity-0');
    }, 10);

    // Animate Out
    setTimeout(() => {
        toast.classList.add('translate-y-20', 'opacity-0');
    }, 4000);
}

// --- Initialization ---
document.getElementById('job-form').addEventListener('submit', createJob);
initTerminal();
connectWS();
fetchAgents();
fetchJobs();
setInterval(() => {
    fetchAgents();
    fetchJobs();
}, 5000);
