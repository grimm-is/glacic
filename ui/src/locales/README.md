# Translations

This directory contains translation files for the Web UI.

## Current Languages

- `en.json` - English (default)

## Adding a New Language

1. **Copy the English template:**
   ```bash
   cp en.json <lang>.json
   ```

2. **Translate all values** (keep keys unchanged):
   ```json
   {
     "nav.dashboard": "Tableau de bord",
     "nav.interfaces": "Interfaces",
     ...
   }
   ```

3. **Register in the app** - Edit `ui/src/lib/i18n.ts`:
   ```typescript
   import fr from '../locales/fr.json';
   
   const translations = { en, fr };
   ```

4. **Add language selector** (optional) - Update `Settings.svelte`

## Key Format

Keys use dot notation: `section.subsection.item`

Examples:
- `nav.dashboard` - Navigation items
- `dashboard.systemStatus` - Dashboard page
- `firewall.rules.add` - Firewall rule actions
- `common.save` - Shared UI elements

## Testing

Run the dev server and switch languages:
```bash
cd ui && npm run dev
```

## Notes

- Missing keys fall back to English
- `keys_found.txt` is auto-generated, do not edit
