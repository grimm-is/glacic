
import json
import os

def process_backend():
    en_path = "locales/en/out.gotext.json"
    de_path = "locales/de/messages.gotext.json"

    if not os.path.exists(en_path):
        print(f"File not found: {en_path}")
        return

    with open(en_path, "r") as f:
        data = json.load(f)

    data["language"] = "de"
    for msg in data["messages"]:
        # Simple pseudo-localization
        msg["translation"] = f"[DE] {msg['message']}"

    with open(de_path, "w") as f:
        json.dump(data, f, indent=4, sort_keys=True)
    print(f"Generated {de_path} with {len(data['messages'])} entries.")

def get_nested(data, key):
    parts = key.split(".")
    for part in parts:
        if isinstance(data, dict) and part in data:
            data = data[part]
        else:
            return None
    return data

def set_nested(data, key, value):
    parts = key.split(".")
    for i, part in enumerate(parts[:-1]):
        if part not in data or not isinstance(data[part], dict):
            data[part] = {}
        data = data[part]
    
    # Only set if not already present
    last_key = parts[-1]
    if last_key not in data:
        data[last_key] = value

def process_frontend():
    keys_path = "ui/src/locales/keys_found.txt"
    en_path = "ui/src/locales/en.json"
    de_path = "ui/src/locales/de.json"

    if not os.path.exists(keys_path):
        print(f"File not found: {keys_path}")
        return

    with open(keys_path, "r") as f:
        keys = [line.strip() for line in f if line.strip()]

    # Load existing EN
    en_data = {}
    if os.path.exists(en_path):
        try:
            with open(en_path, "r") as f:
                en_data = json.load(f)
        except:
            pass
    
    # Helper to create pseudo-translation in same structure
    de_data = {}
    
    # Process keys
    for key in keys:
        # Check if key (flat or nested) already exists in EN
        existing_val = get_nested(en_data, key)
        
        # Determine display name (fallback if new)
        display = key.split(".")[-1].replace("_", " ").title()
        
        if existing_val is None:
            # Add to EN if totally missing
            set_nested(en_data, key, display)
            en_val = display
        else:
            en_val = existing_val

        # Always add to DE (reforming the structure)
        if isinstance(en_val, str):
            set_nested(de_data, key, f"[DE] {en_val}")

    # Write EN back (sorted)
    with open(en_path, "w") as f:
        json.dump(en_data, f, indent=4, sort_keys=True)
    
    # Write DE (sorted)
    with open(de_path, "w") as f:
        json.dump(de_data, f, indent=4, sort_keys=True)

    print(f"Updated {en_path} and {de_path} with {len(keys)} keys.")

if __name__ == "__main__":
    process_backend()
    process_frontend()
