# EHR Project — Agent Handoff & New Machine Setup

## 1. Purpose of this document

I have shifted development of this project to a new machine.

Use this document to:

1. Understand the existing project and scope.
2. Inspect the repository before changing anything.
3. Set up the development environment on this machine.
4. Verify the existing application still works.
5. Continue development from the exact current stopping point.
6. Avoid rebuilding or redesigning functionality that already exists.

Do not make large architectural changes unless there is a genuine issue with the existing implementation.

Before changing code, inspect the repository and confirm how the current code compares with this handoff.

---

# 2. Project Overview

This is a small EHR / practice-management application inspired by TherapyNotes workflows.

The initial client requirements were:

- Add New Patient
- Add a Payer
- Add a New User / Employee
- User Roles / User Creation

Authentication was also added to make the application usable.

The project has since expanded into implementing the billing workflow module-by-module.

The goal is still to keep the implementation clean and incremental rather than building the entire EHR platform at once.

---

# 3. Technology Stack

## Frontend

- Next.js
- React
- TypeScript
- Tailwind CSS
- Next.js App Router

Expected location:

```text
frontend/
```

## Backend

- Go
- `net/http`
- PostgreSQL
- pgx / pgxpool
- JWT authentication
- bcrypt password hashing

Expected location:

```text
backend/
```

## Database

PostgreSQL.

Previously the development database was:

```text
Database: ehr_db
User: ehr_user
```

Do not assume the old local password exists on this machine.

Create/use local credentials appropriate for this machine and place them in:

```text
backend/.env
```

The `.env` file must NOT be committed.

---

# 4. Repository Structure

The repo should roughly contain:

```text
ehr-app/
│
├── backend/
│   ├── cmd/
│   │   └── api/
│   │       └── main.go
│   │
│   ├── internal/
│   │   ├── auth/
│   │   │   └── auth.go
│   │   ├── database/
│   │   │   └── postgres.go
│   │   ├── patients/
│   │   │   └── patients.go
│   │   ├── users/
│   │   │   └── users.go
│   │   ├── roles/
│   │   │   └── roles.go
│   │   └── payers/
│   │       └── payers.go
│   │
│   ├── migrations/
│   │   ├── 001_initial_schema.sql
│   │   └── 002_patient_billing.sql
│   │
│   ├── go.mod
│   ├── .env
│   └── .env.example
│
└── frontend/
    ├── src/
    │   ├── app/
    │   │   ├── login/
    │   │   ├── signup/
    │   │   ├── dashboard/
    │   │   ├── patients/
    │   │   │   ├── page.tsx
    │   │   │   ├── new/
    │   │   │   └── [id]/
    │   │   ├── users/
    │   │   │   ├── page.tsx
    │   │   │   ├── new/
    │   │   │   └── [id]/
    │   │   └── payers/
    │   │       ├── page.tsx
    │   │       ├── new/
    │   │       └── [id]/
    │   │
    │   ├── components/
    │   │   ├── patient-form.tsx
    │   │   ├── user-form.tsx
    │   │   ├── role-selector.tsx
    │   │   └── payer-form.tsx
    │   │
    │   ├── lib/
    │   │   ├── api.ts
    │   │   └── auth.ts
    │   │
    │   └── types/
    │       ├── patient.ts
    │       ├── user.ts
    │       ├── role.ts
    │       └── payer.ts
    │
    └── next.config.ts
```

Inspect the actual repository before assuming every file exactly matches this structure.

---

# 5. Environment Variables

The backend requires:

```env
DATABASE_URL=postgres://<user>:<password>@localhost:5432/ehr_db?sslmode=disable
JWT_SECRET=<secure-random-secret>
```

Generate a JWT secret locally if needed:

```bash
openssl rand -hex 32
```

Do not commit or expose the actual JWT secret.

The backend previously required loading the `.env` manually before running:

```bash
cd backend

set -a
source .env
set +a

go run ./cmd/api
```

If the project has since gained automatic `.env` loading, inspect the code first. Otherwise use the commands above.

---

# 6. Initial Machine Setup

The previous development environment used Linux/WSL.

On the new machine, ensure these are available:

```text
Git
Go
Node.js
npm
PostgreSQL
psql
```

Recommended verification:

```bash
git --version
go version
node --version
npm --version
psql --version
```

Do NOT immediately change versions just because they differ from the previous machine.

First inspect:

```text
frontend/package.json
backend/go.mod
```

and use versions compatible with the repo.

---

# 7. Git Setup

This project should use the existing Git repository.

Do not initialize a new repo if `.git` already exists.

Check:

```bash
git status
git branch --show-current
git remote -v
git log --oneline -10
```

The repo previously used feature branches such as:

```text
feature/auth
feature/patients
feature/users
feature/payers
```

Main integration branch:

```text
main
```

Before continuing development, determine:

- current branch
- uncommitted files
- whether billing migration changes are already committed
- whether the latest remote changes are present

Do not discard any existing work.

---

# 8. PostgreSQL Setup

If the new machine has no database yet, create one.

Example:

```sql
CREATE USER ehr_user WITH PASSWORD '<LOCAL_PASSWORD>';
CREATE DATABASE ehr_db OWNER ehr_user;
GRANT ALL PRIVILEGES ON DATABASE ehr_db TO ehr_user;
```

Then configure `backend/.env`.

Apply migrations in order.

From `backend/`:

```bash
set -a
source .env
set +a

psql "$DATABASE_URL" -f migrations/001_initial_schema.sql
```

Then:

```bash
psql "$DATABASE_URL" -f migrations/002_patient_billing.sql
```

IMPORTANT:

Before applying migrations, inspect whether a migration-management mechanism already exists.

Do not blindly re-run destructive SQL.

These migrations were designed with `CREATE TABLE IF NOT EXISTS` where applicable, but inspect them first.

---

# 9. Existing Database Model

## Users

Existing users support fields such as:

- user comments
- legal name
- preferred name
- pronouns
- username
- date of birth
- languages
- email
- mobile/work/home phones
- text message preference
- address
- password hash
- active status

---

## Roles

Exactly seven original roles exist:

### Practice Administration

- Practice Administrator

### Clinical Access

- Clinician
- Intern / Assistant / Associate
- Supervisor
- Clinical Administrator

### Scheduling Access

- Practice Scheduler

### Billing Access

- Practice Biller

Underlying keys:

```text
practice_administrator
clinician
intern_assistant_associate
supervisor
clinical_administrator
practice_scheduler
practice_biller
```

Users can have multiple roles through:

```text
user_roles
```

Do not replace this with a more complicated permission engine unless required later.

---

## Patients

The Patient module already supports create/list/view/edit.

Relevant data includes:

- patient comments
- legal name
- preferred name
- pronouns
- DOB
- account number
- address
- phones
- email
- appointment reminder setting
- administrative sex
- gender-related fields
- documentation fields
- assigned clinician
- timestamps

There is an `assigned_clinician_id` relationship to users.

A clinician listing endpoint was added so the frontend can populate the assigned clinician selector.

Expected endpoint:

```http
GET /api/clinicians
```

This returns active users with the Clinician role.

---

## Payers

Payers already support:

- payer name
- payer ID
- network status
- address
- phone
- fax
- active status

Existing workflows:

```text
List Payers
Add Payer
View Payer
Edit Payer
Enable / Disable Payer
```

Expected endpoints:

```http
GET    /api/payers
POST   /api/payers
GET    /api/payers/{id}
PUT    /api/payers/{id}
PATCH  /api/payers/{id}/status
```

---

# 10. Authentication

Authentication already exists.

Expected endpoints:

```http
POST /api/auth/signup
POST /api/auth/login
GET  /api/auth/me
```

Implementation includes:

- bcrypt password hashing
- HS256 JWT
- approximately 24-hour token expiration
- authenticated route middleware

Frontend currently stores the token in localStorage.

This is acceptable for the current project stage but would need secure HttpOnly cookie/session handling before production healthcare deployment.

Do not redesign auth during basic environment setup.

---

# 11. Role Enforcement Already Added

A reusable backend middleware exists or was being added:

```go
RequireAnyRole(...)
```

It reads the authenticated user's roles from the database.

Explicit role rules currently implemented:

## Create Patient

Requires either:

```text
clinician
OR
practice_scheduler
```

Expected route:

```go
POST /api/patients
```

protected with:

```go
RequireAnyRole(
    patientHandler.Create,
    "clinician",
    "practice_scheduler",
)
```

## Modify Payers

Requires:

```text
practice_biller
```

This applies to:

```http
POST  /api/payers
PUT   /api/payers/{id}
PATCH /api/payers/{id}/status
```

Viewing/listing payers remains authenticated but not biller-only.

Do not accidentally register the same Go route twice.

There was previously a bug where:

```text
PUT /api/payers/{id}
```

was registered both with `RequireAnyRole` and `RequireAuth`.

It should exist only once and use Practice Biller protection.

---

# 12. Frontend Routing

The frontend uses Next.js App Router.

Expected routes:

```text
/login
/signup
/dashboard

/patients
/patients/new
/patients/[id]

/users
/users/new
/users/[id]

/payers
/payers/new
/payers/[id]
```

The root route previously redirected to:

```text
/login
```

The frontend uses an API helper which attaches:

```text
Authorization: Bearer <JWT>
```

A Next.js rewrite proxies:

```text
/api/*
```

to:

```text
http://localhost:8080/api/*
```

Inspect `frontend/next.config.ts` to confirm.

This avoids needing CORS during local development.

---

# 13. Existing Functional Modules

Current completion before the billing expansion:

```text
FOUNDATION                  DONE
AUTH                        DONE

PATIENTS
- Add                       DONE
- List                      DONE
- View                      DONE
- Edit                      DONE
- Assigned Clinician        DONE / verify after setup
- Create role restriction   DONE / verify

USERS
- Add                       DONE
- List                      DONE
- View                      DONE
- Edit                      DONE

USER ROLES
- Seven supplied roles      DONE
- Assign roles              DONE
- Update roles              DONE
- Persist roles             DONE

PAYERS
- Add                       DONE
- List                      DONE
- View                      DONE
- Edit                      DONE
- Enable / disable          DONE
- Practice Biller rule      DONE
```

All of this should be verified on the new machine before billing development continues.

---

# 14. New Billing Workstream

After the original scope was completed, the project was expanded to implement a fuller TherapyNotes-style billing workflow.

Do this module-by-module.

Current roadmap:

```text
0. Payers + Practice Biller Role
   DONE

1. Patient Billing Settings + Insurance Policies
   IN PROGRESS

2. Prior Authorizations
   NOT STARTED

3. Practice Billing Settings + Service Codes + Rates
   NOT STARTED

4. Billable Services / Charges / Transactions
   NOT STARTED

5. Claims + Validation + Claim Status
   NOT STARTED

6. CMS-1500 Generation
   NOT STARTED

7. Electronic / Paper / External Claim Workflow
   NOT STARTED

8. Patient Payments
   NOT STARTED

9. Insurance Payments + Adjustments
   NOT STARTED

10. Patient / Insurance Balances
    NOT STARTED

11. Patient Statements
    NOT STARTED

12. Claim & Payment Tracking / Billing Dashboard
    NOT STARTED

13. Real Clearinghouse Integration
    LATER / EXTERNAL DEPENDENCY
```

Keep this tracker updated as development proceeds.

---

# 15. Current Billing Stopping Point

We had JUST started:

```text
MODULE 1
Patient Billing Settings + Insurance Policies
```

The intended breakdown:

```text
1A Database
1B Backend API
1C Patient Billing Settings UI
1D Insurance Policy UI
1E Enable / Disable Policy
1F Validation / End-to-End Test
```

The migration planned/created was:

```text
backend/migrations/002_patient_billing.sql
```

Inspect Git and the filesystem to determine whether this migration exists and whether it has already been applied.

Do not recreate it blindly.

---

# 16. Module 1 Database Design

The intended new tables are:

## patient_billing_settings

Conceptually:

```text
patient_id
billing_comments
created_at
updated_at
```

`patient_id` references:

```text
patients(id)
```

---

## insurance_policies

Conceptually includes:

```text
id
patient_id
payer_id

priority

member_id
policy_group
plan_name

policy_comments

signature_on_file

coverage_start
coverage_end

copay
deductible

appointment_limit_type
appointments_allowed
appointments_expiration

relationship_to_policy_holder

policy_holder_first_name
policy_holder_middle_name
policy_holder_last_name
policy_holder_date_of_birth
policy_holder_sex

policy_holder_address_1
policy_holder_address_2
policy_holder_city
policy_holder_state
policy_holder_zip

msp_qualification

is_active

created_at
updated_at
```

Priorities intended:

```text
primary
secondary
tertiary
quaternary
```

IMPORTANT:

Do NOT make:

```text
(patient_id, priority)
```

unique.

A patient may have historical or future policies with the same priority.

Policies should be disabled rather than destroyed when appropriate.

---

# 17. Exact Next Development Task

After the environment works and existing modules pass verification, continue with:

# Module 1B — Billing Backend API

Expected endpoints:

```http
GET /api/patients/{id}/billing-settings

PUT /api/patients/{id}/billing-settings

GET /api/patients/{id}/insurance-policies

POST /api/patients/{id}/insurance-policies

GET /api/insurance-policies/{id}

PUT /api/insurance-policies/{id}

PATCH /api/insurance-policies/{id}/status
```

Billing modification endpoints should require:

```text
practice_biller
```

Do not begin Prior Authorizations until Module 1 works end-to-end.

---

# 18. Important Billing Architecture Principle

Do not require every insurance field during initial policy creation.

The application should allow a policy to be saved while incomplete.

Later, when submitting a claim, implement claim-level validation for required information.

For example, claim submission validation can eventually check:

```text
payer
member ID
subscriber / insured information
relationship to insured
insurance type
signature information
service information
diagnosis codes
provider information
etc.
```

Do not prematurely mix claim validation into the insurance-policy CRUD layer.

---

# 19. Future Billing Dependency Order

Follow this sequence:

```text
Patient
   ↓
Insurance Policy
   ↓
Payer
   ↓
Prior Authorization
   ↓
Service Code / Rate
   ↓
Billable Service / Charge
   ↓
Claim
   ↓
CMS-1500 / EDI
   ↓
Submission
   ↓
Insurance Payment
   ↓
Adjustments
   ↓
Patient Responsibility
   ↓
Patient Payment
   ↓
Balance
   ↓
Statement
```

Do not jump directly to Claims before the underlying insurance, service-code and charge models exist.

---

# 20. Items Intentionally Not Solved Yet

## Patient Contacts

The original patient workflow mentioned contacts, but the supplied requirements did not define the actual contact fields.

This was intentionally not implemented rather than inventing a schema.

If requirements are later supplied, implement it then.

---

## Clearinghouse

Do not fake real clearinghouse submission.

Internal claim lifecycle and CMS-1500 generation can be implemented first.

Actual EDI/clearinghouse submission should remain a separate integration layer.

---

# 21. New Machine Verification Checklist

Before writing new billing code, complete this.

## Git

```text
[ ] Repo cloned/opened correctly
[ ] Correct remote
[ ] Correct current branch
[ ] No accidental uncommitted files lost
```

## Database

```text
[ ] PostgreSQL running
[ ] ehr_db exists
[ ] backend/.env configured
[ ] initial schema applied
[ ] billing migration status checked
```

## Backend

Run:

```bash
cd backend

set -a
source .env
set +a

gofmt -w cmd internal

go test ./...
go vet ./...

go run ./cmd/api
```

Expected startup:

```text
Connected to PostgreSQL
EHR API running on http://localhost:8080
```

Test:

```text
GET /health
```

Expected:

```json
{
  "status": "ok",
  "database": "connected"
}
```

## Frontend

From another terminal:

```bash
cd frontend

npm install
npm run lint
npm run build
npm run dev
```

Do not run `npm install` blindly if this repository uses another package manager. Inspect the lockfile first.

Use:

```text
package-lock.json → npm
pnpm-lock.yaml    → pnpm
yarn.lock         → yarn
```

---

# 22. Browser Smoke Test

Verify:

```text
AUTH
[ ] Signup
[ ] Login
[ ] Dashboard

PATIENTS
[ ] List
[ ] Add
[ ] View
[ ] Edit
[ ] Assigned clinician selector

USERS
[ ] List
[ ] Add
[ ] View
[ ] Edit

ROLES
[ ] Roles display
[ ] Roles save
[ ] Roles remain after refresh

PAYERS
[ ] List
[ ] Add
[ ] View
[ ] Edit
[ ] Disable
[ ] Enable
```

Then test authorization.

A user without:

```text
clinician
practice_scheduler
```

should receive:

```text
403
```

when creating a patient.

A user without:

```text
practice_biller
```

should receive:

```text
403
```

when:

```text
creating a payer
editing a payer
enabling/disabling a payer
```

After assigning the required role, the action should succeed.

---

# 23. Development Rules for the Agent

Follow these carefully.

### Inspect before modifying

Do not assume this document perfectly reflects the latest commit.

First inspect:

```text
git status
git log
backend/cmd/api/main.go
backend/internal/*
backend/migrations/*
frontend/src/app/*
frontend/src/components/*
frontend/src/types/*
```

Report meaningful discrepancies before changing architecture.

### Preserve existing patterns

If existing modules use a particular:

```text
handler style
JSON response format
API helper
form component pattern
error handling style
database convention
```

follow it.

### Build one billing module at a time

Do not create all future billing tables/API routes in advance.

### Keep schema normalized

Use relationships such as:

```text
patient_id
payer_id
insurance_policy_id
claim_id
service_code_id
```

rather than duplicating entire entities.

### No secrets in Git

Never commit:

```text
.env
JWT secrets
database passwords
API keys
```

### Avoid unnecessary infrastructure

Do not add:

```text
Redis
Kafka
AWS
microservices
Kubernetes
```

unless there is an actual requirement later.

This project should remain straightforward.

---

# 24. First Action for the Agent

Do NOT immediately write code.

First perform an exploratory audit and report:

```text
1. Current Git branch/status
2. Repository structure
3. Go version required by go.mod
4. Frontend package manager and Node requirements
5. Whether backend/.env exists
6. Whether PostgreSQL is reachable
7. Which migrations exist
8. Which migrations appear to be applied
9. Existing API routes in main.go
10. Whether Module 1 billing code exists beyond the migration
11. Any compile/lint/test failures
12. Exact recommended setup commands for THIS machine
```

Then set up and verify the existing project.

Only after the existing app is operational should development continue with:

```text
Billing Module 1B:
Patient Billing Settings + Insurance Policy Backend API
```

---

# 25. Current Overall Progress

```text
CORE EHR

Foundation                    DONE
Authentication                DONE
Patients                      DONE
Users                         DONE
Roles                         DONE
Payers                        DONE


BILLING

0. Payers / Practice Biller   DONE

1. Patient Billing
   Database                   STARTED / VERIFY REPO
   Backend API                NEXT
   UI                         NOT STARTED
   Policy status              NOT STARTED
   Testing                    NOT STARTED

2. Prior Authorizations       NOT STARTED
3. Service Codes / Rates      NOT STARTED
4. Charges                    NOT STARTED
5. Claims                     NOT STARTED
6. CMS-1500                   NOT STARTED
7. Submission                 NOT STARTED
8. Patient Payments           NOT STARTED
9. Insurance Payments         NOT STARTED
10. Balances                  NOT STARTED
11. Statements                NOT STARTED
12. Billing Tracking          NOT STARTED
13. Clearinghouse             LATER
```

The immediate goal on this new machine is:

```text
SET UP
→ VERIFY EXISTING APP
→ CONFIRM MIGRATION STATE
→ CONTINUE BILLING MODULE 1B
```

Do not skip the verification step.