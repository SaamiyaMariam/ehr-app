# EHR App

Electronic Health Record and billing application built with:

- **Frontend:** Next.js, React, TypeScript, Tailwind
- **Backend:** Go
- **Database:** PostgreSQL
- **Local DB:** Docker

## Prerequisites

Install:

- Node.js + npm
- Go
- Docker Desktop

Verify:

```powershell
node --version
npm --version
go version
docker --version
```

## 1. Environment

Create a root `.env` file if one does not already exist.

Example:

```env
POSTGRES_DB=ehr_db
POSTGRES_USER=ehr_user
POSTGRES_PASSWORD=your_local_password
POSTGRES_PORT=5222

DATABASE_URL=postgres://ehr_user:your_local_password@localhost:5222/ehr_db?sslmode=disable

JWT_SECRET=your_random_secret
```

Do not commit `.env`.

## 2. Start PostgreSQL

Start Docker Desktop, then from the project root:

```powershell
docker compose up -d
```

Verify:

```powershell
docker ps
```

PostgreSQL should be exposed as:

```text
localhost:5222 → container:5432
```

## 3. Run Database Migrations

From the project root:

```powershell
npm run migration:run
```

Run this on a fresh database before starting the application.

## 4. Start Backend

Open a terminal:

```powershell
cd backend
```

Load the root `.env` into PowerShell:

```powershell
Get-Content ..\.env | ForEach-Object {
    if ($_ -match '^\s*([^#][^=]*)=(.*)$') {
        $name = $matches[1].Trim()
        $value = $matches[2].Trim()
        Set-Item -Path "Env:$name" -Value $value
    }
}
```

Start the API:

```powershell
go run ./cmd/api
```

Backend:

```text
http://localhost:8080
```

Health check:

```text
http://localhost:8080/health
```

## 5. Start Frontend

Open another terminal:

```powershell
cd frontend
npm install
npm run dev
```

Frontend:

```text
http://localhost:3000
```

`npm install` normally only needs to be run the first time or when dependencies change.

## Optional: DBeaver

Connect using:

```text
Host: localhost
Port: 5222
Database: ehr_db
Username: ehr_user
Password: value from POSTGRES_PASSWORD
```

## Daily Startup

After the initial setup:

```powershell
docker compose up -d
```

Then start the backend:

```powershell
cd backend
# load .env
go run ./cmd/api
```

And in another terminal:

```powershell
cd frontend
npm run dev
```
