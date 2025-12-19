/**
 * WebSocket Store
 * Real-time updates via topic-based pub/sub
 */

import { writable, get } from 'svelte/store';

// Connection state
export const wsConnected = writable(false);
export const wsSupported = writable(true);

// Topic data stores
export const wsStatus = writable<any>(null);
export const wsStats = writable<any>(null);
export const wsLogs = writable<any[]>([]);
export const wsNotifications = writable<any[]>([]);

// Internal state
let ws: WebSocket | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
let reconnectAttempts = 0;
const MAX_RECONNECT_ATTEMPTS = 5;
const RECONNECT_DELAY = 3000;

/**
 * Connect to WebSocket and subscribe to topics
 */
export function connectWebSocket(topics: string[] = ['status', 'logs', 'stats', 'notification']) {
    if (typeof window === 'undefined') return;

    // Don't reconnect if WS is not supported
    if (!get(wsSupported)) return;

    // Close existing connection
    if (ws) {
        ws.close();
        ws = null;
    }

    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${proto}//${window.location.host}/api/ws/status`;

    try {
        ws = new WebSocket(wsUrl);
    } catch (e) {
        console.error('WebSocket creation failed:', e);
        wsSupported.set(false);
        return;
    }

    ws.onopen = () => {
        console.log('WebSocket connected');
        wsConnected.set(true);
        reconnectAttempts = 0;

        // Subscribe to topics
        if (ws && ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({
                action: 'subscribe',
                topics: topics,
            }));
        }
    };

    ws.onmessage = (event) => {
        try {
            const msg = JSON.parse(event.data);

            if (msg.topic) {
                switch (msg.topic) {
                    case 'status':
                        wsStatus.set(msg.data);
                        break;
                    case 'stats':
                        wsStats.set(msg.data);
                        break;
                    case 'logs':
                        // Append new logs (keep last 500)
                        wsLogs.update(logs => {
                            const newLogs = Array.isArray(msg.data) ? msg.data : [msg.data];
                            return [...newLogs, ...logs].slice(0, 500);
                        });
                        break;
                    case 'notification':
                        // Add notification and dispatch event
                        wsNotifications.update(notifs => {
                            const newNotif = msg.data;
                            // Dispatch custom event for toast notifications
                            if (typeof window !== 'undefined') {
                                window.dispatchEvent(new CustomEvent('ws-notification', { detail: newNotif }));
                            }
                            return [newNotif, ...notifs].slice(0, 50);
                        });
                        break;
                    case 'config':
                        // Dispatch event for config updates
                        if (typeof window !== 'undefined') {
                            window.dispatchEvent(new CustomEvent('ws-config', { detail: msg.data }));
                        }
                        break;
                    case 'leases':
                        // Dispatch event for lease updates
                        if (typeof window !== 'undefined') {
                            window.dispatchEvent(new CustomEvent('ws-leases', { detail: msg.data }));
                        }
                        break;
                }
            }
        } catch (e) {
            console.error('WS message parse error:', e);
        }
    };

    ws.onclose = (event) => {
        console.log('WebSocket closed:', event.code, event.reason);
        wsConnected.set(false);
        ws = null;

        // Attempt reconnect unless it was a clean close
        if (event.code !== 1000 && reconnectAttempts < MAX_RECONNECT_ATTEMPTS) {
            reconnectAttempts++;
            console.log(`Reconnecting in ${RECONNECT_DELAY}ms (attempt ${reconnectAttempts}/${MAX_RECONNECT_ATTEMPTS})`);
            reconnectTimer = setTimeout(() => connectWebSocket(topics), RECONNECT_DELAY);
        } else if (reconnectAttempts >= MAX_RECONNECT_ATTEMPTS) {
            console.warn('Max reconnect attempts reached, WebSocket disabled');
            wsSupported.set(false);
        }
    };

    ws.onerror = (error) => {
        console.error('WebSocket error:', error);
        // Let onclose handle reconnection
    };
}

/**
 * Disconnect WebSocket
 */
export function disconnectWebSocket() {
    if (reconnectTimer) {
        clearTimeout(reconnectTimer);
        reconnectTimer = null;
    }

    if (ws) {
        ws.close(1000, 'User disconnect');
        ws = null;
    }

    wsConnected.set(false);
}

/**
 * Subscribe to additional topics
 */
export function subscribe(topics: string[]) {
    if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({
            action: 'subscribe',
            topics: topics,
        }));
    }
}

/**
 * Unsubscribe from topics
 */
export function unsubscribe(topics: string[]) {
    if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({
            action: 'unsubscribe',
            topics: topics,
        }));
    }
}

/**
 * Check if WebSocket is connected
 */
export function isConnected(): boolean {
    return ws !== null && ws.readyState === WebSocket.OPEN;
}
