// Mock API server for UI development
// Run with: node mock-api.js

import http from 'http';

const PORT = process.env.PORT || 8080;

// Mock data
const mockConfig = {
  ip_forwarding: true,
  zones: [
    { name: 'WAN', color: 'red', description: 'External/Internet' },
    { name: 'LAN', color: 'green', description: 'Local Network' },
    { name: 'DMZ', color: 'orange', description: 'Demilitarized Zone' },
    { name: 'Guest', color: 'purple', description: 'Guest Network' },
    { name: 'IoT', color: 'cyan', description: 'IoT Devices' },
  ],
  interfaces: [
    { Name: 'eth0', Zone: 'WAN', IPv4: [], DHCP: true, Description: 'Internet Connection' },
    { Name: 'eth1', Zone: 'LAN', IPv4: ['192.168.1.1/24'], Description: 'Local Network' },
    { Name: 'eth2', Zone: 'DMZ', IPv4: ['10.0.0.1/24'], Description: 'DMZ Network' },
  ],
  policies: [
    { from: 'LAN', to: 'WAN', rules: [{ action: 'accept', name: 'Allow outbound' }] },
    { from: 'LAN', to: 'Firewall', rules: [{ action: 'accept', protocol: 'tcp', dest_port: 22, name: 'SSH' }] },
    { from: 'WAN', to: 'LAN', rules: [{ action: 'drop', name: 'Block inbound' }] },
    { from: 'WAN', to: 'DMZ', rules: [{ action: 'accept', protocol: 'tcp', dest_port: 443, name: 'HTTPS' }] },
  ],
  nat: [
    { type: 'masquerade', interface: 'eth0' },
    { type: 'dnat', protocol: 'tcp', destination: '443', to_address: '10.0.0.10', to_port: '443', description: 'Web Server' },
    { type: 'dnat', protocol: 'tcp', destination: '22', to_address: '192.168.1.100', to_port: '22', description: 'SSH to Server' },
  ],
  ipsets: [
    { name: 'firehol_level1', firehol_list: 'firehol_level1', auto_update: true, refresh_hours: 24, action: 'drop', apply_to: 'input', match_on_source: true },
    { name: 'trusted_ips', entries: ['192.168.1.0/24', '10.0.0.0/8'], type: 'ipv4_addr' },
  ],
  dhcp_server: {
    enabled: true,
    scopes: [
      { name: 'LAN', interface: 'eth1', range_start: '192.168.1.100', range_end: '192.168.1.200', router: '192.168.1.1' },
    ]
  },
  dns_server: {
    enabled: true,
    listen_on: ['192.168.1.1'],
    forwarders: ['1.1.1.1', '8.8.8.8'],
    zones: []
  },
  routes: [],
  protection: {
    enabled: true,
    anti_spoofing: true,
    bogon_filter: true,
    syn_flood_protection: true,
  }
};

const mockStatus = {
  status: 'online',
  uptime: '2d 5h 32m',
  version: '0.1.0',
};

const mockLeases = [
  { interface: 'eth1', ip: '192.168.1.100', mac: '00:11:22:33:44:55', hostname: 'Ben-MacBook-Pro', active: true, router: '192.168.1.1' },
  { interface: 'eth1', ip: '192.168.1.101', mac: 'AA:BB:CC:DD:EE:FF', hostname: 'Living-Room-TV', active: true, router: '192.168.1.1' },
  { interface: 'eth1', ip: '192.168.1.102', mac: '11:22:33:44:55:66', hostname: 'iPhone-13', active: false, router: '192.168.1.1' },
  { interface: 'eth1', ip: '192.168.1.103', mac: '22:33:44:55:66:77', hostname: 'IoT-Thermostat', active: true, router: '192.168.1.1' },
];

let authToken = null;
let setupComplete = true; // Set to false to test setup flow

// Simple session store
const sessions = new Map();

function generateToken() {
  return Math.random().toString(36).substring(2) + Date.now().toString(36);
}

function parseBody(req) {
  return new Promise((resolve, reject) => {
    let body = '';
    req.on('data', chunk => body += chunk);
    req.on('end', () => {
      try {
        resolve(body ? JSON.parse(body) : {});
      } catch (e) {
        resolve({});
      }
    });
    req.on('error', reject);
  });
}

function getCookie(req, name) {
  const cookies = req.headers.cookie?.split(';') || [];
  for (const cookie of cookies) {
    const [key, value] = cookie.trim().split('=');
    if (key === name) return value;
  }
  return null;
}

function sendJSON(res, data, status = 200) {
  res.writeHead(status, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify(data));
}

const server = http.createServer(async (req, res) => {
  // CORS headers for development
  res.setHeader('Access-Control-Allow-Origin', '*');
  res.setHeader('Access-Control-Allow-Methods', 'GET, POST, PUT, DELETE, OPTIONS');
  res.setHeader('Access-Control-Allow-Headers', 'Content-Type');

  if (req.method === 'OPTIONS') {
    res.writeHead(204);
    res.end();
    return;
  }

  const url = new URL(req.url, `http://localhost:${PORT}`);
  const path = url.pathname;

  console.log(`${req.method} ${path}`);

  try {
    // Auth endpoints
    if (path === '/api/auth/status') {
      const token = getCookie(req, 'session');
      const session = sessions.get(token);
      sendJSON(res, {
        authenticated: !!session,
        username: session?.username || null,
        setup_required: !setupComplete,
      });
      return;
    }

    if (path === '/api/auth/login' && req.method === 'POST') {
      const body = await parseBody(req);
      if (body.username === 'admin' && body.password === 'admin123') {
        const token = generateToken();
        sessions.set(token, { username: body.username });
        res.setHeader('Set-Cookie', `session=${token}; Path=/; HttpOnly`);
        sendJSON(res, { authenticated: true, username: body.username });
      } else {
        sendJSON(res, { error: 'Invalid credentials' }, 401);
      }
      return;
    }

    if (path === '/api/auth/logout' && req.method === 'POST') {
      const token = getCookie(req, 'session');
      sessions.delete(token);
      res.setHeader('Set-Cookie', 'session=; Path=/; HttpOnly; Max-Age=0');
      sendJSON(res, { success: true });
      return;
    }

    if (path === '/api/setup/create-admin' && req.method === 'POST') {
      const body = await parseBody(req);
      setupComplete = true;
      const token = generateToken();
      sessions.set(token, { username: body.username });
      res.setHeader('Set-Cookie', `session=${token}; Path=/; HttpOnly`);
      sendJSON(res, { authenticated: true, username: body.username });
      return;
    }

    // Data endpoints
    if (path === '/api/status') {
      sendJSON(res, mockStatus);
      return;
    }

    if (path === '/api/config') {
      sendJSON(res, mockConfig);
      return;
    }

    if (path === '/api/leases') {
      sendJSON(res, mockLeases);
      return;
    }

    if (path === '/api/leases/stats') {
      sendJSON(res, {
        total_clients: 15,
        active_clients: 4,
        wifi_clients: 3,
      });
      return;
    }

    if (path === '/api/interfaces/available') {
      sendJSON(res, [
        { name: 'eth3', mac: '00:11:22:33:44:55', assigned: false },
        { name: 'eth4', mac: '00:11:22:33:44:56', assigned: false },
      ]);
      return;
    }

    if (path === '/api/interfaces/update' && req.method === 'POST') {
      const body = await parseBody(req);
      console.log('Update interface:', body);
      sendJSON(res, { success: true });
      return;
    }

    if (path === '/api/traffic') {
      // Simulate fluctuating traffic
      const rx = Math.floor(Math.random() * 50000000); // 0-50 MB/s
      const tx = Math.floor(Math.random() * 10000000); // 0-10 MB/s
      sendJSON(res, {
        rx_bytes_per_sec: rx,
        tx_bytes_per_sec: tx,
        interfaces: {
          eth0: { rx_bytes: 1234567890, tx_bytes: 987654321 },
          eth1: { rx_bytes: 567890123, tx_bytes: 234567890 },
        }
      });
      return;
    }

    if (path === '/api/services') {
      // Common network services
      sendJSON(res, [
        { name: 'ssh', description: 'Secure Shell', ports: [{ port: 22, protocol: 'tcp' }] },
        { name: 'http', description: 'HTTP Web', ports: [{ port: 80, protocol: 'tcp' }] },
        { name: 'https', description: 'HTTPS Web', ports: [{ port: 443, protocol: 'tcp' }] },
        { name: 'dns', description: 'Domain Name System', ports: [{ port: 53, protocol: 'tcp' }, { port: 53, protocol: 'udp' }] },
        { name: 'ftp', description: 'File Transfer Protocol', ports: [{ port: 21, protocol: 'tcp' }] },
        { name: 'smtp', description: 'Simple Mail Transfer', ports: [{ port: 25, protocol: 'tcp' }] },
        { name: 'smtps', description: 'SMTP over TLS', ports: [{ port: 465, protocol: 'tcp' }] },
        { name: 'submission', description: 'Mail Submission', ports: [{ port: 587, protocol: 'tcp' }] },
        { name: 'pop3', description: 'POP3 Mail', ports: [{ port: 110, protocol: 'tcp' }] },
        { name: 'pop3s', description: 'POP3 over TLS', ports: [{ port: 995, protocol: 'tcp' }] },
        { name: 'imap', description: 'IMAP Mail', ports: [{ port: 143, protocol: 'tcp' }] },
        { name: 'imaps', description: 'IMAP over TLS', ports: [{ port: 993, protocol: 'tcp' }] },
        { name: 'telnet', description: 'Telnet', ports: [{ port: 23, protocol: 'tcp' }] },
        { name: 'rdp', description: 'Remote Desktop', ports: [{ port: 3389, protocol: 'tcp' }] },
        { name: 'vnc', description: 'VNC Remote Desktop', ports: [{ port: 5900, protocol: 'tcp' }] },
        { name: 'mysql', description: 'MySQL Database', ports: [{ port: 3306, protocol: 'tcp' }] },
        { name: 'postgresql', description: 'PostgreSQL Database', ports: [{ port: 5432, protocol: 'tcp' }] },
        { name: 'redis', description: 'Redis Cache', ports: [{ port: 6379, protocol: 'tcp' }] },
        { name: 'mongodb', description: 'MongoDB Database', ports: [{ port: 27017, protocol: 'tcp' }] },
        { name: 'ldap', description: 'LDAP Directory', ports: [{ port: 389, protocol: 'tcp' }] },
        { name: 'ldaps', description: 'LDAP over TLS', ports: [{ port: 636, protocol: 'tcp' }] },
        { name: 'ntp', description: 'Network Time Protocol', ports: [{ port: 123, protocol: 'udp' }] },
        { name: 'snmp', description: 'SNMP Monitoring', ports: [{ port: 161, protocol: 'udp' }] },
        { name: 'syslog', description: 'Syslog', ports: [{ port: 514, protocol: 'udp' }] },
        { name: 'dhcp', description: 'DHCP', ports: [{ port: 67, protocol: 'udp' }, { port: 68, protocol: 'udp' }] },
        { name: 'tftp', description: 'Trivial FTP', ports: [{ port: 69, protocol: 'udp' }] },
        { name: 'nfs', description: 'Network File System', ports: [{ port: 2049, protocol: 'tcp' }] },
        { name: 'smb', description: 'SMB/CIFS File Sharing', ports: [{ port: 445, protocol: 'tcp' }] },
        { name: 'netbios', description: 'NetBIOS', ports: [{ port: 137, protocol: 'udp' }, { port: 138, protocol: 'udp' }, { port: 139, protocol: 'tcp' }] },
        { name: 'openvpn', description: 'OpenVPN', ports: [{ port: 1194, protocol: 'udp' }] },
        { name: 'wireguard', description: 'WireGuard VPN', ports: [{ port: 51820, protocol: 'udp' }] },
        { name: 'ipsec', description: 'IPSec VPN', ports: [{ port: 500, protocol: 'udp' }, { port: 4500, protocol: 'udp' }] },
        { name: 'ping', description: 'ICMP Ping', ports: [{ port: 0, protocol: 'icmp' }] },
        { name: 'sip', description: 'SIP VoIP', ports: [{ port: 5060, protocol: 'udp' }] },
        { name: 'rtsp', description: 'RTSP Streaming', ports: [{ port: 554, protocol: 'tcp' }] },
      ]);
      return;
    }

    // Config section endpoints
    if (path === '/api/config/apply' && req.method === 'POST') {
      console.log('Applying config...');
      sendJSON(res, { success: true });
      return;
    }

    if (path === '/api/config/policies') {
      if (req.method === 'GET') {
        sendJSON(res, mockConfig.policies);
      } else if (req.method === 'POST') {
        const body = await parseBody(req);
        mockConfig.policies = body;
        console.log('Updated policies:', body.length, 'rules');
        sendJSON(res, { success: true });
      }
      return;
    }

    if (path === '/api/config/nat') {
      if (req.method === 'GET') {
        sendJSON(res, mockConfig.nat);
      } else if (req.method === 'POST') {
        const body = await parseBody(req);
        mockConfig.nat = body;
        console.log('Updated NAT:', body.length, 'rules');
        sendJSON(res, { success: true });
      }
      return;
    }

    if (path === '/api/config/ipsets') {
      if (req.method === 'GET') {
        sendJSON(res, mockConfig.ipsets);
      } else if (req.method === 'POST') {
        const body = await parseBody(req);
        mockConfig.ipsets = body;
        console.log('Updated IPSets:', body.length, 'sets');
        sendJSON(res, { success: true });
      }
      return;
    }

    if (path === '/api/config/dhcp') {
      if (req.method === 'GET') {
        sendJSON(res, mockConfig.dhcp_server);
      } else if (req.method === 'POST') {
        const body = await parseBody(req);
        mockConfig.dhcp_server = body;
        console.log('Updated DHCP config');
        sendJSON(res, { success: true });
      }
      return;
    }

    if (path === '/api/config/dns') {
      if (req.method === 'GET') {
        sendJSON(res, mockConfig.dns_server);
      } else if (req.method === 'POST') {
        const body = await parseBody(req);
        mockConfig.dns_server = body;
        console.log('Updated DNS config');
        sendJSON(res, { success: true });
      }
      return;
    }

    if (path === '/api/config/routes') {
      if (req.method === 'GET') {
        sendJSON(res, mockConfig.routes || []);
      } else if (req.method === 'POST') {
        const body = await parseBody(req);
        mockConfig.routes = body;
        console.log('Updated routes:', body.length, 'routes');
        sendJSON(res, { success: true });
      }
      return;
    }

    if (path === '/api/config/zones') {
      if (req.method === 'GET') {
        sendJSON(res, mockConfig.zones);
      } else if (req.method === 'POST') {
        const body = await parseBody(req);
        mockConfig.zones = body;
        console.log('Updated zones:', body.length, 'zones');
        sendJSON(res, { success: true });
      }
      return;
    }

    if (path === '/api/config/protection') {
      if (req.method === 'GET') {
        sendJSON(res, mockConfig.protection);
      } else if (req.method === 'POST') {
        const body = await parseBody(req);
        mockConfig.protection = body;
        console.log('Updated protection settings');
        sendJSON(res, { success: true });
      }
      return;
    }

    if (path === '/api/config/qos') {
      if (req.method === 'GET') {
        sendJSON(res, mockConfig.qos || { enabled: false });
      } else if (req.method === 'POST') {
        const body = await parseBody(req);
        mockConfig.qos = body;
        console.log('Updated QoS settings');
        sendJSON(res, { success: true });
      }
      return;
    }

    if (path === '/api/system/reboot' && req.method === 'POST') {
      console.log('REBOOT REQUESTED');
      sendJSON(res, { success: true, message: 'System will reboot' });
      return;
    }

    if (path === '/api/vlans' && req.method === 'POST') {
      const body = await parseBody(req);
      console.log('Create VLAN:', body);
      // Add to interfaces
      mockConfig.interfaces.push({
        Name: `${body.parent}.${body.vlan_id}`,
        Zone: body.zone || 'LAN',
        IPv4: body.ipv4 ? [body.ipv4] : [],
        Description: body.description || `VLAN ${body.vlan_id}`,
      });
      sendJSON(res, { success: true });
      return;
    }

    if (path === '/api/bonds' && req.method === 'POST') {
      const body = await parseBody(req);
      console.log('Create Bond:', body);
      mockConfig.interfaces.push({
        Name: body.name,
        Zone: body.zone || 'LAN',
        IPv4: body.ipv4 ? [body.ipv4] : [],
        Description: body.description || `Bond ${body.name}`,
      });
      sendJSON(res, { success: true });
      return;
    }

    if (path === '/api/topology') {
      sendJSON(res, [
        { name: 'USG-Pro', ip: '192.168.1.1', role: 'gateway', clients: 4 },
        { name: 'Switch-Core', ip: '192.168.1.2', role: 'switch', clients: 12 },
        { name: 'AP-Lounge', ip: '192.168.1.3', role: 'ap', clients: 3 },
      ]);
      return;
    }

    if (path === '/api/scanner/result') {
      sendJSON(res, [
        { ssid: 'Neighbor-Wifi', bssid: 'aa:bb:cc:dd:ee:01', signal: -80, channel: 6, security: 'WPA2' },
        { ssid: 'Public-Net', bssid: 'aa:bb:cc:dd:ee:02', signal: -65, channel: 1, security: 'Open' },
        { ssid: 'Glacic-Secure', bssid: '00:11:22:33:44:55', signal: -30, channel: 11, security: 'WPA3' },
      ]);
      return;
    }

    if (path === '/api/scanner/network' && req.method === 'POST') {
      console.log('Network scan started');
      sendJSON(res, { success: true, message: 'Scan started' });
      return;
    }

    if (path === '/api/scanner/status') {
      sendJSON(res, { scanning: false, last_scan: new Date().toISOString() });
      return;
    }

    if (path === '/api/vpn/peers') {
      sendJSON(res, [
        { name: 'Phone', public_key: 'J5a...9sF', endpoint: '203.0.113.10:59231', allowed_ips: '10.100.0.2/32', handshake: '2 mins ago' },
        { name: 'Laptop', public_key: 'K9s...2dD', endpoint: '198.51.100.22:42111', allowed_ips: '10.100.0.3/32', handshake: '15 secs ago' },
      ]);
      return;
    }

    // IP Forwarding toggle
    if (path === '/api/config/ip-forwarding' && req.method === 'POST') {
      const body = await parseBody(req);
      mockConfig.ip_forwarding = body.enabled;
      console.log('IP Forwarding:', body.enabled ? 'ENABLED' : 'DISABLED');
      sendJSON(res, { success: true });
      return;
    }

    // IPSets refresh
    if (path.startsWith('/api/ipsets') && url.searchParams.get('action') === 'refresh' && req.method === 'POST') {
      const setName = path.replace('/api/ipsets/', '').replace('/', '') || 'all';
      console.log(`IPSet refresh requested: ${setName}`);
      sendJSON(res, { success: true, refreshed: setName });
      return;
    }

    // VPN config
    if (path === '/api/config/vpn') {
      if (req.method === 'GET') {
        sendJSON(res, mockConfig.vpn || { enabled: false, peers: [] });
      } else if (req.method === 'POST') {
        const body = await parseBody(req);
        mockConfig.vpn = body;
        console.log('Updated VPN config');
        sendJSON(res, { success: true });
      }
      return;
    }

    // Interface update
    if (path === '/api/interfaces/update' && req.method === 'POST') {
      const body = await parseBody(req);
      console.log('Interface update:', body);
      // Update interface in mockConfig
      const idx = mockConfig.interfaces.findIndex(i => i.Name === body.name);
      if (idx >= 0) {
        mockConfig.interfaces[idx] = { ...mockConfig.interfaces[idx], ...body };
      }
      sendJSON(res, { success: true });
      return;
    }

    // 404 for unknown endpoints
    sendJSON(res, { error: 'Not found' }, 404);

  } catch (err) {
    console.error('Error:', err);
    sendJSON(res, { error: err.message }, 500);
  }
});

server.listen(PORT, () => {
  console.log(`Mock API server running at http://localhost:${PORT}`);
  console.log('');
  console.log('Test credentials: admin / admin123');
  console.log('');
});
