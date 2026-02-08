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
        .repo-select { margin-bottom: 20px; display: flex; gap: 10px; }
        .repo-select input { padding: 10px; border-radius: 6px; border: 1px solid #30363d; background: #161b22; color: #c9d1d9; width: 200px; }
        .repo-select button { padding: 10px 20px; border-radius: 6px; border: none; background: #238636; color: white; cursor: pointer; }
        .repo-select button:hover { background: #2ea043; }
        .queue { background: #161b22; border-radius: 6px; border: 1px solid #30363d; overflow: hidden; }
        .queue-header { padding: 15px; background: #21262d; border-bottom: 1px solid #30363d; font-weight: 600; }
        .queue-item { padding: 15px; border-bottom: 1px solid #30363d; display: flex; align-items: center; gap: 15px; }
        .queue-item:last-child { border-bottom: none; }
        .position { background: #30363d; padding: 5px 10px; border-radius: 4px; font-weight: 600; min-width: 30px; text-align: center; }
        .pr-info { flex: 1; }
        .pr-title { font-weight: 600; color: #58a6ff; }
        .pr-meta { font-size: 13px; color: #8b949e; margin-top: 4px; }
        .status { padding: 5px 12px; border-radius: 20px; font-size: 12px; font-weight: 600; text-transform: uppercase; }
        .status.queued { background: #1f6feb33; color: #58a6ff; }
        .status.processing { background: #a371f733; color: #a371f7; }
        .status.rebasing { background: #a371f733; color: #a371f7; }
        .status.waiting_ci { background: #d29922; color: #0d1117; }
        .status.merging { background: #238636; color: white; }
        .status.merged { background: #238636; color: white; }
        .status.failed { background: #da3633; color: white; }
        .events { margin-top: 30px; }
        .events h2 { margin-bottom: 15px; color: #58a6ff; font-size: 18px; }
        .event { padding: 10px 15px; border-left: 3px solid #30363d; margin-bottom: 10px; background: #161b22; }
        .event-type { font-weight: 600; color: #58a6ff; }
        .event-time { font-size: 12px; color: #8b949e; }
        .empty { padding: 40px; text-align: center; color: #8b949e; }
        .refresh { font-size: 12px; color: #8b949e; margin-left: auto; }
        .error { padding: 10px; background: #da363322; border: 1px solid #da3633; border-radius: 6px; color: #f85149; margin-bottom: 20px; }
    </style>
</head>
<body>
    <div class="container">
        <h1>🚦 Merge Queue</h1>

        <div class="repo-select">
            <input type="text" id="owner" placeholder="Owner" value="Build-Flow-Labs">
            <input type="text" id="repo" placeholder="Repository" value="">
            <button onclick="loadQueue()">Load Queue</button>
            <span class="refresh">Auto-refreshes every 5s</span>
        </div>

        <div id="error" class="error" style="display: none;"></div>

        <div class="queue">
            <div class="queue-header">Queue</div>
            <div id="queue-items">
                <div class="empty">Enter owner/repo and click Load Queue</div>
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

        async function loadQueue() {
            const owner = document.getElementById('owner').value;
            const repo = document.getElementById('repo').value;

            if (!owner || !repo) {
                showError('Please enter both owner and repo');
                return;
            }

            hideError();

            try {
                // Load queue
                const queueRes = await fetch('/api/v1/queue?owner=' + owner + '&repo=' + repo);
                const queueData = await queueRes.json();
                renderQueue(queueData.queue || []);

                // Load events
                const eventsRes = await fetch('/api/v1/events?owner=' + owner + '&repo=' + repo);
                const eventsData = await eventsRes.json();
                renderEvents(eventsData.events || []);

                // Set up auto-refresh
                if (refreshInterval) clearInterval(refreshInterval);
                refreshInterval = setInterval(loadQueue, 5000);
            } catch (err) {
                showError('Failed to load: ' + err.message);
            }
        }

        function renderQueue(items) {
            const container = document.getElementById('queue-items');
            if (!items.length) {
                container.innerHTML = '<div class="empty">Queue is empty</div>';
                return;
            }
            container.innerHTML = items.map(item =>
                '<div class="queue-item">' +
                    '<span class="position">' + item.position + '</span>' +
                    '<div class="pr-info">' +
                        '<div class="pr-title">#' + item.pr_number + ' ' + (item.pr_title || 'Untitled') + '</div>' +
                        '<div class="pr-meta">' + item.pr_branch + ' → ' + item.base_branch + ' • by ' + (item.pr_author || 'unknown') + '</div>' +
                    '</div>' +
                    '<span class="status ' + item.status + '">' + item.status.replace('_', ' ') + '</span>' +
                '</div>'
            ).join('');
        }

        function renderEvents(events) {
            const container = document.getElementById('events');
            if (!events.length) {
                container.innerHTML = '<div class="empty">No events yet</div>';
                return;
            }
            container.innerHTML = events.slice(0, 20).map(event =>
                '<div class="event">' +
                    '<span class="event-type">' + event.event_type + '</span> ' +
                    'PR #' + event.pr_number + ' in ' + event.repo +
                    '<div class="event-time">' + new Date(event.created_at).toLocaleString() + '</div>' +
                '</div>'
            ).join('');
        }

        function showError(msg) {
            const el = document.getElementById('error');
            el.textContent = msg;
            el.style.display = 'block';
        }

        function hideError() {
            document.getElementById('error').style.display = 'none';
        }
    </script>
</body>
</html>`

func (h *Handlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(dashboardHTML))
}
