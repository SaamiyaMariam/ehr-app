-- Bootstrap an account that can create users, assign roles, and use every
-- practice feature. Initial credentials are documented in
-- docs/BILLING_ARCHITECTURE.md. The password is stored as a bcrypt hash.
-- Existing credentials and account status are preserved when rerun.

BEGIN;

INSERT INTO users (
    first_name,
    last_name,
    username,
    email,
    password_hash
)
VALUES (
    'Practice',
    'Administrator',
    'ehr_admin',
    'admin@ehr.local',
    '$2a$10$eMZmptM5dJkH2vcZjt8hkef8cvq0p4rjZKbMQRVBxnyCZFn4HLT6.'
)
ON CONFLICT (LOWER(email)) DO NOTHING;

-- Assign every available role: administrator alone cannot perform clinical,
-- scheduling, or billing operations protected by their respective roles.
INSERT INTO user_roles (user_id, role_id)
SELECT u.id, r.id
FROM users u
CROSS JOIN roles r
WHERE LOWER(u.email) = 'admin@ehr.local'
ON CONFLICT (user_id, role_id) DO NOTHING;

COMMIT;
