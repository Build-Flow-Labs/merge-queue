# Merge Queue

A standalone GitHub App that provides merge queue functionality for any repository. Coordinates PR merges to prevent merge conflicts and ensure CI passes before merging.

## Features

- **Automated queue processing** - PRs are merged in order, one at a time
- **Auto-rebase** - Automatically updates PRs with latest base branch before merging
- **CI verification** - Waits for status checks to pass before merging
- **Multiple triggers** - Queue via label (`merge-queue`) or comment (`/merge`)
- **Per-repo settings** - Configure merge method, timeouts, and triggers per repository
- **Audit trail** - Full event history for compliance
- **Multi-repo support** - One installation works across all your repos

## Quick Start

### 1. Create a GitHub App

1. Go to GitHub Settings > Developer settings > GitHub Apps > New GitHub App
2. Configure:
   - **Name**: `Your Merge Queue`
   - **Webhook URL**: `https://your-domain.com/webhooks/github`
   - **Webhook Secret**: Generate a secure secret
   - **Permissions**:
     - Repository: Contents (Read & Write), Pull requests (Read & Write), Commit statuses (Read)
     - Organization: Members (Read)
   - **Events**: Pull request, Issue comment, Installation

3. Generate a private key and download it

### 2. Deploy the Service

```bash
# Environment variables
export DATABASE_URL="postgres://user:pass@host:5432/merge_queue"
export GITHUB_APP_ID="123456"
export GITHUB_PRIVATE_KEY_PATH="./private-key.pem"
export GITHUB_WEBHOOK_SECRET="your-webhook-secret"

# Run
go build -o merge-queue ./cmd/server
./merge-queue
```

### 3. Install the App

Install your GitHub App on the organizations/repos you want to use it with.

## Usage

### Add PR to Queue

**Via Label:**
Add the `merge-queue` label to a PR

**Via Comment:**
Comment `/merge` on the PR

**Via API:**
```bash
curl -X POST https://your-domain.com/api/v1/queue \
  -H "Content-Type: application/json" \
  -d '{"owner": "org", "repo": "repo", "pr_number": 123}'
```

### View Queue

```bash
curl "https://your-domain.com/api/v1/queue?owner=org&repo=repo"
```

### Configure Settings

```bash
curl -X PUT https://your-domain.com/api/v1/settings \
  -H "Content-Type: application/json" \
  -d '{
    "owner": "org",
    "repo": "repo",
    "enabled": true,
    "merge_method": "squash",
    "require_ci_pass": true,
    "auto_rebase": true,
    "trigger_label": "merge-queue",
    "trigger_comment": "/merge"
  }'
```

## API Reference

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/healthz` | GET | Health check |
| `/webhooks/github` | POST | GitHub webhook receiver |
| `/api/v1/queue` | GET | Get queue for a repo |
| `/api/v1/queue` | POST | Add PR to queue |
| `/api/v1/queue/{id}` | DELETE | Remove from queue |
| `/api/v1/queue/{id}/retry` | POST | Retry failed item |
| `/api/v1/settings` | GET | Get repo settings |
| `/api/v1/settings` | PUT | Update repo settings |
| `/api/v1/events` | GET | Get queue events |
| `/api/v1/installations` | GET | List installations |

## Configuration

### Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `DATABASE_URL` | Yes | PostgreSQL connection string |
| `GITHUB_APP_ID` | Yes | GitHub App ID |
| `GITHUB_PRIVATE_KEY` | Yes* | PEM content (or use path) |
| `GITHUB_PRIVATE_KEY_PATH` | Yes* | Path to PEM file |
| `GITHUB_WEBHOOK_SECRET` | No | Webhook signature secret |
| `PORT` | No | Server port (default: 8080) |

*One of `GITHUB_PRIVATE_KEY` or `GITHUB_PRIVATE_KEY_PATH` is required.

### Per-Repository Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `enabled` | true | Enable/disable queue for repo |
| `merge_method` | merge | merge, squash, or rebase |
| `require_ci_pass` | true | Wait for CI before merge |
| `auto_rebase` | true | Update branch before merge |
| `delete_branch_on_merge` | true | Delete PR branch after merge |
| `max_queue_size` | 50 | Max items in queue |
| `ci_timeout_minutes` | 60 | CI wait timeout |
| `trigger_label` | merge-queue | Label to trigger queue |
| `trigger_comment` | /merge | Comment to trigger queue |

## Queue Status Flow

```
queued → processing → rebasing → waiting_ci → merging → merged
                ↓         ↓           ↓          ↓
              failed   failed      failed     failed
```

## License

MIT
# Test 1770522805
