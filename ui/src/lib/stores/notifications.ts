import { writable, derived } from 'svelte/store';

export interface Notification {
    id: number;
    type: 'success' | 'error' | 'warning' | 'info';
    title: string;
    message: string;
    time: number;
    read: boolean;
}

// Store for all notifications (history)
const notificationHistory = writable<Notification[]>([]);
let nextId = 0;

// Derived store for unread count
export const unreadCount = derived(notificationHistory, ($notifications) =>
    $notifications.filter(n => !n.read).length
);

// Export the history store
export const notifications = notificationHistory;

// Add a notification
export function addNotification(type: Notification['type'], title: string, message: string) {
    const notification: Notification = {
        id: ++nextId,
        type,
        title,
        message,
        time: Date.now(),
        read: false,
    };

    notificationHistory.update(list => {
        // Keep only last 50 notifications
        const newList = [notification, ...list];
        if (newList.length > 50) {
            newList.pop();
        }
        return newList;
    });

    return notification;
}

// Mark all as read
export function markAllRead() {
    notificationHistory.update(list =>
        list.map(n => ({ ...n, read: true }))
    );
}

// Clear all notifications
export function clearAll() {
    notificationHistory.set([]);
}

// Mark specific notification as read
export function markRead(id: number) {
    notificationHistory.update(list =>
        list.map(n => n.id === id ? { ...n, read: true } : n)
    );
}
