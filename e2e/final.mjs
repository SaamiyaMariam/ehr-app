// FINAL end-to-end workflow in a real browser (Module 14).
//
// Every record is named "E2E FINAL ..." so it is easy to recognise and is
// removed by scripts/cleanup_e2e_data.sql. Setup that an operator would do
// outside the application (first administrator, the practice's NPI) goes
// through the bootstrap command / API; everything else is driven through the UI.
import { readFileSync } from "node:fs";
import {
  API, BASE, PASSWORD, api, assert, assertCards, eventually, expectText, getAdmin, launch, logout, must, recorder, shot, signup, today, uid,
} from "./lib.mjs";

const r = recorder("FINAL end-to-end");
const tag = uid("F");

// ---------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------
const { browser, context, page, problems } = await launch();

// Forms in this app put a <label> directly before its control.
const F = (label) =>
  page
    .locator(`xpath=//label[starts-with(normalize-space(.), "${label}")]/following-sibling::*[self::input or self::select or self::textarea][1]`)
    .first();

async function pick(select, text) {
  const value = await select.locator("option", { hasText: text }).first().getAttribute("value");
  assert(value !== null, `no option containing "${text}"`);
  await select.selectOption(value);
}

async function waitForOk(promise) {
  const response = await promise;
  assert(response.ok(), `request failed: ${response.status()} ${response.url()}`);
  return response;
}

const names = {
  admin: null,
  biller: { first: "E2E", last: `FINAL Biller ${tag}`, username: `finalbiller${tag}`.toLowerCase(), email: `finalbiller${tag}@example.test`.toLowerCase() },
  ordinary: { first: "E2E", last: `FINAL Ordinary ${tag}`, username: `finalordinary${tag}`.toLowerCase(), email: `finalordinary${tag}@example.test`.toLowerCase() },
  primaryPayer: `E2E FINAL Primary Payer ${tag}`,
  secondaryPayer: `E2E FINAL Secondary Payer ${tag}`,
  code: `E2E-FINAL-${tag}`.toUpperCase(),
  codeDescription: "E2E FINAL Psychotherapy",
  schedule: `E2E FINAL Contract ${tag}`,
  patientLast: `FINAL Patient ${tag}`,
  authorization: `E2E-FINAL-AUTH-${tag}`.toUpperCase(),
};

const state = {};
const tokenFor = async (email) => (await must(api("POST", "/api/auth/login", null, { email, password: PASSWORD }))).token;

async function loginAs(user) {
  await logout(page);
  await page.goto(`${BASE}/login`);
  await page.fill('input[name="email"]', user.email);
  await page.fill('input[name="password"]', PASSWORD);
  await page.click('button[type="submit"]');
  await page.waitForURL(/\/dashboard/, { timeout: 15000 });
}

async function adminApi(method, route, body) {
  return api(method, route, state.adminToken, body);
}

try {
  // ===================================================================
  // 1-3 Administration and role safety
  // ===================================================================
  await r.step("1. Practice Administrator logs in (first admin bootstrapped by the operator command)", async () => {
    const admin = await getAdmin();
    state.admin = admin;
    state.adminToken = admin.token;
    await loginAs(admin);
    await expectText(page, "Dashboard");
    await shot(page, "final-01-admin-dashboard");
  });

  await r.step("2. Administrator creates a Practice Biller (and an ordinary user) through the Users screen", async () => {
    for (const [who, roles] of [[names.biller, ["Practice Biller", "Clinician"]], [names.ordinary, ["Practice Scheduler"]]]) {
      await page.goto(`${BASE}/users/new`);
      await F("First name").fill(who.first);
      await F("Last name").fill(who.last);
      await F("Username").fill(who.username);
      await F("Email").fill(who.email);
      await F("Password").fill(PASSWORD);
      for (const role of roles) await page.getByLabel(role, { exact: true }).check();
      await page.getByRole("button", { name: "Create User" }).click();
      await page.waitForURL(/\/users\/[0-9a-f-]{36}$/, { timeout: 15000 });
      who.id = page.url().split("/").pop();
    }

    const roles = await must(adminApi("GET", `/api/users/${names.biller.id}/roles`));
    assert(roles.map((x) => x.key).sort().join() === "clinician,practice_biller", "biller roles persisted");
  });

  await r.step("3. an ordinary user cannot assign privileged roles (UI and API)", async () => {
    await loginAs(names.ordinary);
    await page.goto(`${BASE}/users/${names.ordinary.id}`);
    await page.getByLabel("Practice Administrator", { exact: true }).check();
    await page.getByRole("button", { name: /Save|Update/ }).first().click();
    await expectText(page, "permission");

    const token = await tokenFor(names.ordinary.email);
    const escalate = await api("PUT", `/api/users/${names.ordinary.id}/roles`, token, { roles: ["practice_administrator"] });
    assert(escalate.status === 403, `API escalation returned ${escalate.status}`);
    const roles = await must(adminApi("GET", `/api/users/${names.ordinary.id}/roles`));
    assert(roles.every((x) => x.key !== "practice_administrator"), "the ordinary user was not promoted");
    state.ordinaryToken = token;
  });

  // ===================================================================
  // 4-9 Configuration as the biller
  // ===================================================================
  await r.step("4. Biller logs in", async () => {
    await loginAs(names.biller);
    state.billerToken = await tokenFor(names.biller.email);
    // The practice's billing identity (an operator / administrator task).
    await must(api("PUT", "/api/billing/practice-profile", state.billerToken, {
      practice_name: "E2E Test Practice", npi: "1234567893", tax_id: "123456789", tax_id_type: "ein",
      address_1: "100 Practice Way", city: "Springfield", state: "IL", zip: "62701", phone: "5550100",
    }));
    await must(api("PUT", `/api/users/${names.biller.id}/billing-profile`, state.billerToken, { npi: "1234567893", taxonomy_code: "103T00000X" }));
  });

  await r.step("5. Create the primary and secondary payers", async () => {
    for (const name of [names.primaryPayer, names.secondaryPayer]) {
      await page.goto(`${BASE}/payers/new`);
      await F("Payer name").fill(name);
      await F("Payer ID").fill(`P${uid().slice(-8)}`.toUpperCase());
      await page.getByLabel("In network").check();
      await page.selectOption("#payer-billing-method", "external");
      await page.selectOption("#payer-insurance-type", "group_health_plan");
      await F("Address line 1").fill("1 Payer Plaza");
      await F("City").fill("Springfield");
      await F("State").fill("IL");
      await F("ZIP").fill("62701");
      await page.getByRole("button", { name: "Create Payer" }).click();
      await page.waitForURL(/\/payers(\/[0-9a-f-]{36})?$/, { timeout: 15000 });
    }
    const payers = await must(api("GET", "/api/payers", state.billerToken));
    state.primaryPayerId = payers.find((p) => p.payer_name === names.primaryPayer)?.id;
    state.secondaryPayerId = payers.find((p) => p.payer_name === names.secondaryPayer)?.id;
    assert(state.primaryPayerId && state.secondaryPayerId, "both payers exist");
  });

  await r.step("6-7. Create a service code with a $150.00 standard rate", async () => {
    await page.goto(`${BASE}/settings/service-codes/new`);
    await F("Service code").fill(names.code);
    await F("Standard rate").fill("150.00");
    await F("Description").fill(names.codeDescription);
    await page.getByRole("button", { name: "Create Service Code" }).click();
    await page.waitForURL(/settings\/service-codes$/, { timeout: 15000 });
    const code = await eventually(async () => {
      const codes = await must(api("GET", "/api/service-codes", state.billerToken));
      return codes.find((c) => c.code === names.code);
    });
    assert(code.standard_rate === "150.00", `standard rate ${code.standard_rate}`);
    state.serviceId = code.id;
  });

  await r.step("8. Create a rate schedule ($120.00) for the primary payer", async () => {
    await page.goto(`${BASE}/payers/${state.primaryPayerId}/rate-schedules/new`);
    await page.locator("#schedule-name").fill(names.schedule);
    await page.getByLabel("Use standard practice rates for every service").uncheck();
    await page.getByLabel(`Custom rate for ${names.code}`).fill("120.00");
    await page.getByRole("button", { name: "Create Rate Schedule" }).click();
    await page.waitForURL(new RegExp(`payers/${state.primaryPayerId}`), { timeout: 15000 });
    const schedules = await must(api("GET", `/api/payers/${state.primaryPayerId}/rate-schedules`, state.billerToken));
    assert(schedules.some((s) => s.name === names.schedule), "rate schedule created");
    state.scheduleId = schedules.find((s) => s.name === names.schedule).id;
  });

  await r.step("9. Assign the clinician's rate schedule", async () => {
    await page.goto(`${BASE}/payers/${state.primaryPayerId}`);
    const select = page.getByLabel(new RegExp(`Rate schedule for E2E FINAL Biller ${tag}`, "i"));
    await select.waitFor();
    await pick(select, names.schedule);
    await page.getByRole("button", { name: "Save Clinician Assignments" }).click();
    await page.waitForTimeout(800);
    const assignments = await must(api("GET", `/api/payers/${state.primaryPayerId}/clinician-rate-schedules`, state.billerToken));
    assert(assignments.some((a) => a.clinician_id === names.biller.id && a.rate_schedule_id === state.scheduleId), "clinician assignment saved");
  });

  // ===================================================================
  // 10-14 Patient, policies, authorization, diagnosis, charge
  // ===================================================================
  await r.step("10. Create the patient", async () => {
    await page.goto(`${BASE}/patients/new`);
    await F("First name").fill("E2E");
    await F("Last name").fill(names.patientLast);
    await F("Date of birth").fill("1988-02-14");
    await F("Address line 1").fill("5 Patient Way");
    await F("City").fill("Springfield");
    await F("State").fill("IL");
    await F("ZIP").fill("62701");
    await pick(F("Administrative sex"), "Female");
    await pick(F("Clinician"), `E2E FINAL Biller ${tag}`);
    await page.getByRole("button", { name: "Create Patient" }).click();
    await page.waitForURL(/\/patients(\/[0-9a-f-]{36})?$/, { timeout: 15000 });
    const patients = await must(api("GET", "/api/patients", state.billerToken));
    state.patientId = patients.find((p) => p.last_name === names.patientLast)?.id;
    assert(state.patientId, "patient exists");
  });

  for (const [num, priority, payerKey, key] of [[11, "primary", "primaryPayer", "primaryPolicyId"], [12, "secondary", "secondaryPayer", "secondaryPolicyId"]]) {
    await r.step(`${num}. Add the ${priority} insurance policy`, async () => {
      await page.goto(`${BASE}/patients/${state.patientId}/billing/insurance/new`);
      await pick(F("Payer"), names[payerKey]);
      await pick(F("Priority"), priority[0].toUpperCase() + priority.slice(1));
      await F("Member ID").fill(`MEM${priority}${tag}`.toUpperCase());
      await F("Policy group").fill("GRP-FINAL");
      await F("Coverage start").fill("2020-01-01");
      await pick(F("Relationship to policy holder"), "Self");
      await page.getByLabel("Signature on file").check();
      await page.getByRole("button", { name: "Create Policy" }).click();
      await page.waitForURL(new RegExp(`patients/${state.patientId}/billing$`), { timeout: 15000 });
      const policies = await must(api("GET", `/api/patients/${state.patientId}/insurance-policies`, state.billerToken));
      state[key] = policies.find((p) => p.priority === priority)?.id;
      assert(state[key], `${priority} policy exists`);
    });
  }

  await r.step("13. Add a prior authorization (3 uses) to the primary policy", async () => {
    await page.goto(`${BASE}/patients/${state.patientId}/billing/insurance/${state.primaryPolicyId}/prior-authorizations/new`);
    await F("Authorization code").fill(names.authorization);
    await page.getByLabel("Any Service Code").check();
    await F("Start date").fill("2020-01-01");
    await F("Uses allowed").fill("3");
    await F("Uses remaining").fill("3");
    await page.getByRole("button", { name: "Create Prior Authorization" }).click();
    const pa = await eventually(async () => {
      const list = await must(api("GET", `/api/insurance-policies/${state.primaryPolicyId}/prior-authorizations`, state.billerToken));
      return list.find((a) => a.authorization_code === names.authorization);
    });
    assert(pa.uses_remaining === 3, "authorization with 3 uses");
    state.paId = pa.id;
  });

  await r.step("14. Add the diagnosis (F41.1)", async () => {
    await page.goto(`${BASE}/patients/${state.patientId}/billing`);
    await page.locator("#dx-code").fill("F41.1");
    await page.locator("#dx-description").fill("Generalized anxiety disorder");
    await page.getByLabel("Primary diagnosis").check();
    await page.getByRole("button", { name: "Add Diagnosis" }).click();
    await expectText(page, "Generalized anxiety disorder");
  });

  await r.step("15-16. Create the billing charge and verify the resolved rate ($120.00 from the clinician's schedule)", async () => {
    await page.goto(`${BASE}/patients/${state.patientId}/billing/charges/new`);
    await page.locator("#charge-dos").fill(today(-14));
    await pick(page.locator("#charge-clinician"), `E2E FINAL Biller ${tag}`);
    await pick(page.locator("#charge-service"), names.code);
    await pick(page.locator("#charge-bill-to"), names.primaryPayer);
    await pick(page.locator("#charge-pa"), names.authorization);
    await page.locator("#charge-patient-amount").fill("0.00");
    await page.getByLabel(/F41\.1/).check();

    // The preview shows where the rate came from.
    await expectText(page, "$120.00 / unit");
    await expectText(page, "Payer rate schedule");
    await shot(page, "final-16-charge-rate");

    await page.getByRole("button", { name: "Create Billable Service" }).click();
    await page.waitForURL(new RegExp(`patients/${state.patientId}/billing`), { timeout: 15000 });
    const charges = await must(api("GET", `/api/patients/${state.patientId}/billing-transactions`, state.billerToken));
    assert(charges.length === 1 && charges[0].rate_per_unit === "120.00" && charges[0].rate_source === "payer_rate_schedule", `charge rate ${charges[0]?.rate_per_unit}`);
    assert(charges[0].total_charge === "120.00" && charges[0].insurance_responsibility === "120.00", "charge split");
    state.chargeId = charges[0].id;
  });

  // ===================================================================
  // 17-20 Primary claim, validation, submission, authorization usage
  // ===================================================================
  await r.step("17-18. Create the primary claim and validate it", async () => {
    await page.goto(`${BASE}/patients/${state.patientId}/billing/claims/new`);
    await page.getByLabel(new RegExp(`Select ${names.code}`)).check();
    await page.getByRole("button", { name: "Create Claim" }).click();
    await page.waitForURL(/billing\/claims\/[0-9a-f-]{36}$/, { timeout: 15000 });
    state.primaryClaimId = page.url().split("/").pop();
    await page.getByRole("button", { name: "Validate" }).click();
    await expectText(page, "Claim validated");
    const claim = await must(api("GET", `/api/claims/${state.primaryClaimId}`, state.billerToken));
    assert(claim.status === "ready" && claim.validation.errors.length === 0, `claim ${claim.status} ${JSON.stringify(claim.validation?.errors)}`);
    state.primaryClaimNumber = claim.claim_number;
  });

  await r.step("19-20. Mark external submission; the authorization usage changes exactly once", async () => {
    const before = await must(api("GET", `/api/prior-authorizations/${state.paId}`, state.billerToken));
    assert(before.uses_remaining === 3, "no use consumed before submission");
    await page.getByRole("button", { name: "Mark Submitted Externally" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Mark Submitted" }).click();
    await expectText(page, "Submitted externally");
    const after = await must(api("GET", `/api/prior-authorizations/${state.paId}`, state.billerToken));
    assert(after.uses_remaining === 2, `uses remaining ${after.uses_remaining}`);
    // A repeated submission cannot consume it again.
    const again = await api("POST", `/api/claims/${state.primaryClaimId}/mark-external`, state.billerToken, {});
    assert(again.status === 409, `repeat returned ${again.status}`);
    const final = await must(api("GET", `/api/prior-authorizations/${state.paId}`, state.billerToken));
    assert(final.uses_remaining === 2, "still 2 after a repeated request");
  });

  // ===================================================================
  // 21-23 Primary remittance with deductible transfer; balances
  // ===================================================================
  async function postRemittance({ payerName, claimNumber, amount, paid, deductible, reference, allowed }) {
    await page.goto(`${BASE}/billing/insurance-payments/new`);
    await page.selectOption("#ip-payer", { label: payerName });
    await page.fill("#out-claim", claimNumber);
    await page.getByRole("button", { name: "Find" }).click();
    await page.getByRole("button", { name: new RegExp(`Add ${claimNumber} line 1`) }).click();
    await page.fill("#ip-amount", amount);
    await page.fill("#ip-reference", reference);
    const line = page.getByTestId("remit-line-0");
    await line.locator('input[id^="paid-"]').fill(paid);
    if (allowed) await line.locator('input[id^="allowed-"]').fill(allowed);
    if (deductible) await line.locator('input[id^="deductible-"]').fill(deductible);
    await page.getByRole("button", { name: "Post Insurance Payment" }).click();
    await page.waitForURL(/insurance-payments\/[0-9a-f-]{36}/, { timeout: 15000 });
    await expectText(page, "Insurance payment posted");
  }

  await r.step("21-23. Primary pays $60.00, $20.00 deductible moves to the patient; verify balances", async () => {
    await postRemittance({ payerName: names.primaryPayer, claimNumber: state.primaryClaimNumber, amount: "60.00", paid: "60.00", allowed: "120.00", deductible: "20.00", reference: `CHK-FINAL-1-${tag}` });
    await assertCards(page, state.patientId, { patient: "$20.00", insurance: "$40.00", total: "$60.00", credit: "$0.00" }, "after the primary remittance");
    await shot(page, "final-23-balances");
  });

  await r.step("aging while the insurance balance is open (insurance and patient)", async () => {
    await page.goto(`${BASE}/billing/reports/insurance-aging`);
    await page.fill("#ia-patient", `${names.patientLast}`);
    await page.getByRole("button", { name: "Run" }).click();
    await page.waitForFunction(() => document.querySelectorAll('[data-testid="aging-row"]').length === 1);
    assert((await page.getByTestId("aging-row").innerText()).includes("$40.00"), "insurance aging shows the $40.00 left");
    assert((await page.getByTestId("bucket-total").innerText()).includes("$40.00"), "insurance aging total");
    await shot(page, "final-insurance-aging");

    await page.goto(`${BASE}/billing/reports/patient-aging`);
    await page.fill("#pa-patient", names.patientLast);
    await page.getByRole("button", { name: "Run" }).click();
    await page.waitForFunction(() => document.querySelectorAll('[data-testid="aging-row"]').length === 1);
    assert((await page.getByTestId("bucket-total").innerText()).includes("$20.00"), "patient aging shows the $20.00 deductible");
  });

  // ===================================================================
  // 24-26 Secondary claim
  // ===================================================================
  await r.step("24. Create the secondary claim for the $40.00 left", async () => {
    await page.goto(`${BASE}/billing/claims/${state.primaryClaimId}`);
    await page.getByTestId("next-insurance").waitFor();
    await expectText(page, "Eligible for Secondary Claim");
    assert((await page.getByTestId("next-eligible").innerText()).trim() === "$40.00", "eligible amount");
    await page.getByRole("button", { name: "Create Secondary Claim" }).click();
    await page.waitForURL((url) => /billing\/claims\/[0-9a-f-]{36}$/.test(url.pathname) && !url.pathname.endsWith(state.primaryClaimId), { timeout: 15000 });
    state.secondaryClaimId = page.url().split("/").pop();
    const secondary = await must(api("GET", `/api/claims/${state.secondaryClaimId}`, state.billerToken));
    state.secondaryClaimNumber = secondary.claim_number;
    assert(secondary.sequence === "secondary" && secondary.payer_id === state.secondaryPayerId, "secondary payer");
    assert(secondary.snapshot.adjudication.total_eligible === "40.00", "snapshot of the primary decision");
  });

  await r.step("25. Submit the secondary claim", async () => {
    await expectText(page, "Ready");
    await page.getByRole("button", { name: "Mark Submitted Externally" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Mark Submitted" }).click();
    await expectText(page, "Submitted externally");
    // Authorization usage did not change again (it belongs to the primary policy).
    const pa = await must(api("GET", `/api/prior-authorizations/${state.paId}`, state.billerToken));
    assert(pa.uses_remaining === 2, "the secondary claim did not touch the primary authorization");
  });

  await r.step("26. Post the secondary payment ($40.00)", async () => {
    await postRemittance({ payerName: names.secondaryPayer, claimNumber: state.secondaryClaimNumber, amount: "40.00", paid: "40.00", reference: `CHK-FINAL-2-${tag}` });
    const primary = await must(api("GET", `/api/claims/${state.primaryClaimId}`, state.billerToken));
    const secondary = await must(api("GET", `/api/claims/${state.secondaryClaimId}`, state.billerToken));
    assert(primary.status === "paid" && secondary.status === "paid", `claims ${primary.status} / ${secondary.status}`);
    await assertCards(page, state.patientId, { patient: "$20.00", insurance: "$0.00", total: "$20.00", credit: "$0.00" }, "after the secondary payment");
  });

  // ===================================================================
  // 27-30 Statement snapshot, patient payment
  // ===================================================================
  await r.step("29. Generate a patient statement ($20.00 due)", async () => {
    await page.goto(`${BASE}/patients/${state.patientId}/billing/statements`);
    await page.getByRole("button", { name: "Generate Open Balance Statement" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Generate", exact: true }).click();
    await page.getByTestId("statement-row").first().waitFor();
    assert((await page.getByTestId("statement-balance").first().innerText()).trim() === "$20.00", "statement balance");
    const list = await must(api("GET", `/api/patients/${state.patientId}/statements`, state.billerToken));
    state.statementId = list[0].id;
    state.statementBefore = Buffer.from(await (await fetch(`${API}/api/statements/${state.statementId}/pdf`, { headers: { Authorization: `Bearer ${state.billerToken}` } })).arrayBuffer());
    assert(state.statementBefore.subarray(0, 5).toString() === "%PDF-", "statement PDF");
  });

  await r.step("27-28. Enter the patient payment ($20.00); balances are zero", async () => {
    await page.goto(`${BASE}/patients/${state.patientId}/billing/payments/new`);
    await page.fill("#pay-amount", "20.00");
    await page.getByRole("button", { name: "Post Payment" }).click();
    await page.waitForURL(/billing\/payments\/[0-9a-f-]{36}/, { timeout: 15000 });
    await assertCards(page, state.patientId, { patient: "$0.00", insurance: "$0.00", total: "$0.00", credit: "$0.00" }, "after the patient payment");
  });

  await r.step("30. The old statement snapshot is unchanged after the payment", async () => {
    const after = Buffer.from(await (await fetch(`${API}/api/statements/${state.statementId}/pdf`, { headers: { Authorization: `Bearer ${state.billerToken}` } })).arrayBuffer());
    assert(Buffer.compare(state.statementBefore, after) === 0, "statement PDF changed");
    await page.goto(`${BASE}/patients/${state.patientId}/billing/statements`);
    await page.getByTestId("statement-row").first().waitFor();
    assert((await page.getByTestId("statement-balance").first().innerText()).trim() === "$20.00", "the statement still says $20.00");
    // Nothing is owed now, so a new open-balance statement is refused rather than created empty.
    const empty = await api("POST", `/api/patients/${state.patientId}/statements`, state.billerToken, { statement_type: "open_balance" });
    assert(empty.status === 409, `new empty statement returned ${empty.status}`);
    await shot(page, "final-30-statements");
  });

  // ===================================================================
  // 31-35 Dashboard, reports, CSV
  // ===================================================================
  await r.step("31. Billing dashboard reflects the work", async () => {
    await page.goto(`${BASE}/billing`);
    await page.getByTestId("dash-pending").waitFor();
    await page.waitForFunction(() => !document.querySelector('[data-testid="dash-pending-value"]')?.textContent?.includes("…"));
    const dash = await must(api("GET", "/api/billing/dashboard", state.billerToken));
    assert((await page.getByTestId("dash-pending-value").innerText()).trim() === String(dash.claims.pending), "pending card equals the API");
    await shot(page, "final-31-dashboard");
  });

  await r.step("32-33. Insurance aging and patient aging no longer list this patient (all paid)", async () => {
    const insurance = await must(api("GET", `/api/billing/reports/insurance-aging?patient_id=${state.patientId}`, state.billerToken));
    assert(insurance.items.length === 0 && insurance.totals.total === "0.00", "no insurance balance left");
    const patient = await must(api("GET", `/api/billing/reports/patient-aging?patient_id=${state.patientId}`, state.billerToken));
    assert(patient.items.length === 0, "no patient balance left");
  });

  await r.step("34. Collections summary counts this scenario's money", async () => {
    await page.goto(`${BASE}/billing/reports/collections`);
    await page.fill("#col-from", today());
    await page.fill("#col-to", today());
    await page.getByRole("button", { name: "Run" }).click();
    await page.getByTestId("col-total-amount").waitFor();
    const report = await must(api("GET", `/api/billing/reports/collections?from=${today()}&to=${today()}`, state.billerToken));
    // Insurance $60 + $40 and patient $20 were posted today (other scenarios may add more).
    assert(Number(report.insurance_payments.amount) >= 100 && Number(report.patient_payments.amount) >= 20, "collections include this run's payments");
    assert(report.charges_created.amount !== report.total_collections, "charges are not reported as collections");
    await shot(page, "final-34-collections");
  });

  await r.step("35. Export the transactions to CSV", async () => {
    await page.goto(`${BASE}/billing?patient_id=${state.patientId}`);
    await page.waitForFunction(() => document.querySelectorAll("table tbody tr").length === 1);
    const [download] = await Promise.all([page.waitForEvent("download"), page.getByRole("button", { name: "Export CSV" }).click()]);
    const csv = readFileSync(await download.path(), "utf8").trim().split(/\r?\n/);
    assert(csv.length === 2, `expected header + 1 row, got ${csv.length}`);
    assert(csv[1].includes(`FINAL Patient ${tag}`) && csv[1].includes("120.00") && csv[1].includes("paid"), `csv row: ${csv[1]}`);
  });

  // ===================================================================
  // 36-38 Wrong role, relogin, persistence
  // ===================================================================
  await r.step("36. Wrong-role access is refused (UI and API)", async () => {
    await loginAs(names.ordinary);
    await page.goto(`${BASE}/billing/insurance-payments/new`);
    await page.selectOption("#ip-payer", { label: names.primaryPayer });
    await page.fill("#ip-amount", "0.00");
    const direct = await api("POST", "/api/insurance-payments", state.ordinaryToken, {});
    assert(direct.status === 403, `API status ${direct.status}`);
    const adjust = await api("POST", `/api/charges/${state.chargeId}/adjustments`, state.ordinaryToken, {});
    assert(adjust.status === 403, `adjustment status ${adjust.status}`);
    const statement = await api("POST", `/api/patients/${state.patientId}/statements`, state.ordinaryToken, { statement_type: "open_balance" });
    assert(statement.status === 403, `statement status ${statement.status}`);
    const secondary = await api("POST", `/api/claims/${state.primaryClaimId}/next-sequence`, state.ordinaryToken, {});
    assert(secondary.status === 403, `secondary status ${secondary.status}`);

    // A brand-new sign-up has no role and sees no practice data at all.
    const stranger = await signup("FINAL Stranger");
    for (const route of ["/api/patients", "/api/claims", "/api/billing/dashboard"]) {
      const res = await api("GET", route, stranger.token);
      assert(res.status === 403, `${route} for a role-less account: ${res.status}`);
    }
  });

  await r.step("37-38. Refresh and relogin: the persisted state is identical", async () => {
    await loginAs(names.biller);
    await assertCards(page, state.patientId, { patient: "$0.00", insurance: "$0.00", total: "$0.00", credit: "$0.00" }, "after relogin");

    for (const [id, expected] of [[state.primaryClaimId, "paid"], [state.secondaryClaimId, "paid"]]) {
      await page.goto(`${BASE}/billing/claims/${id}`);
      await expectText(page, "Paid / adjudicated");
      const claim = await must(api("GET", `/api/claims/${id}`, state.billerToken));
      assert(claim.status === expected, `claim ${claim.status}`);
    }

    const ledger = await must(api("GET", `/api/charges/${state.chargeId}/ledger`, state.billerToken));
    assert(ledger.breakdown.total_balance === "0.00" && ledger.breakdown.insurance_paid === "100.00" && ledger.breakdown.patient_paid === "20.00" && ledger.breakdown.transfers_to_patient === "20.00", JSON.stringify(ledger.breakdown));

    const pa = await must(api("GET", `/api/prior-authorizations/${state.paId}`, state.billerToken));
    assert(pa.uses_remaining === 2, "authorization usage is persisted");

    const statements = await must(api("GET", `/api/patients/${state.patientId}/statements`, state.billerToken));
    assert(statements.length === 1 && statements[0].balance_due === "20.00", "statement persisted");
  });
} finally {
  await context.close();
  await browser.close();
}

r.finish(problems);
