// Shared helpers for the real-browser checks. Data is seeded through the HTTP
// API (clearly named "E2E ..." records); money screens are then driven
// through the UI in an installed Chrome (no browser download needed).

import { chromium } from "playwright-core";
import { mkdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

export const BASE = process.env.E2E_BASE ?? "http://localhost:3000";
export const API = process.env.E2E_API ?? "http://localhost:8080";

const here = path.dirname(fileURLToPath(import.meta.url));
export const ARTIFACTS = path.join(here, "artifacts");
mkdirSync(ARTIFACTS, { recursive: true });

export const PASSWORD = "E2e-Password-123!";

let counter = 0;
export function uid(prefix = "") {
  counter += 1;
  return `${prefix}${Date.now().toString(36)}${counter}`;
}

export async function api(method, route, token, body) {
  const response = await fetch(API + route, {
    method,
    headers: {
      ...(body ? { "Content-Type": "application/json" } : {}),
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });

  const text = await response.text();
  let json = {};
  try {
    json = text ? JSON.parse(text) : {};
  } catch {
    json = { raw: text };
  }

  return { status: response.status, json };
}

export async function must(promise, ...okStatuses) {
  const result = await promise;
  const ok = okStatuses.length ? okStatuses : [200, 201];
  if (!ok.includes(result.status)) {
    throw new Error(`API call failed (${result.status}): ${JSON.stringify(result.json)}`);
  }
  return result.json;
}

// ---------------------------------------------------------------------
// Users
// ---------------------------------------------------------------------

export async function signup(label) {
  const name = uid(label.toLowerCase().replace(/[^a-z0-9]/g, ""));
  const email = `${name}@example.test`;

  const json = await must(
    api("POST", "/api/auth/signup", null, {
      first_name: "E2E",
      last_name: label,
      username: name,
      email,
      password: PASSWORD,
    }),
  );

  return { token: json.token, id: json.user.id, email, name: `E2E ${label}` };
}

// `adminToken` must belong to a user allowed to assign roles.
export async function grantRoles(adminToken, userId, roles) {
  await must(api("PUT", `/api/users/${userId}/roles`, adminToken, { roles }));
}

export async function userWithRoles(adminToken, label, roles) {
  const user = await signup(label);
  await grantRoles(adminToken ?? user.token, user.id, roles);
  return user;
}

// ---------------------------------------------------------------------
// Billing fixture (claim-valid)
// ---------------------------------------------------------------------

export function today(offsetDays = 0) {
  const d = new Date();
  d.setDate(d.getDate() + offsetDays);
  return d.toLocaleDateString("en-CA");
}

export async function createPayer(biller, label, extra = {}) {
  return must(
    api("POST", "/api/payers", biller.token, {
      payer_name: label,
      payer_id: uid("P").slice(0, 12).toUpperCase(),
      in_network: true,
      billing_method: "external",
      insurance_type: "group_health_plan",
      address_1: "1 Payer Plaza",
      city: "Springfield",
      state: "IL",
      zip: "62701",
      ...extra,
    }),
  );
}

export async function createServiceCode(biller, rate = "100.00") {
  return must(
    api("POST", "/api/service-codes", biller.token, {
      code: uid("E2E-").toUpperCase(),
      description: "E2E service",
      standard_rate: rate,
    }),
  );
}

export async function createClinician(adminToken, label = "Clinician") {
  const user = await userWithRoles(adminToken, label, ["clinician"]);
  // Claims need a valid rendering NPI on the clinician's billing profile.
  await must(
    api("PUT", `/api/users/${user.id}/billing-profile`, adminToken ?? user.token, { npi: "1234567893", taxonomy_code: "103T00000X" }),
  );
  return user;
}

export async function createPatient(creator, label, clinicianId) {
  return must(
    api("POST", "/api/patients", creator.token, {
      first_name: "E2E",
      last_name: label,
      date_of_birth: "1985-04-12",
      address_1: "5 Patient Way",
      city: "Springfield",
      state: "IL",
      zip: "62701",
      administrative_sex: "female",
      assigned_clinician_id: clinicianId,
    }),
  );
}

export async function createPolicy(biller, patientId, payerId, priority = "primary", extra = {}) {
  return must(
    api("POST", `/api/patients/${patientId}/insurance-policies`, biller.token, {
      payer_id: payerId,
      priority,
      member_id: uid("MEM").toUpperCase(),
      policy_group: "GRP1",
      relationship_to_policy_holder: "self",
      signature_on_file: true,
      coverage_start: "2020-01-01",
      ...extra,
    }),
  );
}

export async function createDiagnosis(biller, patientId) {
  return must(
    api("POST", `/api/patients/${patientId}/diagnoses`, biller.token, {
      icd10_code: "F41.1",
      description: "Generalized anxiety disorder",
      is_primary: true,
    }),
  );
}

export async function createCharge(biller, patientId, { clinicianId, serviceCodeId, policyId, diagnosisId, units = 1, patientShare, dos = today(-10) }) {
  return must(
    api("POST", `/api/patients/${patientId}/charges`, biller.token, {
      clinician_id: clinicianId,
      service_code_id: serviceCodeId,
      date_of_service: dos,
      units,
      place_of_service: "11",
      insurance_policy_id: policyId,
      diagnosis_ids: [diagnosisId],
      ...(patientShare !== undefined ? { patient_responsibility: patientShare } : {}),
    }),
  );
}

// Creates a claim for the charges and marks it submitted outside the app.
export async function submittedClaim(biller, chargeIds) {
  const claim = await must(api("POST", "/api/claims", biller.token, { charge_ids: chargeIds }));

  if (claim.validation?.errors?.length) {
    throw new Error(`claim ${claim.claim_number} is not valid: ${claim.validation.errors.join("; ")}`);
  }

  const submitted = await must(
    api("POST", `/api/claims/${claim.id}/mark-external`, biller.token, { reference: uid("EXT") }),
  );

  return submitted;
}

// ---------------------------------------------------------------------
// Browser
// ---------------------------------------------------------------------

export async function launch() {
  const browser = await chromium.launch({ channel: process.env.E2E_BROWSER ?? "chrome", headless: process.env.E2E_HEADED ? false : true });
  const context = await browser.newContext({ viewport: { width: 1360, height: 900 } });
  const page = await context.newPage();

  const problems = [];
  page.on("pageerror", (error) => problems.push(`pageerror: ${error.message}`));
  page.on("console", (message) => {
    if (message.type() === "error" && !/favicon|Failed to load resource/.test(message.text())) {
      problems.push(`console: ${message.text()}`);
    }
  });

  return { browser, context, page, problems };
}

export async function login(page, user) {
  await page.goto(`${BASE}/login`);
  await page.fill('input[name="email"]', user.email);
  await page.fill('input[name="password"]', PASSWORD);
  await page.click('button[type="submit"]');
  await page.waitForURL(/\/dashboard/, { timeout: 15000 });
}

export async function logout(page) {
  await page.evaluate(() => localStorage.clear());
  await page.goto(`${BASE}/login`);
}

export async function shot(page, name) {
  await page.screenshot({ path: path.join(ARTIFACTS, `${name}.png`), fullPage: true });
}

// Tiny test recorder.
export function recorder(title) {
  const results = [];

  return {
    results,
    async step(name, fn) {
      try {
        await fn();
        results.push({ name, ok: true });
        console.log(`  ✓ ${name}`);
      } catch (error) {
        results.push({ name, ok: false, error: String(error?.message ?? error) });
        console.log(`  ✗ ${name}\n      ${String(error?.message ?? error).split("\n")[0]}`);
      }
    },
    finish(problems = []) {
      const failed = results.filter((r) => !r.ok);
      console.log(`\n${title}: ${results.length - failed.length}/${results.length} steps passed`);
      if (problems.length) {
        console.log(`browser problems:\n  ${problems.join("\n  ")}`);
      }
      process.exitCode = failed.length || problems.length ? 1 : 0;
    },
  };
}

export function assert(condition, message) {
  if (!condition) throw new Error(message);
}

export async function expectText(page, text, options = {}) {
  await page.getByText(text, { exact: false }).first().waitFor({ timeout: options.timeout ?? 10000 });
}

// ---------------------------------------------------------------------
// Patient balance cards (Module 10)
// ---------------------------------------------------------------------

async function cardsLoaded(page) {
  await page.waitForFunction(() => {
    const el = document.querySelector('[data-testid="card-total-outstanding"]');
    return el && !el.textContent.includes("…");
  });
}

export async function readCards(page) {
  const read = async (id) => (await page.getByTestId(id).locator("div").nth(1).innerText()).trim();

  return {
    patient: await read("card-patient-balance"),
    insurance: await read("card-insurance-balance"),
    total: await read("card-total-outstanding"),
    credit: await read("card-patient-credit"),
  };
}

// Opens the patient billing page, reads the cards, reloads and checks the
// values are identical, then compares with what was expected.
export async function assertCards(page, patientId, expected, label) {
  await page.goto(`${BASE}/patients/${patientId}/billing`);
  await cardsLoaded(page);
  const first = await readCards(page);

  await page.reload();
  await cardsLoaded(page);
  const second = await readCards(page);

  assert(JSON.stringify(first) === JSON.stringify(second), `${label}: cards changed after refresh ${JSON.stringify(first)} vs ${JSON.stringify(second)}`);

  for (const [key, value] of Object.entries(expected)) {
    assert(first[key] === value, `${label}: ${key} card is ${first[key]}, expected ${value}`);
  }

  return first;
}
