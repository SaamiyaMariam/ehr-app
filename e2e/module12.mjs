// Module 12 browser check: billing dashboard, work queues, search + CSV, aging, collections.
import { readFileSync } from "node:fs";
import {
  BASE, api, assert, createCharge, createClinician, createDiagnosis, createPatient, createPayer, createPolicy,
  createServiceCode, expectText, launch, login, must, recorder, shot, submittedClaim, today, uid, userWithRoles,
} from "./lib.mjs";

const r = recorder("Module 12 — billing dashboard and reports");

const biller = await userWithRoles(null, "M12 Biller", ["practice_biller", "clinician"]);
const clinician = await createClinician(biller.token, "M12 Clinician");
const payerName = `E2E M12 Payer ${uid()}`;
const payer = await createPayer(biller, payerName);
const service = await createServiceCode(biller, "100.00");
const tag = uid("T");

const patient = await createPatient(biller, `M12 Patient ${tag}`, clinician.id);
const policy = await createPolicy(biller, patient.id, payer.id);
const diagnosis = await createDiagnosis(biller, patient.id);
const base = { clinicianId: clinician.id, serviceCodeId: service.id, policyId: policy.id, diagnosisId: diagnosis.id };

// One rejected claim, one ready (pending, external) claim, one unbilled service, aged patient charges.
const rejectedCharge = await createCharge(biller, patient.id, { ...base, patientShare: "10.00", dos: today(-50) });
const rejectedClaim = await submittedClaim(biller, [rejectedCharge.id]);
await must(api("POST", `/api/claims/${rejectedClaim.id}/record-rejection`, biller.token, { reason: "E2E rejection" }));

const pendingCharge = await createCharge(biller, patient.id, { ...base, patientShare: "0.00", dos: today(-8) });
const pending = await must(api("POST", "/api/claims", biller.token, { charge_ids: [pendingCharge.id] }));
assert(pending.status === "ready", `pending claim status ${pending.status}`);

await createCharge(biller, patient.id, { ...base, patientShare: "0.00", dos: today(-3) }); // unbilled

// Direct (self-pay) charges aged into different buckets for the patient aging report.
const aged = await createPatient(biller, `M12 Aged ${tag}`, clinician.id);
for (const d of [-10, -45, -100]) {
  await must(api("POST", `/api/patients/${aged.id}/charges`, biller.token, {
    clinician_id: clinician.id, service_code_id: service.id, date_of_service: today(d), units: 1, place_of_service: "11", billing_method: "direct",
  }));
}

const { browser, context, page, problems } = await launch();

async function dashboardValue(id) {
  const text = await page.getByTestId(`dash-${id}-value`).innerText();
  return text.trim();
}

try {
  await r.step("login and open the Billing dashboard", async () => {
    await login(page, biller);
    await page.goto(`${BASE}/billing`);
    await page.getByTestId("dash-rejected").waitFor();
    await page.waitForFunction(() => !document.querySelector('[data-testid="dash-rejected-value"]')?.textContent?.includes("…"));
    await shot(page, "m12-dashboard");
  });

  await r.step("dashboard cards equal the API counts and are actionable", async () => {
    const api12 = await must(api("GET", "/api/billing/dashboard", biller.token));
    assert((await dashboardValue("rejected")) === String(api12.claims.rejected), "rejected card");
    assert((await dashboardValue("pending")) === String(api12.claims.pending), "pending card");
    assert((await dashboardValue("external_pending")) === String(api12.claims.external_pending), "external card");
    assert(api12.claims.rejected >= 1 && api12.claims.pending >= 1 && api12.unbilled_services.count >= 1, "seeded data should be counted");
    // No clearinghouse is configured: the electronic card says so and nothing is pretended.
    await expectText(page, "Electronic Claims, No Clearinghouse");
  });

  await r.step("Rejected Claims card opens the filtered queue", async () => {
    const expected = await dashboardValue("rejected");
    await page.getByTestId("dash-rejected").click();
    await page.waitForURL(/\/billing\/claims\?queue=rejected/);
    await expectText(page, "Rejected claims");
    await expectText(page, `${expected} claim`);
    await expectText(page, rejectedClaim.claim_number);
    const statuses = await page.locator("table tbody tr").allInnerTexts();
    assert(statuses.every((s) => /Rejected/.test(s)), "every row in the queue is rejected");
    await shot(page, "m12-rejected-queue");
  });

  await r.step("Pending External Claims card opens its queue", async () => {
    await page.goto(`${BASE}/billing`);
    await page.getByTestId("dash-external_pending").click();
    await page.waitForURL(/queue=external_pending/);
    await expectText(page, pending.claim_number);
  });

  await r.step("transaction search: several filters at once", async () => {
    await page.goto(`${BASE}/billing`);
    await page.fill("#f-patient", `M12 Patient ${tag}`);
    await page.selectOption("#f-claim_status", "none");
    await page.getByLabel("Insurance balance > 0").check();
    await page.getByRole("button", { name: "Search" }).click();
    await page.waitForFunction(() => /1 result/.test(document.body.innerText));
    const text = await page.locator("table tbody").innerText();
    assert(/\$0\.00/.test(text), "unbilled service row is present");
    assert((await page.locator("table tbody tr").count()) === 1, "exactly the one unbilled service matches");
    await shot(page, "m12-search");
  });

  await r.step("export CSV honours the active filters", async () => {
    await page.fill("#f-patient", `M12 Patient ${tag}`);
    await page.selectOption("#f-claim_status", "");
    await page.getByLabel("Insurance balance > 0").uncheck();
    await page.getByRole("button", { name: "Search" }).click();
    await page.waitForFunction(() => /3 results/.test(document.body.innerText));

    const [download] = await Promise.all([
      page.waitForEvent("download"),
      page.getByRole("button", { name: "Export CSV" }).click(),
    ]);
    const path = await download.path();
    const csv = readFileSync(path, "utf8").trim().split(/\r?\n/);
    assert(csv[0].startsWith("Patient,Date of Service,Clinician,Service Code,Payer,Charge"), `header ${csv[0]}`);
    assert(csv.length === 4, `expected header + 3 rows, got ${csv.length}`);
    assert(csv.slice(1).every((line) => line.includes(`M12 Patient ${tag}`)), "only the filtered patient is exported");
  });

  await r.step("Insurance Aging: totals, bucket filter and drill-down", async () => {
    await page.goto(`${BASE}/billing`);
    await page.getByRole("link", { name: "Insurance Aging" }).click();
    await page.waitForURL(/insurance-aging/);
    await page.fill("#ia-patient", `M12 Patient ${tag}`);
    await page.getByRole("button", { name: "Run" }).click();
    await page.waitForFunction(() => document.querySelectorAll('[data-testid="aging-row"]').length === 3);

    const report = await must(api("GET", `/api/billing/reports/insurance-aging?patient=${encodeURIComponent(`M12 Patient ${tag}`)}`, biller.token));
    assert(report.items.length === 3, "three insurance services");
    // $10 patient share on the old charge leaves $90; the other two are fully insurance ($100 each).
    assert(report.totals.total === "290.00", `insurance total ${report.totals.total}`);
    assert((await page.getByTestId("bucket-total").innerText()).includes("$290.00"), "total card");

    await page.getByTestId("bucket-31-60").click();
    await page.waitForFunction(() => document.querySelectorAll('[data-testid="aging-row"]').length === 1);
    await expectText(page, "$90.00");
    await shot(page, "m12-insurance-aging");

    await page.getByRole("link", { name: rejectedClaim.claim_number }).click();
    await page.waitForURL(/billing\/claims\//);
    await expectText(page, "Claim " + rejectedClaim.claim_number);
  });

  await r.step("Patient Aging: buckets, filter, select for statements", async () => {
    await page.goto(`${BASE}/billing/reports/patient-aging`);
    await page.fill("#pa-patient", `M12 Aged ${tag}`);
    await page.getByRole("button", { name: "Run" }).click();
    await page.waitForFunction(() => document.querySelectorAll('[data-testid="aging-row"]').length === 1);
    const row = await page.getByTestId("aging-row").innerText();
    assert(row.includes("$100.00") && row.includes("$300.00"), `aged patient row: ${row}`);
    assert((await page.getByTestId("bucket-91+").innerText()).includes("$100.00"), "91+ bucket card");
    await shot(page, "m12-patient-aging");

    await page.getByLabel(new RegExp(`Select E2E M12 Aged ${tag}`)).check();
    await page.getByRole("button", { name: "Generate Statements for Selected" }).click();
    await expectText(page, "1 statement generated");
  });

  await r.step("Collections summary and date range change", async () => {
    // Money to count: a patient payment today.
    await must(api("POST", `/api/patients/${aged.id}/payments`, biller.token, {
      payment_date: today(), amount: "25.00", method: "cash", idempotency_key: crypto.randomUUID(), auto_allocate: true,
    }));

    await page.goto(`${BASE}/billing/reports/collections`);
    await page.getByTestId("col-patient-amount").waitFor();
    const todayReport = await must(api("GET", `/api/billing/reports/collections?from=${today()}&to=${today()}`, biller.token));
    await page.fill("#col-from", today());
    await page.fill("#col-to", today());
    await page.getByRole("button", { name: "Run" }).click();
    await page.waitForFunction((v) => document.querySelector('[data-testid="col-patient-amount"]')?.textContent?.includes(v), `$${Number(todayReport.patient_payments.amount).toLocaleString("en-US", { minimumFractionDigits: 2 })}`);
    assert((await page.getByTestId("col-total-amount").innerText()).includes("$"), "total shown");
    await expectText(page, "Charges and adjustments (not collections)");
    await shot(page, "m12-collections");

    // A range with no activity shows zeros.
    await page.fill("#col-from", "2015-01-01");
    await page.fill("#col-to", "2015-01-31");
    await page.getByRole("button", { name: "Run" }).click();
    await page.waitForFunction(() => document.querySelector('[data-testid="col-total-amount"]')?.textContent?.trim() === "$0.00");
  });
} finally {
  await context.close();
  await browser.close();
}

r.finish(problems);
