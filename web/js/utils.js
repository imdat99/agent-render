export const API_URL = '';
export const WS_URL = `ws://${window.location.host}/api/ws`;
export const MQTT_URL = 'ws://broker.mqtt-dashboard.com:8000/mqtt';

export function showToast(title, message, type = 'info') {
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
