export function createNetbirdProvider(React) {
  const h = React.createElement;
  const API = '/api/extensions/netbird/api';
  const defaults = { managementUrl: 'https://api.netbird.io:443', hostname: '', allowDNS: false, allowRoutes: false, autoConnect: true };
  const labels = { connected: 'Connected', disconnected: 'Not connected', busy: 'Working…', notInstalled: 'Not installed' };
  const colors = { connected: '#4ade80', disconnected: '#a3a3a3', busy: '#fbbf24', notInstalled: '#f87171' };
  const refreshInterval = 5000;
  let cachedStatus = null;
  let cachedAt = 0;
  let statusRequest = null;
  let changing = false;

  async function request(path, body) {
    const response = await fetch(API + path, {
      method: body === undefined ? 'GET' : 'POST', credentials: 'same-origin', cache: 'no-store',
      headers: body === undefined ? {} : { 'Content-Type': 'application/json', 'X-NanoKVM-NetBird': '1' },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(body === undefined ? 15000 : 100000)
    });
    let data;
    try { data = await response.json(); } catch { throw new Error('The NetBird provider is unavailable. Check its installation.'); }
    if (!response.ok) {
      if (response.status === 401) { cachedStatus = null; cachedAt = 0; }
      throw new Error(data.error || 'NetBird request failed.');
    }
    return data;
  }

  function readStatus(force) {
    if (statusRequest) return statusRequest;
    if (!force && cachedStatus && Date.now() - cachedAt < refreshInterval) return Promise.resolve(cachedStatus);
    statusRequest = request('/status').then(data => {
      cachedStatus = data;
      cachedAt = Date.now();
      return data;
    }).finally(() => { statusRequest = null; });
    return statusRequest;
  }

  function useStatus() {
    const [status, setStatus] = React.useState(() => Date.now() - cachedAt < refreshInterval * 3 ? cachedStatus : null);
    const [loadError, setLoadError] = React.useState('');
    const [refreshing, setRefreshing] = React.useState(false);
    const alive = React.useRef(false);
    const pending = React.useRef(false);
    const refresh = React.useCallback(async (force = false) => {
      if (pending.current || changing) return;
      pending.current = true;
      if (alive.current) setRefreshing(true);
      try {
        const data = await readStatus(force);
        if (alive.current) { setStatus(data); setLoadError(''); }
      } catch (err) {
        if (alive.current) { setLoadError(err.message); if (!cachedStatus) setStatus(null); }
      } finally {
        pending.current = false;
        if (alive.current) setRefreshing(false);
      }
    }, []);
    React.useEffect(() => {
      alive.current = true;
      refresh();
      const whenVisible = () => { if (!document.hidden) refresh(); };
      const timer = setInterval(() => { if (!document.hidden) refresh(true); }, refreshInterval);
      document.addEventListener('visibilitychange', whenVisible);
      return () => { alive.current = false; clearInterval(timer); document.removeEventListener('visibilitychange', whenVisible); };
    }, [refresh]);
    return { status, loadError, refreshing, refresh };
  }

  function Icon({ size = 24 }) {
    return h('svg', { width: size, height: size, viewBox: '0 0 32 32', fill: 'none', 'aria-hidden': true },
      h('path', { d: 'M5 23V9l11 14V9l11 14V9', stroke: '#f59e0b', strokeWidth: 3.5, strokeLinecap: 'round', strokeLinejoin: 'round' }));
  }

  function Badge({ state }) {
    return h('span', { style: { color: colors[state] || '#f87171', fontSize: 12, display: 'inline-flex', alignItems: 'center', gap: 6 } },
      h('span', { style: { width: 6, height: 6, borderRadius: '50%', background: 'currentColor' } }), labels[state] || 'Unavailable');
  }

  function Status() {
    const { status, loadError } = useStatus();
    return h(Badge, { state: loadError ? 'unavailable' : status?.state || 'busy' });
  }

  function View({ onBack, setLocked }) {
    const { status, loadError, refreshing, refresh } = useStatus();
    const [form, setForm] = React.useState(() => status?.preferences || defaults);
    const [setupKey, setSetupKey] = React.useState('');
    const [otp, setOTP] = React.useState('');
    const [error, setError] = React.useState('');
    const [notice, setNotice] = React.useState('');
    const [busy, setBusy] = React.useState(false);
    const [confirmDisconnect, setConfirmDisconnect] = React.useState(false);
    const alive = React.useRef(false);
    const initialized = React.useRef(Boolean(status));

    React.useEffect(() => {
      alive.current = true;
      return () => { alive.current = false; };
    }, []);
    React.useEffect(() => {
      if (status && !initialized.current) { setForm(status.preferences); initialized.current = true; }
    }, [status]);
    React.useEffect(() => { setLocked(busy); return () => setLocked(false); }, [busy, setLocked]);

    async function act(action) {
      if (changing) return;
      changing = true;
      setBusy(true); setError(''); setNotice('');
      const body = action === 'connect' ? { ...form, setupKey, otp } : { otp };
      setSetupKey(''); setOTP('');
      try {
        // Finish any status read before the action takes the daemon's operation lock.
        if (statusRequest) await statusRequest.catch(() => {});
        await request('/' + action, body);
        if (alive.current) setNotice(action === 'connect' ? 'Connection requested. Checking NetBird…' : 'Disconnected. Your enrollment is saved for next time.');
      } catch (err) { if (alive.current) setError(err.message); }
      finally {
        changing = false;
        cachedStatus = null;
        cachedAt = 0;
        await refresh(true);
        if (alive.current) { setBusy(false); setConfirmDisconnect(false); }
      }
    }

    const connected = status?.state === 'connected';
    const disabled = busy || !status || status.state === 'busy' || status.state === 'notInstalled';
    const update = (key, value) => setForm(current => ({ ...current, [key]: value }));
    const inputStyle = { boxSizing: 'border-box', minWidth: 0, width: '100%', padding: '9px 12px', border: '1px solid #525252', borderRadius: 7, background: '#171717', color: '#f5f5f5', fontSize: 14, outlineOffset: 3 };
    const buttonStyle = { padding: '8px 16px', border: '1px solid #525252', borderRadius: 7, background: '#262626', color: '#f5f5f5', cursor: 'pointer' };
    const label = (title, input, help) => h('label', { style: { display: 'grid', gap: 6 } }, h('span', { style: { fontSize: 13, fontWeight: 600 } }, title), input, help && h('span', { style: { fontSize: 12, color: '#a3a3a3' } }, help));
    const checkbox = (key, title, help) => h('label', { style: { display: 'flex', gap: 10, alignItems: 'flex-start', cursor: 'pointer' } },
      h('input', { type: 'checkbox', checked: form[key], disabled: disabled || connected, onChange: event => update(key, event.target.checked), style: { marginTop: 4, accentColor: '#f59e0b' } }),
      h('span', null, h('span', { style: { display: 'block', fontSize: 13 } }, title), h('span', { style: { display: 'block', fontSize: 12, color: '#a3a3a3', marginTop: 2 } }, help)));

    return h('section', { 'aria-label': 'NetBird provider', style: { color: '#e5e5e5', maxWidth: 620, margin: '0 auto', display: 'grid', gap: 22 } },
      h('div', { style: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 16 } },
        h('div', { style: { display: 'flex', alignItems: 'center', gap: 12 } }, h(Icon, { size: 30 }), h('div', null, h('h2', { style: { fontSize: 20, fontWeight: 650, margin: 0 } }, 'NetBird'), h('div', { style: { fontSize: 12, color: '#a3a3a3', marginTop: 3 } }, 'Private remote access to your NanoKVM'))),
        h('button', { type: 'button', style: buttonStyle, disabled: busy, onClick: onBack }, 'Back')),
      h('div', { style: { padding: 16, borderRadius: 10, border: '1px solid #404040', background: '#262626' }, 'aria-live': 'polite', 'aria-busy': refreshing },
        h(Badge, { state: busy ? 'busy' : status?.state || 'busy' }),
        connected && h('div', { style: { display: 'grid', gap: 5, marginTop: 12 } },
          h('span', { style: { fontSize: 22, fontFamily: 'monospace', color: '#fafafa' } }, status.ip),
          h('span', { style: { fontSize: 12, color: '#a3a3a3' } }, [status.fqdn, `${status.peers}/${status.totalPeers} peers connected`, `NetBird ${status.version}`].filter(Boolean).join(' · ')),
          !status.signalConnected && h('span', { style: { color: '#fbbf24', fontSize: 12 } }, 'Signaling is reconnecting. Remote access may be unavailable.'))),
      h('form', { onSubmit: event => { event.preventDefault(); act('connect'); }, style: { display: 'grid', gap: 16 } },
        label('Management server', h('input', { type: 'url', required: true, 'aria-label': 'Management server', value: form.managementUrl, disabled: disabled || connected, onChange: e => update('managementUrl', e.target.value), style: inputStyle, spellCheck: false }), 'Use NetBird Cloud or the HTTPS address of your self-hosted NetBird server.'),
        label('Device name', h('input', { 'aria-label': 'Device name', value: form.hostname, maxLength: 63, placeholder: 'Use the NanoKVM hostname', disabled: disabled || connected, onChange: e => update('hostname', e.target.value), style: inputStyle, autoComplete: 'off', spellCheck: false })),
        !connected && label('Setup key', h('input', { type: 'password', 'aria-label': 'Setup key', value: setupKey, maxLength: 512, disabled, onChange: e => setSetupKey(e.target.value), style: inputStyle, autoComplete: 'off', placeholder: 'Paste a NetBird setup key' }), 'Required for first-time enrollment. Leave blank to reconnect. The plugin does not save this key.'),
        h('div', { style: { display: 'grid', gap: 13, padding: '4px 0' } },
          checkbox('autoConnect', 'Reconnect after reboot', 'Start NetBird automatically when this device starts.'),
          checkbox('allowDNS', 'Use NetBird DNS', 'Allow your NetBird network to manage device DNS.'),
          checkbox('allowRoutes', 'Accept remote subnet routes', 'Allow access to networks advertised by other NetBird peers.')),
        status?.needsOTP && label('NanoKVM two-factor code', h('input', { 'aria-label': 'NanoKVM two-factor code', value: otp, disabled: busy, onChange: e => setOTP(e.target.value), style: inputStyle, autoComplete: 'one-time-code', maxLength: 128 }), 'Your existing Remote Network protection also applies to NetBird.'),
        (error || loadError) && h('div', { role: 'alert', style: { padding: 12, borderRadius: 7, background: '#431e23', color: '#fca5a5', fontSize: 13 } }, error || loadError),
        notice && !error && h('div', { role: 'status', style: { fontSize: 13, color: '#a3a3a3' } }, notice),
        confirmDisconnect && h('div', { role: 'alert', style: { padding: 12, border: '1px solid #a16207', borderRadius: 7, fontSize: 13, color: '#fde68a' } },
          h('p', { style: { margin: '0 0 12px' } }, 'Disconnecting ends access through NetBird. Use the local network to reconnect.'),
          h('button', { type: 'button', disabled: busy, style: buttonStyle, onClick: () => act('disconnect') }, 'Confirm disconnect'), ' ',
          h('button', { type: 'button', disabled: busy, style: buttonStyle, onClick: () => setConfirmDisconnect(false) }, 'Cancel')),
        h('div', { style: { display: 'flex', gap: 10, alignItems: 'center' } },
          connected ? h('button', { type: 'button', disabled, style: buttonStyle, onClick: () => setConfirmDisconnect(true) }, 'Disconnect') :
            h('button', { type: 'submit', disabled, style: { ...buttonStyle, background: '#f59e0b', borderColor: '#f59e0b', color: '#171717', fontWeight: 600, opacity: disabled ? 0.6 : 1 } }, busy ? 'Connecting…' : 'Connect'),
          h('button', { type: 'button', disabled: busy || refreshing, style: buttonStyle, onClick: () => refresh(true) }, refreshing ? 'Refreshing…' : 'Refresh'))),
      h('p', { style: { margin: 0, fontSize: 12, color: '#a3a3a3', lineHeight: 1.6 } }, 'Connect your other device to the same NetBird network and allow access in your NetBird policy. Open this NanoKVM using its NetBird IP once connected.'));
  }

  return { id: 'netbird', name: 'NetBird', Icon, Status, View };
}
