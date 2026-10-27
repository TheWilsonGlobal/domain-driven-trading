# ==============================================================================
# scripts/dev.ps1: Start all backend services and frontend web terminal
# ==============================================================================

param(
    [switch]$ServicesOnly,
    [switch]$Down
)

$RootDir = Split-Path -Parent $PSScriptRoot

if ($Down) {
    Write-Host "Stopping all Docker Compose backend containers..." -ForegroundColor Yellow
    docker compose -f "$RootDir\deployments\docker-compose.yml" down
    docker compose -f "$RootDir\deployments\docker-compose.services.yml" down
    exit 0
}

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host " Starting Domain-Driven Trading Platform (Backend + Web)" -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan

if ($ServicesOnly) {
    Write-Host "[1/2] Launching backend services (Engine, Gateway, Settlement)..." -ForegroundColor Green
    docker compose -f "$RootDir\deployments\docker-compose.services.yml" up -d
} else {
    Write-Host "[1/2] Launching full backend stack (Redpanda, Redis, Postgres, Services)..." -ForegroundColor Green
    docker compose -f "$RootDir\deployments\docker-compose.yml" up -d
}

Write-Host "[2/2] Launching interactive web terminal at http://localhost:5471..." -ForegroundColor Green
Set-Location "$RootDir\web"
pnpm dev
