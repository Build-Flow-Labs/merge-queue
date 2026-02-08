# Deploying Merge Queue

## Quick Deploy to Fly.io

### Prerequisites
1. [Fly.io account](https://fly.io) (free tier available)
2. Fly CLI installed: `curl -L https://fly.io/install.sh | sh`
3. GitHub App configured with webhook URL

### Step 1: Login to Fly.io
```bash
fly auth login
```

### Step 2: Create the app
```bash
fly launch --no-deploy --name merge-queue --region sjc
```

### Step 3: Set secrets
```bash
fly secrets set \
  DATABASE_URL='postgresql://user:pass@host/db?sslmode=require' \
  GITHUB_APP_ID='2819228' \
  GITHUB_PRIVATE_KEY="$(cat private-key.pem)" \
  GITHUB_WEBHOOK_SECRET='your-webhook-secret'
```

### Step 4: Deploy
```bash
fly deploy
```

### Step 5: Update GitHub App webhook
Go to your GitHub App settings and update the webhook URL to:
```
https://merge-queue.fly.dev/webhooks/github
```

---

## Alternative: Docker Compose (Self-hosted)

### With external database (Neon, Supabase, etc.)
```bash
# Create .env file
cat > .env << EOF
DATABASE_URL=postgresql://user:pass@host/db?sslmode=require
GITHUB_APP_ID=2819228
GITHUB_PRIVATE_KEY="$(cat private-key.pem)"
GITHUB_WEBHOOK_SECRET=your-webhook-secret
EOF

# Run
docker-compose up -d
```

### With local PostgreSQL
```bash
# Start with local database
docker-compose --profile local-db up -d

# Run migrations
DATABASE_URL=postgres://merge_queue:merge_queue@localhost:5432/merge_queue?sslmode=disable \
  go run -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate \
  -path ./migrations -database "$DATABASE_URL" up
```

---

## Alternative: Systemd (Linux VPS)

### Step 1: Build binary
```bash
CGO_ENABLED=0 GOOS=linux go build -o merge-queue ./cmd/server
```

### Step 2: Create systemd service
```bash
sudo tee /etc/systemd/system/merge-queue.service << EOF
[Unit]
Description=Merge Queue Service
After=network.target

[Service]
Type=simple
User=www-data
WorkingDirectory=/opt/merge-queue
ExecStart=/opt/merge-queue/merge-queue
Restart=always
RestartSec=5
Environment=PORT=8080
EnvironmentFile=/opt/merge-queue/.env

[Install]
WantedBy=multi-user.target
EOF
```

### Step 3: Start service
```bash
sudo systemctl daemon-reload
sudo systemctl enable merge-queue
sudo systemctl start merge-queue
```

### Step 4: Set up nginx reverse proxy
```nginx
server {
    listen 443 ssl;
    server_name merge-queue.yourdomain.com;

    ssl_certificate /etc/letsencrypt/live/merge-queue.yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/merge-queue.yourdomain.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

---

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `DATABASE_URL` | Yes | PostgreSQL connection string |
| `GITHUB_APP_ID` | Yes | GitHub App ID |
| `GITHUB_PRIVATE_KEY` | Yes* | GitHub App private key (PEM content) |
| `GITHUB_PRIVATE_KEY_PATH` | Yes* | Path to private key file (alternative) |
| `GITHUB_WEBHOOK_SECRET` | Yes | Webhook secret for signature verification |
| `PORT` | No | HTTP port (default: 8080) |

*One of `GITHUB_PRIVATE_KEY` or `GITHUB_PRIVATE_KEY_PATH` is required.

---

## Monitoring

### Health Check
```bash
curl https://merge-queue.fly.dev/healthz
# {"status":"ok"}
```

### View Logs (Fly.io)
```bash
fly logs
```

### View Logs (Docker)
```bash
docker-compose logs -f merge-queue
```

### View Logs (Systemd)
```bash
journalctl -u merge-queue -f
```
