import { state } from './state.js';
import { fetchAgents, fetchJobs, createJob, fetchLogs, cancelJob } from './api.js';

export function renderAgents() {
    const container = document.getElementById('agents-list');

    if (state.agents.length === 0) {
        container.innerHTML = `<div class="text-xs text-slate-500 text-center py-2 flex flex-col items-center gap-2">
            <span class="opacity-50 text-xl">😴</span>
            Waiting for agents...
        </div>`;
        return;
    }

    container.innerHTML = state.agents.map(agent => {
        return `
        <div data-agent-id="${agent.id}" class="agent-item group flex items-center justify-between p-2 rounded-lg hover:bg-white/5 transition-colors cursor-pointer border border-transparent hover:border-slate-800">
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
                         <span class="text-[10px] text-slate-500">${agent.cpu ? agent.cpu.toFixed(1) + '%' : ''}</span>
                    </div>
                </div>
            </div>
            <div class="flex items-center gap-2">
                ${agent.active_job_count > 0
                ? `<span class="text-[10px] bg-indigo-500/20 text-indigo-400 px-1.5 py-0.5 rounded border border-indigo-500/20">${agent.active_job_count} active</span>`
                : ''}
            </div>
        </div>`;
    }).join('');

    // Add click listeners
    container.querySelectorAll('.agent-item').forEach(el => {
        el.addEventListener('click', () => {
            showAgentDetails(el.dataset.agentId);
        });
    });

    // Update count
    const countEl = document.getElementById('agent-count');
    if (countEl) countEl.textContent = state.agents.length;
}

async function showAgentDetails(agentId) {
    const agent = state.agents.find(a => a.id == agentId);
    if (!agent) return;

    const modal = document.getElementById('agent-modal');
    modal.classList.remove('hidden');

    // Populate Info
    document.getElementById('agent-modal-title').textContent = agent.name || `Agent #${agent.id}`;
    document.getElementById('agent-modal-status').textContent = 'Online'; // Assuming online if in list
    document.getElementById('agent-modal-platform').textContent = agent.platform || 'linux/amd64';
    document.getElementById('agent-modal-backend').textContent = agent.backend || 'docker';
    document.getElementById('agent-modal-cpu').textContent = (agent.cpu ? agent.cpu.toFixed(1) : '0') + '% CPU';
    document.getElementById('agent-modal-capacity').textContent = agent.capacity + ' cores';

    // Active jobs count
    const jobCount = agent.active_job_count || 0;
    document.getElementById('agent-modal-jobs-count').textContent = jobCount;

    // Fetch and render jobs
    const jobsList = document.getElementById('agent-modal-jobs-list');
    jobsList.innerHTML = `<div class="text-center py-8 text-slate-500 text-sm animate-pulse">Loading active jobs...</div>`;

    try {
        const jobs = await fetchJobs(0, 50, agent.id); // Fetch up to 50 jobs for this agent
        renderAgentJobs(jobs, jobsList);
    } catch (e) {
        console.error("Failed to fetch agent jobs", e);
        jobsList.innerHTML = `<div class="text-center py-4 text-red-400 text-sm">Failed to load jobs</div>`;
    }
}

function renderAgentJobs(jobs, container) {
    if (!jobs || jobs.length === 0) {
        container.innerHTML = `<div class="text-center py-8 text-slate-500 text-sm italic">No jobs found for this agent</div>`;
        return;
    }

    container.innerHTML = jobs.map(job => {
        const isRunning = job.status === 'running';
        const colorClass = isRunning ? 'text-blue-400 border-blue-500/20 bg-blue-500/10' :
            (job.status === 'success' ? 'text-emerald-400 border-emerald-500/20 bg-emerald-500/10' : 'text-slate-400 border-slate-700/50 bg-slate-800/30');

        return `
        <div class="flex items-center justify-between p-3 rounded-lg border border-slate-700/50 bg-slate-800/20 hover:bg-slate-800/40 transition-colors">
            <div class="flex items-center gap-3">
                <div class="w-2 h-2 rounded-full ${isRunning ? 'bg-blue-500 animate-pulse' : (job.status === 'success' ? 'bg-emerald-500' : 'bg-slate-500')}"></div>
                <div>
                    <div class="text-xs font-mono text-slate-300">${job.id.substring(0, 8)}</div>
                    <div class="text-[10px] text-slate-500 mt-0.5">${new Date(job.created_at).toLocaleString()}</div>
                </div>
            </div>
            <div class="flex items-center gap-2">
                <span class="text-[10px] px-2 py-0.5 rounded border font-medium uppercase tracking-wider ${colorClass}">
                    ${job.status}
                </span>
                <div class="text-xs text-slate-400 w-24 truncate text-right">${job.config.image || 'unknown'}</div>
            </div>
        </div>`;
    }).join('');
}

// Modal Event Listeners
document.addEventListener('DOMContentLoaded', () => {
    const closeBtn = document.getElementById('close-agent-modal');
    const modal = document.getElementById('agent-modal');

    if (closeBtn && modal) {
        closeBtn.addEventListener('click', () => {
            modal.classList.add('hidden');
        });

        // Close on backdrop click
        modal.addEventListener('click', (e) => {
            if (e.target === modal) {
                modal.classList.add('hidden');
            }
        });
    }
});

export function renderJobs(onSelectJob) {
    const list = document.getElementById('jobs-list');

    // Sort jobs: pending first, then running, then others by date desc
    const sortedJobs = [...state.jobs].sort((a, b) => {
        const statusOrder = { 'running': 1, 'pending': 2, 'submitted': 3 };
        const orderA = statusOrder[a.status] || 10;
        const orderB = statusOrder[b.status] || 10;
        if (orderA !== orderB) return orderA - orderB;
        return new Date(b.created_at) - new Date(a.created_at);
    });

    if (sortedJobs.length === 0) {
        list.innerHTML = `<div class="text-center py-10">
            <div class="text-slate-600 text-sm">No active jobs in queue</div>
            <button onclick="document.getElementById('new-job-modal').classList.remove('hidden')" class="mt-2 text-indigo-400 text-xs hover:underline">Create your first job</button>
        </div>`;
        return;
    }

    list.innerHTML = sortedJobs.map(job => {
        const isSelected = state.currentJobId === job.id;
        const statusColors = {
            'pending': 'bg-yellow-500/20 text-yellow-400 border-yellow-500/20',
            'submitted': 'bg-purple-500/20 text-purple-400 border-purple-500/20',
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
        <div data-job-id="${job.id}" 
             class="job-item ${activeClass} p-3 rounded-lg border transition-all cursor-pointer group relative overflow-hidden">
            
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

    // Add event listeners
    list.querySelectorAll('.job-item').forEach(el => {
        el.addEventListener('click', () => {
            if (onSelectJob) onSelectJob(el.dataset.jobId);
        });
    });
}

function getJobIcon(status) {
    if (status === 'running') return '⚙️';
    if (status === 'success') return '✅';
    if (status === 'failure') return '❌';
    if (status === 'cancelled') return '🚫';
    return '⏳';
}

export function updateProgress(progress) {
    const text = document.getElementById('current-job-progress-text');
    const container = document.getElementById('job-progress-container');

    if (text && container) {
        container.classList.remove('hidden');
        text.textContent = typeof progress === 'number' ? progress.toFixed(1) + '%' : progress;
        document.getElementById('current-job-progress').style.width = typeof progress === 'number' ? `${progress}%` : '0%';
    }
}

function formatDuration(start, end) {
    if (!start) return '-';
    const startTime = new Date(start);
    const endTime = end ? new Date(end) : new Date();
    const diff = Math.floor((endTime - startTime) / 1000);

    if (diff < 60) return `${diff}s`;
    const m = Math.floor(diff / 60);
    const s = diff % 60;
    if (m < 60) return `${m}m ${s}s`;
    const h = Math.floor(m / 60);
    const m2 = m % 60;
    return `${h}h ${m2}m`;
}

export function updateJobDetails(job) {
    if (!job) return;
    document.getElementById('empty-state').classList.add('hidden');
    document.getElementById('job-header').classList.remove('hidden');

    // ID
    document.getElementById('current-job-id').textContent = job.id.substring(0, 8);

    // Status Badge
    const statusEl = document.getElementById('current-job-status');
    statusEl.textContent = job.status;

    const statusColors = {
        'pending': 'text-yellow-400 bg-yellow-400/10 border-yellow-400/20 shadow-[0_0_10px_rgba(250,204,21,0.1)]',
        'submitted': 'text-purple-400 bg-purple-400/10 border-purple-400/20 shadow-[0_0_10px_rgba(192,132,252,0.1)]',
        'running': 'text-blue-400 bg-blue-400/10 border-blue-400/20 animate-pulse shadow-[0_0_10px_rgba(96,165,250,0.1)]',
        'success': 'text-emerald-400 bg-emerald-400/10 border-emerald-400/20 shadow-[0_0_10px_rgba(52,211,153,0.1)]',
        'failure': 'text-red-400 bg-red-400/10 border-red-400/20 shadow-[0_0_10px_rgba(248,113,113,0.1)]',
        'cancelled': 'text-slate-400 bg-slate-400/10 border-slate-400/20'
    };
    statusEl.className = `px-2 py-0.5 rounded text-[10px] uppercase font-bold tracking-wide border ${statusColors[job.status] || statusColors['pending']}`;

    // Config & Image
    const config = job.config || {};
    document.getElementById('current-job-image').textContent = config.image || 'unknown';

    // Agent
    const agent = state.agents.find(a => a.id == job.agent_id);
    const agentName = agent ? (agent.name || `Node #${agent.id}`) : (job.agent_id ? `Node #${job.agent_id}` : 'Unassigned');
    document.getElementById('current-job-agent').textContent = agentName;

    // Duration
    const isFinished = ['success', 'failure', 'cancelled'].includes(job.status);
    // If running, we might want to update duration periodically? 
    // For now, static update on select/refresh.
    document.getElementById('current-job-duration').textContent = formatDuration(job.created_at, isFinished ? job.updated_at : null);

    // Command
    const cmd = job.config.commands ? job.config.commands.join(' ') : '';
    document.getElementById('current-job-command').textContent = cmd || '-';

    // Cancel Button
    const cancelBtn = document.getElementById('cancel-job-btn');
    if (job.status === 'pending' || job.status === 'running') {
        cancelBtn.classList.remove('hidden');
        cancelBtn.onclick = async () => {
            if (confirm('Are you sure you want to cancel this job?')) {
                try {
                    await cancelJob(job.id);
                    // Optimistic update or wait for refresh
                    job.status = 'cancelled';
                    updateJobDetails(job);
                } catch (e) {
                    // Error handled in api.js (toast)
                }
            }
        };
    } else {
        cancelBtn.classList.add('hidden');
    }

    // Progress Bar
    const progressContainer = document.getElementById('job-progress-container');
    if (job.status === 'running') {
        progressContainer.classList.remove('hidden');
        updateProgress(job.progress || 0);
    } else {
        progressContainer.classList.add('hidden');
    }
}
