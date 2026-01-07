<script lang="ts">
    /**
     * NotificationBell Component
     * Shows bell icon with unread count badge and dropdown history
     */

    import {
        notifications,
        unreadCount,
        markAllRead,
        clearAll,
        type Notification,
    } from "$lib/stores/notifications";
    import { t } from "svelte-i18n";

    let isOpen = $state(false);
    let mounted = $state(false);

    function toggle() {
        isOpen = !isOpen;
        if (isOpen) {
            markAllRead();
        }
    }

    function formatTime(timestamp: number): string {
        const diff = Date.now() - timestamp;
        if (diff < 60000) return $t("time_ago.just_now");
        if (diff < 3600000)
            return $t("time_ago.minutes", {
                values: { n: Math.floor(diff / 60000) },
            });
        if (diff < 86400000)
            return $t("time_ago.hours", {
                values: { n: Math.floor(diff / 3600000) },
            });
        return new Date(timestamp).toLocaleDateString();
    }

    function getTypeClass(type: string): string {
        switch (type) {
            case "success":
                return "notif-success";
            case "error":
                return "notif-error";
            case "warning":
                return "notif-warning";
            default:
                return "notif-info";
        }
    }

    function getIcon(type: string): string {
        switch (type) {
            case "success":
                return "✓";
            case "error":
                return "✕";
            case "warning":
                return "⚠";
            default:
                return "ℹ";
        }
    }

    // Close dropdown when clicking outside
    function handleClickOutside(event: MouseEvent) {
        const target = event.target as HTMLElement;
        if (!target.closest(".notification-bell")) {
            isOpen = false;
        }
    }

    $effect(() => {
        if (typeof window !== "undefined") {
            mounted = true;
            window.addEventListener("click", handleClickOutside);
            return () =>
                window.removeEventListener("click", handleClickOutside);
        }
    });
</script>

<div class="notification-bell">
    <button
        class="bell-button"
        onclick={toggle}
        aria-label={$t("notifications.title")}
    >
        <svg
            xmlns="http://www.w3.org/2000/svg"
            width="20"
            height="20"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
        >
            <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
            <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
        </svg>
        {#if $unreadCount > 0}
            <span class="badge">{$unreadCount > 9 ? "9+" : $unreadCount}</span>
        {/if}
    </button>

    {#if isOpen}
        <div class="dropdown">
            <div class="dropdown-header">
                <span>{$t("notifications.title")}</span>
                {#if $notifications.length > 0}
                    <button class="clear-btn" onclick={clearAll}
                        >{$t("notifications.clear_all")}</button
                    >
                {/if}
            </div>
            <div class="dropdown-body">
                {#if $notifications.length === 0}
                    <div class="empty-state">{$t("notifications.empty")}</div>
                {:else}
                    {#each $notifications as notif (notif.id)}
                        <div class="notif-item {getTypeClass(notif.type)}">
                            <span class="notif-icon">{getIcon(notif.type)}</span
                            >
                            <div class="notif-content">
                                <strong class="notif-title"
                                    >{notif.title}</strong
                                >
                                <p class="notif-message">{notif.message}</p>
                                <span class="notif-time"
                                    >{formatTime(notif.time)}</span
                                >
                            </div>
                        </div>
                    {/each}
                {/if}
            </div>
        </div>
    {/if}
</div>

<style>
    .notification-bell {
        position: relative;
    }

    .bell-button {
        background: none;
        border: none;
        padding: var(--space-2);
        cursor: pointer;
        color: var(--color-muted);
        position: relative;
        display: flex;
        align-items: center;
        justify-content: center;
    }

    .bell-button:hover {
        color: var(--color-foreground);
    }

    .badge {
        position: absolute;
        top: 0;
        right: 0;
        background: var(--color-destructive);
        color: white;
        font-size: 10px;
        font-weight: 600;
        min-width: 16px;
        height: 16px;
        border-radius: 8px;
        display: flex;
        align-items: center;
        justify-content: center;
        padding: 0 4px;
    }

    .dropdown {
        position: absolute;
        top: 100%;
        right: 0;
        width: 360px;
        max-height: 400px;
        background: var(--color-background);
        border: 1px solid var(--color-border);
        border-radius: var(--radius-md);
        box-shadow: var(--shadow-lg);
        z-index: 9999;
        overflow: hidden;
    }

    .dropdown-header {
        display: flex;
        align-items: center;
        justify-content: space-between;
        padding: var(--space-3) var(--space-4);
        border-bottom: 1px solid var(--color-border);
        font-weight: 600;
        color: var(--color-foreground);
    }

    .clear-btn {
        background: none;
        border: none;
        color: var(--color-primary);
        font-size: var(--text-sm);
        cursor: pointer;
    }

    .clear-btn:hover {
        text-decoration: underline;
    }

    .dropdown-body {
        max-height: 340px;
        overflow-y: auto;
    }

    .empty-state {
        padding: var(--space-6);
        text-align: center;
        color: var(--color-muted);
    }

    .notif-item {
        display: flex;
        gap: var(--space-3);
        padding: var(--space-3) var(--space-4);
        border-bottom: 1px solid var(--color-border);
    }

    .notif-item:last-child {
        border-bottom: none;
    }

    .notif-icon {
        flex-shrink: 0;
        width: 24px;
        height: 24px;
        display: flex;
        align-items: center;
        justify-content: center;
        border-radius: 50%;
        font-size: 12px;
    }

    .notif-success .notif-icon {
        color: var(--color-success);
    }
    .notif-error .notif-icon {
        color: var(--color-destructive);
    }
    .notif-warning .notif-icon {
        color: #f59e0b;
    }
    .notif-info .notif-icon {
        color: var(--color-primary);
    }

    .notif-content {
        flex: 1;
        min-width: 0;
    }

    .notif-title {
        display: block;
        font-size: var(--text-sm);
        color: var(--color-foreground);
        margin-bottom: 2px;
    }

    .notif-message {
        font-size: var(--text-sm);
        color: var(--color-muted);
        margin: 0;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .notif-time {
        font-size: var(--text-xs);
        color: var(--color-muted);
    }
</style>
