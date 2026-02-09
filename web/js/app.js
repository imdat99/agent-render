import { state } from './state.js';
import * as api from './api.js';
import * as ui from './ui.js';
import * as terminal from './terminal.js';
import * as ws from './ws.js';
import { showToast } from './utils.js';

// --- Initialization ---
document.addEventListener('DOMContentLoaded', init);

async function init() {
    // 1. Helper function for selectors
    window.$ = (selector) => document.querySelector(selector);

    // 2. Init Terminal
    terminal.initTerminal();

    // 3. Connect WebSocket
    ws.connectWS();

    // 4. Bind UI Events
    bindDictionaryEvents();

    // 5. Initial Data Load
    await reloadData();
}

async function reloadData() {
    await Promise.all([updateAgents(), updateJobs()]);
}

async function updateAgents() {
    state.agents = await api.fetchAgents();
    ui.renderAgents();
}

async function updateJobs() {
    state.jobs = await api.fetchJobs();
    ui.renderJobs(handleJobSelect);
}

// --- Event Handlers ---

function bindDictionaryEvents() {
    // Job Creation Form
    const jobForm = document.getElementById('job-form');
    if (jobForm) {
        jobForm.addEventListener('submit', handleCreateJob);
    }

    // WebSocket Events (dispatched from ws.js)
    window.addEventListener('ws:connected', reloadData);

    window.addEventListener('ws:log', (e) => {
        const payload = e.detail;
        if (state.currentJobId && payload.job_id === state.currentJobId) {
            terminal.writeLogLine(payload.line);
            if (payload.progress !== undefined && payload.progress >= 0) {
                ui.updateProgress(payload.progress);
            }
        }
    });

    window.addEventListener('ws:job-update', async (e) => {
        const msg = e.detail;
        await updateJobs();
        if (msg.type === 'job_created') {
            showToast('New Job', `Job ${msg.payload.id.substring(0, 8)} queued`, 'info');
        }
    });

    window.addEventListener('ws:agent-update', async () => {
        await updateAgents();
    });

    window.addEventListener('ws:resource-update', (e) => {
        const { agent_id, cpu, ram } = e.detail;
        const agentIndex = state.agents.findIndex(a => a.id === agent_id);
        if (agentIndex !== -1) {
            state.agents[agentIndex].cpu = cpu;
            state.agents[agentIndex].ram = ram;
            ui.renderAgents();
        } else {
            // New agent appeared?
            updateAgents();
        }
    });
}

async function handleCreateJob(e) {
    e.preventDefault();
    const btn = e.target.querySelector('button[type="submit"]');
    const originalText = btn.innerHTML;

    // Loading state
    btn.disabled = true;
    btn.innerHTML = `<svg class="animate-spin h-4 w-4 text-white" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg> Deploying...`;

    const image = document.getElementById('image').value;
    const command = document.getElementById('command').value;

    try {
        const job = await api.createJob(image, command);
        if (job) {
            document.getElementById('new-job-modal').classList.add('hidden');
            e.target.reset();
            await updateJobs();
            handleJobSelect(job.id);
        }
    } catch (err) {
        // Error handled in api.createJob (alert)
    } finally {
        btn.disabled = false;
        btn.innerHTML = originalText;
    }
}

async function handleJobSelect(id) {
    if (state.currentJobId === id) return;

    state.currentJobId = id;
    ui.renderJobs(handleJobSelect); // Re-render to update active state

    const job = state.jobs.find(j => j.id === id);
    ui.updateJobDetails(job);

    // Clear Terminal
    terminal.clearTerminal();
    terminal.writeLogLine(`\x1b[90m> Loading context for job ${id}...\x1b[0m`);

    // Fetch History
    const logs = await api.fetchLogs(id);
    terminal.clearTerminal();
    if (logs && logs.trim()) {
        const lines = logs.split('\n');
        lines.forEach(terminal.writeLogLine);
        terminal.writeLogLine('\x1b[90m--- End of historical logs ---\x1b[0m');
    } else {
        terminal.writeLogLine('\x1b[33mNo logs available yet.\x1b[0m');
    }
}
