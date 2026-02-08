package main

import "net/http"

const dashboardHTML = `<!DOCTYPE html>
<html>
<head>
    <title>Merge Queue Dashboard</title>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #0d1117; color: #c9d1d9; display: flex; height: 100vh; }

        /* Sidebar */
        .sidebar { width: 260px; background: #161b22; border-right: 1px solid #30363d; display: flex; flex-direction: column; flex-shrink: 0; }
        .sidebar-header { padding: 20px; border-bottom: 1px solid #30363d; }
        .sidebar-header h1 { font-size: 18px; color: #58a6ff; display: flex; align-items: center; gap: 8px; }
        .nav-section { padding: 10px 0; border-bottom: 1px solid #30363d; }
        .nav-item { padding: 10px 15px; cursor: pointer; display: flex; align-items: center; gap: 10px; text-decoration: none; color: #c9d1d9; }
        .nav-item:hover { background: #21262d; }
        .nav-item.active { background: #1f6feb22; border-left: 2px solid #58a6ff; color: #58a6ff; }
        .org-section { padding: 10px 0; border-bottom: 1px solid #30363d; }
        .org-header { padding: 8px 15px; font-size: 11px; text-transform: uppercase; color: #8b949e; font-weight: 600; display: flex; justify-content: space-between; align-items: center; }
        .org-name { padding: 8px 15px; cursor: pointer; display: flex; align-items: center; gap: 8px; }
        .org-name:hover { background: #21262d; }
        .org-name.active { background: #1f6feb22; border-left: 2px solid #58a6ff; }
        .org-icon { width: 20px; height: 20px; background: #30363d; border-radius: 4px; display: flex; align-items: center; justify-content: center; font-size: 10px; }
        .repo-list { max-height: calc(100vh - 200px); overflow-y: auto; }
        .repo-item { padding: 8px 15px 8px 43px; cursor: pointer; font-size: 14px; display: flex; align-items: center; gap: 8px; }
        .repo-item:hover { background: #21262d; }
        .repo-item.active { background: #1f6feb22; color: #58a6ff; }
        .repo-item .queue-badge { background: #1f6feb; color: white; font-size: 10px; padding: 2px 6px; border-radius: 10px; margin-left: auto; }
        .repo-item .ci-badge { background: #d29922; color: #0d1117; font-size: 10px; padding: 2px 6px; border-radius: 10px; margin-left: 4px; }
        .all-repos { font-style: italic; color: #8b949e; }

        /* Main content */
        .main { flex: 1; overflow-y: auto; padding: 20px 30px; }
        .main-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
        .main-header h2 { color: #c9d1d9; font-size: 20px; }
        .refresh-info { font-size: 12px; color: #8b949e; }

        .queue { background: #161b22; border-radius: 6px; border: 1px solid #30363d; overflow: hidden; }
        .queue-header { padding: 15px; background: #21262d; border-bottom: 1px solid #30363d; font-weight: 600; display: flex; justify-content: space-between; align-items: center; }
        .queue-count { font-size: 12px; color: #8b949e; font-weight: normal; }
        .queue-item { padding: 15px; border-bottom: 1px solid #30363d; display: flex; align-items: center; gap: 15px; }
        .queue-item:last-child { border-bottom: none; }
        .queue-item:hover { background: #1c2128; }
        .position { background: #30363d; padding: 5px 10px; border-radius: 4px; font-weight: 600; min-width: 30px; text-align: center; }
        .pr-info { flex: 1; }
        .pr-title { font-weight: 600; color: #58a6ff; }
        .pr-title a { color: inherit; text-decoration: none; }
        .pr-title a:hover { text-decoration: underline; }
        .pr-meta { font-size: 13px; color: #8b949e; margin-top: 4px; }
        .status { padding: 5px 12px; border-radius: 20px; font-size: 12px; font-weight: 600; text-transform: uppercase; white-space: nowrap; }
        .status.queued { background: #1f6feb33; color: #58a6ff; }
        .status.processing { background: #a371f733; color: #a371f7; }
        .status.rebasing { background: #a371f733; color: #a371f7; }
        .status.resolving_conflicts { background: #d29922; color: #0d1117; }
        .status.waiting_ci { background: #d29922; color: #0d1117; }
        .status.merging { background: #238636; color: white; }
        .status.merged { background: #238636; color: white; }
        .status.failed { background: #da3633; color: white; }
        .status.paused { background: #6e7681; color: white; }
        .status.cancelled { background: #6e7681; color: white; }
        .actions { display: flex; gap: 5px; }
        .actions button { padding: 5px 10px; border-radius: 4px; border: 1px solid #30363d; background: #21262d; color: #c9d1d9; cursor: pointer; font-size: 12px; }
        .actions button:hover { background: #30363d; }
        .actions button.danger { border-color: #da3633; color: #f85149; }
        .actions button.danger:hover { background: #da363322; }

        .events { margin-top: 30px; }
        .events h3 { margin-bottom: 15px; color: #c9d1d9; font-size: 16px; }
        .event { padding: 10px 15px; border-left: 3px solid #30363d; margin-bottom: 8px; background: #161b22; border-radius: 0 6px 6px 0; }
        .event.merged, .event.conflicts_resolved { border-left-color: #238636; }
        .event.failed, .event.conflicts_unresolved { border-left-color: #da3633; }
        .event.conflicts_detected { border-left-color: #d29922; }
        .event-type { font-weight: 600; color: #58a6ff; }
        .event-time { font-size: 12px; color: #8b949e; }
        .event-repo { font-size: 12px; color: #8b949e; }

        .empty { padding: 40px; text-align: center; color: #8b949e; }
        .error { padding: 10px; background: #da363322; border: 1px solid #da3633; border-radius: 6px; color: #f85149; margin-bottom: 20px; }
    </style>
</head>
<body>
    <div class="sidebar">
        <div class="sidebar-header">
            <h1>🚦 Merge Queue</h1>
        </div>
        <div class="nav-section">
            <a href="/" class="nav-item active">🚦 Merge Queue</a>
            <a href="/runners" class="nav-item">🖥️ Runners</a>
            <a href="/workflows" class="nav-item">⚡ Workflows</a>
        </div>
        <div class="org-section">
            <div class="org-header">Organizations</div>
            <div id="org-list"></div>
        </div>
        <div class="org-section" style="flex: 1;">
            <div class="org-header">
                <span>Repositories</span>
                <span id="repo-count"></span>
            </div>
            <div id="repo-list" class="repo-list"></div>
        </div>
    </div>

    <div class="main">
        <div class="main-header">
            <h2 id="current-view">Select a repository</h2>
            <span class="refresh-info">Auto-refreshes every 5s</span>
        </div>

        <div id="error" class="error" style="display: none;"></div>

        <div class="queue">
            <div class="queue-header">
                <span>Queue</span>
                <span class="queue-count" id="queue-count"></span>
            </div>
            <div id="queue-items">
                <div class="empty">Select a repository from the sidebar</div>
            </div>
        </div>

        <div class="events">
            <h3>Recent Events</h3>
            <div id="events">
                <div class="empty">No events yet</div>
            </div>
        </div>
    </div>

    <script>
        let refreshInterval;
        let orgs = [];
        let reposByOrg = {};
        let queueCounts = {};  // repo -> count of queued items
        let ciPending = {};    // repo -> count of waiting_ci items
        let currentOrg = localStorage.getItem('mq_org') || '';
        let currentRepo = localStorage.getItem('mq_repo') || '';

        async function init() {
            try {
                // Load installations (orgs)
                const res = await fetch('/api/v1/installations');
                const data = await res.json();
                orgs = (data.installations || []).map(i => i.owner_login);

                renderSidebar();

                // Restore last selection
                if (currentOrg && orgs.includes(currentOrg)) {
                    await selectOrg(currentOrg, false);
                    if (currentRepo) {
                        selectRepo(currentRepo);
                    }
                } else if (orgs.length > 0) {
                    await selectOrg(orgs[0], true);
                }
            } catch (err) {
                console.error('Init error:', err);
                showError('Failed to initialize: ' + err.message);
            }
        }

        function renderSidebar() {
            const orgList = document.getElementById('org-list');
            orgList.innerHTML = orgs.map(org =>
                '<div class="org-name' + (org === currentOrg ? ' active' : '') + '" onclick="selectOrg(\'' + org + '\')">' +
                    '<span class="org-icon">' + org.charAt(0).toUpperCase() + '</span>' +
                    '<span>' + org + '</span>' +
                '</div>'
            ).join('');
        }

        function renderRepoList() {
            const repoList = document.getElementById('repo-list');
            const repos = reposByOrg[currentOrg] ? Array.from(reposByOrg[currentOrg]).sort() : [];

            document.getElementById('repo-count').textContent = repos.length;

            let html = '<div class="repo-item all-repos' + (currentRepo === '__all__' ? ' active' : '') + '" onclick="selectRepo(\'__all__\')">All repositories</div>';

            html += repos.map(repo => {
                const count = queueCounts[repo] || 0;
                const ci = ciPending[repo] || 0;
                let badges = '';
                if (count > 0) badges += '<span class="queue-badge">' + count + '</span>';
                if (ci > 0) badges += '<span class="ci-badge">CI ' + ci + '</span>';
                return '<div class="repo-item' + (repo === currentRepo ? ' active' : '') + '" onclick="selectRepo(\'' + repo + '\')">' +
                    '<span>📁</span> ' + repo + badges +
                '</div>';
            }).join('');

            repoList.innerHTML = html;
        }

        async function selectOrg(org, autoSelectRepo = true) {
            currentOrg = org;
            localStorage.setItem('mq_org', org);
            renderSidebar();

            // Fetch repos from GitHub API
            try {
                const reposRes = await fetch('/api/v1/repos?owner=' + org);
                const reposData = await reposRes.json();
                reposByOrg[org] = new Set(reposData.repos || []);
            } catch (err) {
                console.error('Failed to load repos:', err);
                reposByOrg[org] = new Set();
            }

            renderRepoList();

            if (autoSelectRepo) {
                const repos = reposByOrg[org] ? Array.from(reposByOrg[org]) : [];
                if (repos.length > 0) {
                    selectRepo(repos[0]);
                } else {
                    selectRepo('__all__');
                }
            }
        }

        function selectRepo(repo) {
            currentRepo = repo;
            localStorage.setItem('mq_repo', repo);
            renderRepoList();

            const displayName = repo === '__all__' ? currentOrg + ' (all repos)' : currentOrg + '/' + repo;
            document.getElementById('current-view').textContent = displayName;

            loadQueue();
        }

        async function loadQueue() {
            if (!currentOrg) return;

            try {
                let queueUrl = '/api/v1/queue?owner=' + currentOrg;
                let eventsUrl = '/api/v1/events?owner=' + currentOrg;

                if (currentRepo && currentRepo !== '__all__') {
                    queueUrl += '&repo=' + currentRepo;
                    eventsUrl += '&repo=' + currentRepo;
                }

                const queueRes = await fetch(queueUrl);
                const queueData = await queueRes.json();
                const queueItems = queueData.queue || [];
                renderQueue(queueItems);

                // Update counts for sidebar badges and prefetch CI status
                queueCounts = {};
                ciPending = {};
                queueItems.forEach(item => {
                    const key = item.repo;
                    queueCounts[key] = (queueCounts[key] || 0) + 1;
                    if (item.status === 'waiting_ci') {
                        ciPending[key] = (ciPending[key] || 0) + 1;
                        // Prefetch CI status for waiting items
                        fetchCIStatus(item.owner, item.repo, item.pr_branch);
                    }
                });

                const eventsRes = await fetch(eventsUrl);
                const eventsData = await eventsRes.json();
                renderEvents(eventsData.events || []);

                // Update repo list from events
                (eventsData.events || []).forEach(e => {
                    if (!reposByOrg[e.owner]) reposByOrg[e.owner] = new Set();
                    reposByOrg[e.owner].add(e.repo);
                });
                renderRepoList();

                // Clear CI status cache on refresh to get fresh data
                ciStatusCache = {};

                if (refreshInterval) clearInterval(refreshInterval);
                refreshInterval = setInterval(loadQueue, 5000);
            } catch (err) {
                showError('Failed to load: ' + err.message);
            }
        }

        function renderQueue(items) {
            const container = document.getElementById('queue-items');
            document.getElementById('queue-count').textContent = items.length + ' items';

            if (!items.length) {
                container.innerHTML = '<div class="empty">Queue is empty - PRs will appear here when opened</div>';
                return;
            }

            container.innerHTML = items.map(item => {
                const prUrl = 'https://github.com/' + item.owner + '/' + item.repo + '/pull/' + item.pr_number;
                let actions = '';

                if (item.status === 'failed') {
                    actions = '<button onclick="retryItem(\'' + item.id + '\')">Retry</button>' +
                              '<button onclick="pauseItem(\'' + item.id + '\')">Pause</button>' +
                              '<button class="danger" onclick="cancelItem(\'' + item.id + '\')">Cancel</button>';
                } else if (item.status === 'paused') {
                    actions = '<button onclick="retryItem(\'' + item.id + '\')">Resume</button>' +
                              '<button class="danger" onclick="cancelItem(\'' + item.id + '\')">Cancel</button>';
                } else if (item.status === 'queued') {
                    actions = '<button onclick="pauseItem(\'' + item.id + '\')">Pause</button>' +
                              '<button class="danger" onclick="cancelItem(\'' + item.id + '\')">Cancel</button>';
                }

                let statusInfo = '';
                if (item.status === 'failed' && item.next_retry_at) {
                    const retryTime = new Date(item.next_retry_at);
                    const now = new Date();
                    const secsUntil = Math.max(0, Math.floor((retryTime - now) / 1000));
                    if (secsUntil > 0) {
                        const mins = Math.floor(secsUntil / 60);
                        const secs = secsUntil % 60;
                        statusInfo = '<div class="pr-meta" style="color: #a371f7;">Auto-retry in ' + mins + 'm ' + secs + 's (attempt ' + (item.retry_count + 1) + '/' + item.max_retries + ')</div>';
                    }
                } else if (item.status === 'failed' && item.retry_count >= item.max_retries) {
                    statusInfo = '<div class="pr-meta" style="color: #f85149;">Max retries reached (' + item.retry_count + '/' + item.max_retries + ')</div>';
                } else if (item.status === 'waiting_ci') {
                    const waitTime = item.started_at ? Math.floor((new Date() - new Date(item.started_at)) / 1000) : 0;
                    const mins = Math.floor(waitTime / 60);
                    // Fetch CI status for this item
                    const ciKey = item.owner + '/' + item.repo + '/' + item.pr_branch;
                    const ciInfo = ciStatusCache[ciKey];
                    if (ciInfo) {
                        const runnerInfo = ciInfo.has_self_hosted ? '🖥️ Self-hosted' : '☁️ GitHub-hosted';
                        const checksInfo = ciInfo.passed_checks + '/' + ciInfo.total_checks + ' passed, ' + ciInfo.pending_checks + ' pending';
                        statusInfo = '<div class="pr-meta" style="color: #d29922;">⏳ ' + runnerInfo + ' · ' + checksInfo + ' (' + mins + 'm)</div>';
                        if (ciInfo.checks && ciInfo.checks.length > 0) {
                            const pendingChecks = ciInfo.checks.filter(c => c.status !== 'completed').map(c => c.name).slice(0, 3);
                            if (pendingChecks.length > 0) {
                                statusInfo += '<div class="pr-meta" style="color: #8b949e; font-size: 11px;">Running: ' + pendingChecks.join(', ') + '</div>';
                            }
                        }
                    } else {
                        statusInfo = '<div class="pr-meta" style="color: #d29922;">⏳ Waiting for CI checks... (' + mins + 'm elapsed)</div>';
                        // Trigger async fetch of CI status
                        fetchCIStatus(item.owner, item.repo, item.pr_branch);
                    }
                } else if (item.status === 'resolving_conflicts') {
                    statusInfo = '<div class="pr-meta" style="color: #d29922;">🔧 Resolving merge conflicts...</div>';
                } else if (item.status === 'rebasing') {
                    statusInfo = '<div class="pr-meta" style="color: #a371f7;">🔄 Rebasing branch...</div>';
                } else if (item.status === 'merging') {
                    statusInfo = '<div class="pr-meta" style="color: #238636;">🚀 Merging...</div>';
                }

                const repoLabel = currentRepo === '__all__' ? '<span style="color: #8b949e;">' + item.repo + '</span> · ' : '';

                return '<div class="queue-item">' +
                    '<span class="position">' + item.position + '</span>' +
                    '<div class="pr-info">' +
                        '<div class="pr-title"><a href="' + prUrl + '" target="_blank">' + repoLabel + '#' + item.pr_number + ' ' + (item.pr_title || 'Untitled') + '</a></div>' +
                        '<div class="pr-meta">' + item.pr_branch + ' → ' + item.base_branch + ' · by ' + (item.pr_author || 'unknown') + '</div>' +
                        (item.error_message ? '<div class="pr-meta" style="color: #f85149;">' + formatError(item.error_message) + '</div>' : '') +
                        statusInfo +
                    '</div>' +
                    '<span class="status ' + item.status + '">' + item.status.replace('_', ' ') + '</span>' +
                    '<div class="actions">' + actions + '</div>' +
                '</div>';
            }).join('');
        }

        function renderEvents(events) {
            const container = document.getElementById('events');
            if (!events.length) {
                container.innerHTML = '<div class="empty">No events yet</div>';
                return;
            }
            container.innerHTML = events.slice(0, 15).map(event => {
                const prUrl = 'https://github.com/' + event.owner + '/' + event.repo + '/pull/' + event.pr_number;
                const repoLabel = currentRepo === '__all__' ? ' in ' + event.repo : '';
                return '<div class="event ' + event.event_type + '">' +
                    '<span class="event-type">' + event.event_type.replace('_', ' ') + '</span> ' +
                    '<a href="' + prUrl + '" target="_blank" style="color: #58a6ff;">PR #' + event.pr_number + '</a>' + repoLabel +
                    '<div class="event-time">' + new Date(event.created_at).toLocaleString() + '</div>' +
                '</div>';
            }).join('');
        }

        async function retryItem(id) {
            await fetch('/api/v1/queue/' + id + '/retry', { method: 'POST' });
            loadQueue();
        }

        async function pauseItem(id) {
            await fetch('/api/v1/queue/' + id + '/pause', { method: 'POST' });
            loadQueue();
        }

        async function cancelItem(id) {
            if (confirm('Cancel this item from the queue?')) {
                await fetch('/api/v1/queue/' + id, { method: 'DELETE' });
                loadQueue();
            }
        }

        function showError(msg) {
            const el = document.getElementById('error');
            el.textContent = msg;
            el.style.display = 'block';
        }

        let ciStatusCache = {};

        async function fetchCIStatus(owner, repo, ref) {
            const key = owner + '/' + repo + '/' + ref;
            if (ciStatusCache[key]) return;
            try {
                const res = await fetch('/api/v1/ci-status?owner=' + owner + '&repo=' + repo + '&ref=' + ref);
                if (res.ok) {
                    ciStatusCache[key] = await res.json();
                }
            } catch (err) {
                console.error('Failed to fetch CI status:', err);
            }
        }

        function formatError(msg) {
            // Parse common merge failure reasons
            let reasons = [];

            if (msg.includes('approving review')) {
                reasons.push('👤 Review required');
            }
            if (msg.includes('status checks')) {
                const match = msg.match(/(\d+) of (\d+) required status checks/);
                if (match) {
                    reasons.push('🔴 CI: ' + match[1] + '/' + match[2] + ' checks pending');
                } else {
                    reasons.push('🔴 CI checks required');
                }
            }
            if (msg.includes('conflict')) {
                reasons.push('⚠️ Merge conflicts');
            }
            if (msg.includes('not mergeable')) {
                reasons.push('⚠️ Not mergeable');
            }
            if (msg.includes('CI failed')) {
                reasons.push('❌ CI failed');
            }
            if (msg.includes('CI timeout')) {
                reasons.push('⏱️ CI timeout');
            }

            if (reasons.length > 0) {
                return reasons.join(' · ');
            }

            // Fallback to truncated message
            return 'Error: ' + msg.substring(0, 80) + (msg.length > 80 ? '...' : '');
        }

        init();
    </script>
</body>
</html>`

func (h *Handlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(dashboardHTML))
}
