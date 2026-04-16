import { useEffect, useState } from 'react'
import { Routes, Route, Link, Navigate, useNavigate } from 'react-router-dom'
import ProjectList from './pages/ProjectList'
import ProjectDetail from './pages/ProjectDetail'
import Login from './pages/Login'
import Setup from './pages/Setup'
import APIKeys from './pages/APIKeys'
import type { User, Notification, SearchResult } from './types'
import { getMe, logout, getUnreadCount, listNotifications, markNotificationRead, markNotificationUnread, markAllNotificationsRead, search, checkNeedsSetup } from './api/client'
import type { SearchFilters } from './api/client'

function App() {
  const navigate = useNavigate()
  const [token, setToken] = useState<string>(() => localStorage.getItem('anpm_token') || '')
  const [me, setMe] = useState<User | null>(null)
  const [checkingAuth, setCheckingAuth] = useState(true)
  const [needsSetup, setNeedsSetup] = useState(false)
  const [unreadCount, setUnreadCount] = useState(0)
  const [notifications, setNotifications] = useState<Notification[]>([])
  const [showNotifications, setShowNotifications] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [searchFilters, setSearchFilters] = useState<SearchFilters>({
    type: 'all',
    staleness: 'all',
  })
  const [searchResult, setSearchResult] = useState<SearchResult | null>(null)

  useEffect(() => {
    let mounted = true
    async function bootstrap() {
      // Always check setup state first (fast, public endpoint)
      try {
        const setupResp = await checkNeedsSetup()
        if (mounted) {
          setNeedsSetup(setupResp.data.needs_setup)
        }
        if (mounted && setupResp.data.needs_setup) {
          setCheckingAuth(false)
          return
        }
      } catch {
        // ignore — proceed to normal auth check
      }

      if (!token) {
        if (mounted) {
          setMe(null)
          setCheckingAuth(false)
        }
        return
      }
      try {
        const meResp = await getMe()
        if (mounted) setMe(meResp.data)

        const countResp = await getUnreadCount()
        if (mounted) setUnreadCount((countResp.data as { unread?: number }).unread ?? 0)
      } catch {
        localStorage.removeItem('anpm_token')
        if (mounted) {
          setToken('')
          setMe(null)
        }
      } finally {
        if (mounted) setCheckingAuth(false)
      }
    }
    bootstrap()
    return () => {
      mounted = false
    }
  }, [token])

  async function handleLogin(nextToken: string) {
    localStorage.setItem('anpm_token', nextToken)
    setToken(nextToken)
    navigate('/')
  }

  function handleSetupComplete() {
    setNeedsSetup(false)
    localStorage.removeItem('anpm_token')
    setToken('')
    setMe(null)
    navigate('/login')
  }

  function handleSetupRequired() {
    setNeedsSetup(true)
    navigate('/setup')
  }

  async function handleLogout() {
    try {
      await logout()
    } catch {
      // noop
    }
    localStorage.removeItem('anpm_token')
    setToken('')
    setMe(null)
    navigate('/login')
  }

  async function handleNotificationsToggle() {
    const next = !showNotifications
    setShowNotifications(next)
    if (next) {
      try {
        const resp = await listNotifications(false)
        setNotifications(resp.data)
      } catch {
        setNotifications([])
      }
    }
  }

  async function handleToggleRead(n: Notification) {
    try {
      if (n.is_read) {
        await markNotificationUnread(n.id)
        setNotifications(prev => prev.map(x => x.id === n.id ? { ...x, is_read: false } : x))
        setUnreadCount(c => c + 1)
      } else {
        await markNotificationRead(n.id)
        setNotifications(prev => prev.map(x => x.id === n.id ? { ...x, is_read: true } : x))
        setUnreadCount(c => Math.max(0, c - 1))
      }
    } catch {
      // ignore toggle errors
    }
  }

  async function handleMarkAllRead() {
    try {
      await markAllNotificationsRead()
      setNotifications(prev => prev.map(x => ({ ...x, is_read: true })))
      setUnreadCount(0)
    } catch {
      // ignore
    }
  }

  async function handleSearchSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!searchQuery.trim()) {
      setSearchResult(null)
      return
    }
    try {
      const resp = await search(searchQuery.trim(), searchFilters)
      setSearchResult(resp.data)
    } catch {
      setSearchResult({ tasks: [], documents: [] })
    }
  }

  if (checkingAuth) return <div className="loading">Loading…</div>

  if (needsSetup) {
    return (
      <main className="container">
        <Routes>
          <Route path="/setup" element={<Setup onSetupComplete={handleSetupComplete} />} />
          <Route path="*" element={<Navigate to="/setup" replace />} />
        </Routes>
      </main>
    )
  }

  if (!me) {
    return (
      <main className="container">
        <Routes>
          <Route path="/login" element={<Login onLogin={handleLogin} onSetupRequired={handleSetupRequired} />} />
          <Route path="/setup" element={<Setup onSetupComplete={handleSetupComplete} />} />
          <Route path="*" element={<Navigate to="/login" replace />} />
        </Routes>
      </main>
    )
  }

  return (
    <>
      <header className="header">
        <div className="container header-inner">
          <h1><Link to="/" style={{ color: 'inherit' }}>Agent Native PM</Link></h1>
          <form className="header-search" onSubmit={handleSearchSubmit}>
            <input
              value={searchQuery}
              onChange={e => setSearchQuery(e.target.value)}
              placeholder="Search tasks/docs"
            />
            <select
              value={searchFilters.type ?? 'all'}
              onChange={e => setSearchFilters(prev => ({ ...prev, type: e.target.value as SearchFilters['type'] }))}
            >
              <option value="all">All</option>
              <option value="tasks">Tasks</option>
              <option value="documents">Documents</option>
            </select>
            <select
              value={searchFilters.status ?? ''}
              onChange={e => setSearchFilters(prev => ({ ...prev, status: (e.target.value || undefined) as SearchFilters['status'] }))}
            >
              <option value="">Any Status</option>
              <option value="todo">Todo</option>
              <option value="in_progress">In Progress</option>
              <option value="done">Done</option>
              <option value="cancelled">Cancelled</option>
            </select>
            <select
              value={searchFilters.docType ?? ''}
              onChange={e => setSearchFilters(prev => ({ ...prev, docType: (e.target.value || undefined) as SearchFilters['docType'] }))}
            >
              <option value="">Any Doc Type</option>
              <option value="api">API</option>
              <option value="architecture">Architecture</option>
              <option value="guide">Guide</option>
              <option value="adr">ADR</option>
              <option value="general">General</option>
            </select>
            <select
              value={searchFilters.staleness ?? 'all'}
              onChange={e => setSearchFilters(prev => ({ ...prev, staleness: e.target.value as SearchFilters['staleness'] }))}
            >
              <option value="all">All Freshness</option>
              <option value="stale">Stale Only</option>
              <option value="fresh">Fresh Only</option>
            </select>
            <button className="btn btn-sm btn-primary" type="submit">Search</button>
          </form>
          <nav>
            <Link to="/">Projects</Link>
            <Link to="/api-keys">API Keys</Link>
            <button className="notification-btn" onClick={handleNotificationsToggle}>
              Notifications {unreadCount > 0 && <span className="notification-badge">{unreadCount}</span>}
            </button>
            <button className="btn btn-ghost btn-sm" onClick={handleLogout}>Logout</button>
          </nav>
        </div>
      </header>

      {(searchResult || showNotifications) && (
        <div className="container" style={{ marginTop: '-1rem', marginBottom: '1rem' }}>
          {searchResult && (
            <div className="card">
              <h3>Search Results</h3>
              <p>Tasks: {searchResult.tasks.length} • Documents: {searchResult.documents.length}</p>
            </div>
          )}
          {showNotifications && (
            <div className="card">
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem' }}>
                <h3 style={{ margin: 0 }}>Notifications</h3>
                {notifications.some(n => !n.is_read) && (
                  <button className="btn btn-ghost btn-sm" onClick={handleMarkAllRead}>Mark All Read</button>
                )}
              </div>
              {notifications.length === 0 ? (
                <p>No notifications.</p>
              ) : (
                <ul style={{ listStyle: 'none', padding: 0, margin: 0 }}>
                  {notifications.slice(0, 20).map(n => (
                    <li key={n.id} style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', padding: '0.5rem 0', borderBottom: '1px solid var(--border)', opacity: n.is_read ? 0.6 : 1 }}>
                      <span style={{ flex: 1 }}>
                        {!n.is_read && <span style={{ display: 'inline-block', width: '6px', height: '6px', borderRadius: '50%', background: 'var(--info)', marginRight: '0.5rem', verticalAlign: 'middle' }} />}
                        {n.title}
                      </span>
                      <button className="btn btn-ghost btn-sm" onClick={() => handleToggleRead(n)} style={{ whiteSpace: 'nowrap' }}>
                        {n.is_read ? 'Mark Unread' : 'Mark Read'}
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}
        </div>
      )}

      <main className="container">
        <Routes>
          <Route path="/" element={<ProjectList />} />
          <Route path="/projects/:id" element={<ProjectDetail />} />
          <Route path="/api-keys" element={<APIKeys />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </main>
    </>
  )
}

export default App
