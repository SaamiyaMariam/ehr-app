$ErrorActionPreference = "Stop"

$container = "ehr-postgres"
$dbUser = "ehr_user"
$dbName = "ehr_db"

Write-Host "Checking PostgreSQL container..."

$running = docker inspect -f "{{.State.Running}}" $container 2>$null

if ($running -ne "true") {
    throw "PostgreSQL container '$container' is not running. Run: docker compose up -d"
}

$migrations = Get-ChildItem "$PSScriptRoot\..\backend\migrations\*.sql" |
    Sort-Object Name

if ($migrations.Count -eq 0) {
    throw "No migrations found in backend/migrations"
}

foreach ($migration in $migrations) {
    Write-Host ""
    Write-Host "Applying $($migration.Name)..."

    Get-Content $migration.FullName -Raw |
        docker exec -i $container `
            psql `
            -U $dbUser `
            -d $dbName `
            -v ON_ERROR_STOP=1

    if ($LASTEXITCODE -ne 0) {
        throw "Migration failed: $($migration.Name)"
    }
}

Write-Host ""
Write-Host "Migrations complete."
