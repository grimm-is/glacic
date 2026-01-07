<script lang="ts">
    import { t } from "svelte-i18n";
    import { Button, Card } from "$lib/components";
    import { currentPage } from "$lib/stores/app";

    function goHome() {
        currentPage.set("dashboard");
    }
</script>

<div class="not-found-page">
    <div class="content">
        <div class="glitch-wrapper">
            <div class="code">404</div>
        </div>

        <h1>{$t("common.page_not_found") || "Packet Dropped"}</h1>

        <p class="message">
            The request to <span class="path">#{$currentPage}</span> was dropped
            by the default policy.
            <br />
            This route does not exist in the routing table.
        </p>

        <div class="actions">
            <Button onclick={goHome} variant="default">
                Return to Dashboard
            </Button>
        </div>
    </div>
</div>

<style>
    .not-found-page {
        display: flex;
        align-items: center;
        justify-content: center;
        min-height: 60vh;
        text-align: center;
    }

    .content {
        max-width: 500px;
        padding: var(--space-6);
    }

    .code {
        font-size: 8rem;
        font-weight: 800;
        line-height: 1;
        margin-bottom: var(--space-4);
        background: linear-gradient(
            135deg,
            var(--color-primary) 0%,
            var(--color-secondary) 100%
        );
        background-clip: text;
        -webkit-background-clip: text;
        -webkit-text-fill-color: transparent;
        opacity: 0.8;
    }

    h1 {
        font-size: var(--text-2xl);
        margin-bottom: var(--space-4);
        color: var(--color-foreground);
    }

    .message {
        color: var(--color-muted);
        line-height: 1.6;
        margin-bottom: var(--space-8);
    }

    .path {
        font-family: monospace;
        background: var(--color-surface);
        padding: var(--space-1) var(--space-2);
        border-radius: var(--radius-sm);
        color: var(--color-primary);
    }
</style>
