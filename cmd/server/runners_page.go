package main

import "net/http"

const runnersHTML = `<!DOCTYPE html>
<html>
<head>
    <title>Self-Hosted Runners</title>
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
        .org-header { padding: 8px 15px; font-size: 11px; text-transform: uppercase; color: #8b949e; font-weight: 600; }
        .org-name { padding: 8px 15px; cursor: pointer; display: flex; align-items: center; gap: 8px; }
        .org-name:hover { background: #21262d; }
        .org-name.active { background: #1f6feb22; border-left: 2px solid #58a6ff; }
        .org-icon { width: 20px; height: 20px; background: #30363d; border-radius: 4px; display: flex; align-items: center; justify-content: center; font-size: 10px; }

        .runner-list { flex: 1; overflow-y: auto; padding: 10px 0; }
        .runner-item { padding: 10px 15px; cursor: pointer; display: flex; align-items: center; gap: 10px; }
        .runner-item:hover { background: #21262d; }
        .runner-item.active { background: #1f6feb22; color: #58a6ff; }
        .runner-status { width: 8px; height: 8px; border-radius: 50%; }
        .runner-status.online { background: #238636; }
        .runner-status.offline { background: #6e7681; }
        .runner-status.busy { background: #d29922; }
        .runner-name { flex: 1; font-size: 14px; }
        .runner-os { font-size: 11px; color: #8b949e; }

        /* Main content */
        .main { flex: 1; overflow-y: auto; padding: 20px 30px; }
        .main-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
        .main-header h2 { color: #c9d1d9; font-size: 20px; }
        .refresh-btn { padding: 8px 16px; background: #21262d; border: 1px solid #30363d; color: #c9d1d9; border-radius: 6px; cursor: pointer; }
        .refresh-btn:hover { background: #30363d; }

        .runner-details { background: #161b22; border-radius: 6px; border: 1px solid #30363d; padding: 20px; margin-bottom: 20px; }
        .runner-details h3 { margin-bottom: 15px; display: flex; align-items: center; gap: 10px; }
        .detail-row { display: flex; gap: 20px; margin-bottom: 10px; }
        .detail-label { color: #8b949e; min-width: 80px; }
        .detail-value { color: #c9d1d9; }
        .label-tag { display: inline-block; padding: 3px 8px; background: #30363d; border-radius: 12px; font-size: 12px; margin-right: 5px; margin-bottom: 5px; }
        .label-tag.self-hosted { background: #1f6feb33; color: #58a6ff; }

        .jobs-section { background: #161b22; border-radius: 6px; border: 1px solid #30363d; overflow: hidden; }
        .jobs-header { padding: 15px; background: #21262d; border-bottom: 1px solid #30363d; font-weight: 600; }
        .job-item { padding: 12px 15px; border-bottom: 1px solid #30363d; display: flex; align-items: center; gap: 15px; cursor: pointer; }
        .job-item:last-child { border-bottom: none; }
        .job-item:hover { background: #1c2128; }
        .job-status { width: 10px; height: 10px; border-radius: 50%; }
        .job-status.success { background: #238636; }
        .job-status.failure { background: #da3633; }
        .job-status.in_progress { background: #d29922; }
        .job-status.queued { background: #6e7681; }
        .job-info { flex: 1; }
        .job-name { font-weight: 600; color: #c9d1d9; }
        .job-meta { font-size: 12px; color: #8b949e; margin-top: 2px; }
        .job-time { font-size: 12px; color: #8b949e; }
        .view-logs-btn { padding: 5px 10px; background: #21262d; border: 1px solid #30363d; color: #c9d1d9; border-radius: 4px; font-size: 12px; cursor: pointer; }
        .view-logs-btn:hover { background: #30363d; }

        /* Logs modal */
        .modal { display: none; position: fixed; top: 0; left: 0; right: 0; bottom: 0; background: rgba(0,0,0,0.8); z-index: 1000; padding: 40px; }
        .modal.show { display: flex; }
        .modal-content { background: #161b22; border-radius: 8px; border: 1px solid #30363d; flex: 1; display: flex; flex-direction: column; max-width: 1200px; margin: 0 auto; }
        .modal-header { padding: 15px 20px; border-bottom: 1px solid #30363d; display: flex; justify-content: space-between; align-items: center; }
        .modal-header h3 { color: #c9d1d9; }
        .modal-close { background: none; border: none; color: #8b949e; font-size: 24px; cursor: pointer; }
        .modal-close:hover { color: #c9d1d9; }
        .modal-body { flex: 1; overflow: auto; padding: 0; }
        .log-content { font-family: 'SF Mono', 'Menlo', monospace; font-size: 12px; line-height: 1.5; white-space: pre-wrap; padding: 15px 20px; color: #c9d1d9; background: #0d1117; min-height: 100%; }
        .log-content .timestamp { color: #8b949e; }
        .log-content .error { color: #f85149; }
        .log-content .success { color: #3fb950; }
        .log-content .warning { color: #d29922; }

        .empty { padding: 40px; text-align: center; color: #8b949e; }
        .loading { padding: 40px; text-align: center; color: #8b949e; }
    </style>
</head>
<body>
    <div class="sidebar">
        <div class="sidebar-header">
            <h1>🖥️ Runners</h1>
        </div>
        <div class="nav-section">
            <a href="/" class="nav-item">🚦 Merge Queue</a>
            <a href="/runners" class="nav-item active">🖥️ Runners</a>
            <a href="/workflows" class="nav-item">⚡ Workflows</a>
        </div>
        <div class="org-section">
            <div class="org-header">Organizations</div>
            <div id="org-list"></div>
        </div>
        <div class="org-section" style="flex: 1;">
            <div class="org-header">Self-Hosted Runners</div>
            <div id="runner-list" class="runner-list">
                <div class="empty">Select an organization</div>
            </div>
        </div>
    </div>

    <div class="main">
        <div class="main-header">
            <h2 id="current-view">Select a runner</h2>
            <button class="refresh-btn" onclick="loadRunners()">↻ Refresh</button>
        </div>

        <div id="runner-details" style="display: none;">
            <div class="runner-details">
                <h3>
                    <span id="detail-status" class="runner-status"></span>
                    <span id="detail-name"></span>
                </h3>
                <div class="detail-row">
                    <span class="detail-label">Status</span>
                    <span class="detail-value" id="detail-status-text"></span>
                </div>
                <div class="detail-row">
                    <span class="detail-label">OS</span>
                    <span class="detail-value" id="detail-os"></span>
                </div>
                <div class="detail-row">
                    <span class="detail-label">Labels</span>
                    <span class="detail-value" id="detail-labels"></span>
                </div>
            </div>

            <div class="jobs-section">
                <div class="jobs-header">Recent Jobs</div>
                <div id="jobs-list">
                    <div class="loading">Loading jobs...</div>
                </div>
            </div>
        </div>

        <div id="no-runner" class="empty">
            Select a runner from the sidebar to view details and logs
        </div>
    </div>

    <div id="logs-modal" class="modal">
        <div class="modal-content">
            <div class="modal-header">
                <h3 id="logs-title">Job Logs</h3>
                <button class="modal-close" onclick="closeLogsModal()">&times;</button>
            </div>
            <div class="modal-body">
                <div id="logs-content" class="log-content">Loading logs...</div>
            </div>
        </div>
    </div>

    <script>
        let orgs = [];
        let runners = [];
        let currentOrg = localStorage.getItem('runners_org') || '';
        let currentRunner = null;

        async function init() {
            // Load installations (orgs)
            const res = await fetch('/api/v1/installations');
            const data = await res.json();
            orgs = (data.installations || []).map(i => i.owner_login);

            renderOrgList();

            // Restore last selection
            if (currentOrg && orgs.includes(currentOrg)) {
                await selectOrg(currentOrg);
            } else if (orgs.length > 0) {
                await selectOrg(orgs[0]);
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
            localStorage.setItem('runners_org', org);
            currentRunner = null;
            renderOrgList();
            await loadRunners();
        }

        async function loadRunners() {
            if (!currentOrg) return;

            document.getElementById('runner-list').innerHTML = '<div class="loading">Loading...</div>';

            try {
                const res = await fetch('/api/v1/runners?owner=' + currentOrg);
                if (!res.ok) {
                    const text = await res.text();
                    if (text.includes('403') || text.includes('not accessible')) {
                        document.getElementById('runner-list').innerHTML = '<div class="empty" style="font-size: 12px;">GitHub App needs<br><b>organization_self_hosted_runners: read</b><br>permission</div>';
                    } else {
                        document.getElementById('runner-list').innerHTML = '<div class="empty">Failed to load runners</div>';
                    }
                    return;
                }
                const data = await res.json();
                runners = data.runners || [];
                renderRunnerList();
            } catch (err) {
                document.getElementById('runner-list').innerHTML = '<div class="empty">Failed to load runners</div>';
            }
        }

        function renderRunnerList() {
            const list = document.getElementById('runner-list');

            if (!runners.length) {
                list.innerHTML = '<div class="empty">No self-hosted runners found</div>';
                return;
            }

            list.innerHTML = runners.map(r => {
                const statusClass = r.busy ? 'busy' : r.status;
                const isActive = currentRunner && currentRunner.id === r.id;
                return '<div class="runner-item' + (isActive ? ' active' : '') + '" onclick="selectRunner(' + r.id + ')">' +
                    '<span class="runner-status ' + statusClass + '"></span>' +
                    '<span class="runner-name">' + r.name + '</span>' +
                    '<span class="runner-os">' + r.os + '</span>' +
                '</div>';
            }).join('');
        }

        async function selectRunner(id) {
            currentRunner = runners.find(r => r.id === id);
            if (!currentRunner) return;

            renderRunnerList();
            document.getElementById('current-view').textContent = currentRunner.name;
            document.getElementById('no-runner').style.display = 'none';
            document.getElementById('runner-details').style.display = 'block';

            // Update details
            const statusClass = currentRunner.busy ? 'busy' : currentRunner.status;
            document.getElementById('detail-status').className = 'runner-status ' + statusClass;
            document.getElementById('detail-name').textContent = currentRunner.name;
            document.getElementById('detail-status-text').textContent = currentRunner.busy ? 'Busy' : (currentRunner.status === 'online' ? 'Online' : 'Offline');
            document.getElementById('detail-os').textContent = currentRunner.os;

            // Render labels
            const labelsHtml = currentRunner.labels.map(l => {
                const cls = l === 'self-hosted' ? 'label-tag self-hosted' : 'label-tag';
                return '<span class="' + cls + '">' + l + '</span>';
            }).join('');
            document.getElementById('detail-labels').innerHTML = labelsHtml;

            // Load jobs
            await loadRunnerJobs();
        }

        async function loadRunnerJobs() {
            if (!currentRunner) return;

            document.getElementById('jobs-list').innerHTML = '<div class="loading">Loading jobs...</div>';

            try {
                const res = await fetch('/api/v1/runners/jobs?owner=' + currentOrg + '&runner=' + currentRunner.name);
                const data = await res.json();
                renderJobs(data.jobs || []);
            } catch (err) {
                document.getElementById('jobs-list').innerHTML = '<div class="empty">Failed to load jobs</div>';
            }
        }

        function renderJobs(jobs) {
            const list = document.getElementById('jobs-list');

            if (!jobs.length) {
                list.innerHTML = '<div class="empty">No recent jobs found</div>';
                return;
            }

            list.innerHTML = jobs.map(job => {
                const statusClass = job.conclusion || job.status;
                const time = job.completed_at ? new Date(job.completed_at).toLocaleString() : (job.started_at ? 'Started ' + new Date(job.started_at).toLocaleString() : '');

                return '<div class="job-item">' +
                    '<span class="job-status ' + statusClass + '"></span>' +
                    '<div class="job-info">' +
                        '<div class="job-name">' + job.name + '</div>' +
                        '<div class="job-meta">' + job.workflow_name + ' · ' + job.repo_name + '</div>' +
                    '</div>' +
                    '<span class="job-time">' + time + '</span>' +
                    '<button class="view-logs-btn" onclick="viewLogs(\'' + job.repo_name + '\', ' + job.id + ', \'' + job.name + '\')">View Logs</button>' +
                    '<a href="' + job.html_url + '" target="_blank" class="view-logs-btn" style="text-decoration: none; margin-left: 5px;">GitHub ↗</a>' +
                '</div>';
            }).join('');
        }

        async function viewLogs(repo, jobId, jobName) {
            document.getElementById('logs-modal').classList.add('show');
            document.getElementById('logs-title').textContent = jobName + ' - Logs';
            document.getElementById('logs-content').textContent = 'Loading logs...';

            try {
                const res = await fetch('/api/v1/runners/logs?owner=' + currentOrg + '&repo=' + repo + '&job_id=' + jobId);
                if (!res.ok) {
                    throw new Error('Failed to fetch logs');
                }
                const logs = await res.text();
                document.getElementById('logs-content').innerHTML = formatLogs(logs);
            } catch (err) {
                document.getElementById('logs-content').textContent = 'Failed to load logs: ' + err.message + '\n\nNote: Logs may have expired (GitHub retains logs for 90 days).';
            }
        }

        function formatLogs(logs) {
            // Escape HTML
            let html = logs.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

            // Highlight timestamps
            html = html.replace(/(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z)/g, '<span class="timestamp">$1</span>');

            // Highlight errors
            html = html.replace(/(error|failed|failure|fatal)/gi, '<span class="error">$1</span>');

            // Highlight success
            html = html.replace(/(success|passed|completed)/gi, '<span class="success">$1</span>');

            // Highlight warnings
            html = html.replace(/(warning|warn)/gi, '<span class="warning">$1</span>');

            return html;
        }

        function closeLogsModal() {
            document.getElementById('logs-modal').classList.remove('show');
        }

        // Close modal on escape
        document.addEventListener('keydown', function(e) {
            if (e.key === 'Escape') {
                closeLogsModal();
            }
        });

        // Close modal on background click
        document.getElementById('logs-modal').addEventListener('click', function(e) {
            if (e.target === this) {
                closeLogsModal();
            }
        });

        init();
    </script>
</body>
</html>`

func (h *Handlers) RunnersPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(runnersHTML))
}
