import { state } from './state.js';
import { MQTT_URL, showToast } from './utils.js';

export function connectWS() {
    const indicator = document.getElementById('ws-indicator');
    const statusText = document.getElementById('ws-status-text');
    const dot1 = indicator.querySelector('div span:nth-child(1)');
    const dot2 = indicator.querySelector('div span:nth-child(2)');

    // Set connecting state
    statusText.textContent = "MQTT Connecting...";
    statusText.className = "text-yellow-400";
    dot2.className = "relative inline-flex rounded-full h-2 w-2 bg-yellow-500";
    indicator.className = "flex items-center gap-2 px-3 py-1.5 rounded-full bg-yellow-900/10 border border-yellow-700/30 text-xs font-medium transition-colors";

    const clientId = 'picpic-ui-' + Math.random().toString(16).substring(2, 10);

    // Connect to MQTT broker via WebSockets
    state.mqtt = mqtt.connect(MQTT_URL, {
        clientId: clientId,
        clean: true,
        connectTimeout: 4000,
        reconnectPeriod: 1000,
    });

    state.mqtt.on('connect', () => {
        console.log('MQTT Connected as ' + clientId);
        state.reconnectAttempts = 0;

        // Subscribe to topics
        state.mqtt.subscribe('picpic/events', (err) => {
            if (err) console.error('MQTT subscribe error (events)', err);
        });
        state.mqtt.subscribe('picpic/logs/#', (err) => {
            if (err) console.error('MQTT subscribe error (logs)', err);
        });

        // Connected visuals
        statusText.textContent = "MQTT Live";
        statusText.className = "text-emerald-400";
        dot1.className = "animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75";
        dot2.className = "relative inline-flex rounded-full h-2 w-2 bg-emerald-500";
        indicator.className = "flex items-center gap-2 px-3 py-1.5 rounded-full bg-emerald-900/10 border border-emerald-700/30 text-xs font-medium transition-colors shadow-[0_0_10px_rgba(16,185,129,0.2)]";

        showToast("System Connected (MQTT)", "Using public broker for sync", "success");

        // Trigger initial fetch on connect to ensure sync
        window.dispatchEvent(new CustomEvent('ws:connected'));
    });

    state.mqtt.on('error', (err) => {
        console.error('MQTT Connection Error', err);
        statusText.textContent = "MQTT Error";
        statusText.className = "text-red-400";
    });

    state.mqtt.on('close', () => {
        console.log('MQTT Disconnected');
        statusText.textContent = "MQTT Offline";
        statusText.className = "text-slate-500";
        dot1.className = "hidden";
        dot2.className = "relative inline-flex rounded-full h-2 w-2 bg-slate-500";
        indicator.className = "flex items-center gap-2 px-3 py-1.5 rounded-full bg-slate-900/10 border border-slate-700/30 text-xs font-medium transition-colors";
    });

    state.mqtt.on('message', (topic, message) => {
        try {
            const msg = JSON.parse(message.toString());

            if (topic.startsWith('picpic/logs/')) {
                // Log entries are published directly as JSON in the logs topic
                window.dispatchEvent(new CustomEvent('ws:log', { detail: msg }));
            } else if (topic === 'picpic/events') {
                // Events follow {type: ..., payload: ...} format
                handleWSMessage(msg);
            }
        } catch (e) {
            console.error('MQTT Message Parse Error', e);
        }
    });
}

function handleWSMessage(msg) {
    if (msg.type === 'job_update' || msg.type === 'job_created' || msg.type === 'job_cancelled' || msg.type === 'job_retried') {
        window.dispatchEvent(new CustomEvent('ws:job-update', { detail: msg }));
    }
    else if (msg.type === 'agent_update') {
        window.dispatchEvent(new CustomEvent('ws:agent-update', { detail: msg }));
    }
    else if (msg.type === 'resource_update') {
        window.dispatchEvent(new CustomEvent('ws:resource-update', { detail: msg.payload }));
    }
}
