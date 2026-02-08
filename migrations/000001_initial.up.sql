-- Organizations/installations that have the merge queue app installed
CREATE TABLE IF NOT EXISTS installations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    github_installation_id BIGINT NOT NULL UNIQUE,
    owner_type TEXT NOT NULL, -- 'Organization' or 'User'
    owner_login TEXT NOT NULL,
    owner_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_installations_owner ON installations(owner_login);

-- Merge queue items
CREATE TABLE IF NOT EXISTS merge_queue (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id UUID NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    owner TEXT NOT NULL,
    repo TEXT NOT NULL,
    pr_number INTEGER NOT NULL,
    pr_title TEXT,
    pr_branch TEXT NOT NULL,
    base_branch TEXT NOT NULL DEFAULT 'main',
    pr_author TEXT,
    position INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued', -- queued, processing, rebasing, waiting_ci, merging, merged, failed, cancelled
    error_message TEXT,
    queued_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    queued_by TEXT,

    UNIQUE(installation_id, owner, repo, pr_number)
);

CREATE INDEX idx_merge_queue_position ON merge_queue(installation_id, owner, repo, status, position);
CREATE INDEX idx_merge_queue_active ON merge_queue(status) WHERE status IN ('queued', 'processing', 'rebasing', 'waiting_ci', 'merging');

-- Per-repository settings
CREATE TABLE IF NOT EXISTS repo_settings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id UUID NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    owner TEXT NOT NULL,
    repo TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    merge_method TEXT NOT NULL DEFAULT 'merge', -- merge, squash, rebase
    require_ci_pass BOOLEAN NOT NULL DEFAULT true,
    auto_rebase BOOLEAN NOT NULL DEFAULT true,
    delete_branch_on_merge BOOLEAN NOT NULL DEFAULT true,
    max_queue_size INTEGER DEFAULT 50,
    ci_timeout_minutes INTEGER DEFAULT 60,
    trigger_label TEXT DEFAULT 'merge-queue',
    trigger_comment TEXT DEFAULT '/merge',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(installation_id, owner, repo)
);

-- Audit events
CREATE TABLE IF NOT EXISTS queue_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    queue_item_id UUID REFERENCES merge_queue(id) ON DELETE SET NULL,
    installation_id UUID NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    owner TEXT NOT NULL,
    repo TEXT NOT NULL,
    pr_number INTEGER NOT NULL,
    event_type TEXT NOT NULL, -- enqueued, started, rebased, ci_passed, ci_failed, merged, failed, cancelled
    actor TEXT,
    details JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_queue_events_item ON queue_events(queue_item_id);
CREATE INDEX idx_queue_events_repo ON queue_events(installation_id, owner, repo, created_at DESC);
