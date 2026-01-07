/**
 * Glacic Theme Configuration
 *
 * All visual tokens in a single file. Swap this file to completely
 * change the look and feel of the UI.
 */

export const theme = {
    // Semantic colors - light and dark variants
    colors: {
        background: { light: '#ffffff', dark: '#0a0a0f' },
        backgroundSecondary: { light: '#f4f4f5', dark: '#18181b' },
        surface: { light: '#ffffff', dark: '#1f1f23' },
        surfaceHover: { light: '#f4f4f5', dark: '#27272a' },
        border: { light: '#e4e4e7', dark: '#27272a' },
        borderSubtle: { light: '#f4f4f5', dark: '#1f1f23' },

        primary: { light: '#3b82f6', dark: '#60a5fa' },
        primaryForeground: { light: '#ffffff', dark: '#0a0a0f' },

        success: { light: '#22c55e', dark: '#4ade80' },
        successForeground: { light: '#ffffff', dark: '#0a0a0f' },

        destructive: { light: '#ef4444', dark: '#f87171' },
        destructiveForeground: { light: '#ffffff', dark: '#0a0a0f' },

        warning: { light: '#f59e0b', dark: '#fbbf24' },
        warningForeground: { light: '#0a0a0f', dark: '#0a0a0f' },

        muted: { light: '#71717a', dark: '#a1a1aa' },
        mutedForeground: { light: '#52525b', dark: '#71717a' },

        foreground: { light: '#09090b', dark: '#fafafa' },
        foregroundSecondary: { light: '#3f3f46', dark: '#d4d4d8' },
    },

    // Typography
    fonts: {
        sans: 'Inter, system-ui, -apple-system, sans-serif',
        mono: 'JetBrains Mono, ui-monospace, monospace',
    },

    fontSizes: {
        xs: '0.75rem',     // 12px
        sm: '0.875rem',    // 14px
        base: '1rem',      // 16px
        lg: '1.125rem',    // 18px
        xl: '1.25rem',     // 20px
        '2xl': '1.5rem',   // 24px
        '3xl': '1.875rem', // 30px
    },

    fontWeights: {
        normal: '400',
        medium: '500',
        semibold: '600',
        bold: '700',
    },

    lineHeights: {
        tight: '1.25',
        normal: '1.5',
        relaxed: '1.75',
    },

    // Spacing scale (rem-based)
    spacing: {
        0: '0',
        0.5: '0.125rem',   // 2px
        1: '0.25rem',      // 4px
        1.5: '0.375rem',   // 6px
        2: '0.5rem',       // 8px
        2.5: '0.625rem',   // 10px
        3: '0.75rem',      // 12px
        4: '1rem',         // 16px
        5: '1.25rem',      // 20px
        6: '1.5rem',       // 24px
        8: '2rem',         // 32px
        10: '2.5rem',      // 40px
        12: '3rem',        // 48px
        16: '4rem',        // 64px
    },

    // Border radii
    radii: {
        none: '0',
        sm: '0.25rem',     // 4px
        md: '0.375rem',    // 6px
        lg: '0.5rem',      // 8px
        xl: '0.75rem',     // 12px
        '2xl': '1rem',     // 16px
        full: '9999px',
    },

    // Shadows
    shadows: {
        none: 'none',
        sm: '0 1px 2px 0 rgb(0 0 0 / 0.05)',
        md: '0 4px 6px -1px rgb(0 0 0 / 0.1), 0 2px 4px -2px rgb(0 0 0 / 0.1)',
        lg: '0 10px 15px -3px rgb(0 0 0 / 0.1), 0 4px 6px -4px rgb(0 0 0 / 0.1)',
        xl: '0 20px 25px -5px rgb(0 0 0 / 0.1), 0 8px 10px -6px rgb(0 0 0 / 0.1)',
    },

    // Transitions
    transitions: {
        fast: '150ms ease',
        normal: '200ms ease',
        slow: '300ms ease',
    },

    // Breakpoints
    breakpoints: {
        sm: '640px',
        md: '768px',
        lg: '1024px',
        xl: '1280px',
        '2xl': '1536px',
    },

    // Z-index scale
    zIndex: {
        dropdown: '50',
        modal: '100',
        overlay: '90',
        toast: '200',
    },
} as const;

export type Theme = typeof theme;

/**
 * Get CSS variable declarations for the theme
 * Injects into :root for light mode and .dark for dark mode
 */
export function generateCSSVariables(mode: 'light' | 'dark'): string {
    const vars: string[] = [];

    // Colors
    for (const [name, value] of Object.entries(theme.colors)) {
        const colorValue = value as { light: string; dark: string };
        vars.push(`--color-${name}: ${colorValue[mode]};`);
    }

    // Fonts
    vars.push(`--font-sans: ${theme.fonts.sans};`);
    vars.push(`--font-mono: ${theme.fonts.mono};`);

    // Font sizes
    for (const [name, value] of Object.entries(theme.fontSizes)) {
        vars.push(`--text-${name}: ${value};`);
    }

    // Spacing
    for (const [name, value] of Object.entries(theme.spacing)) {
        vars.push(`--space-${name}: ${value};`);
    }

    // Radii
    for (const [name, value] of Object.entries(theme.radii)) {
        vars.push(`--radius-${name}: ${value};`);
    }

    // Shadows
    for (const [name, value] of Object.entries(theme.shadows)) {
        vars.push(`--shadow-${name}: ${value};`);
    }

    return vars.join('\n  ');
}
