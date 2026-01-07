/**
 * Common network services with their port/protocol definitions.
 * Used for the service dropdown in firewall rule modals.
 */

export interface ServiceDefinition {
    name: string;
    label: string;
    protocol: 'tcp' | 'udp' | 'both';
    port: number | string;  // string for ranges like "20-21"
    description?: string;
}

// Core web services
export const WEB_SERVICES: ServiceDefinition[] = [
    { name: 'http', label: 'HTTP', protocol: 'tcp', port: 80, description: 'Web traffic' },
    { name: 'https', label: 'HTTPS', protocol: 'tcp', port: 443, description: 'Secure web traffic' },
    { name: 'http-alt', label: 'HTTP (8080)', protocol: 'tcp', port: 8080, description: 'Alternative HTTP' },
];

// Remote access services
export const REMOTE_ACCESS: ServiceDefinition[] = [
    { name: 'ssh', label: 'SSH', protocol: 'tcp', port: 22, description: 'Secure shell' },
    { name: 'rdp', label: 'RDP', protocol: 'tcp', port: 3389, description: 'Remote Desktop' },
    { name: 'vnc', label: 'VNC', protocol: 'tcp', port: 5900, description: 'Virtual Network Computing' },
    { name: 'telnet', label: 'Telnet', protocol: 'tcp', port: 23, description: 'Insecure remote access' },
];

// Email services
export const EMAIL_SERVICES: ServiceDefinition[] = [
    { name: 'smtp', label: 'SMTP', protocol: 'tcp', port: 25, description: 'Email sending' },
    { name: 'smtps', label: 'SMTPS', protocol: 'tcp', port: 465, description: 'Secure SMTP' },
    { name: 'submission', label: 'Submission', protocol: 'tcp', port: 587, description: 'Email submission' },
    { name: 'imap', label: 'IMAP', protocol: 'tcp', port: 143, description: 'Email retrieval' },
    { name: 'imaps', label: 'IMAPS', protocol: 'tcp', port: 993, description: 'Secure IMAP' },
    { name: 'pop3', label: 'POP3', protocol: 'tcp', port: 110, description: 'Email retrieval' },
    { name: 'pop3s', label: 'POP3S', protocol: 'tcp', port: 995, description: 'Secure POP3' },
];

// File transfer services
export const FILE_SERVICES: ServiceDefinition[] = [
    { name: 'ftp', label: 'FTP', protocol: 'tcp', port: '20-21', description: 'File Transfer Protocol' },
    { name: 'sftp', label: 'SFTP', protocol: 'tcp', port: 22, description: 'Secure FTP (via SSH)' },
    { name: 'smb', label: 'SMB', protocol: 'tcp', port: 445, description: 'Windows file sharing' },
    { name: 'nfs', label: 'NFS', protocol: 'tcp', port: 2049, description: 'Network File System' },
];

// DNS and network services
export const NETWORK_SERVICES: ServiceDefinition[] = [
    { name: 'dns', label: 'DNS', protocol: 'both', port: 53, description: 'Domain Name System' },
    { name: 'dhcp', label: 'DHCP', protocol: 'udp', port: '67-68', description: 'Dynamic Host Configuration' },
    { name: 'ntp', label: 'NTP', protocol: 'udp', port: 123, description: 'Network Time Protocol' },
    { name: 'snmp', label: 'SNMP', protocol: 'udp', port: 161, description: 'Network monitoring' },
];

// Database services
export const DATABASE_SERVICES: ServiceDefinition[] = [
    { name: 'mysql', label: 'MySQL', protocol: 'tcp', port: 3306, description: 'MySQL/MariaDB' },
    { name: 'postgres', label: 'PostgreSQL', protocol: 'tcp', port: 5432, description: 'PostgreSQL database' },
    { name: 'redis', label: 'Redis', protocol: 'tcp', port: 6379, description: 'Redis cache/database' },
    { name: 'mongodb', label: 'MongoDB', protocol: 'tcp', port: 27017, description: 'MongoDB database' },
    { name: 'mssql', label: 'MS SQL', protocol: 'tcp', port: 1433, description: 'Microsoft SQL Server' },
];

// Gaming/streaming services
export const GAMING_SERVICES: ServiceDefinition[] = [
    { name: 'minecraft', label: 'Minecraft', protocol: 'tcp', port: 25565, description: 'Minecraft server' },
    { name: 'steam', label: 'Steam', protocol: 'udp', port: 27015, description: 'Steam game server' },
];

// VPN services
export const VPN_SERVICES: ServiceDefinition[] = [
    { name: 'openvpn', label: 'OpenVPN', protocol: 'udp', port: 1194, description: 'OpenVPN' },
    { name: 'wireguard', label: 'WireGuard', protocol: 'udp', port: 51820, description: 'WireGuard VPN' },
    { name: 'ipsec', label: 'IPsec', protocol: 'udp', port: 500, description: 'IKE for IPsec' },
];

// Special/other
export const OTHER_SERVICES: ServiceDefinition[] = [
    { name: 'ping', label: 'ICMP/Ping', protocol: 'both', port: 0, description: 'ICMP echo (use proto icmp)' },
];

// All services grouped for dropdown
export const SERVICE_GROUPS = [
    { label: 'Web', services: WEB_SERVICES },
    { label: 'Remote Access', services: REMOTE_ACCESS },
    { label: 'Email', services: EMAIL_SERVICES },
    { label: 'File Transfer', services: FILE_SERVICES },
    { label: 'Network', services: NETWORK_SERVICES },
    { label: 'Database', services: DATABASE_SERVICES },
    { label: 'Gaming', services: GAMING_SERVICES },
    { label: 'VPN', services: VPN_SERVICES },
] as const;

// Flat list of all services for easy lookup
export const ALL_SERVICES: ServiceDefinition[] = [
    ...WEB_SERVICES,
    ...REMOTE_ACCESS,
    ...EMAIL_SERVICES,
    ...FILE_SERVICES,
    ...NETWORK_SERVICES,
    ...DATABASE_SERVICES,
    ...GAMING_SERVICES,
    ...VPN_SERVICES,
    ...OTHER_SERVICES,
];

// Get service by name
export function getService(name: string): ServiceDefinition | undefined {
    return ALL_SERVICES.find(s => s.name === name);
}
