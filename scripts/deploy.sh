#!/bin/bash
set -e

# Deploy Merge Queue to Fly.io
# Usage: ./scripts/deploy.sh

echo "🚀 Deploying Merge Queue to Fly.io"

# Check if fly CLI is installed
if ! command -v fly &> /dev/null; then
    echo "❌ Fly CLI not found. Install it with:"
    echo "   curl -L https://fly.io/install.sh | sh"
    exit 1
fi

# Check if logged in
if ! fly auth whoami &> /dev/null; then
    echo "❌ Not logged in to Fly.io. Run: fly auth login"
    exit 1
fi

# Check if app exists
if ! fly apps list | grep -q "merge-queue"; then
    echo "📦 Creating new Fly.io app..."
    fly launch --no-deploy --name merge-queue --region sjc
fi

# Check if secrets are set
echo "🔐 Checking secrets..."
SECRETS_SET=$(fly secrets list 2>/dev/null | wc -l)

if [ "$SECRETS_SET" -lt 4 ]; then
    echo ""
    echo "⚠️  Secrets not configured. Set them with:"
    echo ""
    echo "fly secrets set \\"
    echo "  DATABASE_URL='your-neon-database-url' \\"
    echo "  GITHUB_APP_ID='your-app-id' \\"
    echo "  GITHUB_PRIVATE_KEY=\"\$(cat private-key.pem)\" \\"
    echo "  GITHUB_WEBHOOK_SECRET='your-webhook-secret'"
    echo ""
    read -p "Have you set the secrets? (y/n) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

# Deploy
echo "🚀 Deploying..."
fly deploy

echo ""
echo "✅ Deployed successfully!"
echo ""
echo "📊 Dashboard: https://merge-queue.fly.dev"
echo "🖥️  Runners:   https://merge-queue.fly.dev/runners"
echo "❤️  Health:    https://merge-queue.fly.dev/healthz"
echo ""
echo "📝 Update GitHub App webhook URL to:"
echo "   https://merge-queue.fly.dev/webhooks/github"
