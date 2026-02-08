package main

import "net/http"

const workflowsHTML = `<!DOCTYPE html>
<html>
<head>
    <title>Workflow Runs</title>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #0d1117; color: #c9d1d9; display: flex; height: 100vh; }

        .sidebar { width: 260px; background: #161b22; border-right: 1px solid #30363d; display: flex; flex-direction: column; flex-shrink: 0; }
        .sidebar-header { padding: 20px; border-bottom: 1px solid #30363d; }
        .sidebar-header h1 { font-size: 18px; color: #58a6ff; display: flex; align-items: center; gap: 8px; }
        .nav-section { padding: 10px 0; border-bottom: 1px solid #30363d; }
        .nav-item { padding: 10px 15px; cursor: pointer; display: flex; align-items: center; gap: 10px; text-decoration: none; color: #c9d1d9; }
        .nav-item:hover { background: #21262d; }
        .nav-item.active { background: #1f6feb22; border-left: 2px solid #58a6ff; color: #58a6ff; }
        .org-section { padding: 10px 0; border-bottom: 1px solid #30363d; }
        .org-header { padding: 8px 15px; font-size: 11px; text-transform: uppercase; color: #8b949e; font-weight: 600; }
        .org-name { padding: 8px 15px; cursor: pointer; display: flex; align-items: center; gap: 8px; }
        .org-name:hover { background: #21262d; }
        .org-name.active { background: #1f6feb22; border-left: 2px solid #58a6ff; }
        .org-icon { width: 20px; height: 20px; background: #30363d; border-radius: 4px; display: flex; align-items: center; justify-content: center; font-size: 10px; }

        .main { flex: 1; overflow-y: auto; padding: 20px 30px; }
        .main-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
        .main-header h2 { color: #c9d1d9; font-size: 20px; }
        .refresh-btn { padding: 8px 16px; background: #21262d; border: 1px solid #30363d; color: #c9d1d9; border-radius: 6px; cursor: pointer; }
        .refresh-btn:hover { background: #30363d; }

        .filters { display: flex; gap: 10px; margin-bottom: 20px; flex-wrap: wrap; }
        .filter-btn { padding: 6px 12px; background: #21262d; border: 1px solid #30363d; color: #c9d1d9; border-radius: 20px; cursor: pointer; font-size: 13px; }
        .filter-btn:hover { background: #30363d; }
        .filter-btn.active { background: #1f6feb33; border-color: #1f6feb; color: #58a6ff; }

        .runs-list { background: #161b22; border-radius: 6px; border: 1px solid #30363d; overflow: hidden; }
        .run-item { padding: 12px 15px; border-bottom: 1px solid #30363d; display: flex; align-items: center; gap: 12px; }
        .run-item:last-child { border-bottom: none; }
        .run-item:hover { background: #1c2128; }

        .run-status { width: 16px; height: 16px; border-radius: 50%; flex-shrink: 0; }
        .run-status.success { background: #238636; }
        .run-status.failure { background: #da3633; }
        .run-status.cancelled { background: #6e7681; }
        .run-status.in_progress { background: #d29922; animation: pulse 1.5s infinite; }
        .run-status.queued { background: #6e7681; animation: pulse 1.5s infinite; }
        @keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.5; } }

        .run-info { flex: 1; min-width: 0; }
        .run-name { font-weight: 600; color: #c9d1d9; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
        .run-meta { font-size: 12px; color: #8b949e; margin-top: 2px; display: flex; gap: 10px; flex-wrap: wrap; }
        .run-repo { color: #58a6ff; }
        .run-event { padding: 2px 6px; background: #30363d; border-radius: 10px; font-size: 11px; }
        .run-event.pull_request { background: #1f6feb33; color: #58a6ff; }
        .run-event.push { background: #23863633; color: #3fb950; }
        .run-event.schedule { background: #a371f733; color: #a371f7; }
        .run-event.workflow_dispatch { background: #d2992233; color: #d29922; }

        .run-time { font-size: 12px; color: #8b949e; white-space: nowrap; }
        .run-link { padding: 5px 10px; background: #21262d; border: 1px solid #30363d; color: #c9d1d9; border-radius: 4px; font-size: 12px; text-decoration: none; }
        .run-link:hover { background: #30363d; }

        .empty { padding: 40px; text-align: center; color: #8b949e; }
        .loading { padding: 40px; text-align: center; color: #8b949e; }
    </style>
</head>
<body>
    <div class="sidebar">
        <div class="sidebar-header">
            <h1>⚡ Workflows</h1>
        </div>
        <div class="nav-section">
            <a href="/" class="nav-item">🚦 Merge Queue</a>
            <a href="/runners" class="nav-item">🖥️ Runners</a>
            <a href="/workflows" class="nav-item active">⚡ Workflows</a>
        </div>
        <div class="org-section">
            <div class="org-header">Organizations</div>
            <div id="org-list"></div>
        </div>
    </div>

    <div class="main">
        <div class="main-header">
            <h2 id="current-view">Workflow Runs</h2>
            <button class="refresh-btn" onclick="loadWorkflows()">↻ Refresh</button>
        </div>

        <div class="filters">
            <button class="filter-btn active" data-filter="all" onclick="setFilter('all')">All</button>
            <button class="filter-btn" data-filter="in_progress" onclick="setFilter('in_progress')">Running</button>
            <button class="filter-btn" data-filter="success" onclick="setFilter('success')">Success</button>
            <button class="filter-btn" data-filter="failure" onclick="setFilter('failure')">Failed</button>
        </div>

        <div class="runs-list" id="runs-list">
            <div class="loading">Loading workflow runs...</div>
        </div>
    </div>

    <script>
        let orgs = [];
        let runs = [];
        let currentOrg = localStorage.getItem('workflows_org') || '';
        let currentFilter = 'all';
        let refreshInterval;

        async function init() {
            try {
                const res = await fetch('/api/v1/installations');
                const data = await res.json();
                orgs = (data.installations || []).map(i => i.owner_login);

                renderOrgList();

                if (currentOrg && orgs.includes(currentOrg)) {
                    await selectOrg(currentOrg);
                } else if (orgs.length > 0) {
                    await selectOrg(orgs[0]);
                }
            } catch (err) {
                console.error('Init error:', err);
                document.getElementById('runs-list').innerHTML = '<div class="empty">Failed to initialize: ' + err.message + '</div>';
            }
        }

        function renderOrgList() {
            const orgList = document.getElementById('org-list');
            orgList.innerHTML = orgs.map(org =>
                '<div class="org-name' + (org === currentOrg ? ' active' : '') + '" onclick="selectOrg(\'' + org + '\')">' +
                    '<span class="org-icon">' + org.charAt(0).toUpperCase() + '</span>' +
                    '<span>' + org + '</span>' +
                '</div>'
            ).join('');
        }

        async function selectOrg(org) {
            currentOrg = org;
            localStorage.setItem('workflows_org', org);
            renderOrgList();
            document.getElementById('current-view').textContent = org + ' - Workflow Runs';
            await loadWorkflows();
        }

        async function loadWorkflows() {
            if (!currentOrg) return;

            document.getElementById('runs-list').innerHTML = '<div class="loading">Loading...</div>';

            try {
                const res = await fetch('/api/v1/workflows?owner=' + currentOrg);
                const data = await res.json();
                runs = data.runs || [];
                renderRuns();

                if (refreshInterval) clearInterval(refreshInterval);
                refreshInterval = setInterval(loadWorkflows, 10000);
            } catch (err) {
                document.getElementById('runs-list').innerHTML = '<div class="empty">Failed to load workflows: ' + err.message + '</div>';
            }
        }

        function setFilter(filter) {
            currentFilter = filter;
            document.querySelectorAll('.filter-btn').forEach(btn => {
                btn.classList.toggle('active', btn.dataset.filter === filter);
            });
            renderRuns();
        }

        function renderRuns() {
            const list = document.getElementById('runs-list');

            let filtered = runs;
            if (currentFilter === 'in_progress') {
                filtered = runs.filter(r => r.status === 'in_progress' || r.status === 'queued');
            } else if (currentFilter !== 'all') {
                filtered = runs.filter(r => r.conclusion === currentFilter);
            }

            if (!filtered.length) {
                list.innerHTML = '<div class="empty">No workflow runs found</div>';
                return;
            }

            list.innerHTML = filtered.map(run => {
                const statusClass = run.conclusion || run.status;
                const time = formatTime(run.created_at);

                return '<div class="run-item">' +
                    '<span class="run-status ' + statusClass + '"></span>' +
                    '<div class="run-info">' +
                        '<div class="run-name">' + run.name + '</div>' +
                        '<div class="run-meta">' +
                            '<span class="run-repo">' + run.repo_name + '</span>' +
                            '<span class="run-event ' + run.event + '">' + run.event + '</span>' +
                            '<span>' + run.branch + '</span>' +
                            '<span>' + run.commit_sha + '</span>' +
                        '</div>' +
                    '</div>' +
                    '<span class="run-time">' + time + '</span>' +
                    '<a href="' + run.html_url + '" target="_blank" class="run-link">View ↗</a>' +
                '</div>';
            }).join('');
        }

        function formatTime(isoTime) {
            const date = new Date(isoTime);
            const now = new Date();
            const diff = (now - date) / 1000;

            if (diff < 60) return Math.floor(diff) + 's ago';
            if (diff < 3600) return Math.floor(diff / 60) + 'm ago';
            if (diff < 86400) return Math.floor(diff / 3600) + 'h ago';
            return date.toLocaleDateString();
        }

        init();
    </script>
</body>
</html>`

func (h *Handlers) WorkflowsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(workflowsHTML))
}
