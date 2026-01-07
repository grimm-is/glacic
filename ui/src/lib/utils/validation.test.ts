
import { describe, it, expect } from 'vitest';
import {
    isValidIPv4,
    isValidIPv6,
    isValidCIDR,
    isValidHostname,
    isValidPort,
    getAddressType
} from './validation';

describe('Network Validation', () => {
    it('validates IPv4', () => {
        expect(isValidIPv4('1.1.1.1')).toBe(true);
        expect(isValidIPv4('192.168.0.1')).toBe(true);
        expect(isValidIPv4('256.0.0.1')).toBe(false);
        expect(isValidIPv4('1.1.1')).toBe(false);
        expect(isValidIPv4('')).toBe(false);
    });

    it('validates IPv6', () => {
        expect(isValidIPv6('::1')).toBe(true);
        expect(isValidIPv6('2001:db8::1')).toBe(true);
        expect(isValidIPv6('1234::5678:90ab')).toBe(true);
        expect(isValidIPv6('1.1.1.1')).toBe(false); // Valid regex might catch this if not careful, but my regex expects colons
    });

    it('validates CIDR', () => {
        expect(isValidCIDR('10.0.0.0/24')).toBe(true);
        expect(isValidCIDR('10.0.0.0/33')).toBe(false);
        expect(isValidCIDR('::1/128')).toBe(true);
        expect(isValidCIDR('1.1.1.1')).toBe(false);
    });

    it('validates Hostname', () => {
        expect(isValidHostname('google.com')).toBe(true);
        expect(isValidHostname('localhost')).toBe(true);
        expect(isValidHostname('foo')).toBe(false); // My regex requires TLD
        expect(isValidHostname('foo.bar')).toBe(true);
        expect(isValidHostname('-foo.com')).toBe(false);
    });

    it('identifies address types', () => {
        expect(getAddressType('1.2.3.4')).toBe('ipv4');
        expect(getAddressType('10.0.0.0/8')).toBe('cidr');
        expect(getAddressType('example.com')).toBe('hostname');
        expect(getAddressType('whitelist')).toBe('name');
        expect(getAddressType('tag_iot')).toBe('name');
        expect(getAddressType('invalid!')).toBe('invalid');
    });
});
