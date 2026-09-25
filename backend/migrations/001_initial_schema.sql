CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- =========================================================
-- ROLES
-- =========================================================

CREATE TABLE roles (
    id SMALLSERIAL PRIMARY KEY,
    key VARCHAR(100) NOT NULL UNIQUE,
    name VARCHAR(150) NOT NULL UNIQUE
);

INSERT INTO roles (key, name) VALUES
    ('practice_administrator', 'Practice Administrator'),
    ('clinician', 'Clinician'),
    ('intern_assistant_associate', 'Intern / Assistant / Associate'),
    ('supervisor', 'Supervisor'),
    ('clinical_administrator', 'Clinical Administrator'),
    ('practice_scheduler', 'Practice Scheduler'),
    ('practice_biller', 'Practice Biller');


-- =========================================================
-- USERS / EMPLOYEES
-- =========================================================

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    user_comments TEXT,

    first_name VARCHAR(100) NOT NULL,
    middle_name VARCHAR(100),
    last_name VARCHAR(100) NOT NULL,
    suffix VARCHAR(50),

    preferred_name VARCHAR(100),
    pronouns VARCHAR(100),

    username VARCHAR(100) NOT NULL,
    date_of_birth DATE,
    languages TEXT[] NOT NULL DEFAULT '{}',

    email VARCHAR(255) NOT NULL,
    mobile_phone VARCHAR(50),
    can_receive_text_messages BOOLEAN NOT NULL DEFAULT FALSE,
    work_phone VARCHAR(50),
    home_phone VARCHAR(50),

    address_1 VARCHAR(255),
    address_2 VARCHAR(255),
    zip VARCHAR(30),
    city VARCHAR(100),
    state VARCHAR(100),

    password_hash TEXT NOT NULL,

    is_active BOOLEAN NOT NULL DEFAULT TRUE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX users_email_unique
    ON users (LOWER(email));

CREATE UNIQUE INDEX users_username_unique
    ON users (LOWER(username));


-- =========================================================
-- USER ROLES
-- =========================================================

CREATE TABLE user_roles (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id SMALLINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,

    PRIMARY KEY (user_id, role_id)
);


-- =========================================================
-- PATIENTS
-- =========================================================

CREATE TABLE patients (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    patient_comments TEXT,

    first_name VARCHAR(100),
    middle_name VARCHAR(100),
    last_name VARCHAR(100) NOT NULL,
    suffix VARCHAR(50),

    preferred_name VARCHAR(100),
    pronouns VARCHAR(100),

    date_of_birth DATE,
    account_number VARCHAR(100),

    address_1 VARCHAR(255),
    address_2 VARCHAR(255),
    zip VARCHAR(30),
    city VARCHAR(100),
    state VARCHAR(100),
    time_zone VARCHAR(100),

    mobile_phone VARCHAR(50),
    mobile_message_preference VARCHAR(100),

    home_phone VARCHAR(50),
    home_message_preference VARCHAR(100),

    work_phone VARCHAR(50),
    work_message_preference VARCHAR(100),

    other_phone VARCHAR(50),
    other_message_preference VARCHAR(100),

    email VARCHAR(255),

    appointment_reminder_setting VARCHAR(150),

    administrative_sex VARCHAR(50),
    gender_identity VARCHAR(100),
    sexual_orientation VARCHAR(100),
    race VARCHAR(100),
    ethnicity VARCHAR(100),
    languages TEXT[] NOT NULL DEFAULT '{}',
    smoking_status VARCHAR(100),
    marital_status VARCHAR(100),
    employment VARCHAR(150),
    religious_affiliation VARCHAR(150),

    hipaa_npp_on_file BOOLEAN NOT NULL DEFAULT FALSE,
    pcp_release VARCHAR(100),

    pad_acknowledged BOOLEAN NOT NULL DEFAULT FALSE,
    pad_acknowledged_date DATE,

    assigned_clinician_id UUID REFERENCES users(id) ON DELETE SET NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX patients_account_number_unique
    ON patients (account_number)
    WHERE account_number IS NOT NULL;


-- =========================================================
-- PAYERS
-- =========================================================

CREATE TABLE payers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    payer_name VARCHAR(255) NOT NULL,
    payer_id VARCHAR(100),

    in_network BOOLEAN NOT NULL DEFAULT FALSE,

    billing_method VARCHAR(100),
    insurance_type VARCHAR(150),

    address_1 VARCHAR(255),
    address_2 VARCHAR(255),
    zip VARCHAR(30),
    city VARCHAR(100),
    state VARCHAR(100),

    phone VARCHAR(50),
    fax VARCHAR(50),

    telehealth_place_of_service_code VARCHAR(50),
    telehealth_modifier_code VARCHAR(50),

    is_active BOOLEAN NOT NULL DEFAULT TRUE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX payers_name_idx
    ON payers (payer_name);

CREATE INDEX payers_payer_id_idx
    ON payers (payer_id);