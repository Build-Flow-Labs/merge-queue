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
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #0d1117; color: #c9d1d9; padding: 20px; }
        h1 { margin-bottom: 20px; color: #58a6ff; }
        .container { max-width: 1200px; margin: 0 auto; }
        .repo-select { margin-bottom: 20px; display: flex; gap: 10px; align-items: center; }
        .repo-select select { padding: 10px 15px; border-radius: 6px; border: 1px solid #30363d; background: #161b22; color: #c9d1d9; font-size: 14px; min-width: 200px; cursor: pointer; }
        .repo-select select:focus { outline: none; border-color: #58a6ff; }
        .refresh { font-size: 12px; color: #8b949e; margin-left: auto; }
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
        .status { padding: 5px 12px; border-radius: 20px; font-size: 12px; font-weight: 600; text-transform: uppercase; }
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
        .events h2 { margin-bottom: 15px; color: #58a6ff; font-size: 18px; }
        .event { padding: 10px 15px; border-left: 3px solid #30363d; margin-bottom: 10px; background: #161b22; }
        .event.merged { border-left-color: #238636; }
        .event.failed { border-left-color: #da3633; }
        .event-type { font-weight: 600; color: #58a6ff; }
        .event-time { font-size: 12px; color: #8b949e; }
        .empty { padding: 40px; text-align: center; color: #8b949e; }
        .error { padding: 10px; background: #da363322; border: 1px solid #da3633; border-radius: 6px; color: #f85149; margin-bottom: 20px; }
        .loading { color: #8b949e; padding: 20px; text-align: center; }
    </style>
</head>
<body>
    <div class="container">
        <h1>🚦 Merge Queue</h1>

        <div class="repo-select">
            <select id="org" onchange="onOrgChange()">
                <option value="">Select Organization...</option>
            </select>
            <select id="repo" onchange="loadQueue()">
                <option value="">Select Repository...</option>
            </select>
            <span class="refresh">Auto-refreshes every 5s</span>
        </div>

        <div id="error" class="error" style="display: none;"></div>

        <div class="queue">
            <div class="queue-header">
                <span>Queue</span>
                <span class="queue-count" id="queue-count"></span>
            </div>
            <div id="queue-items">
                <div class="empty">Select an organization and repository</div>
            </div>
        </div>

        <div class="events">
            <h2>Recent Events</h2>
            <div id="events">
                <div class="empty">No events yet</div>
            </div>
        </div>
    </div>

    <script>
        let refreshInterval;
        let repos = {};

        async function init() {
            // Load installations (orgs)
            const res = await fetch('/api/v1/installations');
            const data = await res.json();
            const orgSelect = document.getElementById('org');

            (data.installations || []).forEach(inst => {
                const opt = document.createElement('option');
                opt.value = inst.owner_login;
                opt.textContent = inst.owner_login;
                orgSelect.appendChild(opt);
            });

            // Load all events to get repo list
            const eventsRes = await fetch('/api/v1/events');
            const eventsData = await eventsRes.json();

            (eventsData.events || []).forEach(e => {
                if (!repos[e.owner]) repos[e.owner] = new Set();
                repos[e.owner].add(e.repo);
            });

            // Also load queue for repos
            // For now, we'll populate repos as we see them in events
        }

        function onOrgChange() {
            const org = document.getElementById('org').value;
            const repoSelect = document.getElementById('repo');
            repoSelect.innerHTML = '<option value="">Select Repository...</option>';

            if (org && repos[org]) {
                repos[org].forEach(repo => {
                    const opt = document.createElement('option');
                    opt.value = repo;
                    opt.textContent = repo;
                    repoSelect.appendChild(opt);
                });
            }

            // Also add option to view all
            if (org) {
                const allOpt = document.createElement('option');
                allOpt.value = '__all__';
                allOpt.textContent = '-- All Repositories --';
                repoSelect.insertBefore(allOpt, repoSelect.options[1]);
            }

            document.getElementById('queue-items').innerHTML = '<div class="empty">Select a repository</div>';
            document.getElementById('events').innerHTML = '<div class="empty">No events yet</div>';
            if (refreshInterval) clearInterval(refreshInterval);
        }

        async function loadQueue() {
            const owner = document.getElementById('org').value;
            const repo = document.getElementById('repo').value;

            if (!owner || !repo) return;

            try {
                let queueUrl = '/api/v1/queue?owner=' + owner;
                let eventsUrl = '/api/v1/events?owner=' + owner;

                if (repo !== '__all__') {
                    queueUrl += '&repo=' + repo;
                    eventsUrl += '&repo=' + repo;
                }

                const queueRes = await fetch(queueUrl);
                const queueData = await queueRes.json();
                renderQueue(queueData.queue || [], owner);

                const eventsRes = await fetch(eventsUrl);
                const eventsData = await eventsRes.json();
                renderEvents(eventsData.events || [], owner);

                // Update repo list from events
                (eventsData.events || []).forEach(e => {
                    if (!repos[e.owner]) repos[e.owner] = new Set();
                    repos[e.owner].add(e.repo);
                });

                if (refreshInterval) clearInterval(refreshInterval);
                refreshInterval = setInterval(loadQueue, 5000);
            } catch (err) {
                showError('Failed to load: ' + err.message);
            }
        }

        function renderQueue(items, owner) {
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

                let retryInfo = '';
                if (item.status === 'failed' && item.next_retry_at) {
                    const retryTime = new Date(item.next_retry_at);
                    const now = new Date();
                    const secsUntil = Math.max(0, Math.floor((retryTime - now) / 1000));
                    if (secsUntil > 0) {
                        const mins = Math.floor(secsUntil / 60);
                        const secs = secsUntil % 60;
                        retryInfo = '<div class="pr-meta" style="color: #a371f7;">Auto-retry in ' + mins + 'm ' + secs + 's (attempt ' + (item.retry_count + 1) + '/' + item.max_retries + ')</div>';
                    }
                } else if (item.status === 'failed' && item.retry_count >= item.max_retries) {
                    retryInfo = '<div class="pr-meta" style="color: #f85149;">Max retries reached (' + item.retry_count + '/' + item.max_retries + ')</div>';
                }

                return '<div class="queue-item">' +
                    '<span class="position">' + item.position + '</span>' +
                    '<div class="pr-info">' +
                        '<div class="pr-title"><a href="' + prUrl + '" target="_blank">#' + item.pr_number + ' ' + (item.pr_title || 'Untitled') + '</a></div>' +
                        '<div class="pr-meta">' + item.repo + ': ' + item.pr_branch + ' → ' + item.base_branch + ' • by ' + (item.pr_author || 'unknown') + '</div>' +
                        (item.error_message ? '<div class="pr-meta" style="color: #f85149;">Error: ' + item.error_message.substring(0, 100) + '...</div>' : '') +
                        retryInfo +
                    '</div>' +
                    '<span class="status ' + item.status + '">' + item.status.replace('_', ' ') + '</span>' +
                    '<div class="actions">' + actions + '</div>' +
                '</div>';
            }).join('');
        }

        function renderEvents(events, owner) {
            const container = document.getElementById('events');
            if (!events.length) {
                container.innerHTML = '<div class="empty">No events yet</div>';
                return;
            }
            container.innerHTML = events.slice(0, 20).map(event => {
                const prUrl = 'https://github.com/' + event.owner + '/' + event.repo + '/pull/' + event.pr_number;
                return '<div class="event ' + event.event_type + '">' +
                    '<span class="event-type">' + event.event_type + '</span> ' +
                    '<a href="' + prUrl + '" target="_blank" style="color: #58a6ff;">PR #' + event.pr_number + '</a> in ' + event.repo +
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

        function hideError() {
            document.getElementById('error').style.display = 'none';
        }

        init();
    </script>
</body>
</html>`

func (h *Handlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(dashboardHTML))
}
