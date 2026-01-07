<script lang="ts">
    /**
     * PillInput Component
     * Multi-select input with pill-based UX
     * Supports predefined options and freeform text entry
     */

    interface Option {
        value: string;
        label: string;
        group?: string; // Optional grouping
    }

    interface Props {
        id?: string;
        label?: string;
        value?: string[]; // Array of selected values
        options?: Option[]; // Predefined options
        placeholder?: string;
        allowCustom?: boolean; // Allow freeform text entry
        validate?: (input: string) => string | null; // Validate custom input, returns normalized value or null if invalid
        class?: string;
    }

    let {
        id = "",
        label = "",
        value = $bindable([]),
        options = [],
        placeholder = "Type to add...",
        allowCustom = false,
        validate = undefined,
        class: className = "",
    }: Props = $props();

    let inputValue = $state("");
    let showSuggestions = $state(false);
    let inputRef: HTMLInputElement | null = $state(null);

    // Get display label for a value
    function getLabel(val: string): string {
        const opt = options.find((o) => o.value === val);
        return opt?.label || val;
    }

    // Filter available options (not already selected)
    const availableOptions = $derived(
        options.filter((o) => !value.includes(o.value)),
    );

    // Filtered suggestions based on input
    const filteredSuggestions = $derived(
        inputValue
            ? availableOptions.filter(
                  (o) =>
                      o.label
                          .toLowerCase()
                          .includes(inputValue.toLowerCase()) ||
                      o.value.toLowerCase().includes(inputValue.toLowerCase()),
              )
            : availableOptions,
    );

    function addValue(val: string) {
        if (val && !value.includes(val)) {
            value = [...value, val];
        }
        inputValue = "";
        showSuggestions = false;
    }

    function removeValue(val: string) {
        value = value.filter((v) => v !== val);
    }

    function handleInput() {
        showSuggestions = true;
    }

    function handleKeydown(e: KeyboardEvent) {
        if (e.key === "Enter" && inputValue) {
            e.preventDefault();
            // Check if it matches an option
            const match = options.find(
                (o) =>
                    o.value.toLowerCase() === inputValue.toLowerCase() ||
                    o.label.toLowerCase() === inputValue.toLowerCase(),
            );
            if (match) {
                addValue(match.value);
            } else if (allowCustom && validate) {
                const validated = validate(inputValue);
                if (validated) addValue(validated);
            } else if (allowCustom) {
                addValue(inputValue.trim());
            }
        } else if (e.key === "Backspace" && !inputValue && value.length > 0) {
            // Remove last pill on backspace when input is empty
            value = value.slice(0, -1);
        } else if (e.key === "Escape") {
            showSuggestions = false;
        }
    }

    function handleBlur() {
        // Delay to allow click on suggestion
        setTimeout(() => {
            showSuggestions = false;
        }, 150);
    }

    function handleFocus() {
        showSuggestions = true;
    }
</script>

<div class="pill-input-wrapper {className}">
    {#if label}
        <label for={id} class="pill-input-label">{label}</label>
    {/if}

    <!-- svelte-ignore a11y_no_static_element_interactions a11y_click_events_have_key_events -->
    <div class="pill-input-container" onclick={() => inputRef?.focus()}>
        <!-- Selected pills -->
        {#each value as val}
            <span class="pill">
                {getLabel(val)}
                <button
                    type="button"
                    class="pill-remove"
                    onclick={(e) => {
                        e.stopPropagation();
                        removeValue(val);
                    }}
                    aria-label="Remove {getLabel(val)}">×</button
                >
            </span>
        {/each}

        <!-- Text input -->
        <input
            bind:this={inputRef}
            {id}
            type="text"
            bind:value={inputValue}
            {placeholder}
            class="pill-text-input"
            oninput={handleInput}
            onkeydown={handleKeydown}
            onblur={handleBlur}
            onfocus={handleFocus}
        />
    </div>

    <!-- Suggestions dropdown -->
    {#if showSuggestions && filteredSuggestions.length > 0}
        <div class="suggestions">
            {#each filteredSuggestions as opt}
                <button
                    type="button"
                    class="suggestion"
                    onmousedown={() => addValue(opt.value)}
                >
                    {opt.label}
                </button>
            {/each}
        </div>
    {/if}
</div>

<style>
    .pill-input-wrapper {
        display: flex;
        flex-direction: column;
        gap: var(--space-1);
        position: relative;
    }

    .pill-input-label {
        font-size: var(--text-sm);
        font-weight: 500;
        color: var(--color-foreground);
    }

    .pill-input-container {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-1);
        padding: var(--space-2);
        min-height: 42px;
        background-color: var(--color-background);
        border: 1px solid var(--color-border);
        border-radius: var(--radius-md);
        cursor: text;
        transition: border-color var(--transition-fast);
    }

    .pill-input-container:focus-within {
        border-color: var(--color-primary);
        box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.15);
    }

    .pill {
        display: inline-flex;
        align-items: center;
        gap: var(--space-1);
        padding: var(--space-1) var(--space-2);
        background-color: var(--color-primary);
        color: white;
        font-size: var(--text-xs);
        font-weight: 500;
        border-radius: var(--radius-full);
        white-space: nowrap;
    }

    .pill-remove {
        display: inline-flex;
        align-items: center;
        justify-content: center;
        width: 16px;
        height: 16px;
        padding: 0;
        margin: 0;
        background: rgba(255, 255, 255, 0.2);
        border: none;
        border-radius: 50%;
        color: inherit;
        font-size: 14px;
        line-height: 1;
        cursor: pointer;
        transition: background var(--transition-fast);
    }

    .pill-remove:hover {
        background: rgba(255, 255, 255, 0.4);
    }

    .pill-text-input {
        flex: 1;
        min-width: 80px;
        padding: var(--space-1);
        font-size: var(--text-sm);
        font-family: inherit;
        color: var(--color-foreground);
        background: transparent;
        border: none;
        outline: none;
    }

    .pill-text-input::placeholder {
        color: var(--color-muted);
    }

    .suggestions {
        position: absolute;
        top: 100%;
        left: 0;
        right: 0;
        margin-top: var(--space-1);
        max-height: 200px;
        overflow-y: auto;
        background-color: var(--color-backgroundSecondary);
        border: 1px solid var(--color-border);
        border-radius: var(--radius-md);
        box-shadow: var(--shadow-lg);
        z-index: 50;
    }

    .suggestion {
        display: block;
        width: 100%;
        padding: var(--space-2) var(--space-3);
        text-align: left;
        font-size: var(--text-sm);
        color: var(--color-foreground);
        background: transparent;
        border: none;
        cursor: pointer;
        transition: background var(--transition-fast);
    }

    .suggestion:hover {
        background-color: var(--color-primary);
        color: white;
    }
</style>
