<script lang="ts">
    /**
     * NetworkInput Component
     * Unified input for IP, CIDR, Hostname, IPSet, Tag
     * With optional Port field
     */
    import {
        getAddressType,
        isValidPort,
        type AddressType,
    } from "$lib/utils/validation";
    import { createEventDispatcher } from "svelte";
    import { fade } from "svelte/transition";

    const dispatch = createEventDispatcher();

    interface Props {
        id?: string;
        value?: string;
        port?: string | number | null;
        label?: string;
        placeholder?: string;
        disabled?: boolean;
        required?: boolean;
        class?: string;
        suggestions?: string[]; // List of available IPSets/Tags/Hosts

        // Feature flags
        allowIPv4?: boolean;
        allowIPv6?: boolean;
        allowCIDR?: boolean;
        allowHostname?: boolean; // If false, hostname shows warning/error
        allowPort?: boolean; // If true, shows port input
    }

    let {
        id = "network-input",
        value = $bindable(""),
        port = $bindable(null),
        label = "",
        placeholder = "IP, Hostname, or IPSet",
        disabled = false,
        required = false,
        class: className = "",
        suggestions = [],
        allowIPv4 = true,
        allowIPv6 = true,
        allowCIDR = true,
        allowHostname = true,
        allowPort = false,
    }: Props = $props();

    // State
    let focused = $state(false);
    let showSuggestions = $state(false);
    let filteredSuggestions = $state<string[]>([]);
    let inputElement: HTMLInputElement;

    // Derived
    let type = $derived(getAddressType(value));
    let isValid = $derived(validateType(type));
    let portError = $derived(
        allowPort && port && !isValidPort(port) ? "Invalid port" : null,
    );
    let error = $derived(
        !value && required
            ? "Required"
            : !isValid && value
              ? getErrorMessage(type)
              : portError,
    );

    function validateType(t: AddressType): boolean {
        if (!value) return true; // Let required handle empty
        switch (t) {
            case "ipv4":
                return allowIPv4;
            case "ipv6":
                return allowIPv6;
            case "cidr":
                return allowCIDR;
            case "hostname":
                return allowHostname;
            case "name":
                // IPSets/Tags fall under 'name' (alphanumeric)
                // If not a valid IP/Host, we check if it matches Name regex
                return true;
            default:
                return false;
        }
    }

    function getErrorMessage(t: AddressType): string {
        if (t === "hostname" && !allowHostname) return "Hostnames not allowed";
        if (t === "cidr" && !allowCIDR) return "CIDR ranges not allowed";
        if (t === "invalid") return "Invalid format";
        return "Invalid address";
    }

    function getTypeIcon(t: AddressType) {
        switch (t) {
            case "ipv4":
            case "ipv6":
            case "cidr":
                return "cpu"; // Chip icon
            case "hostname":
                return "globe";
            case "name":
                return "tag";
            default:
                return "alert-circle";
        }
    }

    function handleInput() {
        if (!value) {
            filteredSuggestions = [];
            showSuggestions = false;
            return;
        }

        // Filter suggestions
        const lower = value.toLowerCase();
        filteredSuggestions = suggestions
            .filter((s) => s.toLowerCase().includes(lower) && s !== value)
            .slice(0, 5);

        showSuggestions = filteredSuggestions.length > 0;
    }

    function selectSuggestion(s: string) {
        value = s;
        showSuggestions = false;
        // Keep focus (or refocus if lost)
        inputElement?.focus();
    }

    function handleKeydown(e: KeyboardEvent) {
        if (showSuggestions) {
            if (e.key === "Enter") {
                e.preventDefault();
                selectSuggestion(filteredSuggestions[0]);
            } else if (e.key === "Escape") {
                showSuggestions = false;
            }
        }
    }

    // Close suggestions on blur (delayed to allow click)
    function handleBlur() {
        focused = false;
        setTimeout(() => {
            showSuggestions = false;
        }, 200);
    }
</script>

<div class="network-input-group {className}" class:has-port={allowPort}>
    <!-- Address Input -->
    <div class="input-container function-field">
        {#if label}
            <label for={id} class="input-label">
                {label}
                {#if required}<span class="required">*</span>{/if}
            </label>
        {/if}

        <div class="input-wrapper" class:focused class:error={!!error && value}>
            <!-- Icon Indicator -->
            <div class="type-icon" title={type}>
                <svg
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                    class="icon {type}"
                >
                    {#if getTypeIcon(type) === "globe"}
                        <circle cx="12" cy="12" r="10" />
                        <line x1="2" y1="12" x2="22" y2="12" />
                        <path
                            d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"
                        />
                    {:else if getTypeIcon(type) === "tag"}
                        <path
                            d="M20.59 13.41l-7.17 7.17a2 2 0 0 1-2.83 0L2 12V2h10l8.59 8.59a2 2 0 0 1 0 2.82z"
                        />
                        <line x1="7" y1="7" x2="7.01" y2="7" />
                    {:else if getTypeIcon(type) === "cpu"}
                        <rect
                            x="4"
                            y="4"
                            width="16"
                            height="16"
                            rx="2"
                            ry="2"
                        />
                        <rect x="9" y="9" width="6" height="6" />
                        <line x1="9" y1="1" x2="9" y2="4" />
                        <line x1="15" y1="1" x2="15" y2="4" />
                        <line x1="9" y1="20" x2="9" y2="23" />
                        <line x1="15" y1="20" x2="15" y2="23" />
                        <line x1="20" y1="9" x2="23" y2="9" />
                        <line x1="20" y1="14" x2="23" y2="14" />
                        <line x1="1" y1="9" x2="4" y2="9" />
                        <line x1="1" y1="14" x2="4" y2="14" />
                    {:else}
                        <circle cx="12" cy="12" r="10" />
                        <line x1="12" y1="8" x2="12" y2="12" />
                        <line x1="12" y1="16" x2="12.01" y2="16" />
                    {/if}
                </svg>
            </div>

            <input
                {id}
                bind:this={inputElement}
                type="text"
                bind:value
                oninput={handleInput}
                onfocus={() => {
                    focused = true;
                    handleInput();
                }}
                onblur={handleBlur}
                onkeydown={handleKeydown}
                {placeholder}
                {disabled}
                class="input-field"
                autocomplete="off"
            />

            <!-- Autocomplete Dropdown -->
            {#if showSuggestions}
                <div class="suggestions" transition:fade={{ duration: 100 }}>
                    {#each filteredSuggestions as suggestion}
                        <button
                            class="suggestion-item"
                            onmousedown={() => selectSuggestion(suggestion)}
                        >
                            {suggestion}
                        </button>
                    {/each}
                </div>
            {/if}
        </div>

        {#if error && value}
            <p class="error-text">{error}</p>
        {/if}
    </div>

    <!-- Optional Port Field -->
    {#if allowPort}
        <div class="port-container">
            {#if label}
                <label for="{id}-port" class="input-label">Port</label>
            {/if}
            <div class="input-wrapper" class:focused class:error={!!portError}>
                <input
                    id="{id}-port"
                    type="number"
                    bind:value={port}
                    placeholder="Any"
                    min="1"
                    max="65535"
                    {disabled}
                    class="input-field port-field"
                />
            </div>
        </div>
    {/if}
</div>

<style>
    .network-input-group {
        display: flex;
        gap: var(--space-4);
        align-items: flex-start;
    }

    .network-input-group.has-port .input-container {
        flex: 3;
    }

    .port-container {
        flex: 1;
        min-width: 80px;
        display: flex;
        flex-direction: column;
        gap: var(--space-1);
    }

    .input-container {
        display: flex;
        flex-direction: column;
        gap: var(--space-1);
        position: relative;
        width: 100%;
    }

    .input-label {
        font-size: var(--text-sm);
        font-weight: 500;
        color: var(--color-foreground);
        margin-bottom: 2px;
    }

    .required {
        color: var(--color-destructive);
        margin-left: 2px;
    }

    .input-wrapper {
        display: flex;
        align-items: center;
        background: var(--color-background);
        border: 1px solid var(--color-border);
        border-radius: var(--radius-md);
        transition: all var(--transition-fast);
        position: relative;
    }

    .input-wrapper.focused {
        border-color: var(--color-primary);
        box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.1);
    }

    .input-wrapper.error {
        border-color: var(--color-destructive);
    }

    .type-icon {
        padding: 0 var(--space-3);
        color: var(--color-muted);
        display: flex;
        align-items: center;
        justify-content: center;
        border-right: 1px solid var(--color-border);
        height: 38px;
    }

    .icon {
        width: 16px;
        height: 16px;
        transition: color 0.2s;
    }

    .icon.ipv4,
    .icon.ipv6,
    .icon.cidr {
        color: var(--color-primary);
    }
    .icon.hostname {
        color: var(--color-warning);
    }
    .icon.name {
        color: var(--color-success);
    }
    .icon.invalid {
        color: var(--color-destructive);
    }

    .input-field {
        flex: 1;
        border: none;
        background: transparent;
        padding: var(--space-2) var(--space-3);
        font-size: var(--text-sm);
        color: var(--color-foreground);
        outline: none;
        width: 100%;
    }

    .port-field {
        text-align: center;
    }

    .suggestions {
        position: absolute;
        top: 100%;
        left: 0;
        right: 0;
        background: var(--color-background);
        border: 1px solid var(--color-border);
        border-radius: var(--radius-md);
        margin-top: 4px;
        max-height: 200px;
        overflow-y: auto;
        z-index: 50;
        box-shadow: var(--shadow-lg);
    }

    .suggestion-item {
        width: 100%;
        text-align: left;
        padding: var(--space-2) var(--space-3);
        background: transparent;
        border: none;
        cursor: pointer;
        font-size: var(--text-sm);
        color: var(--color-foreground);
    }

    .suggestion-item:hover {
        background: var(--color-backgroundSecondary);
    }

    .error-text {
        font-size: var(--text-xs);
        color: var(--color-destructive);
        margin: 2px 0 0 0;
    }

    /* Remove number arrows for port input */
    input[type="number"]::-webkit-inner-spin-button,
    input[type="number"]::-webkit-outer-spin-button {
        -webkit-appearance: none;
        margin: 0;
    }
</style>
