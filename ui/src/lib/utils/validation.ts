/**
 * Network Validation Utilities
 */

// IPv4 Regex: 4 groups of 0-255
const IPV4_REGEX = /^(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$/;

// IPv6 Regex: Simplified, covers most common cases (full and compressed)
const IPV6_REGEX = /^(?:[A-F0-9]{1,4}:){7}[A-F0-9]{1,4}$|^([A-F0-9]{1,4}:){1,7}:|::(?:[A-F0-9]{1,4}:){0,7}[A-F0-9]{1,4}$/i;

// Hostname Regex: RFC 1123 compliant (approx)
// Labels: 1-63 chars, alphanumeric or hyphen (cannot start/end with hyphen)
// TLD: 2+ chars
const HOSTNAME_REGEX = /^(?=.{1,253}$)(?:(?!-)[a-zA-Z0-9-]{1,63}(?<!-)\.)+[a-zA-Z]{2,63}$/;

// Tag/IPSet Name Regex: alphanumeric, underscore, hyphen
const NAME_REGEX = /^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$/;

export function isValidIPv4(value: string): boolean {
    if (!value) return false;
    return IPV4_REGEX.test(value);
}

export function isValidIPv6(value: string): boolean {
    if (!value) return false;
    // Basic regex check fits most UI needs
    return IPV6_REGEX.test(value);
}

export function isValidIP(value: string): boolean {
    return isValidIPv4(value) || isValidIPv6(value);
}

export function isValidCIDR(value: string): boolean {
    if (!value) return false;
    const parts = value.split('/');
    if (parts.length !== 2) return false;

    const [ip, maskStr] = parts;
    const mask = parseInt(maskStr, 10);

    if (isNaN(mask)) return false;

    if (isValidIPv4(ip)) {
        return mask >= 0 && mask <= 32;
    }

    if (isValidIPv6(ip)) {
        return mask >= 0 && mask <= 128;
    }

    return false;
}

export function isValidHostname(value: string): boolean {
    if (!value) return false;
    // Allow "localhost" as special case
    if (value === 'localhost') return true;
    return HOSTNAME_REGEX.test(value);
}

export function isValidPort(value: number | string): boolean {
    const port = typeof value === 'string' ? parseInt(value, 10) : value;
    return !isNaN(port) && port >= 1 && port <= 65535;
}

export function isValidName(value: string): boolean {
    if (!value) return false;
    return NAME_REGEX.test(value);
}

export type AddressType = 'ipv4' | 'ipv6' | 'cidr' | 'hostname' | 'name' | 'invalid';

export function getAddressType(value: string): AddressType {
    if (!value) return 'invalid';
    if (isValidIPv4(value)) return 'ipv4';
    if (isValidIPv6(value)) return 'ipv6';
    if (isValidCIDR(value)) return 'cidr';
    if (isValidHostname(value)) return 'hostname';
    if (isValidName(value)) return 'name'; // Fallback for IPSet/Tag
    return 'invalid';
}
