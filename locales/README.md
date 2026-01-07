# Locales (Backend)

Go text/message translations for CLI and backend messages.

## Current Languages

- `en/` - English (default)

## Adding a New Language

1. **Create language directory:**
   ```bash
   mkdir <lang>
   ```

2. **Copy message catalog:**
   ```bash
   cp en/out.gotext.json <lang>/out.gotext.json
   ```

3. **Translate messages** in the JSON file

4. **Register in code** - See `internal/i18n/i18n.go`

## Notes

- Backend translations are separate from UI translations
- CLI messages use Go's `golang.org/x/text/message` package
- See `scripts/generate_translations.py` for extraction tools
