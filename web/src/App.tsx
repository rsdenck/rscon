import { useCallback, useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { api, ApiError, duration, formatDate } from './api'
import type {
  AccessRequest,
  AuditEvent,
  Dashboard,
  Environment,
  Group,
  Page,
  Policy,
  Role,
  Server,
  Session,
  Token,
  User,
} from './types'

type Notify = (message: string, kind?: 'ok' | 'error') => void

const nav = [
  { key: 'dashboard', label: 'Overview', permission: '' },
  { key: 'servers', label: 'Destinations', permission: 'server.read' },
  { key: 'access', label: 'Just-in-time', permission: 'access.read' },
  { key: 'sessions', label: 'Sessions', permission: 'session.read' },
  { key: 'audit', label: 'Audit', permission: 'audit.read' },
  { key: 'policies', label: 'Policies', permission: 'policy.read' },
  { key: 'users', label: 'People', permission: 'user.read' },
  { key: 'tokens', label: 'API tokens', permission: '' },
]

function hasPermission(permissions: string[], action: string): boolean {
  if (!action) return true
  if (permissions.includes('*') || permissions.includes(action)) return true
  return permissions.some((p) => p.endsWith('*') && action.startsWith(p.slice(0, -1)))
}

function statusBadge(status: string) {
  const tone =
    status === 'active' || status === 'approved' || status === 'success' || status === 'closed'
      ? 'bg-emerald-950 text-emerald-300'
      : status === 'pending'
        ? 'bg-amber-950 text-amber-300'
        : status === 'denied' || status === 'failed' || status === 'disabled'
          ? 'bg-red-950 text-red-300'
          : 'bg-slate-800 text-slate-300'
  return <span className={`badge ${tone}`}>{status}</span>
}

export default function App() {
  const [session, setSession] = useState<{ loading: boolean; user?: User; permissions: string[]; roles: string[] }>({
    loading: true,
    permissions: [],
    roles: [],
  })
  const [page, setPage] = useState(() => window.location.hash.slice(1) || 'dashboard')
  const [toast, setToast] = useState<{ message: string; kind: 'ok' | 'error' } | null>(null)

  const notify = useCallback<Notify>((message, kind = 'ok') => {
    setToast({ message, kind })
    window.setTimeout(() => setToast(null), 5000)
  }, [])

  const loadSession = useCallback(async () => {
    try {
      const result = await api<{
        authenticated: boolean
        user?: User
        permissions?: string[]
        roles?: string[]
      }>('/api/v1/auth/session')
      setSession({
        loading: false,
        user: result.authenticated ? result.user : undefined,
        permissions: result.permissions ?? [],
        roles: result.roles ?? [],
      })
    } catch {
      setSession({ loading: false, permissions: [], roles: [] })
    }
  }, [])

  useEffect(() => {
    void loadSession()
    const onHash = () => setPage(window.location.hash.slice(1) || 'dashboard')
    window.addEventListener('hashchange', onHash)
    return () => window.removeEventListener('hashchange', onHash)
  }, [loadSession])

  if (session.loading) {
    return <div className="grid min-h-screen place-items-center text-slate-400">Loading RSCON…</div>
  }
  if (!session.user) {
    return <Login onSuccess={loadSession} notify={notify} toast={toast} />
  }

  const permissions = session.permissions
  const can = (action: string) => hasPermission(permissions, action)
  const navigate = (next: string) => {
    window.location.hash = next
    setPage(next)
  }
  const logout = async () => {
    await api('/api/v1/auth/logout', { method: 'POST' })
    await loadSession()
  }

  return (
    <div className="flex min-h-screen">
      <aside className="hidden w-64 shrink-0 border-r border-edge bg-panel/60 p-5 lg:block">
        <div className="mb-8 flex items-center gap-3">
          <div className="grid h-9 w-9 place-items-center rounded-lg bg-accent font-black text-ink">R</div>
          <div>
            <div className="font-semibold tracking-wide">RSCON</div>
            <div className="text-xs text-slate-500">Secure Infrastructure Access</div>
          </div>
        </div>
        <nav className="space-y-1">
          {nav
            .filter((item) => can(item.permission))
            .map((item) => (
              <button
                key={item.key}
                onClick={() => navigate(item.key)}
                className={`w-full rounded-lg px-3 py-2 text-left text-sm transition ${
                  page === item.key ? 'bg-accent/10 text-accent' : 'text-slate-400 hover:bg-slate-900 hover:text-slate-200'
                }`}
              >
                {item.label}
              </button>
            ))}
        </nav>
        <div className="mt-10 border-t border-edge pt-4 text-xs text-slate-500">
          <div className="truncate font-medium text-slate-300">{session.user.display_name}</div>
          <div className="truncate">{session.user.username}</div>
          <button onClick={() => void logout()} className="mt-3 text-slate-400 hover:text-slate-200">
            Sign out
          </button>
        </div>
      </aside>
      <main className="min-w-0 flex-1">
        <header className="flex items-center justify-between border-b border-edge bg-panel/30 px-6 py-4">
          <div>
            <div className="text-xs uppercase tracking-widest text-slate-500">Console</div>
            <h1 className="text-xl font-semibold capitalize">{page}</h1>
          </div>
          <div className="flex items-center gap-2">
            <span className="hidden text-xs text-slate-500 sm:block">{session.roles.join(', ') || 'no role'}</span>
            <span className="badge bg-emerald-950 text-emerald-300">session active</span>
          </div>
        </header>
        <div className="p-4 sm:p-6">
          {page === 'dashboard' && <DashboardPage navigate={navigate} />}
          {page === 'servers' && <ServersPage can={can} notify={notify} />}
          {page === 'access' && <AccessPage me={session.user} can={can} notify={notify} />}
          {page === 'sessions' && <SessionsPage can={can} notify={notify} />}
          {page === 'audit' && <AuditPage notify={notify} />}
          {page === 'policies' && <PoliciesPage can={can} notify={notify} />}
          {page === 'users' && <UsersPage can={can} notify={notify} />}
          {page === 'tokens' && <TokensPage notify={notify} />}
          {!nav.some((item) => item.key === page) && <div className="text-slate-400">Unknown page.</div>}
        </div>
      </main>
      {toast && (
        <div
          className={`fixed bottom-5 right-5 max-w-md rounded-lg border px-4 py-3 text-sm shadow-xl ${
            toast.kind === 'error'
              ? 'border-red-900 bg-red-950 text-red-200'
              : 'border-emerald-900 bg-emerald-950 text-emerald-200'
          }`}
        >
          {toast.message}
        </div>
      )}
    </div>
  )
}

function Login({ onSuccess, notify, toast }: { onSuccess: () => Promise<void>; notify: Notify; toast: { message: string; kind: string } | null }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    try {
      await api('/api/v1/auth/login', {
        method: 'POST',
        body: JSON.stringify({ username, password }),
      })
      await onSuccess()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'login failed', 'error')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="grid min-h-screen place-items-center p-6">
      <div className="card w-full max-w-md p-8">
        <div className="mb-8 flex items-center gap-3">
          <div className="grid h-11 w-11 place-items-center rounded-xl bg-accent text-xl font-black text-ink">R</div>
          <div>
            <div className="text-lg font-semibold">RSCON</div>
            <div className="text-xs text-slate-500">Secure Infrastructure Access</div>
          </div>
        </div>
        <form onSubmit={(event) => void submit(event)} className="space-y-4">
          <div>
            <label className="label" htmlFor="username">
              Username
            </label>
            <input
              id="username"
              className="input"
              autoComplete="username"
              value={username}
              onChange={(event) => setUsername(event.target.value)}
              required
            />
          </div>
          <div>
            <label className="label" htmlFor="password">
              Password
            </label>
            <input
              id="password"
              type="password"
              className="input"
              autoComplete="current-password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              required
            />
          </div>
          <button className="btn-primary w-full" disabled={busy}>
            {busy ? 'Signing in…' : 'Sign in'}
          </button>
        </form>
        <p className="mt-6 text-xs leading-relaxed text-slate-500">
          Local password authentication is the only authenticator in this build. SSO, LDAP and MFA are not yet
          implemented.
        </p>
        {toast?.kind === 'error' && <p className="mt-3 text-sm text-red-300">{toast.message}</p>}
      </div>
    </div>
  )
}

function DashboardPage({ navigate }: { navigate: (page: string) => void }) {
  const [counts, setCounts] = useState<Dashboard | null>(null)
  const [health, setHealth] = useState<Record<string, unknown> | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    Promise.all([
      api<Dashboard>('/api/v1/dashboard'),
      api<Record<string, unknown>>('/api/v1/health').catch(() => null),
    ])
      .then(([dash, status]) => {
        setCounts(dash)
        setHealth(status)
      })
      .catch((err: unknown) => setError(err instanceof Error ? err.message : 'failed to load overview'))
  }, [])

  const cards = [
    { label: 'People', value: counts?.users ?? 0, page: 'users' },
    { label: 'Destinations', value: counts?.servers ?? 0, page: 'servers' },
    { label: 'Active sessions', value: counts?.active_sessions ?? 0, page: 'sessions' },
    { label: 'Pending approvals', value: counts?.pending_access_requests ?? 0, page: 'access' },
    { label: 'Policies', value: counts?.policies ?? 0, page: 'policies' },
  ]

  return (
    <div className="space-y-6">
      {error && <div className="rounded-lg border border-red-900 bg-red-950/50 p-3 text-sm text-red-200">{error}</div>}
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
        {cards.map((card) => (
          <button key={card.label} onClick={() => navigate(card.page)} className="card p-4 text-left hover:border-slate-600">
            <div className="text-xs uppercase tracking-wide text-slate-500">{card.label}</div>
            <div className="mt-2 text-3xl font-semibold">{card.value}</div>
          </button>
        ))}
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        <div className="card p-5">
          <h2 className="mb-3 font-semibold">Platform status</h2>
          <dl className="space-y-2 text-sm">
            <div className="flex justify-between gap-4">
              <dt className="text-slate-500">Control plane</dt>
              <dd>{health ? String(health.status ?? 'unknown') : 'unavailable'}</dd>
            </div>
            <div className="flex justify-between gap-4">
              <dt className="text-slate-500">Database</dt>
              <dd>{health ? String(health.database ?? 'unknown') : 'unknown'}</dd>
            </div>
            <div className="flex justify-between gap-4">
              <dt className="text-slate-500">Certificate authority</dt>
              <dd>{health ? String(health.certificate_authority ?? 'unknown') : 'unknown'}</dd>
            </div>
            <div className="flex justify-between gap-4">
              <dt className="text-slate-500">Version</dt>
              <dd>{health ? String(health.version ?? '—') : '—'}</dd>
            </div>
          </dl>
        </div>
        <div className="card p-5">
          <h2 className="mb-3 font-semibold">Security invariants</h2>
          <ul className="space-y-2 text-sm text-slate-400">
            <li>Default deny: every SSH connection needs an explicit policy.</li>
            <li>Certificates are short-lived and bound to one destination and account.</li>
            <li>Audit writes are mandatory and hash-chained.</li>
            <li>Private keys never leave the client or Vault.</li>
          </ul>
        </div>
      </div>
    </div>
  )
}

function ServersPage({ can, notify }: { can: (action: string) => boolean; notify: Notify }) {
  const [servers, setServers] = useState<Server[]>([])
  const [environments, setEnvironments] = useState<Environment[]>([])
  const [search, setSearch] = useState('')
  const [error, setError] = useState('')
  const [showCreate, setShowCreate] = useState(false)

  const load = useCallback(async () => {
    try {
      const query = search ? `?search=${encodeURIComponent(search)}` : ''
      const [list, envs] = await Promise.all([
        api<Page<Server>>(`/api/v1/servers${query}`),
        api<{ items: Environment[] }>('/api/v1/environments'),
      ])
      setServers(list.items)
      setEnvironments(envs.items)
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'failed to load destinations')
    }
  }, [search])

  useEffect(() => {
    void load()
  }, [load])

  const toggle = async (server: Server) => {
    try {
      await api(`/api/v1/servers/${server.id}`, {
        method: 'PATCH',
        body: JSON.stringify({ enabled: !server.enabled }),
      })
      notify(`Destination ${server.name} ${server.enabled ? 'disabled' : 'enabled'}`)
      await load()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'update failed', 'error')
    }
  }

  const remove = async (server: Server) => {
    if (!window.confirm(`Delete destination ${server.name}?`)) return
    try {
      await api(`/api/v1/servers/${server.id}`, { method: 'DELETE' })
      notify('Destination removed')
      await load()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'delete failed', 'error')
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <input
          className="input max-w-xs"
          placeholder="Search name, hostname or address"
          value={search}
          onChange={(event) => setSearch(event.target.value)}
        />
        {can('server.manage') && (
          <button className="btn-primary" onClick={() => setShowCreate(true)}>
            Register destination
          </button>
        )}
      </div>
      <ErrorBanner message={error} />
      <div className="card overflow-x-auto">
        <table className="min-w-full divide-y divide-edge">
          <thead>
            <tr>
              <th className="th">Name</th>
              <th className="th">Address</th>
              <th className="th">Environment</th>
              <th className="th">Account</th>
              <th className="th">Host key</th>
              <th className="th">Status</th>
              <th className="th">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-edge">
            {servers.map((server) => (
              <tr key={server.id}>
                <td className="td font-medium text-slate-200">
                  {server.name}
                  <div className="text-xs text-slate-500">{server.os_family || 'Linux'}</div>
                </td>
                <td className="td font-mono text-xs">
                  {server.address}:{server.port}
                </td>
                <td className="td">{server.environment || server.environment_id}</td>
                <td className="td">{server.default_account || '—'}</td>
                <td className="td max-w-[14rem] truncate font-mono text-xs" title={server.host_key_fingerprint}>
                  {server.host_key_fingerprint ? 'pinned' : 'known_hosts / CA'}
                </td>
                <td className="td">{statusBadge(server.enabled ? 'active' : 'disabled')}</td>
                <td className="td space-x-2 whitespace-nowrap">
                  {can('server.manage') && (
                    <>
                      <button className="text-accent hover:underline" onClick={() => void toggle(server)}>
                        {server.enabled ? 'Disable' : 'Enable'}
                      </button>
                      <button className="text-red-300 hover:underline" onClick={() => void remove(server)}>
                        Delete
                      </button>
                    </>
                  )}
                </td>
              </tr>
            ))}
            {servers.length === 0 && (
              <tr>
                <td className="td text-slate-500" colSpan={7}>
                  No destinations registered.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {showCreate && (
        <CreateServerModal
          environments={environments}
          onClose={() => setShowCreate(false)}
          onCreated={async () => {
            setShowCreate(false)
            notify('Destination registered')
            await load()
          }}
          notify={notify}
        />
      )}
    </div>
  )
}

function CreateServerModal({
  environments,
  onClose,
  onCreated,
  notify,
}: {
  environments: Environment[]
  onClose: () => void
  onCreated: () => Promise<void>
  notify: Notify
}) {
  const [form, setForm] = useState({
    name: '',
    hostname: '',
    address: '',
    port: 22,
    environment_id: environments[0]?.id ?? '',
    os_family: 'linux',
    location: '',
    default_account: '',
    host_key_fingerprint: '',
  })
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (!form.environment_id && environments[0]) setForm((f) => ({ ...f, environment_id: environments[0].id }))
  }, [environments, form.environment_id])

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    try {
      await api('/api/v1/servers', {
        method: 'POST',
        body: JSON.stringify({
          ...form,
          hostname: form.hostname || form.name,
          address: form.address || form.hostname || form.name,
        }),
      })
      await onCreated()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'create failed', 'error')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal title="Register destination" onClose={onClose}>
      <form onSubmit={(event) => void submit(event)} className="space-y-3">
        <div className="grid gap-3 sm:grid-cols-2">
          <Input label="Name" value={form.name} onChange={(v) => setForm({ ...form, name: v })} required />
          <Input label="Hostname" value={form.hostname} onChange={(v) => setForm({ ...form, hostname: v })} />
          <Input label="Address" value={form.address} onChange={(v) => setForm({ ...form, address: v })} required />
          <Input
            label="Port"
            type="number"
            value={String(form.port)}
            onChange={(v) => setForm({ ...form, port: Number(v) })}
          />
          <div>
            <label className="label">Environment</label>
            <select
              className="input"
              value={form.environment_id}
              onChange={(event) => setForm({ ...form, environment_id: event.target.value })}
              required
            >
              {environments.map((env) => (
                <option key={env.id} value={env.id}>
                  {env.name}
                </option>
              ))}
            </select>
          </div>
          <Input
            label="Default account"
            value={form.default_account}
            onChange={(v) => setForm({ ...form, default_account: v })}
          />
          <Input label="Location" value={form.location} onChange={(v) => setForm({ ...form, location: v })} />
          <Input
            label="Host key fingerprint (optional)"
            placeholder="SHA256:…"
            value={form.host_key_fingerprint}
            onChange={(v) => setForm({ ...form, host_key_fingerprint: v })}
          />
        </div>
        <div className="flex justify-end gap-2 pt-2">
          <button type="button" className="btn-secondary" onClick={onClose}>
            Cancel
          </button>
          <button className="btn-primary" disabled={busy}>
            {busy ? 'Creating…' : 'Create'}
          </button>
        </div>
      </form>
    </Modal>
  )
}

function AccessPage({ me, can, notify }: { me: User; can: (action: string) => boolean; notify: Notify }) {
  const [requests, setRequests] = useState<AccessRequest[]>([])
  const [servers, setServers] = useState<Server[]>([])
  const [showCreate, setShowCreate] = useState(false)
  const [error, setError] = useState('')
  const [status, setStatus] = useState('')

  const load = useCallback(async () => {
    try {
      const query = status ? `?status=${encodeURIComponent(status)}` : ''
      const result = await api<Page<AccessRequest>>(`/api/v1/access-requests${query}`)
      setRequests(result.items)
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'failed to load requests')
    }
  }, [status])

  useEffect(() => {
    void load()
    api<Page<Server>>('/api/v1/servers?enabled=true')
      .then((result) => setServers(result.items))
      .catch(() => undefined)
  }, [load])

  const decide = async (request: AccessRequest, decision: 'approve' | 'deny') => {
    const comment = window.prompt(`${decision === 'approve' ? 'Approval' : 'Denial'} comment (optional)`) ?? ''
    try {
      await api(`/api/v1/access-requests/${request.id}/decision`, {
        method: 'POST',
        body: JSON.stringify({ decision, comment }),
      })
      notify(`Request ${decision}d`)
      await load()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'decision failed', 'error')
    }
  }

  const cancel = async (request: AccessRequest) => {
    try {
      await api(`/api/v1/access-requests/${request.id}/cancel`, { method: 'POST' })
      notify('Request canceled')
      await load()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'cancel failed', 'error')
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <select className="input max-w-xs" value={status} onChange={(event) => setStatus(event.target.value)}>
          <option value="">All statuses</option>
          {['pending', 'approved', 'denied', 'expired', 'canceled'].map((value) => (
            <option key={value} value={value}>
              {value}
            </option>
          ))}
        </select>
        {can('access.request') && (
          <button className="btn-primary" onClick={() => setShowCreate(true)}>
            Request access
          </button>
        )}
      </div>
      <ErrorBanner message={error} />
      <div className="card overflow-x-auto">
        <table className="min-w-full divide-y divide-edge">
          <thead>
            <tr>
              <th className="th">Requester</th>
              <th className="th">Destination</th>
              <th className="th">Account</th>
              <th className="th">Reason</th>
              <th className="th">Window</th>
              <th className="th">Approvals</th>
              <th className="th">Status</th>
              <th className="th">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-edge">
            {requests.map((request) => (
              <tr key={request.id}>
                <td className="td">{request.requester_name || request.requester_id}</td>
                <td className="td">{request.server_name || request.server_group_id || request.environment_id}</td>
                <td className="td font-mono text-xs">{request.account}</td>
                <td className="td max-w-xs truncate" title={request.reason}>
                  {request.reason}
                </td>
                <td className="td text-xs">
                  {formatDate(request.starts_at)} → {formatDate(request.ends_at)}
                </td>
                <td className="td">
                  {request.approvals_count}/{request.approvals_required}
                </td>
                <td className="td">{statusBadge(request.status)}</td>
                <td className="td space-x-2 whitespace-nowrap">
                  {request.status === 'pending' && can('access.approve') && (
                    <>
                      <button className="text-emerald-300 hover:underline" onClick={() => void decide(request, 'approve')}>
                        Approve
                      </button>
                      <button className="text-red-300 hover:underline" onClick={() => void decide(request, 'deny')}>
                        Deny
                      </button>
                    </>
                  )}
                  {request.status === 'pending' && request.requester_id === me.id && (
                    <button className="text-slate-300 hover:underline" onClick={() => void cancel(request)}>
                      Cancel
                    </button>
                  )}
                </td>
              </tr>
            ))}
            {requests.length === 0 && (
              <tr>
                <td className="td text-slate-500" colSpan={8}>
                  No access requests.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {showCreate && (
        <CreateAccessModal
          servers={servers}
          onClose={() => setShowCreate(false)}
          onCreated={async () => {
            setShowCreate(false)
            notify('Access requested')
            await load()
          }}
          notify={notify}
        />
      )}
    </div>
  )
}

function CreateAccessModal({
  servers,
  onClose,
  onCreated,
  notify,
}: {
  servers: Server[]
  onClose: () => void
  onCreated: () => Promise<void>
  notify: Notify
}) {
  const [serverID, setServerID] = useState(servers[0]?.id ?? '')
  const [account, setAccount] = useState(servers[0]?.default_account ?? '')
  const [reason, setReason] = useState('')
  const [ticket, setTicket] = useState('')
  const [minutes, setMinutes] = useState(60)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    const selected = servers.find((server) => server.id === serverID)
    if (selected?.default_account) setAccount(selected.default_account)
  }, [serverID, servers])

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    try {
      await api('/api/v1/access-requests', {
        method: 'POST',
        body: JSON.stringify({
          server_id: serverID,
          account,
          reason,
          ticket,
          requested_seconds: minutes * 60,
        }),
      })
      await onCreated()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'request failed', 'error')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal title="Request just-in-time access" onClose={onClose}>
      <form onSubmit={(event) => void submit(event)} className="space-y-3">
        <div>
          <label className="label">Destination</label>
          <select className="input" value={serverID} onChange={(event) => setServerID(event.target.value)} required>
            {servers.map((server) => (
              <option key={server.id} value={server.id}>
                {server.name}
              </option>
            ))}
          </select>
        </div>
        <Input label="Linux account" value={account} onChange={setAccount} required />
        <Input label="Reason" value={reason} onChange={setReason} required />
        <div className="grid gap-3 sm:grid-cols-2">
          <Input label="Ticket" value={ticket} onChange={setTicket} />
          <Input label="Minutes" type="number" value={String(minutes)} onChange={(v) => setMinutes(Number(v))} />
        </div>
        <div className="flex justify-end gap-2 pt-2">
          <button type="button" className="btn-secondary" onClick={onClose}>
            Cancel
          </button>
          <button className="btn-primary" disabled={busy || !serverID}>
            {busy ? 'Submitting…' : 'Submit request'}
          </button>
        </div>
      </form>
    </Modal>
  )
}

function SessionsPage({ can, notify }: { can: (action: string) => boolean; notify: Notify }) {
  const [sessions, setSessions] = useState<Session[]>([])
  const [status, setStatus] = useState('')
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    try {
      const query = status ? `?status=${status}` : ''
      const result = await api<Page<Session>>(`/api/v1/sessions${query}`)
      setSessions(result.items)
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'failed to load sessions')
    }
  }, [status])

  useEffect(() => {
    void load()
    const timer = window.setInterval(() => void load(), 15000)
    return () => window.clearInterval(timer)
  }, [load])

  const terminate = async (session: Session) => {
    if (!window.confirm(`Terminate session ${session.id}?`)) return
    try {
      await api(`/api/v1/sessions/${session.id}/terminate`, { method: 'POST' })
      notify('Termination requested')
      await load()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'termination failed', 'error')
    }
  }

  return (
    <div className="space-y-4">
      <select className="input max-w-xs" value={status} onChange={(event) => setStatus(event.target.value)}>
        <option value="">All statuses</option>
        {['active', 'closed', 'failed', 'interrupted'].map((value) => (
          <option key={value} value={value}>
            {value}
          </option>
        ))}
      </select>
      <ErrorBanner message={error} />
      <div className="card overflow-x-auto">
        <table className="min-w-full divide-y divide-edge">
          <thead>
            <tr>
              <th className="th">User</th>
              <th className="th">Destination</th>
              <th className="th">Account</th>
              <th className="th">Source</th>
              <th className="th">Started</th>
              <th className="th">Duration</th>
              <th className="th">Status</th>
              <th className="th">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-edge">
            {sessions.map((session) => (
              <tr key={session.id}>
                <td className="td">{session.username || session.user_id}</td>
                <td className="td">{session.server_name || session.server_id}</td>
                <td className="td font-mono text-xs">{session.account}</td>
                <td className="td font-mono text-xs">{session.source_ip}</td>
                <td className="td text-xs">{formatDate(session.started_at)}</td>
                <td className="td">{duration(session.duration_seconds)}</td>
                <td className="td">{statusBadge(session.status)}</td>
                <td className="td space-x-2 whitespace-nowrap">
                  {session.status === 'active' && can('session.terminate') && (
                    <button className="text-red-300 hover:underline" onClick={() => void terminate(session)}>
                      Terminate
                    </button>
                  )}
                  {session.recording_id && can('recording.read') && (
                    <a
                      className="text-accent hover:underline"
                      href={`/api/v1/sessions/${session.id}/recording`}
                      download
                    >
                      Recording
                    </a>
                  )}
                </td>
              </tr>
            ))}
            {sessions.length === 0 && (
              <tr>
                <td className="td text-slate-500" colSpan={8}>
                  No sessions.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function AuditPage({ notify }: { notify: Notify }) {
  const [events, setEvents] = useState<AuditEvent[]>([])
  const [category, setCategory] = useState('')
  const [error, setError] = useState('')
  const [verification, setVerification] = useState<Record<string, unknown> | null>(null)

  const load = useCallback(async () => {
    try {
      const query = category ? `?category=${encodeURIComponent(category)}` : ''
      const result = await api<Page<AuditEvent>>(`/api/v1/audit${query}`)
      setEvents(result.items)
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'failed to load audit events')
    }
  }, [category])

  useEffect(() => {
    void load()
  }, [load])

  const verify = async () => {
    try {
      const result = await api<Record<string, unknown>>('/api/v1/audit/verify')
      setVerification(result)
      notify(result.valid ? 'Audit chain is valid' : 'Audit chain verification failed', result.valid ? 'ok' : 'error')
    } catch (err) {
      notify(err instanceof Error ? err.message : 'verification failed', 'error')
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <select className="input max-w-xs" value={category} onChange={(event) => setCategory(event.target.value)}>
          <option value="">All categories</option>
          {[
            'authentication',
            'authorization',
            'ssh',
            'session',
            'certificate',
            'admin',
            'approval',
            'audit',
            'security',
            'system',
          ].map((value) => (
            <option key={value} value={value}>
              {value}
            </option>
          ))}
        </select>
        <button className="btn-secondary" onClick={() => void verify()}>
          Verify hash chain
        </button>
      </div>
      {verification && (
        <div className="rounded-lg border border-edge bg-panel p-3 text-sm">
          Events: {String(verification.events)} · Valid: {String(verification.valid)}
          {verification.reason ? ` · ${String(verification.reason)}` : ''}
        </div>
      )}
      <ErrorBanner message={error} />
      <div className="card overflow-x-auto">
        <table className="min-w-full divide-y divide-edge">
          <thead>
            <tr>
              <th className="th">Time</th>
              <th className="th">Category</th>
              <th className="th">Action</th>
              <th className="th">Actor</th>
              <th className="th">Result</th>
              <th className="th">Message</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-edge">
            {events.map((event) => (
              <tr key={event.event_id}>
                <td className="td whitespace-nowrap text-xs">{formatDate(event.created_at)}</td>
                <td className="td">{event.category}</td>
                <td className="td font-mono text-xs">{event.action}</td>
                <td className="td">{event.actor_name || '—'}</td>
                <td className="td">{statusBadge(event.result)}</td>
                <td className="td max-w-md truncate" title={event.message}>
                  {event.message || '—'}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function PoliciesPage({ can, notify }: { can: (action: string) => boolean; notify: Notify }) {
  const [policies, setPolicies] = useState<Policy[]>([])
  const [error, setError] = useState('')
  const [showCreate, setShowCreate] = useState(false)

  const load = useCallback(async () => {
    try {
      const result = await api<{ items: Policy[] }>('/api/v1/policies')
      setPolicies(result.items)
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'failed to load policies')
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const toggle = async (policy: Policy) => {
    try {
      await api(`/api/v1/policies/${policy.id}`, {
        method: 'PATCH',
        body: JSON.stringify({ enabled: !policy.enabled }),
      })
      notify(`Policy ${policy.enabled ? 'disabled' : 'enabled'}`)
      await load()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'update failed', 'error')
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex justify-end">
        {can('policy.manage') && (
          <button className="btn-primary" onClick={() => setShowCreate(true)}>
            New policy
          </button>
        )}
      </div>
      <ErrorBanner message={error} />
      <div className="card overflow-x-auto">
        <table className="min-w-full divide-y divide-edge">
          <thead>
            <tr>
              <th className="th">Priority</th>
              <th className="th">Name</th>
              <th className="th">Effect</th>
              <th className="th">Subjects</th>
              <th className="th">Actions</th>
              <th className="th">Approval</th>
              <th className="th">Status</th>
              <th className="th">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-edge">
            {policies.map((policy) => (
              <tr key={policy.id}>
                <td className="td">{policy.priority}</td>
                <td className="td font-medium text-slate-200">{policy.name}</td>
                <td className="td">
                  <span className={`badge ${policy.effect === 'deny' ? 'bg-red-950 text-red-300' : 'bg-emerald-950 text-emerald-300'}`}>
                    {policy.effect}
                  </span>
                </td>
                <td className="td max-w-xs truncate font-mono text-xs">{policy.subjects.join(', ') || '*'}</td>
                <td className="td max-w-xs truncate font-mono text-xs">{policy.actions.join(', ')}</td>
                <td className="td text-xs">
                  {policy.require_approval ? `${policy.approvals_required || 1} approval(s)` : 'automatic'}
                </td>
                <td className="td">{statusBadge(policy.enabled ? 'active' : 'disabled')}</td>
                <td className="td">
                  {can('policy.manage') && (
                    <button className="text-accent hover:underline" onClick={() => void toggle(policy)}>
                      {policy.enabled ? 'Disable' : 'Enable'}
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {showCreate && (
        <CreatePolicyModal
          onClose={() => setShowCreate(false)}
          onCreated={async () => {
            setShowCreate(false)
            notify('Policy created')
            await load()
          }}
          notify={notify}
        />
      )}
    </div>
  )
}

function CreatePolicyModal({ onClose, onCreated, notify }: { onClose: () => void; onCreated: () => Promise<void>; notify: Notify }) {
  const [form, setForm] = useState({
    name: '',
    description: '',
    priority: 100,
    effect: 'allow',
    subjects: '*',
    actions: 'ssh.connect, access.request',
    server_ids: '',
    accounts: '',
    require_approval: true,
    approvals_required: 1,
    require_mfa: false,
    max_duration_seconds: 3600,
  })
  const [busy, setBusy] = useState(false)
  const split = (value: string) => value.split(',').map((v) => v.trim()).filter(Boolean)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    try {
      await api('/api/v1/policies', {
        method: 'POST',
        body: JSON.stringify({
          name: form.name,
          description: form.description,
          priority: Number(form.priority),
          effect: form.effect,
          subjects: split(form.subjects),
          actions: split(form.actions),
          server_ids: split(form.server_ids),
          accounts: split(form.accounts),
          require_approval: form.require_approval,
          approvals_required: Number(form.approvals_required),
          require_mfa: form.require_mfa,
          max_duration_seconds: Number(form.max_duration_seconds),
          enabled: true,
        }),
      })
      await onCreated()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'create failed', 'error')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal title="New access policy" onClose={onClose} wide>
      <form onSubmit={(event) => void submit(event)} className="space-y-3">
        <div className="grid gap-3 sm:grid-cols-2">
          <Input label="Name" value={form.name} onChange={(v) => setForm({ ...form, name: v })} required />
          <div>
            <label className="label">Effect</label>
            <select className="input" value={form.effect} onChange={(event) => setForm({ ...form, effect: event.target.value })}>
              <option value="allow">allow</option>
              <option value="deny">deny</option>
            </select>
          </div>
        </div>
        <Input label="Description" value={form.description} onChange={(v) => setForm({ ...form, description: v })} />
        <div className="grid gap-3 sm:grid-cols-3">
          <Input label="Priority" type="number" value={String(form.priority)} onChange={(v) => setForm({ ...form, priority: Number(v) })} />
          <Input label="Max duration (s)" type="number" value={String(form.max_duration_seconds)} onChange={(v) => setForm({ ...form, max_duration_seconds: Number(v) })} />
          <Input label="Approvals required" type="number" value={String(form.approvals_required)} onChange={(v) => setForm({ ...form, approvals_required: Number(v) })} />
        </div>
        <Input label="Subjects (user:id, group:id, role:name or *)" value={form.subjects} onChange={(v) => setForm({ ...form, subjects: v })} />
        <Input label="Actions" value={form.actions} onChange={(v) => setForm({ ...form, actions: v })} />
        <div className="grid gap-3 sm:grid-cols-2">
          <Input label="Server IDs (optional)" value={form.server_ids} onChange={(v) => setForm({ ...form, server_ids: v })} />
          <Input label="Accounts (optional)" value={form.accounts} onChange={(v) => setForm({ ...form, accounts: v })} />
        </div>
        <div className="flex flex-wrap gap-6 pt-1 text-sm">
          <label className="flex items-center gap-2">
            <input type="checkbox" checked={form.require_approval} onChange={(event) => setForm({ ...form, require_approval: event.target.checked })} />
            Require approval
          </label>
          <label className="flex items-center gap-2">
            <input type="checkbox" checked={form.require_mfa} onChange={(event) => setForm({ ...form, require_mfa: event.target.checked })} />
            Require MFA
          </label>
        </div>
        <p className="text-xs text-slate-500">
          MFA is not implemented in this build; enabling it makes the policy deny access until MFA is enrolled.
        </p>
        <div className="flex justify-end gap-2 pt-2">
          <button type="button" className="btn-secondary" onClick={onClose}>
            Cancel
          </button>
          <button className="btn-primary" disabled={busy}>
            {busy ? 'Creating…' : 'Create policy'}
          </button>
        </div>
      </form>
    </Modal>
  )
}

function UsersPage({ can, notify }: { can: (action: string) => boolean; notify: Notify }) {
  const [users, setUsers] = useState<User[]>([])
  const [roles, setRoles] = useState<Role[]>([])
  const [groups, setGroups] = useState<Group[]>([])
  const [error, setError] = useState('')
  const [showCreate, setShowCreate] = useState(false)

  const load = useCallback(async () => {
    try {
      const [people, roleList, groupList] = await Promise.all([
        api<Page<User>>('/api/v1/users'),
        api<{ items: Role[] }>('/api/v1/roles').catch(() => ({ items: [] })),
        api<{ items: Group[] }>('/api/v1/groups').catch(() => ({ items: [] })),
      ])
      setUsers(people.items)
      setRoles(roleList.items)
      setGroups(groupList.items)
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'failed to load users')
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const toggle = async (user: User) => {
    try {
      await api(`/api/v1/users/${user.id}/status`, {
        method: 'POST',
        body: JSON.stringify({ status: user.status === 'active' ? 'disabled' : 'active' }),
      })
      notify('Account status updated')
      await load()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'update failed', 'error')
    }
  }

  const remove = async (user: User) => {
    if (!window.confirm(`Delete account ${user.username}?`)) return
    try {
      await api(`/api/v1/users/${user.id}`, { method: 'DELETE' })
      notify('Account deleted')
      await load()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'delete failed', 'error')
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="text-sm text-slate-500">
          {groups.length} group(s) · {roles.length} role(s)
        </div>
        {can('user.manage') && (
          <button className="btn-primary" onClick={() => setShowCreate(true)}>
            Create account
          </button>
        )}
      </div>
      <ErrorBanner message={error} />
      <div className="card overflow-x-auto">
        <table className="min-w-full divide-y divide-edge">
          <thead>
            <tr>
              <th className="th">Username</th>
              <th className="th">Display name</th>
              <th className="th">Email</th>
              <th className="th">Last login</th>
              <th className="th">Status</th>
              <th className="th">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-edge">
            {users.map((user) => (
              <tr key={user.id}>
                <td className="td font-mono text-xs">{user.username}</td>
                <td className="td">{user.display_name}</td>
                <td className="td">{user.email || '—'}</td>
                <td className="td text-xs">{formatDate(user.last_login_at)}</td>
                <td className="td">{statusBadge(user.status)}</td>
                <td className="td space-x-2 whitespace-nowrap">
                  {can('user.manage') && (
                    <>
                      <button className="text-accent hover:underline" onClick={() => void toggle(user)}>
                        {user.status === 'active' ? 'Disable' : 'Enable'}
                      </button>
                      <button className="text-red-300 hover:underline" onClick={() => void remove(user)}>
                        Delete
                      </button>
                    </>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {showCreate && (
        <CreateUserModal
          roles={roles}
          onClose={() => setShowCreate(false)}
          onCreated={async () => {
            setShowCreate(false)
            notify('Account created')
            await load()
          }}
          notify={notify}
        />
      )}
    </div>
  )
}

function CreateUserModal({
  roles,
  onClose,
  onCreated,
  notify,
}: {
  roles: Role[]
  onClose: () => void
  onCreated: () => Promise<void>
  notify: Notify
}) {
  const [form, setForm] = useState({ username: '', display_name: '', email: '', password: '', role: '' })
  const [busy, setBusy] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    try {
      await api('/api/v1/users', {
        method: 'POST',
        body: JSON.stringify({
          username: form.username,
          display_name: form.display_name,
          email: form.email,
          password: form.password,
          role_names: form.role ? [form.role] : [],
        }),
      })
      await onCreated()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'create failed', 'error')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal title="Create account" onClose={onClose}>
      <form onSubmit={(event) => void submit(event)} className="space-y-3">
        <Input label="Username" value={form.username} onChange={(v) => setForm({ ...form, username: v })} required />
        <Input label="Display name" value={form.display_name} onChange={(v) => setForm({ ...form, display_name: v })} />
        <Input label="Email" type="email" value={form.email} onChange={(v) => setForm({ ...form, email: v })} />
        <Input
          label="Initial password (min 12 chars)"
          type="password"
          value={form.password}
          onChange={(v) => setForm({ ...form, password: v })}
        />
        <div>
          <label className="label">Role</label>
          <select className="input" value={form.role} onChange={(event) => setForm({ ...form, role: event.target.value })}>
            <option value="">No role</option>
            {roles.map((role) => (
              <option key={role.id} value={role.name}>
                {role.name}
              </option>
            ))}
          </select>
        </div>
        <div className="flex justify-end gap-2 pt-2">
          <button type="button" className="btn-secondary" onClick={onClose}>
            Cancel
          </button>
          <button className="btn-primary" disabled={busy}>
            {busy ? 'Creating…' : 'Create'}
          </button>
        </div>
      </form>
    </Modal>
  )
}

function TokensPage({ notify }: { notify: Notify }) {
  const [tokens, setTokens] = useState<Token[]>([])
  const [created, setCreated] = useState('')
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    try {
      const result = await api<{ items: Token[] }>('/api/v1/tokens')
      setTokens(result.items)
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'failed to load tokens')
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const create = async () => {
    try {
      const result = await api<{ token: string }>('/api/v1/tokens', {
        method: 'POST',
        body: JSON.stringify({ expires_in_days: 90 }),
      })
      setCreated(result.token)
      notify('API token created')
      await load()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'create failed', 'error')
    }
  }

  const revoke = async (token: Token) => {
    if (!window.confirm('Revoke this token?')) return
    try {
      await api(`/api/v1/tokens/${token.id}`, { method: 'DELETE' })
      notify('Token revoked')
      await load()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'revoke failed', 'error')
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex justify-end">
        <button className="btn-primary" onClick={() => void create()}>
          Create API token
        </button>
      </div>
      {created && (
        <div className="rounded-lg border border-amber-900 bg-amber-950/40 p-4 text-sm text-amber-100">
          <div className="mb-2 font-semibold">Copy this token now; it is not shown again.</div>
          <code className="block break-all rounded bg-black/40 p-3 font-mono text-xs">{created}</code>
        </div>
      )}
      <ErrorBanner message={error} />
      <div className="card overflow-x-auto">
        <table className="min-w-full divide-y divide-edge">
          <thead>
            <tr>
              <th className="th">Kind</th>
              <th className="th">Created</th>
              <th className="th">Expires</th>
              <th className="th">Last used</th>
              <th className="th">Status</th>
              <th className="th">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-edge">
            {tokens.map((token) => (
              <tr key={token.id}>
                <td className="td">{token.kind}</td>
                <td className="td text-xs">{formatDate(token.created_at)}</td>
                <td className="td text-xs">{formatDate(token.expires_at)}</td>
                <td className="td text-xs">{formatDate(token.last_used_at)}</td>
                <td className="td">{statusBadge(token.revoked_at ? 'disabled' : 'active')}</td>
                <td className="td">
                  {!token.revoked_at && (
                    <button className="text-red-300 hover:underline" onClick={() => void revoke(token)}>
                      Revoke
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="text-xs text-slate-500">
        Use tokens as <code className="font-mono">Authorization: Bearer …</code> for the REST API. Token digests, not
        token values, are stored.
      </p>
    </div>
  )
}

function Modal({ title, onClose, children, wide }: { title: string; onClose: () => void; children: ReactNode; wide?: boolean }) {
  return (
    <div className="fixed inset-0 z-40 grid place-items-center bg-black/70 p-4" onMouseDown={onClose}>
      <div
        className={`card max-h-[90vh] w-full overflow-y-auto p-5 ${wide ? 'max-w-3xl' : 'max-w-xl'}`}
        onMouseDown={(event) => event.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">{title}</h2>
          <button className="text-slate-400 hover:text-slate-200" onClick={onClose} aria-label="Close">
            ✕
          </button>
        </div>
        {children}
      </div>
    </div>
  )
}

function Input({
  label,
  value,
  onChange,
  type = 'text',
  required,
  placeholder,
}: {
  label: string
  value: string
  onChange: (value: string) => void
  type?: string
  required?: boolean
  placeholder?: string
}) {
  return (
    <div>
      <label className="label">{label}</label>
      <input
        className="input"
        type={type}
        value={value}
        placeholder={placeholder}
        required={required}
        onChange={(event) => onChange(event.target.value)}
      />
    </div>
  )
}

function ErrorBanner({ message }: { message: string }) {
  if (!message) return null
  return <div className="rounded-lg border border-red-900 bg-red-950/50 p-3 text-sm text-red-200">{message}</div>
}

// Keep ApiError imported for consumers that need instanceof checks.
export { ApiError }
