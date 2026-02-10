import { API_URL, showToast } from './utils.js';

export async function fetchAgents() {
    try {
        const res = await fetch(`${API_URL}/api/agents`);
        const data = await res.json();
        return data || [];
    } catch (e) {
        console.error("Fetch Agents Error", e);
        return [];
    }
}

export async function fetchJobs(offset = 0, limit = 20, agentId) {
    let url = `${API_URL}/api/jobs?offset=${offset}&limit=${limit}`;
    if (agentId) {
        url += `&agent_id=${agentId}`;
    }
    try {
        const res = await fetch(url);
        const data = await res.json();
        let jobs = data.jobs || data || [];

        // Parse config if it's a string
        jobs = jobs.map(job => {
            if (job.config && typeof job.config === 'string') {
                try {
                    job.config = JSON.parse(job.config);
                } catch (e) {
                    console.error("Failed to parse config for job", job.id);
                    job.config = {};
                }
            }
            return job;
        });

        return jobs;
    } catch (e) {
        console.error("Fetch Jobs Error", e);
        return [];
    }
}

export async function createJob(image, command) {
    try {
        const res = await fetch(`${API_URL}/api/jobs`, {
            method: 'POST',
            body: JSON.stringify({ image, command, env: {} }),
            headers: { 'Content-Type': 'application/json' }
        });

        if (res.ok) {
            const job = await res.json();
            showToast('Job Dispatched', `ID: ${job.id.substring(0, 8)}`, 'success');
            return job;
        } else {
            throw new Error(await res.text());
        }
    } catch (err) {
        alert('Failed: ' + err.message);
        throw err;
    }
}


export async function fetchLogs(jobId) {
    try {
        const res = await fetch(`${API_URL}/api/jobs/${jobId}/logs`);
        if (res.ok) {
            return await res.text();
        }
        return null;
    } catch (e) {
        console.error('Error fetching logs', e);
        return null;
    }
}

export async function cancelJob(jobId) {
    try {
        const res = await fetch(`${API_URL}/api/jobs/${jobId}/cancel`, {
            method: 'POST'
        });
        if (res.ok) {
            showToast('Job Cancelled', `ID: ${jobId.substring(0, 8)}`, 'success');
            return await res.json();
        } else {
            const err = await res.text();
            showToast('Failed to Cancel', err, 'error');
            throw new Error(err);
        }
    } catch (e) {
        console.error("Cancel Job Error", e);
        throw e;
    }
}

export async function restartAgent(agentId) {
    try {
        const res = await fetch(`${API_URL}/api/agents/${agentId}/restart`, {
            method: 'POST'
        });
        if (res.ok) {
            showToast('Agent Restart Sent', `ID: ${agentId}`, 'success');
            return await res.json();
        } else {
            throw new Error(await res.text());
        }
    } catch (e) {
        console.error("Restart Agent Error", e);
        showToast('Failed to Restart', e.message, 'error');
        throw e;
    }
}

export async function updateAgent(agentId) {
    try {
        const res = await fetch(`${API_URL}/api/agents/${agentId}/update`, {
            method: 'POST'
        });
        if (res.ok) {
            showToast('Agent Update Sent', `ID: ${agentId}`, 'success');
            return await res.json();
        } else {
            throw new Error(await res.text());
        }
    } catch (e) {
        console.error("Update Agent Error", e);
        showToast('Failed to Update', e.message, 'error');
        throw e;
    }
}
